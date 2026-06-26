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
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/notify"
	"depsilo/internal/rules"
	"depsilo/internal/security"
	"depsilo/internal/trial"
	"depsilo/internal/upstream"
	"depsilo/internal/version"
)

var startTime = time.Now()

// Deps holds shared dependencies for route registration.
//
// Pools is keyed by ecosystem name ("pypi", "apt", ...); Ecosystems
// is the ordered list used by UIs to render upstreams deterministically.
// See docs/adr/0001-pools-map.md.
type Deps struct {
	DB         *gorm.DB
	Storage    cache.Storage
	Config     *config.Config
	Pools      map[string]*upstream.Pool
	Ecosystems []string
	CacheMgr   *cache.Manager
	EventBus   *cache.EventBus
	LicenseManager   *license.Manager
	TrialManager     *trial.Manager       // NEW
	Entitlement      *entitlement.Checker // NEW
	AuditLogger      *audit.Logger
	RulesStore       *rules.Store
	RulesEngine      *rules.Engine
	SecurityScanner  *security.Scanner
	SecurityImporter *security.Importer
	WebhookNotifier  *notify.Notifier
}

func RegisterRoutes(r *gin.Engine, deps Deps) {
	// Public routes
	r.GET("/health", healthHandler)
	r.GET("/metrics", MetricsHandler())

	apiV1 := r.Group("/api/v1")

	// Self-describing endpoints for AI agents + automation (no auth)
	discoverHandler := public.NewDiscoverHandler(deps.Ecosystems)
	apiV1.GET("/discover", discoverHandler.Discover)
	apiV1.GET("/agent-prompt", discoverHandler.AgentPrompt)

	// Brand-neutral project-integration prompt: users paste this into their
	// coding LLM and the LLM edits Dockerfile/CI/build scripts to route
	// installs through this mirror. Different audience from agent-prompt
	// (which configures a developer's local machine).
	integrationPromptHandler := public.NewIntegrationPromptHandler()
	apiV1.GET("/integration-prompt", integrationPromptHandler.Get)

	// Model Context Protocol endpoint — JSON-RPC 2.0 over Streamable HTTP.
	// AI clients (Claude Code, Cursor, etc.) POST initialize / tools/list /
	// tools/call / resources/read / prompts/get here.
	mcpHandler := public.NewMCPHandler(deps.DB, deps.Ecosystems, deps.Config.AccessLog.RollupEnabled)
	r.POST("/mcp", mcpHandler.Handle)

	// Public stats
	statsHandler := public.NewStatsHandler(deps.DB, deps.Storage, deps.Pools, deps.Ecosystems, deps.Config.ExtraIndexes, deps.Config.AccessLog.RollupEnabled)
	apiV1.GET("/stats", statsHandler.GetStats)
	apiV1.GET("/latency-series", statsHandler.GetLatencySeries)

	// Public packages
	pkgHandler := public.NewPackagesHandler(deps.DB)
	apiV1.GET("/packages", pkgHandler.List)
	apiV1.GET("/packages/:type/:name", pkgHandler.Detail)

	// Real-time events (SSE)
	eventsHandler := public.NewEventsHandler(deps.EventBus)
	apiV1.GET("/events/stream", eventsHandler.Stream)

	// Setup wizard (no auth required)
	setupHandler := NewSetupHandler(deps.Config)
	apiV1.GET("/setup/status", setupHandler.Status)
	apiV1.POST("/setup/complete", setupHandler.Complete)

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
	dashHandler := admin.NewDashboardHandler(deps.DB, deps.Storage, deps.Pools, deps.Ecosystems, deps.Config.AccessLog.RollupEnabled)
	adminGroup.GET("/dashboard", dashHandler.GetDashboard)
	adminGroup.GET("/dashboard/trends", dashHandler.GetTrends)

	// Bandwidth report
	bandwidthHandler := admin.NewBandwidthHandler(deps.DB, deps.Config.AccessLog.RollupEnabled)
	adminGroup.GET("/bandwidth", bandwidthHandler.GetReport)

	// Cache management
	cacheHandler := admin.NewCacheHandler(deps.DB, deps.Storage, deps.Config.Cache.MaxSizeGB)
	adminGroup.GET("/cache", cacheHandler.List)
	adminGroup.DELETE("/cache/:id", cacheHandler.Delete)
	adminGroup.POST("/cache/cleanup", cacheHandler.Cleanup)
	adminGroup.GET("/cache/distribution", cacheHandler.GetDistribution)

	// Cache warmup
	warmupHandler := admin.NewWarmupHandler(deps.CacheMgr, deps.Pools, deps.Config)
	adminGroup.POST("/cache/warmup", warmupHandler.Warmup)

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

	// Webhook notifications
	webhookHandler := admin.NewWebhookHandler(deps.DB, deps.WebhookNotifier)
	adminGroup.GET("/webhooks", webhookHandler.List)
	adminGroup.POST("/webhooks", webhookHandler.Create)
	adminGroup.PUT("/webhooks/:id", webhookHandler.Update)
	adminGroup.DELETE("/webhooks/:id", webhookHandler.Delete)
	adminGroup.POST("/webhooks/:id/test", webhookHandler.Test)

	// License — status, key mutation, trial activation (no Pro gate; free users need these)
	licenseHandler := admin.NewLicenseHandler(deps.LicenseManager, deps.TrialManager, deps.Entitlement)
	adminGroup.GET("/license/status", licenseHandler.GetStatus)
	adminGroup.POST("/license/revalidate", licenseHandler.Revalidate)
	adminGroup.POST("/license/trial/activate", licenseHandler.ActivateTrial)
	adminGroup.PUT("/license/key", licenseHandler.SetKey)
	adminGroup.DELETE("/license/key", licenseHandler.ClearKey)

	// Pro features (require entitlement)
	proGroup := adminGroup.Group("")
	proGroup.Use(entitlement.RequirePro(deps.Entitlement))

	auditHandler := admin.NewAuditHandler(deps.DB)
	proGroup.GET("/audit-logs", auditHandler.List)
	proGroup.GET("/audit-logs/export", auditHandler.Export)

	rulesHandler := admin.NewRulesHandler(deps.DB, deps.RulesStore, deps.RulesEngine)
	proGroup.GET("/rules", rulesHandler.List)
	proGroup.POST("/rules", rulesHandler.Create)
	proGroup.PUT("/rules/:id", rulesHandler.Update)
	proGroup.DELETE("/rules/:id", rulesHandler.Delete)
	proGroup.POST("/rules/test", rulesHandler.Test)

	// Security intelligence (Pro)
	securityHandler := admin.NewSecurityHandler(deps.DB, deps.SecurityScanner, deps.SecurityImporter)
	proGroup.GET("/security/dashboard", securityHandler.Dashboard)
	proGroup.GET("/security/vulnerabilities", securityHandler.ListVulnerabilities)
	proGroup.GET("/security/packages", securityHandler.ListPackages)
	proGroup.GET("/security/suggestions", securityHandler.ListSuggestions)
	proGroup.POST("/security/suggestions/:vuln_id/approve", securityHandler.ApproveSuggestion)
	proGroup.POST("/security/suggestions/:vuln_id/dismiss", securityHandler.DismissSuggestion)
	proGroup.POST("/security/scan", securityHandler.TriggerScan)
	proGroup.POST("/security/import", securityHandler.ImportData)
	proGroup.GET("/security/policies", securityHandler.ListPolicies)
	proGroup.PUT("/security/policies/:ecosystem", securityHandler.UpdatePolicy)

	// Project management (Pro)
	projectsHandler := admin.NewProjectsHandler(deps.DB)
	proGroup.GET("/projects", projectsHandler.List)
	proGroup.POST("/projects", projectsHandler.Create)
	proGroup.GET("/projects/:id", projectsHandler.Detail)
	proGroup.PUT("/projects/:id", projectsHandler.Update)
	proGroup.DELETE("/projects/:id", projectsHandler.Delete)
	proGroup.GET("/projects/:id/packages", projectsHandler.ListPackages)
	proGroup.POST("/projects/:id/token", projectsHandler.RegenerateToken)
	proGroup.GET("/projects/:id/sbom", projectsHandler.ExportSBOM)
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"version": version.Version,
		"uptime":  time.Since(startTime).String(),
	})
}
