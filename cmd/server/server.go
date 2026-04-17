package main

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
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/adapter/rubygems"
	"depsilo/internal/api"
	"depsilo/internal/audit"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/rules"
	"depsilo/internal/security"
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

	// Sync configured upstreams to database
	syncUpstreams(database, "pypi", cfg.PyPI.Upstreams)
	syncUpstreams(database, "apt", cfg.APT.Upstreams)
	syncUpstreams(database, "npm", cfg.NPM.Upstreams)
	syncUpstreams(database, "go", cfg.Go.Upstreams)
	syncUpstreams(database, "cargo", cfg.Cargo.Upstreams)
	syncUpstreams(database, "maven", cfg.Maven.Upstreams)
	syncUpstreams(database, "rubygems", cfg.RubyGems.Upstreams)
	syncUpstreams(database, "composer", cfg.Composer.Upstreams)
	syncUpstreams(database, "nuget", cfg.NuGet.Upstreams)
	syncUpstreams(database, "conda", cfg.Conda.Upstreams)
	syncUpstreams(database, "cran", cfg.CRAN.Upstreams)
	syncUpstreams(database, "helm", cfg.Helm.Upstreams)

	// Initialize upstream pools
	pypiPool, err := upstream.NewPool(cfg.PyPI.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create pypi pool: %w", err)
	}
	aptPool, err := upstream.NewPool(cfg.APT.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create apt pool: %w", err)
	}
	npmPool, err := upstream.NewPool(cfg.NPM.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create npm pool: %w", err)
	}
	goPool, err := upstream.NewPool(cfg.Go.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create go pool: %w", err)
	}
	cargoPool, err := upstream.NewPool(cfg.Cargo.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create cargo pool: %w", err)
	}
	mavenPool, err := upstream.NewPool(cfg.Maven.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create maven pool: %w", err)
	}
	rubygemsPool, err := upstream.NewPool(cfg.RubyGems.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create rubygems pool: %w", err)
	}
	composerPool, err := upstream.NewPool(cfg.Composer.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create composer pool: %w", err)
	}
	nugetPool, err := upstream.NewPool(cfg.NuGet.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create nuget pool: %w", err)
	}
	condaPool, err := upstream.NewPool(cfg.Conda.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create conda pool: %w", err)
	}
	cranPool, err := upstream.NewPool(cfg.CRAN.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create cran pool: %w", err)
	}
	helmPool, err := upstream.NewPool(cfg.Helm.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("create helm pool: %w", err)
	}

	// Initialize license manager
	licenseManager := license.NewManager(cfg.License)
	go licenseManager.Start(ctx)

	rulesStore := rules.NewStore(database)
	rulesEngine := rules.NewEngine(rulesStore, licenseManager)

	auditLogger := audit.NewLogger(database, licenseManager)
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

	cache.SetSecurityScanner(securityScanner)

	if cfg.License.Key == "" {
		zap.L().Info("running as Community edition")
	} else {
		zap.L().Info("license key configured, validation in progress")
	}

	// Restore latency metrics from DB before starting health checks
	allPools := []*upstream.Pool{
		pypiPool, aptPool, npmPool, goPool, cargoPool, mavenPool,
		rubygemsPool, composerPool, nugetPool, condaPool, cranPool, helmPool,
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

	// Register all API routes
	api.RegisterRoutes(r, api.Deps{
		DB:               database,
		Storage:          storage,
		Config:           cfg,
		PyPIPool:         pypiPool,
		APTPool:          aptPool,
		NPMPool:          npmPool,
		GoPool:           goPool,
		CargoPool:        cargoPool,
		MavenPool:        mavenPool,
		RubyGemsPool:     rubygemsPool,
		ComposerPool:     composerPool,
		NuGetPool:        nugetPool,
		CondaPool:        condaPool,
		CRANPool:         cranPool,
		HelmPool:         helmPool,
		CacheMgr:         cacheMgr,
		EventBus:         eventBus,
		LicenseManager:   licenseManager,
		AuditLogger:      auditLogger,
		RulesStore:       rulesStore,
		RulesEngine:      rulesEngine,
		SecurityScanner:  securityScanner,
		SecurityImporter: securityImporter,
	})

	// Register adapter handlers
	pypiHandler := pypi.New(cacheMgr, upstream.NewPrioritySelector(pypiPool), cfg.Cache, database)
	pypiGroup := r.Group("/pypi")
	pypiHandler.Register(pypiGroup)

	aptHandler := apt.New(cacheMgr, upstream.NewPrioritySelector(aptPool), cfg.Cache, database)
	aptGroup := r.Group("/apt")
	aptHandler.Register(aptGroup)

	npmHandler := npm.New(cacheMgr, upstream.NewPrioritySelector(npmPool), cfg.Cache, database)
	npmGroup := r.Group("/npm")
	npmHandler.Register(npmGroup)

	goHandler := goproxy.New(cacheMgr, upstream.NewPrioritySelector(goPool), cfg.Cache, database)
	goGroup := r.Group("/go")
	goHandler.Register(goGroup)

	cargoHandler := cargo.New(cacheMgr, upstream.NewPrioritySelector(cargoPool), cfg.Cache, database)
	cargoGroup := r.Group("/crates")
	cargoHandler.Register(cargoGroup)

	mavenHandler := maven.New(cacheMgr, upstream.NewPrioritySelector(mavenPool), cfg.Cache, database)
	mavenGroup := r.Group("/maven")
	mavenHandler.Register(mavenGroup)

	rubygemsHandler := rubygems.New(cacheMgr, upstream.NewPrioritySelector(rubygemsPool), cfg.Cache, database)
	rubygemsGroup := r.Group("/rubygems")
	rubygemsHandler.Register(rubygemsGroup)

	composerHandler := composer.New(cacheMgr, upstream.NewPrioritySelector(composerPool), cfg.Cache, database)
	composerGroup := r.Group("/composer")
	composerHandler.Register(composerGroup)

	nugetHandler := nuget.New(cacheMgr, upstream.NewPrioritySelector(nugetPool), cfg.Cache, database)
	nugetGroup := r.Group("/nuget")
	nugetHandler.Register(nugetGroup)

	condaHandler := conda.New(cacheMgr, upstream.NewPrioritySelector(condaPool), cfg.Cache, database)
	condaGroup := r.Group("/conda")
	condaHandler.Register(condaGroup)

	cranHandler := cran.New(cacheMgr, upstream.NewPrioritySelector(cranPool), cfg.Cache, database)
	cranGroup := r.Group("/cran")
	cranHandler.Register(cranGroup)

	helmHandler := helm.New(cacheMgr, upstream.NewPrioritySelector(helmPool), cfg.Cache, database)
	helmGroup := r.Group("/helm")
	helmHandler.Register(helmGroup)

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
	pypiHandler.Register(projectGroup.Group("/pypi"))
	aptHandler.Register(projectGroup.Group("/apt"))
	npmHandler.Register(projectGroup.Group("/npm"))
	goHandler.Register(projectGroup.Group("/go"))
	cargoHandler.Register(projectGroup.Group("/crates"))
	mavenHandler.Register(projectGroup.Group("/maven"))
	rubygemsHandler.Register(projectGroup.Group("/rubygems"))
	composerHandler.Register(projectGroup.Group("/composer"))
	nugetHandler.Register(projectGroup.Group("/nuget"))
	condaHandler.Register(projectGroup.Group("/conda"))
	cranHandler.Register(projectGroup.Group("/cran"))
	helmHandler.Register(projectGroup.Group("/helm"))

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
		name := cache.ExtractPackageName(e.AdapterType, e.Key)
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
