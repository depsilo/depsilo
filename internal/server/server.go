package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/apt"
	"depsilo/internal/adapter/cargo"
	"depsilo/internal/adapter/composer"
	"depsilo/internal/adapter/conda"
	"depsilo/internal/adapter/cran"
	dockeradapter "depsilo/internal/adapter/docker"
	"depsilo/internal/adapter/goproxy"
	"depsilo/internal/adapter/helm"
	"depsilo/internal/adapter/maven"
	"depsilo/internal/adapter/npm"
	"depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/adapter/rubygems"
	"depsilo/internal/api"
	"depsilo/internal/audit"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/rules"
	"depsilo/internal/security"
	"depsilo/internal/trial"
	"depsilo/internal/upstream"
	web "depsilo/web"
)

// StartServer initializes all components and starts the HTTP server.
// Returns the *http.Server for lifecycle control by the caller.
// The provided context controls background goroutine shutdown.
func StartServer(ctx context.Context) (*http.Server, error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	zap.L().Info("config loaded",
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("storage_type", cfg.Storage.Type),
	)

	// Initialize database
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	// Create default admin user if none exists
	api.EnsureDefaultAdmin(database)

	// Backfill PackageName for existing cache entries
	backfillPackageNames(database)

	// Initialize storage
	var storage cache.Storage
	switch cfg.Storage.Type {
	case "local":
		storage, err = cache.NewLocalStorage(cfg.Storage.Path)
		if err != nil {
			return nil, fmt.Errorf("init local storage: %w", err)
		}
	case "s3":
		storage, err = cache.NewS3Storage(
			cfg.Storage.Endpoint,
			cfg.Storage.Bucket,
			cfg.Storage.Region,
			cfg.Storage.AccessKey,
			cfg.Storage.SecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("init s3 storage: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}

	// Initialize event bus and cache manager
	eventBus := cache.NewEventBus()
	cacheMgr := cache.NewManager(storage, database, eventBus)

	// Ecosystem definitions: name, route path, and upstream config
	type ecosystemDef struct {
		name      string
		route     string
		upstreams []config.UpstreamConfig
	}
	ecosystems := []ecosystemDef{
		{"pypi", "/pypi", cfg.PyPI.Upstreams},
		{"apt", "/apt", cfg.APT.Upstreams},
		{"npm", "/npm", cfg.NPM.Upstreams},
		{"go", "/go", cfg.Go.Upstreams},
		{"cargo", "/crates", cfg.Cargo.Upstreams},
		{"maven", "/maven", cfg.Maven.Upstreams},
		{"rubygems", "/rubygems", cfg.RubyGems.Upstreams},
		{"composer", "/composer", cfg.Composer.Upstreams},
		{"nuget", "/nuget", cfg.NuGet.Upstreams},
		{"conda", "/conda", cfg.Conda.Upstreams},
		{"cran", "/cran", cfg.CRAN.Upstreams},
		{"helm", "/helm", cfg.Helm.Upstreams},
	}

	// Sync upstreams and create pools
	pools := make(map[string]*upstream.Pool, len(ecosystems))
	for _, eco := range ecosystems {
		syncUpstreams(database, eco.name, eco.upstreams)
		pool, err := upstream.NewPool(eco.upstreams)
		if err != nil {
			return nil, fmt.Errorf("create %s pool: %w", eco.name, err)
		}
		pools[eco.name] = pool
	}

	// Initialize license manager
	licenseManager := license.NewManager(cfg.License, database)
	go licenseManager.Start(ctx)

	// Phase 5: construct trial + checker BEFORE audit and rules
	// (these modules query IsPro to decide whether to record entries / enforce rules)
	trialManager, err := trial.NewManager(database)
	if err != nil {
		zap.L().Warn("trial.NewManager failed, continuing with nil trial", zap.Error(err))
		// trialManager is nil but NewChecker handles that
	}
	checker := entitlement.NewChecker(licenseManager, trialManager)

	rulesStore := rules.NewStore(database)
	rulesEngine := rules.NewEngine(rulesStore, checker)

	auditLogger := audit.NewLogger(database, checker)
	go auditLogger.Start(ctx)
	adapter.SetAuditLogger(auditLogger)

	// Security scanner
	secCfg := cfg.Security
	if secCfg.OSVURL == "" {
		secCfg.OSVURL = "https://api.osv.dev"
	}
	if secCfg.ScanInterval == 0 {
		secCfg.ScanInterval = 24 * time.Hour
	}
	if secCfg.CheckTTL == 0 {
		secCfg.CheckTTL = 24 * time.Hour
	}

	osvFetcher := security.NewFetcher(secCfg.OSVURL, secCfg.Proxy)
	securityScanner := security.NewScanner(database, osvFetcher, secCfg.CheckTTL)
	securityImporter := security.NewImporter(database)

	if secCfg.Enabled {
		go security.StartBackgroundScan(ctx, securityScanner, secCfg.ScanInterval)
		zap.L().Info("security vulnerability scanner enabled",
			zap.Duration("scan_interval", secCfg.ScanInterval),
			zap.Duration("check_ttl", secCfg.CheckTTL),
		)
	}

	cacheMgr.SetSecurityScanner(securityScanner)

	if cfg.License.Key == "" {
		zap.L().Info("running as Community edition")
	} else {
		zap.L().Info("license key configured, validation in progress")
	}

	// Restore latency metrics from DB before starting health checks
	allPools := make([]*upstream.Pool, 0, len(pools))
	for _, eco := range ecosystems {
		allPools = append(allPools, pools[eco.name])
	}
	for _, pool := range allPools {
		upstream.RestoreFromDB(pool, database)
	}
	for _, pool := range allPools {
		go upstream.StartHealthCheck(ctx, pool, database, 30*time.Second)
	}
	go upstream.StartLatencyLogCleanup(ctx, database)
	go cache.StartLRUCleanup(ctx, storage, database, cfg.Cache.MaxSizeGB, cfg.Cache.LRUThreshold, 5*time.Minute)

	// Setup Gin
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(rules.Middleware(rulesEngine))
	r.Use(middleware.ProjectTokenMiddleware(database))

	// Build ordered ecosystem name list (defines UI iteration order)
	ecosystemNames := make([]string, 0, len(ecosystems))
	for _, eco := range ecosystems {
		ecosystemNames = append(ecosystemNames, eco.name)
	}

	// Register all API routes
	api.RegisterRoutes(r, api.Deps{
		DB:               database,
		Storage:          storage,
		Config:           cfg,
		Pools:            pools,
		Ecosystems:       ecosystemNames,
		CacheMgr:         cacheMgr,
		EventBus:         eventBus,
		LicenseManager:   licenseManager,
		TrialManager:     trialManager,
		Entitlement:      checker,
		AuditLogger:      auditLogger,
		RulesStore:       rulesStore,
		RulesEngine:      rulesEngine,
		SecurityScanner:  securityScanner,
		SecurityImporter: securityImporter,
	})

	// Register adapter handlers
	type adapterFactory func(*cache.Manager, upstream.Selector, config.CacheConfig, *gorm.DB) adapter.Adapter

	adapterFactories := map[string]adapterFactory{
		"pypi":     func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return pypi.New(cm, s, cc, db) },
		"apt":      func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return apt.New(cm, s, cc, db) },
		"npm":      func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return npm.New(cm, s, cc, db) },
		"go":       func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return goproxy.New(cm, s, cc, db) },
		"cargo":    func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return cargo.New(cm, s, cc, db) },
		"maven":    func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return maven.New(cm, s, cc, db) },
		"rubygems": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return rubygems.New(cm, s, cc, db) },
		"composer": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return composer.New(cm, s, cc, db) },
		"nuget":    func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return nuget.New(cm, s, cc, db) },
		"conda":    func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return conda.New(cm, s, cc, db) },
		"cran":     func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return cran.New(cm, s, cc, db) },
		"helm":     func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter { return helm.New(cm, s, cc, db) },
	}

	handlers := make(map[string]adapter.Adapter, len(ecosystems))
	for _, eco := range ecosystems {
		factory := adapterFactories[eco.name]
		h := factory(cacheMgr, upstream.NewPrioritySelector(pools[eco.name]), cfg.Cache, database)
		h.Register(r.Group(eco.route))
		handlers[eco.name] = h
	}

	if len(cfg.Docker.Registries) > 0 {
		dockerHandler := dockeradapter.New(cacheMgr, cfg.Cache, database, cfg.Docker)
		dockerGroup := r.Group("/v2")
		dockerHandler.Register(dockerGroup)
		zap.L().Info("docker registry proxy enabled",
			zap.Int("registries", len(cfg.Docker.Registries)),
			zap.String("default", cfg.Docker.DefaultRegistry),
		)
	}

	// Project-scoped proxy routes (/p/:slug/...)
	projectGroup := r.Group("/p/:slug")
	projectGroup.Use(middleware.ProjectSlugMiddleware(database))
	// Re-register all adapter handlers under the project group
	for _, eco := range ecosystems {
		handlers[eco.name].Register(projectGroup.Group(eco.route))
	}

	// Register extra PyPI-compatible indexes
	for _, idx := range cfg.ExtraIndexes {
		idxPool, err := upstream.NewPool(idx.Upstreams)
		if err != nil {
			return nil, fmt.Errorf("create extra index %s pool: %w", idx.Name, err)
		}
		syncUpstreams(database, "extra:"+idx.Name, idx.Upstreams)
		upstream.RestoreFromDB(idxPool, database)
		go upstream.StartHealthCheck(ctx, idxPool, database, 30*time.Second)

		idxHandler := pypi.NewWithPrefix(cacheMgr, upstream.NewPrioritySelector(idxPool), cfg.Cache, database, "/"+idx.Path, "extra:"+idx.Name)
		idxHandler.Register(r.Group("/" + idx.Path))
		idxHandler.Register(projectGroup.Group("/" + idx.Path))

		zap.L().Info("extra index registered",
			zap.String("name", idx.Name),
			zap.String("path", "/"+idx.Path),
			zap.Int("upstreams", len(idx.Upstreams)),
		)
	}

	// Serve embedded frontend (SPA fallback)
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}
	staticHandler := http.FileServer(http.FS(distFS))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/" || strings.HasPrefix(path, "/assets") {
			staticHandler.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Request.URL.Path = "/"
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	// Create and start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		zap.L().Info("starting server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("server failed", zap.Error(err))
		}
	}()

	return srv, nil
}

// backfillPackageNames updates existing cache entries that have an empty PackageName.
func backfillPackageNames(database *gorm.DB) {
	var entries []db.CacheEntry
	database.Where("package_name = '' OR package_name IS NULL").Find(&entries)
	if len(entries) == 0 {
		return
	}
	zap.L().Info("backfilling package names", zap.Int("count", len(entries)))
	for _, e := range entries {
		name := packagekey.ExtractName(e.AdapterType, e.Key)
		if name != "" {
			database.Model(&e).Update("package_name", name)
		}
	}
	zap.L().Info("package name backfill complete")
}

// syncUpstreams ensures configured upstreams exist in the database.
func syncUpstreams(database *gorm.DB, adapterType string, upstreams []config.UpstreamConfig) {
	for _, u := range upstreams {
		var record db.UpstreamRecord
		result := database.Where("name = ? AND adapter_type = ?", u.Name, adapterType).First(&record)
		if result.Error == gorm.ErrRecordNotFound {
			record = db.UpstreamRecord{
				AdapterType: adapterType,
				Name:        u.Name,
				URL:         u.URL,
				Proxy:       u.Proxy,
				Priority:    u.Priority,
				Healthy:     true,
				SuccessRate: 1.0,
			}
			if err := database.Create(&record).Error; err != nil {
				zap.L().Warn("failed to sync upstream to db", zap.String("name", u.Name), zap.Error(err))
			} else {
				zap.L().Info("synced upstream to db", zap.String("name", u.Name), zap.String("type", adapterType))
			}
		} else if result.Error == nil {
			database.Model(&record).Updates(map[string]interface{}{
				"url":      u.URL,
				"proxy":    u.Proxy,
				"priority": u.Priority,
			})
		}
	}
}
