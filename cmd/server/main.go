package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter/apt"
	"depsilo/internal/adapter/cargo"
	"depsilo/internal/adapter/composer"
	"depsilo/internal/adapter/conda"
	"depsilo/internal/adapter/cran"
	"depsilo/internal/adapter/goproxy"
	dockeradapter "depsilo/internal/adapter/docker"
	"depsilo/internal/adapter/helm"
	"depsilo/internal/adapter/maven"
	"depsilo/internal/adapter/npm"
	"depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/adapter/rubygems"
	"depsilo/internal/adapter"
	"depsilo/internal/api"
	"depsilo/internal/audit"
	"depsilo/internal/rules"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/license"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/upstream"
	web "depsilo/web"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		zap.L().Fatal("failed to load config", zap.Error(err))
	}
	zap.L().Info("config loaded",
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("storage_type", cfg.Storage.Type),
	)

	// Initialize database
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		zap.L().Fatal("failed to open database", zap.Error(err))
	}
	if err := db.AutoMigrate(database); err != nil {
		zap.L().Fatal("failed to migrate database", zap.Error(err))
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
			zap.L().Fatal("failed to init local storage", zap.Error(err))
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
			zap.L().Fatal("failed to init s3 storage", zap.Error(err))
		}
	default:
		zap.L().Fatal("unsupported storage type", zap.String("type", cfg.Storage.Type))
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
		zap.L().Fatal("failed to create pypi upstream pool", zap.Error(err))
	}
	aptPool, err := upstream.NewPool(cfg.APT.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create apt upstream pool", zap.Error(err))
	}
	npmPool, err := upstream.NewPool(cfg.NPM.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create npm upstream pool", zap.Error(err))
	}
	goPool, err := upstream.NewPool(cfg.Go.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create go upstream pool", zap.Error(err))
	}
	cargoPool, err := upstream.NewPool(cfg.Cargo.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create cargo upstream pool", zap.Error(err))
	}
	mavenPool, err := upstream.NewPool(cfg.Maven.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create maven upstream pool", zap.Error(err))
	}
	rubygemsPool, err := upstream.NewPool(cfg.RubyGems.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create rubygems upstream pool", zap.Error(err))
	}
	composerPool, err := upstream.NewPool(cfg.Composer.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create composer upstream pool", zap.Error(err))
	}
	nugetPool, err := upstream.NewPool(cfg.NuGet.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create nuget upstream pool", zap.Error(err))
	}
	condaPool, err := upstream.NewPool(cfg.Conda.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create conda upstream pool", zap.Error(err))
	}
	cranPool, err := upstream.NewPool(cfg.CRAN.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create cran upstream pool", zap.Error(err))
	}
	helmPool, err := upstream.NewPool(cfg.Helm.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create helm upstream pool", zap.Error(err))
	}

	// Initialize license manager
	licenseManager := license.NewManager(cfg.License)

	// Start background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go licenseManager.Start(ctx)

	rulesStore := rules.NewStore(database)
	rulesEngine := rules.NewEngine(rulesStore, licenseManager)

	auditLogger := audit.NewLogger(database, licenseManager)
	go auditLogger.Start(ctx)
	adapter.SetAuditLogger(auditLogger)

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

	// Register all API routes
	api.RegisterRoutes(r, api.Deps{
		DB:       database,
		Storage:  storage,
		Config:   cfg,
		PyPIPool: pypiPool,
		APTPool:  aptPool,
		NPMPool:  npmPool,
		GoPool:    goPool,
		CargoPool:    cargoPool,
		MavenPool:    mavenPool,
		RubyGemsPool: rubygemsPool,
		ComposerPool: composerPool,
		NuGetPool:    nugetPool,
		CondaPool:    condaPool,
		CRANPool:     cranPool,
		HelmPool:     helmPool,
		CacheMgr:       cacheMgr,
		EventBus:       eventBus,
		LicenseManager: licenseManager,
		AuditLogger:    auditLogger,
		RulesStore:     rulesStore,
		RulesEngine:    rulesEngine,
	})

	// Register PyPI adapter
	pypiHandler := pypi.New(cacheMgr, upstream.NewPrioritySelector(pypiPool), cfg.Cache, database)
	pypiGroup := r.Group("/pypi")
	pypiHandler.Register(pypiGroup)

	// Register APT adapter
	aptHandler := apt.New(cacheMgr, upstream.NewPrioritySelector(aptPool), cfg.Cache, database)
	aptGroup := r.Group("/apt")
	aptHandler.Register(aptGroup)

	// Register npm adapter
	npmHandler := npm.New(cacheMgr, upstream.NewPrioritySelector(npmPool), cfg.Cache, database)
	npmGroup := r.Group("/npm")
	npmHandler.Register(npmGroup)

	// Register Go module proxy adapter
	goHandler := goproxy.New(cacheMgr, upstream.NewPrioritySelector(goPool), cfg.Cache, database)
	goGroup := r.Group("/go")
	goHandler.Register(goGroup)

	// Register Cargo registry proxy adapter
	cargoHandler := cargo.New(cacheMgr, upstream.NewPrioritySelector(cargoPool), cfg.Cache, database)
	cargoGroup := r.Group("/crates")
	cargoHandler.Register(cargoGroup)

	// Register Maven adapter
	mavenHandler := maven.New(cacheMgr, upstream.NewPrioritySelector(mavenPool), cfg.Cache, database)
	mavenGroup := r.Group("/maven")
	mavenHandler.Register(mavenGroup)

	// Register RubyGems adapter
	rubygemsHandler := rubygems.New(cacheMgr, upstream.NewPrioritySelector(rubygemsPool), cfg.Cache, database)
	rubygemsGroup := r.Group("/rubygems")
	rubygemsHandler.Register(rubygemsGroup)

	// Register Composer adapter
	composerHandler := composer.New(cacheMgr, upstream.NewPrioritySelector(composerPool), cfg.Cache, database)
	composerGroup := r.Group("/composer")
	composerHandler.Register(composerGroup)

	// Register NuGet adapter
	nugetHandler := nuget.New(cacheMgr, upstream.NewPrioritySelector(nugetPool), cfg.Cache, database)
	nugetGroup := r.Group("/nuget")
	nugetHandler.Register(nugetGroup)

	// Register Conda adapter
	condaHandler := conda.New(cacheMgr, upstream.NewPrioritySelector(condaPool), cfg.Cache, database)
	condaGroup := r.Group("/conda")
	condaHandler.Register(condaGroup)

	// Register CRAN adapter
	cranHandler := cran.New(cacheMgr, upstream.NewPrioritySelector(cranPool), cfg.Cache, database)
	cranGroup := r.Group("/cran")
	cranHandler.Register(cranGroup)

	// Register Helm adapter
	helmHandler := helm.New(cacheMgr, upstream.NewPrioritySelector(helmPool), cfg.Cache, database)
	helmGroup := r.Group("/helm")
	helmHandler.Register(helmGroup)

	// Register Docker Registry adapter (only if registries configured)
	if len(cfg.Docker.Registries) > 0 {
		dockerHandler := dockeradapter.New(cacheMgr, cfg.Cache, database, cfg.Docker)
		dockerGroup := r.Group("/v2")
		dockerHandler.Register(dockerGroup)
		zap.L().Info("docker registry proxy enabled",
			zap.Int("registries", len(cfg.Docker.Registries)),
			zap.String("default", cfg.Docker.DefaultRegistry),
		)
	}

	// Serve embedded frontend (SPA fallback)
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		zap.L().Fatal("failed to load embedded frontend", zap.Error(err))
	}
	staticHandler := http.FileServer(http.FS(distFS))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Try serving the static file first
		if path == "/" || strings.HasPrefix(path, "/assets") {
			staticHandler.ServeHTTP(c.Writer, c.Request)
			return
		}

		// For /admin/* and other SPA routes, serve index.html
		c.Request.URL.Path = "/"
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	zap.L().Info("starting server", zap.String("addr", addr))
	if err := r.Run(addr); err != nil {
		zap.L().Fatal("server failed", zap.Error(err))
	}
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
			// Update existing record with config values
			database.Model(&record).Updates(map[string]interface{}{
				"url":      u.URL,
				"proxy":    u.Proxy,
				"priority": u.Priority,
			})
		}
	}
}
