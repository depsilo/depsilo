package public

import (
	"fmt"
	"net/http"
	"strings"

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
	ecosystems    []string
	setupRequired func() bool
}

// NewDiscoverHandler builds the handler. The ecosystems slice is the
// canonical "what proxies are wired" list from server.go.
func NewDiscoverHandler(ecosystems []string) *DiscoverHandler {
	return &DiscoverHandler{ecosystems: ecosystems}
}

// SetSetupRequired wires the first-run setup state: pending reports whether
// setup still has to be completed. Without it the handler describes a
// configured service.
func (h *DiscoverHandler) SetSetupRequired(pending func() bool) {
	h.setupRequired = pending
}

func (h *DiscoverHandler) setupPending() bool {
	return h.setupRequired != nil && h.setupRequired()
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
	"huggingface": "Hugging Face — models + datasets (hf CLI, transformers, datasets)",
	"docker":      "OCI / Docker registry mirror (configure the service root)",
}

// Discover returns the catalog. Stable JSON shape consumed by AI agents.
func (h *DiscoverHandler) Discover(c *gin.Context) {
	c.JSON(http.StatusOK, h.catalog(requestBase(c)))
}

// catalog builds the same service description for HTTP and in-process MCP
// callers. The supplied base is display data only; this function never
// dereferences it.
func (h *DiscoverHandler) catalog(base string) gin.H {
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

	// While first-run setup is pending nothing is served yet. Say so, and
	// say who finishes it; agent-facing text never explains how.
	setupRequired := h.setupPending()
	agentSummary := "Use the bootstrap prompt for local developer-machine settings or the Portal integration prompt for project build/CI edits."
	if setupRequired {
		agentSummary = "First-run setup is not finished, so this service does not serve packages yet. A human operator must complete setup in the Portal at " + base + "/. Do not configure package managers against this service until setup_required is false."
	}

	return gin.H{
		"service":        "depsilo",
		"version":        version.Version,
		"commit":         version.Commit,
		"description":    "Supply-chain enforcement proxy for 14 package ecosystems plus Docker OCI",
		"homepage":       "https://depsilo.com",
		"repository":     "https://github.com/depsilo/depsilo",
		"setup_required": setupRequired,
		"ecosystems":     ecos,
		"endpoints": gin.H{
			"health":             "/health",
			"metrics":            "/metrics",
			"discover":           "/api/v1/discover",
			"setup_status":       "/api/v1/setup/status",
			"agent_prompt":       "/api/v1/agent-prompt",
			"integration_prompt": "/api/v1/integration-prompt",
			"mcp":                "/mcp",
			"public_stats":       "/api/v1/stats",
			// Stable legacy key: the endpoint now requires read authentication,
			// but discover consumers must not lose the field during that change.
			"public_packages": "/api/v1/packages",
			"events_stream":   "/api/v1/events/stream",
			"admin_login":     "/api/v1/auth/login",
			"admin_dashboard": "/api/v1/admin/dashboard",
			"admin_license":   "/api/v1/admin/license/status",
			"admin_trial":     "/api/v1/admin/license/trial/activate",
		},
		"endpoint_authentication": gin.H{
			"mcp":             "Authorization: Bearer <JWT or readonly API token>",
			"public_packages": "Authorization: Bearer <JWT or readonly API token>",
			"events_stream":   "Authorization: Bearer <JWT or readonly API token>",
		},
		"mcp": gin.H{
			"url":            base + "/mcp",
			"protocol":       "Model Context Protocol (JSON-RPC 2.0 over HTTP)",
			"transport":      "streamable-http",
			"authentication": "Authorization: Bearer <JWT or readonly API token>",
			"summary":        "Authenticate, POST initialize, then tools/list to discover what you can call. depsilo_status is a good first call.",
		},
		"agent_setup": gin.H{
			"prompt_url":                     base + "/api/v1/agent-prompt",
			"local_bootstrap_prompt_url":     base + "/api/v1/agent-prompt",
			"project_integration_prompt_url": base + "/api/v1/integration-prompt",
			"portal_url":                     base + "/",
			"summary":                        agentSummary,
			// Stable key from when this guidance was a README section. It now
			// lives in the deployment guide.
			"readme_anchor": "https://github.com/depsilo/depsilo/blob/master/docs/deployment.md#ai-coding-agents",
		},
	}
}

// AgentPrompt returns the local developer-machine bootstrap prompt as plain
// text. It is distinct from /api/v1/integration-prompt, which is the Portal's
// project build/CI integration prompt.
//
// embeddedAgentPrompt in internal/cli/initagent.go is the offline fallback
// "depsilo init-agent" uses when it cannot reach this endpoint: keep its
// configuration steps in step with the text here. The operator-facing
// description is the "AI coding agents" section of docs/deployment.md.
// URLs are templated with the request host so deployments behind LB/ingress get
// the correct URL.
func (h *DiscoverHandler) AgentPrompt(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, h.agentPrompt(requestBase(c)))
}

// agentPrompt builds bootstrap guidance without making a request back into the
// service. The base remains visible to the client in every generated command.
func (h *DiscoverHandler) agentPrompt(base string) string {
	if h.setupPending() {
		return setupPendingPrompt(base)
	}
	return fmt.Sprintf(`This workspace has a local dependency cache called Depsilo at %s.
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
Cursor): create a read-only API token in Admin > Users, point your client at
%s/mcp, and send it as an Authorization: Bearer header. POST initialize to
begin, then tools/list to enumerate. Available tools:
%s.
`,
		base, base,
		base, base, base, base, base, base, base, base, base, base, base, base,
		base,
		wrapCommaList(mcpToolNames(), 78),
	)
}

// setupPendingPrompt replaces the bootstrap prompt until first-run setup is
// done. Package routes are not mounted before that, so configuration commands
// would only break installs. It names who completes setup and where, and
// deliberately nothing about how: that is a human operator's step.
func setupPendingPrompt(base string) string {
	return fmt.Sprintf(`Depsilo at %[1]s has not finished first-run setup, so it does not
serve packages yet.

Do not point any package manager at this service, and do not attempt the setup
yourself: a human operator must complete it in the Portal at %[1]s/.

Tell the user that setup is pending, then stop. After the operator has
finished, fetch %[1]s/api/v1/agent-prompt again for the configuration
instructions. "setup_required" in %[1]s/api/v1/discover is false once setup is
done.
`, base)
}

// wrapCommaList joins items with ", " and breaks the result into lines of at
// most width columns, matching the hard-wrapped prompt text around it.
func wrapCommaList(items []string, width int) string {
	var out strings.Builder
	column := 0
	for index, item := range items {
		if index < len(items)-1 {
			item += ","
		}
		switch {
		case column == 0:
		case column+1+len(item) > width:
			out.WriteByte('\n')
			column = 0
		default:
			out.WriteByte(' ')
			column++
		}
		out.WriteString(item)
		column += len(item)
	}
	return out.String()
}

func requestBase(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}
