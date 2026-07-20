package public

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/ecosystem"
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

// ecosystemPurposes adds human-facing copy to the canonical ecosystem
// catalog; names and routes remain owned by internal/ecosystem.
var ecosystemPurposes = map[string]string{
	"pypi":        "Python — pip, uv, Poetry, PDM",
	"apt":         "Debian / Ubuntu APT packages",
	"npm":         "Node.js — npm, yarn, pnpm, bun",
	"go":          "Go modules (GOPROXY protocol)",
	"cargo":       "Rust crates (sparse index)",
	"maven":       "Java — Maven, Gradle, sbt",
	"rubygems":    "Ruby — gem, bundler",
	"composer":    "PHP — Composer",
	"nuget":       ".NET — NuGet v3 protocol",
	"conda":       "Conda channels",
	"cran":        "R — CRAN packages",
	"alpine":      "Alpine Linux apk packages",
	"helm":        "Kubernetes Helm charts",
	"huggingface": "Hugging Face — models + datasets (huggingface-cli, transformers, datasets)",
	"docker":      "OCI / Docker registry mirror (configure the service root)",
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
		path := "/" + name + "/"
		if definition, ok := ecosystem.Lookup(name); ok {
			path = definition.Route + "/"
		}
		purpose, ok := ecosystemPurposes[name]
		if !ok {
			purpose = name
		}
		ecos = append(ecos, ecosystemInfo{
			Name:    name,
			Path:    path,
			Purpose: purpose,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"service":     "depsilo",
		"version":     version.Version,
		"commit":      version.Commit,
		"description": "Supply-chain enforcement proxy for 14 package ecosystems plus Docker OCI",
		"homepage":    "https://depsilo.com",
		"repository":  "https://github.com/depsilo/depsilo",
		"ecosystems":  ecos,
		"endpoints": gin.H{
			"health":             "/health",
			"metrics":            "/metrics",
			"discover":           "/api/v1/discover",
			"agent_prompt":       "/api/v1/agent-prompt",
			"integration_prompt": "/api/v1/integration-prompt",
			"mcp":                "/mcp",
			"public_stats":       "/api/v1/stats",
			"public_packages":    "/api/v1/packages",
			"events_stream":      "/api/v1/events/stream",
			"admin_login":        "/api/v1/auth/login",
			"admin_dashboard":    "/api/v1/admin/dashboard",
			"admin_license":      "/api/v1/admin/license/status",
			"admin_trial":        "/api/v1/admin/license/trial/activate",
		},
		"mcp": gin.H{
			"url":       base + "/mcp",
			"protocol":  "Model Context Protocol (JSON-RPC 2.0 over HTTP)",
			"transport": "streamable-http",
			"summary":   "POST initialize, then tools/list to discover what you can call. depsilo_status is a good first call.",
		},
		"agent_setup": gin.H{
			"prompt_url":                     base + "/api/v1/agent-prompt",
			"local_bootstrap_prompt_url":     base + "/api/v1/agent-prompt",
			"project_integration_prompt_url": base + "/api/v1/integration-prompt",
			"portal_url":                     base + "/",
			"summary":                        "Use the bootstrap prompt for local developer-machine settings or the Portal integration prompt for project build/CI edits.",
			"readme_anchor":                  "https://github.com/depsilo/depsilo#use-with-ai-agents",
		},
	})
}

// AgentPrompt returns the local developer-machine bootstrap prompt as plain
// text. It is distinct from /api/v1/integration-prompt, which is the Portal's
// project build/CI integration prompt.
//
// The content here MUST stay in sync with embeddedAgentPrompt in
// internal/cli/initagent.go and README's "Use with AI coding agents" section.
// URLs are templated with the request host so deployments behind LB/ingress get
// the correct URL.
func (h *DiscoverHandler) AgentPrompt(c *gin.Context) {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

	body := fmt.Sprintf(`This workspace has a local dependency cache called Depsilo at %s.
It proxies 14 package ecosystems plus Docker OCI and serves cached artifacts at LAN speed.

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

These settings do not provide reliable outage failover. Even Go's ",direct"
suffix advances only after a 404/410 response, not when Depsilo is unreachable.
Keep the original registry settings as documented rollback instructions; do
not use GOPROXY "|direct", which would also bypass Depsilo's 451 enforcement.

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
