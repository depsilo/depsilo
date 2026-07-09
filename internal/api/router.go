package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/admin"
	"depsilo/internal/api/public"
	"depsilo/internal/audit"
	"depsilo/internal/blocklist"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/notify"
	"depsilo/internal/quarantine"
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
	DB               *gorm.DB
	Storage          cache.Storage
	Config           *config.Config
	Pools            map[string]*upstream.Pool
	Ecosystems       []string
	CacheMgr         *cache.Manager
	EventBus         *cache.EventBus
	LicenseManager   *license.Manager
	TrialManager     *trial.Manager       // NEW
	Entitlement      *entitlement.Checker // NEW
	AuditLogger      *audit.Logger
	RulesStore       *rules.Store
	RulesEngine      *rules.Engine
	SecurityScanner  *security.Scanner
	SecurityImporter *security.Importer
	WebhookNotifier  *notify.Notifier
	// QuarantineStore is the supply-chain quarantine helper exposed via
	// admin endpoints for events / approvals. Separate from the
	// adapter-side gate which goes through internal/adapter's
	// package-level SetQuarantineChecker; this is the admin-control-
	// plane access path.
	QuarantineStore *quarantine.Store
	// BlocklistStore / BlocklistSyncer power the known-malicious
	// blocklist admin endpoints (status / manual sync / overrides).
	// Both nil when [supply_chain.blocklist] enabled = false.
	BlocklistStore  *blocklist.Store
	BlocklistSyncer *blocklist.Syncer
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

	// Live "Now" strip — polled every 5s by the dashboard. Small JSON,
	// focused on liveness signal (rate / hit_rate / upstream health /
	// last activity / 30-min sparkline). Reuses the StatsHandler's
	// startTime so uptime values agree across endpoints.
	nowHandler := public.NewNowHandler(deps.DB, deps.Pools, statsHandler.StartTime())
	apiV1.GET("/now", nowHandler.Get)

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

	// Audit logs, rules engine, and the security intelligence dashboard
	// landed in open-source on 2026-06-28 as part of the pricing reset —
	// these are governance primitives a self-hosted control point needs
	// from day one. See docs/DIRECTION.md "Pricing reset" decisions log
	// and ADR-0003. They mount on the regular adminGroup (auth-only, no
	// entitlement gate).
	auditHandler := admin.NewAuditHandler(deps.DB)
	adminGroup.GET("/audit-logs", auditHandler.List)
	adminGroup.GET("/audit-logs/export", auditHandler.Export)

	rulesHandler := admin.NewRulesHandler(deps.DB, deps.RulesStore, deps.RulesEngine)
	adminGroup.GET("/rules", rulesHandler.List)
	adminGroup.POST("/rules", rulesHandler.Create)
	adminGroup.PUT("/rules/:id", rulesHandler.Update)
	adminGroup.DELETE("/rules/:id", rulesHandler.Delete)
	adminGroup.POST("/rules/test", rulesHandler.Test)

	securityHandler := admin.NewSecurityHandler(deps.DB, deps.SecurityScanner, deps.SecurityImporter)
	adminGroup.GET("/security/dashboard", securityHandler.Dashboard)
	adminGroup.GET("/security/vulnerabilities", securityHandler.ListVulnerabilities)
	adminGroup.GET("/security/packages", securityHandler.ListPackages)
	adminGroup.GET("/security/suggestions", securityHandler.ListSuggestions)
	adminGroup.POST("/security/suggestions/:vuln_id/approve", securityHandler.ApproveSuggestion)
	adminGroup.POST("/security/suggestions/:vuln_id/dismiss", securityHandler.DismissSuggestion)
	adminGroup.POST("/security/scan", securityHandler.TriggerScan)
	adminGroup.POST("/security/import", securityHandler.ImportData)
	adminGroup.GET("/security/policies", securityHandler.ListPolicies)
	adminGroup.PUT("/security/policies/:ecosystem", securityHandler.UpdatePolicy)

	// Supply-chain quarantine — minimum-release-age event log +
	// operator approvals. Wedge feature, open-source per
	// docs/DIRECTION.md, so endpoints mount under adminGroup (NOT
	// proGroup). See ADR-0003.
	quarantineHandler := admin.NewQuarantineHandler(deps.DB, deps.QuarantineStore)
	adminGroup.GET("/quarantine/events", quarantineHandler.ListEvents)
	adminGroup.GET("/quarantine/approvals", quarantineHandler.ListApprovals)
	adminGroup.POST("/quarantine/approve", quarantineHandler.Approve)
	adminGroup.DELETE("/quarantine/approvals/:id", quarantineHandler.Revoke)

	// Known-malicious blocklist (DIRECTION Task 2) — sync status,
	// manual refresh, and 24h-expiring false-positive overrides.
	// Open-source like quarantine; blocked-request events surface via
	// /quarantine/events (action = malware_blocked).
	blocklistHandler := admin.NewBlocklistHandler(deps.BlocklistStore, deps.BlocklistSyncer)
	adminGroup.GET("/blocklist/status", blocklistHandler.Status)
	adminGroup.POST("/blocklist/sync", blocklistHandler.TriggerSync)
	adminGroup.GET("/blocklist/overrides", blocklistHandler.ListOverrides)
	adminGroup.POST("/blocklist/overrides", blocklistHandler.CreateOverride)
	adminGroup.DELETE("/blocklist/overrides/:id", blocklistHandler.RevokeOverride)

	// Pro features (require entitlement). Multi-project workspaces are
	// the only UI surface gated today — production teams running Depsilo
	// across many projects/teams are the buyer the Pro contract is built
	// for, so "do you need multiple projects?" is the natural surfaced
	// trigger for the sales conversation. Per-project SBOM export lives
	// here because it's an artifact of the multi-project surface; depsilo's
	// own SBOM (and a single-project user's SBOM) is generated through the
	// open-source CI workflow.
	proGroup := adminGroup.Group("")
	proGroup.Use(entitlement.RequirePro(deps.Entitlement))

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
