package public

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/version"
)

// DiscoverHandler serves a self-describing JSON catalog of the Depsilo service
// (capabilities, ecosystems, endpoints) plus a plain-text agent prompt. Both
// are designed to be reachable without authentication so AI coding agents and
// generic automation can self-bootstrap by hitting two URLs:
//
//	GET /api/v1/discover       — JSON catalog (this file)
//	GET /api/v1/agent-prompt   — copy-paste prompt text (this file)
type DiscoverHandler struct {
	ecosystems []string
}

// NewDiscoverHandler builds the handler. The ecosystems slice is the
// canonical "what proxies are wired" list from server.go.
func NewDiscoverHandler(ecosystems []string) *DiscoverHandler {
	return &DiscoverHandler{ecosystems: ecosystems}
}

// ecosystemInfo describes one wired ecosystem in the discover catalog.
type ecosystemInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// ecosystemPurposes is a static map from ecosystem name → short human/AI
// description. Lives alongside the handler so the catalog stays self-contained.
var ecosystemPurposes = map[string]struct {
	Path    string
	Purpose string
}{
	"pypi":        {"/pypi/", "Python — pip, uv, Poetry, PDM"},
	"apt":         {"/apt/", "Debian / Ubuntu APT packages"},
	"npm":         {"/npm/", "Node.js — npm, yarn, pnpm, bun"},
	"go":          {"/go/", "Go modules (GOPROXY protocol)"},
	"cargo":       {"/crates/", "Rust crates (sparse index)"},
	"maven":       {"/maven/", "Java — Maven, Gradle, sbt"},
	"rubygems":    {"/rubygems/", "Ruby — gem, bundler"},
	"composer":    {"/composer/", "PHP — Composer"},
	"nuget":       {"/nuget/", ".NET — NuGet v3 protocol"},
	"conda":       {"/conda/", "Conda channels"},
	"cran":        {"/cran/", "R — CRAN packages"},
	"helm":        {"/helm/", "Kubernetes Helm charts"},
	"huggingface": {"/huggingface/", "Hugging Face — models + datasets (huggingface-cli, transformers, datasets)"},
	"docker":      {"/docker/", "OCI / Docker registry mirror"},
}

// Discover returns the catalog. Stable JSON shape consumed by AI agents.
func (h *DiscoverHandler) Discover(c *gin.Context) {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	ecos := make([]ecosystemInfo, 0, len(h.ecosystems))
	for _, name := range h.ecosystems {
		meta, ok := ecosystemPurposes[name]
		if !ok {
			meta.Path = "/" + name + "/"
			meta.Purpose = name
		}
		ecos = append(ecos, ecosystemInfo{
			Name:    name,
			Path:    meta.Path,
			Purpose: meta.Purpose,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"service":     "depsilo",
		"version":     version.Version,
		"commit":      version.Commit,
		"description": "Local dependency cache proxy gateway for 13+ package ecosystems",
		"homepage":    "https://depsilo.com",
		"repository":  "https://github.com/depsilo/depsilo",
		"ecosystems":  ecos,
		"endpoints": gin.H{
			"health":              "/health",
			"metrics":             "/metrics",
			"discover":            "/api/v1/discover",
			"agent_prompt":        "/api/v1/agent-prompt",
			"mcp":                 "/mcp",
			"public_stats":        "/api/v1/stats",
			"public_packages":     "/api/v1/packages",
			"events_stream":       "/api/v1/events/stream",
			"admin_login":         "/api/v1/auth/login",
			"admin_dashboard":     "/api/v1/admin/dashboard",
			"admin_license":       "/api/v1/admin/license/status",
			"admin_trial":         "/api/v1/admin/license/trial/activate",
		},
		"mcp": gin.H{
			"url":       base + "/mcp",
			"protocol":  "Model Context Protocol (JSON-RPC 2.0 over HTTP)",
			"transport": "streamable-http",
			"summary":   "POST initialize, then tools/list to discover what you can call. depsilo_status is a good first call.",
		},
		"agent_setup": gin.H{
			"prompt_url":    base + "/api/v1/agent-prompt",
			"portal_url":    base + "/",
			"summary":       "Paste the contents of /api/v1/agent-prompt into any AI coding agent (Hermes, OpenClaw, Claude Code, Cursor). The agent will detect the project's package managers and configure them to route through this Depsilo instance.",
			"readme_anchor": "https://github.com/depsilo/depsilo#use-with-ai-agents",
		},
	})
}

// AgentPrompt returns the same MEDIUM-granularity prompt that the Portal
// renders, as plain text — so an AI agent can `curl -sf .../agent-prompt`
// and consume it directly without needing to parse JSON or fetch the SPA.
//
// The content here MUST stay in sync with web/src/lib/ecosystemData.ts
// `buildAgentPrompt` and README "Use with AI agents". The
// `endpoint`-bearing URLs are templated with the request host so deployments
// behind LB/ingress get the correct URL.
func (h *DiscoverHandler) AgentPrompt(c *gin.Context) {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	body := fmt.Sprintf(`This workspace has a local dependency cache called Depsilo at %s.
It caches packages from 13+ ecosystems and serves them at LAN speed.

Before running any package install in this workspace:

1. Verify it's reachable: curl -sf %s/health

2. Detect which package managers this project uses (requirements.txt,
   package.json, go.mod, Cargo.toml, pom.xml, Gemfile, composer.json,
   *.csproj, environment.yml, DESCRIPTION, Chart.yaml, or import transformers /
   import huggingface_hub in Python source, etc.).

3. Configure ONLY the detected tools. Pick from:

   pip:          pip config set global.index-url %s/pypi/simple/
   npm:          npm config set registry %s/npm/
   go:           go env -w GOPROXY=%s/go,direct
   cargo:        visit %s/ and copy the Cargo block to ~/.cargo/config.toml
   maven:        visit %s/ and copy the Maven mirror block to ~/.m2/settings.xml
   gem:          bundle config mirror.https://rubygems.org %s/rubygems/
   composer:     composer config -g repo.packagist composer %s/composer/
   nuget:        dotnet nuget add source %s/nuget/v3/index.json -n depsilo
   conda:        add channel %s/conda/ to ~/.condarc
   helm:         helm repo add depsilo %s/helm/
   huggingface:  export HF_ENDPOINT=%s/huggingface
   R/CRAN:       options(repos = c(CRAN = "%s/cran/")) in ~/.Rprofile

4. Run install commands normally — they auto-route through Depsilo.

If Depsilo is down, tools fall back to public registries — installs still
work, just not cached. Don't waste effort on retry logic for Depsilo itself.

Native MCP integration (preferred for MCP-aware agents like Claude Code,
Cursor): point your client at %s/mcp. Available tools include
depsilo_status, depsilo_doctor, depsilo_configure, depsilo_search,
depsilo_recent. POST initialize to begin, then tools/list to enumerate.
`,
		base, base,
		base, base, base, base, base, base, base, base, base, base, base, base,
		base,
	)

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, body)
}
