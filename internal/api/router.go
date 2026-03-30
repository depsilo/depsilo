package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depslio/internal/api/admin"
	"depslio/internal/api/public"
	"depslio/internal/cache"
	"depslio/internal/config"
	"depslio/internal/middleware"
	"depslio/internal/upstream"
)

var startTime = time.Now()

// Deps holds shared dependencies for route registration.
type Deps struct {
	DB       *gorm.DB
	Storage  cache.Storage
	Config   *config.Config
	PyPIPool *upstream.Pool
	APTPool  *upstream.Pool
}

func RegisterRoutes(r *gin.Engine, deps Deps) {
	// Public routes
	r.GET("/health", healthHandler)
	r.GET("/metrics", MetricsHandler())

	apiV1 := r.Group("/api/v1")

	// Public stats
	statsHandler := public.NewStatsHandler(deps.DB, deps.Storage, deps.PyPIPool, deps.APTPool)
	apiV1.GET("/stats", statsHandler.GetStats)

	// Public packages
	pkgHandler := public.NewPackagesHandler(deps.DB)
	apiV1.GET("/packages", pkgHandler.List)
	apiV1.GET("/packages/:type/:name", pkgHandler.Detail)

	// Auth routes
	authHandler := NewAuthHandler(deps.DB, deps.Config.Auth)
	authGroup := apiV1.Group("/auth")
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.POST("/refresh",
		middleware.JWTAuth(deps.Config.Auth.JWTSecret, deps.DB),
		authHandler.Refresh,
	)

	// Admin routes (require JWT + admin role)
	adminGroup := apiV1.Group("/admin")
	adminGroup.Use(middleware.JWTAuth(deps.Config.Auth.JWTSecret, deps.DB))
	adminGroup.Use(middleware.AdminRequired())

	// Dashboard
	dashHandler := admin.NewDashboardHandler(deps.DB, deps.Storage, deps.PyPIPool, deps.APTPool)
	adminGroup.GET("/dashboard", dashHandler.GetDashboard)

	// Cache management
	cacheHandler := admin.NewCacheHandler(deps.DB, deps.Storage)
	adminGroup.GET("/cache", cacheHandler.List)
	adminGroup.DELETE("/cache/:id", cacheHandler.Delete)
	adminGroup.POST("/cache/cleanup", cacheHandler.Cleanup)

	// Upstream management
	upstreamHandler := admin.NewUpstreamHandler(deps.DB)
	adminGroup.GET("/upstreams", upstreamHandler.List)
	adminGroup.POST("/upstreams", upstreamHandler.Create)
	adminGroup.PUT("/upstreams/:id", upstreamHandler.Update)
	adminGroup.DELETE("/upstreams/:id", upstreamHandler.Delete)
	adminGroup.POST("/upstreams/:id/check", upstreamHandler.Check)

	// Access logs
	logHandler := admin.NewAccessLogHandler(deps.DB)
	adminGroup.GET("/logs", logHandler.List)

	// User management
	userHandler := admin.NewUserHandler(deps.DB)
	adminGroup.GET("/users", userHandler.List)
	adminGroup.POST("/users", userHandler.Create)
	adminGroup.PUT("/users/:id", userHandler.Update)
	adminGroup.DELETE("/users/:id", userHandler.Delete)

	// API Tokens
	tokenHandler := admin.NewTokenHandler(deps.DB)
	adminGroup.GET("/tokens", tokenHandler.List)
	adminGroup.POST("/tokens", tokenHandler.Create)
	adminGroup.DELETE("/tokens/:id", tokenHandler.Delete)

	// Settings
	settingsHandler := admin.NewSettingsHandler(deps.Config)
	adminGroup.GET("/settings", settingsHandler.Get)
	adminGroup.PUT("/settings", settingsHandler.Update)
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"uptime": time.Since(startTime).String(),
	})
}
