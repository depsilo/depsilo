package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/admin"
	"depsilo/internal/api/public"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/blocklist"
	"depsilo/internal/cache"
	"depsilo/internal/compilecache"
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
	"depsilo/internal/upstreamupdates"
	"depsilo/internal/version"
)

var startTime = time.Now()

// Deps holds shared dependencies for route registration.
//
// Pools is keyed by ecosystem name ("pypi", "apt", ...); Ecosystems
// is the ordered list used by UIs to render upstreams deterministically.
// See docs/adr/0001-pools-map.md.
type Deps struct {
	LifecycleContext context.Context
	DB               *gorm.DB
	Storage          cache.Storage
	Config           *config.Config
	ConfigStore      *config.Store
	Pools            map[string]*upstream.Pool
	UpstreamRegistry *upstream.Registry
	Ecosystems       []string
	CacheMgr         *cache.Manager
	CacheRetention   *cache.Retention
	CompileCache     CompileCacheRouteDependencies
	IndexRefresher   upstreamupdates.Refresher
	EventBus         *cache.EventBus
	LicenseManager   *license.Manager
	TrialManager     *trial.Manager       // NEW
	Entitlement      *entitlement.Checker // NEW
	RulesStore       *rules.Store
	RulesEngine      *rules.Engine
	SecurityScanner  *security.Scanner
	SecurityCatalog  *security.AdvisoryCatalog
	WebhookNotifier  *notify.Notifier
	// QuarantineStore is the supply-chain quarantine helper exposed via
	// admin endpoints for events / approvals. Separate from the
	// adapter-side gate bound to each request by the server's adapter
	// RequestScope; this is the admin-control-plane access path.
	QuarantineStore *quarantine.Store
	// BlocklistStore / BlocklistSyncer power the known-malicious
	// blocklist admin endpoints (status / manual sync / overrides).
	// Both nil when [supply_chain.blocklist] enabled = false.
	BlocklistStore  *blocklist.Store
	BlocklistSyncer *blocklist.Syncer
	Tasks           asyncruntime.Submitter
}

// CompileCacheRouteDependencies is the coherent handler-facing view of the
// optional compiler-cache runtime. Disabled routes remain registered with a
// nil Service so package-client misses can never fall through to the SPA.
type CompileCacheRouteDependencies struct {
	Enabled    bool
	PublicURL  string
	Service    *compilecache.Service
	Authorizer *compilecache.Authorizer
}

func RegisterRoutes(r *gin.Engine, deps Deps) {
	// Public routes
	r.GET("/health", healthHandler)
	r.GET("/live", healthHandler)
	r.GET("/ready", readinessHandler(deps.DB, deps.Storage))
	r.GET("/metrics", MetricsHandler())

	// Stock ccache remote-storage data plane. The route remains registered
	// while disabled so the SPA fallback never returns HTML as a false hit.
	ccacheHandler := NewCCacheHandler(deps.CompileCache.Enabled, deps.CompileCache.Service, deps.CompileCache.Authorizer)
	ccacheGroup := r.Group("/ccache/v1/:namespace")
	ccacheGroup.Any("/*key", ccacheHandler.Handle)
	// sccache uses a narrow WebDAV Adapter over the same quota/LRU/storage
	// Module. Keep this route registered while disabled for the same SPA-safety
	// reason as the ccache route above.
	sccacheHandler := NewSCCacheHandler(deps.CompileCache.Enabled, deps.CompileCache.Service, deps.CompileCache.Authorizer)
	sccacheGroup := r.Group("/sccache/v1/:namespace")
	sccacheGroup.Any("/*path", sccacheHandler.Handle)
	sccacheGroup.Handle(methodPropfind, "/*path", sccacheHandler.Handle)
	sccacheGroup.Handle(methodMkcol, "/*path", sccacheHandler.Handle)

	apiV1 := r.Group("/api/v1")

	// Self-describing endpoints for AI agents + automation (no auth)
	discoverHandler := public.NewDiscoverHandler(deps.Ecosystems)
	apiV1.GET("/discover", discoverHandler.Discover)
	apiV1.GET("/agent-prompt", discoverHandler.AgentPrompt)

	// Transparent project-integration prompt: users paste this into their
	// coding LLM and review Dockerfile/CI/build-script edits that name Depsilo
	// and its URL. Different audience from agent-prompt, which configures a
	// developer's local machine.
	integrationPromptHandler := public.NewIntegrationPromptHandler()
	apiV1.GET("/integration-prompt", integrationPromptHandler.Get)

	// Public stats. The same handler builds the MCP stats resource in-process,
	// so both interfaces keep one response contract without HTTP self-fetching.
	statsHandler := public.NewStatsHandler(deps.DB, deps.Storage, deps.Pools, deps.Ecosystems, deps.Config.ExtraIndexes, deps.Config.AccessLog.RollupEnabled)
	apiV1.GET("/stats", statsHandler.GetStats)
	apiV1.GET("/latency-series", statsHandler.GetLatencySeries)

	// Model Context Protocol endpoint — JSON-RPC 2.0 over Streamable HTTP.
	// AI clients (Claude Code, Cursor, etc.) POST initialize / tools/list /
	// tools/call / resources/read / prompts/get here.
	mcpHandler := public.NewMCPHandler(deps.DB, deps.Ecosystems, deps.Config.AccessLog.RollupEnabled, statsHandler)
	r.POST(
		"/mcp",
		middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB),
		middleware.ReadRequired(),
		mcpHandler.Handle,
	)

	// Live "Now" strip — polled every 5s by authenticated Admin clients.
	// It includes the most recent request identity, so it must not share the
	// anonymous Portal status boundary. Reuses StatsHandler's startTime so
	// uptime values agree across endpoints.
	nowHandler := public.NewNowHandler(deps.DB, deps.Pools, statsHandler.StartTime())

	// Package inventory and request history belong to authenticated Operators.
	// Keep this group separate from the anonymous Portal status surface.
	packageHistoryRead := apiV1.Group("")
	packageHistoryRead.Use(middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB))
	packageHistoryRead.Use(middleware.ReadRequired())
	packageHistoryRead.GET("/now", nowHandler.Get)

	pkgHandler := public.NewPackagesHandler(deps.DB)
	packageHistoryRead.GET("/packages", pkgHandler.List)
	packageHistoryRead.GET("/packages/:type/:name", pkgHandler.Detail)

	// Real-time events (SSE)
	eventsHandler := public.NewEventsHandler(deps.EventBus)
	packageHistoryRead.GET("/events/stream", eventsHandler.Stream)

	// Setup wizard (no auth required)
	setupHandler := NewSetupHandler(deps.Config, deps.DB)
	apiV1.GET("/setup/status", setupHandler.Status)
	apiV1.POST("/setup/complete", setupHandler.Complete)

	// Auth routes
	authHandler := NewAuthHandler(deps.DB, deps.Config.Auth)
	authGroup := apiV1.Group("/auth")
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.GET("/me", middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB), authHandler.Me)
	authGroup.POST("/refresh", middleware.JWTOnly(deps.Config.Auth.JWTSecret, deps.DB), authHandler.Refresh)

	// Admin routes require authentication and an explicit capability.
	adminGroup := apiV1.Group("/admin")
	adminGroup.Use(middleware.Authenticate(deps.Config.Auth.JWTSecret, deps.DB))
	adminRead := adminGroup.Group("")
	adminRead.Use(middleware.ReadRequired())
	adminWrite := adminGroup.Group("")
	adminWrite.Use(middleware.WriteRequired())

	// First-project onboarding is authenticated because its status exposes a
	// narrow view of package request history. Existing deployments default to
	// completed when no durable onboarding state exists.
	onboardingHandler := NewOnboardingHandler(deps.DB)
	adminRead.GET("/onboarding/status", onboardingHandler.Status)
	adminWrite.PUT("/onboarding", onboardingHandler.Update)

	// Dashboard
	dashHandler := admin.NewDashboardHandler(deps.DB, deps.Pools, deps.Ecosystems, deps.Config.AccessLog.RollupEnabled, deps.Config.Cache.MaxSizeGB)
	adminRead.GET("/dashboard", dashHandler.GetDashboard)
	adminRead.GET("/dashboard/recent-downloads", dashHandler.GetRecentDownloads)
	adminRead.GET("/dashboard/trends", dashHandler.GetTrends)

	// Bandwidth report
	bandwidthHandler := admin.NewBandwidthHandler(deps.DB, deps.Config.AccessLog.RollupEnabled)
	adminRead.GET("/bandwidth", bandwidthHandler.GetReport)

	// Cache management
	cacheHandler := admin.NewCacheHandler(deps.DB, deps.CacheRetention, deps.Config.Cache.MaxSizeGB)
	cacheHandler.SetIndexRefresher(deps.IndexRefresher)
	adminRead.GET("/cache", cacheHandler.List)
	adminRead.GET("/cache/indexes", cacheHandler.ListIndexes)
	adminRead.GET("/cache/distribution", cacheHandler.GetDistribution)
	adminWrite.DELETE("/cache/:id", cacheHandler.Delete)
	adminWrite.POST("/cache/cleanup", cacheHandler.Cleanup)
	adminWrite.POST("/cache/indexes/:id/refresh", cacheHandler.RefreshIndex)

	// Cache warmup
	warmupHandler := admin.NewWarmupHandler(deps.Tasks, deps.CacheMgr, deps.Pools, deps.Config)
	adminWrite.POST("/cache/warmup", warmupHandler.Warmup)

	// Compiler cache is a separate data domain from package cache. Its machine
	// credentials can only access one compiler-cache namespace and grant no Admin API
	// authority.
	compileCacheHandler := admin.NewCompileCacheHandler(
		deps.DB,
		deps.CompileCache.Service,
		deps.CompileCache.Enabled,
		deps.CompileCache.PublicURL,
	)
	adminRead.GET("/compile-cache/status", compileCacheHandler.Status)
	adminRead.GET("/compile-cache/credentials", compileCacheHandler.ListCredentials)
	adminWrite.POST("/compile-cache/credentials", compileCacheHandler.CreateCredential)
	adminWrite.DELETE("/compile-cache/credentials/:id", compileCacheHandler.DeleteCredential)
	adminWrite.POST("/compile-cache/cleanup", compileCacheHandler.Cleanup)

	// Upstream management
	upstreamHandler := admin.NewUpstreamHandler(deps.UpstreamRegistry)
	adminRead.GET("/upstreams", upstreamHandler.List)
	adminWrite.POST("/upstreams", upstreamHandler.Create)
	adminWrite.PUT("/upstreams/:id", upstreamHandler.Update)
	adminWrite.DELETE("/upstreams/:id", upstreamHandler.Delete)
	adminWrite.POST("/upstreams/:id/check", upstreamHandler.Check)

	// Upstream latency history
	latencyHandler := admin.NewLatencyHandler(deps.DB)
	adminRead.GET("/upstreams/latency", latencyHandler.GetLatencySeries)
	adminRead.GET("/upstreams/:id/latency", latencyHandler.GetLatencyHistory)

	// Access logs
	logHandler := admin.NewAccessLogHandler(deps.DB)
	adminRead.GET("/logs", logHandler.List)
	adminRead.GET("/logs/export", logHandler.Export)

	// User management
	userHandler := admin.NewUserHandler(deps.DB)
	adminRead.GET("/users", userHandler.List)
	adminWrite.POST("/users", userHandler.Create)
	adminWrite.PUT("/users/:id", userHandler.Update)
	adminWrite.DELETE("/users/:id", userHandler.Delete)

	// API Tokens
	tokenHandler := admin.NewTokenHandler(deps.DB)
	adminRead.GET("/tokens", tokenHandler.List)
	adminWrite.POST("/tokens", tokenHandler.Create)
	adminWrite.DELETE("/tokens/:id", tokenHandler.Delete)

	// Settings
	settingsHandler := admin.NewSettingsHandler(deps.ConfigStore)
	adminRead.GET("/settings", settingsHandler.Get)
	adminWrite.PUT("/settings", settingsHandler.Update)

	// Webhook notifications
	webhookHandler := admin.NewWebhookHandler(deps.DB, deps.WebhookNotifier)
	adminRead.GET("/webhooks", webhookHandler.List)
	adminWrite.POST("/webhooks", webhookHandler.Create)
	adminWrite.PUT("/webhooks/:id", webhookHandler.Update)
	adminWrite.DELETE("/webhooks/:id", webhookHandler.Delete)
	adminWrite.POST("/webhooks/:id/test", webhookHandler.Test)

	// License — status, key mutation, trial activation (no Pro gate; free users need these)
	licenseHandler := admin.NewLicenseHandler(deps.LicenseManager, deps.TrialManager, deps.Entitlement)
	adminRead.GET("/license/status", licenseHandler.GetStatus)
	adminWrite.POST("/license/revalidate", licenseHandler.Revalidate)
	adminWrite.POST("/license/trial/activate", licenseHandler.ActivateTrial)
	adminWrite.PUT("/license/key", licenseHandler.SetKey)
	adminWrite.DELETE("/license/key", licenseHandler.ClearKey)

	// Audit logs, rules engine, and the security intelligence dashboard
	// landed in open-source on 2026-06-28 as part of the pricing reset —
	// these are governance primitives a self-hosted enforcement layer needs
	// from day one. See docs/DIRECTION.md "Pricing reset" decisions log
	// and ADR-0003. They mount on the regular adminGroup (auth-only, no
	// entitlement gate).
	auditHandler := admin.NewAuditHandler(deps.DB)
	adminRead.GET("/audit-logs", auditHandler.List)
	adminRead.GET("/audit-logs/export", auditHandler.Export)
	upstreamUpdateHandler := admin.NewUpstreamUpdateHandler(deps.DB)
	adminRead.GET("/upstream-updates", upstreamUpdateHandler.List)

	rulesHandler := admin.NewRulesHandler(deps.RulesStore, deps.RulesEngine)
	adminRead.GET("/rules", rulesHandler.List)
	adminRead.POST("/rules/test", rulesHandler.Test)
	adminWrite.POST("/rules", rulesHandler.Create)
	adminWrite.PUT("/rules/:id", rulesHandler.Update)
	adminWrite.DELETE("/rules/:id", rulesHandler.Delete)

	var invalidateSecurityRules func()
	if deps.RulesEngine != nil {
		invalidateSecurityRules = deps.RulesEngine.InvalidateCache
	}
	securityHandler := admin.NewSecurityHandlerWithContext(
		deps.LifecycleContext,
		deps.DB,
		deps.SecurityScanner,
		deps.SecurityCatalog,
		invalidateSecurityRules,
	)
	adminRead.GET("/security/dashboard", securityHandler.Dashboard)
	adminRead.GET("/security/vulnerabilities", securityHandler.ListVulnerabilities)
	adminRead.GET("/security/packages", securityHandler.ListPackages)
	adminRead.GET("/security/suggestions", securityHandler.ListSuggestions)
	adminRead.GET("/security/policies", securityHandler.ListPolicies)
	adminWrite.POST("/security/suggestions/:vuln_id/approve", securityHandler.ApproveSuggestion)
	adminWrite.POST("/security/suggestions/:vuln_id/dismiss", securityHandler.DismissSuggestion)
	adminWrite.POST("/security/scan", securityHandler.TriggerScan)
	adminWrite.POST("/security/import", securityHandler.ImportData)
	adminWrite.PUT("/security/policies/:ecosystem", securityHandler.UpdatePolicy)

	// Supply-chain quarantine — minimum-release-age event log +
	// operator approvals. Wedge feature, open-source per
	// docs/DIRECTION.md, so endpoints mount under adminGroup (NOT
	// proGroup). See ADR-0003.
	quarantineHandler := admin.NewQuarantineHandler(deps.DB, deps.QuarantineStore)
	adminRead.GET("/quarantine/events", quarantineHandler.ListEvents)
	adminRead.GET("/quarantine/approvals", quarantineHandler.ListApprovals)
	adminWrite.POST("/quarantine/approve", quarantineHandler.Approve)
	adminWrite.DELETE("/quarantine/approvals/:id", quarantineHandler.Revoke)

	// Known-malicious blocklist (DIRECTION Task 2) — sync status,
	// manual refresh, and 24h-expiring false-positive overrides.
	// Open-source like quarantine; blocked-request events surface via
	// /quarantine/events (action = malware_blocked).
	blocklistHandler := admin.NewBlocklistHandler(deps.Tasks, deps.BlocklistStore, deps.BlocklistSyncer)
	adminRead.GET("/blocklist/status", blocklistHandler.Status)
	adminRead.GET("/blocklist/overrides", blocklistHandler.ListOverrides)
	adminWrite.POST("/blocklist/sync", blocklistHandler.TriggerSync)
	adminWrite.POST("/blocklist/overrides", blocklistHandler.CreateOverride)
	adminWrite.DELETE("/blocklist/overrides/:id", blocklistHandler.RevokeOverride)

	// Pro features (require entitlement). Multi-project workspaces are
	// the only UI surface gated today — production teams running Depsilo
	// across many projects/teams are the buyer the Pro contract is built
	// for, so "do you need multiple projects?" is the natural surfaced
	// trigger for the sales conversation. Runtime per-project SBOM export
	// lives here because it is an artifact of the multi-project surface.
	// Depsilo's own release SBOMs are generated by the open CI workflows;
	// there is no separate open runtime export endpoint today.
	proRead := adminRead.Group("")
	proRead.Use(entitlement.RequirePro(deps.Entitlement))
	proWrite := adminWrite.Group("")
	proWrite.Use(entitlement.RequirePro(deps.Entitlement))

	projectsHandler := admin.NewProjectsHandler(deps.DB)
	proRead.GET("/projects", projectsHandler.List)
	proRead.GET("/projects/:id", projectsHandler.Detail)
	proRead.GET("/projects/:id/packages", projectsHandler.ListPackages)
	proRead.GET("/projects/:id/sbom", projectsHandler.ExportSBOM)
	proWrite.POST("/projects", projectsHandler.Create)
	proWrite.PUT("/projects/:id", projectsHandler.Update)
	proWrite.DELETE("/projects/:id", projectsHandler.Delete)
	proWrite.POST("/projects/:id/token", projectsHandler.RegenerateToken)
}

func healthHandler(c *gin.Context) {
	// Setup may move the service to a different port. Allow the setup page on
	// the old origin to inspect the status code instead of relying on an opaque
	// no-cors response, which cannot distinguish a healthy 200 from a 503.
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"version": version.Version,
		"uptime":  time.Since(startTime).String(),
	})
}
