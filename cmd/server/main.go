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

	"repocache/internal/adapter/apt"
	"repocache/internal/adapter/pypi"
	"repocache/internal/api"
	"repocache/internal/cache"
	"repocache/internal/config"
	"repocache/internal/db"
	"repocache/internal/middleware"
	"repocache/internal/upstream"
	web "repocache/web"
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

	// Initialize cache manager
	cacheMgr := cache.NewManager(storage, database)

	// Initialize upstream pools
	pypiPool, err := upstream.NewPool(cfg.PyPI.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create pypi upstream pool", zap.Error(err))
	}
	aptPool, err := upstream.NewPool(cfg.APT.Upstreams)
	if err != nil {
		zap.L().Fatal("failed to create apt upstream pool", zap.Error(err))
	}

	// Start background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go upstream.StartHealthCheck(ctx, pypiPool, 30*time.Second)
	go upstream.StartHealthCheck(ctx, aptPool, 30*time.Second)
	go cache.StartLRUCleanup(ctx, storage, database, cfg.Cache.MaxSizeGB, cfg.Cache.LRUThreshold, 5*time.Minute)

	// Setup Gin
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())

	// Register all API routes
	api.RegisterRoutes(r, api.Deps{
		DB:       database,
		Storage:  storage,
		Config:   cfg,
		PyPIPool: pypiPool,
		APTPool:  aptPool,
	})

	// Register PyPI adapter
	pypiHandler := pypi.New(cacheMgr, upstream.NewPrioritySelector(pypiPool), cfg.Cache, database)
	pypiGroup := r.Group("/pypi")
	pypiHandler.Register(pypiGroup)

	// Register APT adapter
	aptHandler := apt.New(cacheMgr, upstream.NewPrioritySelector(aptPool), cfg.Cache, database)
	aptGroup := r.Group("/apt")
	aptHandler.Register(aptGroup)

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
