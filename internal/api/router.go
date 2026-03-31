package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/admin"
	"depsilo/internal/api/public"
	"depsilo/internal/audit"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/rules"
	"depsilo/internal/upstream"
)

var startTime = time.Now()

// Deps holds shared dependencies for route registration.
type Deps struct {
	DB       *gorm.DB
	Storage  cache.Storage
	Config   *config.Config
	PyPIPool *upstream.Pool
	APTPool  *upstream.Pool
	NPMPool  *upstream.Pool
	GoPool    *upstream.Pool
	CargoPool    *upstream.Pool
	MavenPool    *upstream.Pool
	RubyGemsPool *upstream.Pool
	ComposerPool *upstream.Pool
	NuGetPool    *upstream.Pool
	CondaPool    *upstream.Pool
	CRANPool     *upstream.Pool
	HelmPool       *upstream.Pool
	EventBus       *cache.EventBus
	LicenseManager *license.Manager
	AuditLogger    *audit.Logger
	RulesStore     *rules.Store
	RulesEngine    *rules.Engine
}

func RegisterRoutes(r *gin.Engine, deps Deps) {
	// Public routes
	r.GET("/health", healthHandler)
	r.GET("/metrics", MetricsHandler())

	apiV1 := r.Group("/api/v1")

	// Public stats
	statsHandler := public.NewStatsHandler(deps.DB, deps.Storage, deps.PyPIPool, deps.APTPool, deps.NPMPool, deps.GoPool, deps.CargoPool, deps.MavenPool, deps.RubyGemsPool, deps.ComposerPool, deps.NuGetPool, deps.CondaPool, deps.CRANPool, deps.HelmPool)
	apiV1.GET("/stats", statsHandler.GetStats)

	// Public packages
	pkgHandler := public.NewPackagesHandler(deps.DB)
	apiV1.GET("/packages", pkgHandler.List)
	apiV1.GET("/packages/:type/:name", pkgHandler.Detail)

	// Real-time events (SSE)
	eventsHandler := public.NewEventsHandler(deps.EventBus)
	apiV1.GET("/events/stream", eventsHandler.Stream)

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
	dashHandler := admin.NewDashboardHandler(deps.DB, deps.Storage, deps.PyPIPool, deps.APTPool, deps.NPMPool, deps.GoPool, deps.CargoPool, deps.MavenPool, deps.RubyGemsPool, deps.ComposerPool, deps.NuGetPool, deps.CondaPool, deps.CRANPool, deps.HelmPool)
	adminGroup.GET("/dashboard", dashHandler.GetDashboard)
	adminGroup.GET("/dashboard/trends", dashHandler.GetTrends)

	// Cache management
	cacheHandler := admin.NewCacheHandler(deps.DB, deps.Storage, deps.Config.Cache.MaxSizeGB)
	adminGroup.GET("/cache", cacheHandler.List)
	adminGroup.DELETE("/cache/:id", cacheHandler.Delete)
	adminGroup.POST("/cache/cleanup", cacheHandler.Cleanup)
	adminGroup.GET("/cache/distribution", cacheHandler.GetDistribution)

	// Upstream management
	upstreamHandler := admin.NewUpstreamHandler(deps.DB)
	adminGroup.GET("/upstreams", upstreamHandler.List)
	adminGroup.POST("/upstreams", upstreamHandler.Create)
	adminGroup.PUT("/upstreams/:id", upstreamHandler.Update)
	adminGroup.DELETE("/upstreams/:id", upstreamHandler.Delete)
	adminGroup.POST("/upstreams/:id/check", upstreamHandler.Check)

	// Upstream latency history
	latencyHandler := admin.NewLatencyHandler(deps.DB)
	adminGroup.GET("/upstreams/:id/latency", latencyHandler.GetLatencyHistory)

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

	// License
	licenseHandler := admin.NewLicenseHandler(deps.LicenseManager)
	adminGroup.GET("/license", licenseHandler.GetStatus)
	adminGroup.POST("/license/revalidate", licenseHandler.Revalidate)

	// Pro features (require license)
	proGroup := adminGroup.Group("")
	proGroup.Use(license.RequirePro(deps.LicenseManager))

	auditHandler := admin.NewAuditHandler(deps.DB)
	proGroup.GET("/audit-logs", auditHandler.List)
	proGroup.GET("/audit-logs/export", auditHandler.Export)

	rulesHandler := admin.NewRulesHandler(deps.DB, deps.RulesStore, deps.RulesEngine)
	proGroup.GET("/rules", rulesHandler.List)
	proGroup.POST("/rules", rulesHandler.Create)
	proGroup.PUT("/rules/:id", rulesHandler.Update)
	proGroup.DELETE("/rules/:id", rulesHandler.Delete)
	proGroup.POST("/rules/test", rulesHandler.Test)
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"uptime": time.Since(startTime).String(),
	})
}
