package public

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/version"
)

// MCPHandler exposes Depsilo over the Model Context Protocol (Streamable
// HTTP transport). A single POST /mcp endpoint accepts JSON-RPC 2.0
// requests and dispatches to:
//
//	initialize, notifications/initialized
//	tools/list, tools/call
//	resources/list, resources/read
//	prompts/list, prompts/get
//
// Any MCP-aware client (Claude Code, Cursor, Hermes, OpenClaw,
// modelcontextprotocol.io clients) can connect by pointing at the URL.
// Unlike /api/v1/agent-prompt — which is text the agent must parse and
// translate into actions — MCP tools give the agent structured function
// calls it can invoke directly.
//
// All current tools are read-only. depsilo_warmup returns a request template
// for the authenticated Admin API; it does not execute the mutation itself.
type MCPHandler struct {
	DB         *gorm.DB
	Ecosystems []string
	UseRollup  bool
}

func NewMCPHandler(db *gorm.DB, ecosystems []string, useRollup bool) *MCPHandler {
	return &MCPHandler{DB: db, Ecosystems: ecosystems, UseRollup: useRollup}
}

// ── JSON-RPC framing ──────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

// Handle is the single POST /mcp endpoint.
func (h *MCPHandler) Handle(c *gin.Context) {
	var raw json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, nil, errParse, "Parse error", nil)
		return
	}

	// Batch handling — MCP spec allows an array of requests.
	if len(raw) > 0 && raw[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(raw, &batch); err != nil {
			respondError(c, nil, errInvalidRequest, "Invalid batch", nil)
			return
		}
		responses := make([]rpcResponse, 0, len(batch))
		for _, req := range batch {
			if resp, ok := h.dispatch(c, req); ok {
				responses = append(responses, resp)
			}
		}
		c.JSON(http.StatusOK, responses)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		respondError(c, nil, errInvalidRequest, "Invalid request", nil)
		return
	}
	if resp, ok := h.dispatch(c, req); ok {
		c.JSON(http.StatusOK, resp)
	} else {
		// Notification: per JSON-RPC spec, no response body.
		c.Status(http.StatusOK)
	}
}

// dispatch routes a single request. Returns (response, false) for
// notifications (which have no `id` field and expect no reply).
func (h *MCPHandler) dispatch(c *gin.Context, req rpcRequest) (rpcResponse, bool) {
	isNotification := len(req.ID) == 0
	mk := func(result any) rpcResponse {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
	}
	mkErr := func(code int, msg string, data any) rpcResponse {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg, Data: data}}
	}

	switch req.Method {
	case "initialize":
		return mk(map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "depsilo",
				"version": version.Version,
			},
			"instructions": "Depsilo is a supply-chain enforcement proxy for 14 package ecosystems plus Docker OCI. Call tools/list to see what you can do; depsilo_status is a good first call to verify connectivity. For human-readable bootstrap text, also fetch GET /api/v1/agent-prompt.",
		}), true

	case "notifications/initialized", "notifications/cancelled":
		return rpcResponse{}, false // notifications have no reply

	case "tools/list":
		return mk(map[string]any{"tools": h.toolDefinitions()}), true

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mkErr(errInvalidParams, "params must be {name, arguments}", nil), true
		}
		result, callErr := h.callTool(c, params.Name, params.Arguments)
		if callErr != nil {
			return mk(map[string]any{
				"content": []map[string]any{{"type": "text", "text": callErr.Error()}},
				"isError": true,
			}), true
		}
		return mk(result), true

	case "resources/list":
		return mk(map[string]any{"resources": h.resourceDefinitions()}), true

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mkErr(errInvalidParams, "params must be {uri}", nil), true
		}
		content, err := h.readResource(c, params.URI)
		if err != nil {
			return mkErr(errInternal, err.Error(), nil), true
		}
		return mk(map[string]any{"contents": []map[string]any{content}}), true

	case "prompts/list":
		return mk(map[string]any{"prompts": h.promptDefinitions()}), true

	case "prompts/get":
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mkErr(errInvalidParams, "params must be {name}", nil), true
		}
		prompt, err := h.getPrompt(c, params.Name)
		if err != nil {
			return mkErr(errInvalidParams, err.Error(), nil), true
		}
		return mk(prompt), true

	case "ping":
		return mk(map[string]any{}), true

	default:
		if isNotification {
			return rpcResponse{}, false
		}
		return mkErr(errMethodNotFound, "Method not found: "+req.Method, nil), true
	}
}

func respondError(c *gin.Context, id json.RawMessage, code int, msg string, data any) {
	c.JSON(http.StatusOK, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	})
}

// ── Tools ─────────────────────────────────────────────────────────────

func (h *MCPHandler) toolDefinitions() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intP := func(desc string, def int) map[string]any {
		return map[string]any{"type": "integer", "description": desc, "default": def}
	}
	arr := func(itemType, desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": itemType}, "description": desc}
	}
	obj := func(properties map[string]any, required ...string) map[string]any {
		o := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			o["required"] = required
		}
		return o
	}

	return []map[string]any{
		{
			"name":        "depsilo_status",
			"description": "Overall service status: health, uptime, today's request totals, cache hit rate, configured ecosystems.",
			"inputSchema": obj(nil),
		},
		{
			"name":        "depsilo_doctor",
			"description": "Run an end-to-end diagnosis. Returns a list of checks (ok / warn / fail) with actionable hints. Use this when something feels wrong before deeper investigation.",
			"inputSchema": obj(nil),
		},
		{
			"name":        "depsilo_configure",
			"description": "Return shell / config snippets for a package manager. Apply project-scoped changes only after review; host-level Docker daemon settings must be shown to the operator, not edited automatically.",
			"inputSchema": obj(map[string]any{
				"ecosystem": str("Ecosystem name (pypi, npm, go, cargo, maven, rubygems, composer, nuget, conda, cran, helm, huggingface, apt, alpine, docker)"),
			}, "ecosystem"),
		},
		{
			"name":        "depsilo_search",
			"description": "Search the local cache for packages whose name matches a substring. Returns up to 25 entries with adapter type, size, hit count, last-accessed timestamp.",
			"inputSchema": obj(map[string]any{
				"query":     str("Substring to match against package_name (case-insensitive)"),
				"ecosystem": str("Optional ecosystem filter (pypi, npm, etc.)"),
				"limit":     intP("Max results to return (1-100)", 25),
			}, "query"),
		},
		{
			"name":        "depsilo_recent",
			"description": "List the most recent cache access events (hits + misses). Useful for showing the user what has been installed lately or for diagnosing why a cache entry is unexpectedly missing.",
			"inputSchema": obj(map[string]any{
				"limit":     intP("Max events to return (1-200)", 20),
				"ecosystem": str("Optional ecosystem filter"),
				"only_miss": map[string]any{"type": "boolean", "description": "If true, only return cache misses", "default": false},
			}),
		},
		{
			"name":        "depsilo_warmup",
			"description": "Return the authenticated Admin API request needed to pre-fetch packages. The MCP tool does not execute or queue the warmup yet.",
			"inputSchema": obj(map[string]any{
				"ecosystem": str("Target ecosystem (pypi, npm, cargo, etc.)"),
				"packages":  arr("string", "Package names to fetch"),
			}, "ecosystem", "packages"),
		},
	}
}

func (h *MCPHandler) callTool(c *gin.Context, name string, args json.RawMessage) (any, error) {
	switch name {
	case "depsilo_status":
		return h.toolStatus(c)
	case "depsilo_doctor":
		return h.toolDoctor(c)
	case "depsilo_configure":
		var a struct {
			Ecosystem string `json:"ecosystem"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolConfigure(c, a.Ecosystem)
	case "depsilo_search":
		var a struct {
			Query     string `json:"query"`
			Ecosystem string `json:"ecosystem"`
			Limit     int    `json:"limit"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolSearch(a.Query, a.Ecosystem, a.Limit)
	case "depsilo_recent":
		var a struct {
			Limit     int    `json:"limit"`
			Ecosystem string `json:"ecosystem"`
			OnlyMiss  bool   `json:"only_miss"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolRecent(a.Limit, a.Ecosystem, a.OnlyMiss)
	case "depsilo_warmup":
		var a struct {
			Ecosystem string   `json:"ecosystem"`
			Packages  []string `json:"packages"`
		}
		_ = json.Unmarshal(args, &a)
		return h.toolWarmup(c, a.Ecosystem, a.Packages)
	}
	return nil, fmt.Errorf("unknown tool: %s", name)
}

// ── Tool implementations ──────────────────────────────────────────────

func textResult(text string) any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
	}
}

func jsonResult(v any) any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textResult(fmt.Sprintf("error encoding result: %v", err))
	}
	return textResult(string(b))
}

func (h *MCPHandler) toolStatus(c *gin.Context) (any, error) {
	// Use the live request to figure out our base URL so the values we
	// return are usable from the caller's perspective.
	base := requestBase(c)

	var hits, misses, totalFiles int64
	var totalSize int64
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -1)
	if h.UseRollup {
		// "Last 24h" rolls neatly onto access_log_hourly because the
		// window is hour-aligned by design (the rollup tables themselves
		// granulate to hours). Compare on date string to match the
		// rollup PK; include partial-hour rows by including today.
		startDate := start.Format("2006-01-02")
		var t struct{ Hits, Misses int64 }
		h.DB.Table("access_log_hourly").
			Select(`COALESCE(SUM(CASE WHEN hit = 1 THEN request_count ELSE 0 END), 0) AS hits,
				COALESCE(SUM(CASE WHEN hit = 0 THEN request_count ELSE 0 END), 0) AS misses`).
			Where("date >= ?", startDate).
			Scan(&t)
		hits = t.Hits
		misses = t.Misses
	} else {
		h.DB.Model(&db.AccessLog{}).Where("hit = ? AND created_at >= ?", true, start).Count(&hits)
		h.DB.Model(&db.AccessLog{}).Where("hit = ? AND created_at >= ?", false, start).Count(&misses)
	}
	h.DB.Model(&db.CacheEntry{}).Count(&totalFiles)
	h.DB.Model(&db.CacheEntry{}).Select("COALESCE(SUM(size), 0)").Row().Scan(&totalSize)

	requests := hits + misses
	hitRate := 0.0
	if requests > 0 {
		hitRate = float64(hits) / float64(requests)
	}

	return jsonResult(map[string]any{
		"service": map[string]any{
			"version": version.Version,
			"url":     base,
			"status":  "healthy",
		},
		"last_24h": map[string]any{
			"requests": requests,
			"hits":     hits,
			"misses":   misses,
			"hit_rate": hitRate,
		},
		"cache": map[string]any{
			"entries":    totalFiles,
			"size_bytes": totalSize,
		},
		"ecosystems": h.Ecosystems,
	}), nil
}

type doctorCheck struct {
	Name    string `json:"name"`
	Level   string `json:"level"` // ok | warn | fail | info
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (h *MCPHandler) toolDoctor(c *gin.Context) (any, error) {
	checks := []doctorCheck{
		{Name: "Service", Level: "ok", Message: "reachable at " + requestBase(c)},
	}

	// Check ecosystems configured
	if len(h.Ecosystems) == 0 {
		checks = append(checks, doctorCheck{
			Name: "Ecosystems", Level: "fail",
			Message: "no ecosystems wired",
			Hint:    "review your config.toml [[ecosystem.upstreams]] sections",
		})
	} else {
		checks = append(checks, doctorCheck{
			Name: "Ecosystems", Level: "ok",
			Message: fmt.Sprintf("%d wired (%s)", len(h.Ecosystems), strings.Join(h.Ecosystems, ", ")),
		})
	}

	// Recent traffic + hit rate
	since := time.Now().Add(-time.Hour)
	var hits, misses int64
	h.DB.Model(&db.AccessLog{}).Where("hit = ? AND created_at >= ?", true, since).Count(&hits)
	h.DB.Model(&db.AccessLog{}).Where("hit = ? AND created_at >= ?", false, since).Count(&misses)
	total := hits + misses
	switch {
	case total == 0:
		checks = append(checks, doctorCheck{
			Name: "Recent traffic", Level: "info",
			Message: "no requests in the last hour",
			Hint:    "configure a client (depsilo activate) and try an install",
		})
	case total > 0 && hits == 0:
		checks = append(checks, doctorCheck{
			Name: "Recent traffic", Level: "warn",
			Message: fmt.Sprintf("%d requests, 0%% hit rate", total),
			Hint:    "cache is cold; expect this on a fresh install",
		})
	default:
		hitRate := float64(hits) / float64(total) * 100
		level := "ok"
		if hitRate < 30 {
			level = "warn"
		}
		checks = append(checks, doctorCheck{
			Name: "Recent traffic", Level: level,
			Message: fmt.Sprintf("%d requests, %.1f%% hit rate", total, hitRate),
		})
	}

	// Tally summary
	sum := map[string]int{"ok": 0, "warn": 0, "fail": 0, "info": 0}
	for _, ch := range checks {
		sum[ch.Level]++
	}

	return jsonResult(map[string]any{
		"ok":      sum["fail"] == 0,
		"checks":  checks,
		"summary": sum,
	}), nil
}

func (h *MCPHandler) toolConfigure(c *gin.Context, ecosystem string) (any, error) {
	base := requestBase(c)
	snippets := map[string]map[string]string{
		"pypi": {
			"shell":  fmt.Sprintf("pip config set global.index-url %s/pypi/simple/", base),
			"env":    fmt.Sprintf("PIP_INDEX_URL=%s/pypi/simple/", base),
			"config": fmt.Sprintf("# ~/.config/pip/pip.conf\n[global]\nindex-url = %s/pypi/simple/", base),
			"verify": fmt.Sprintf("pip install -i %s/pypi/simple/ --dry-run six", base),
		},
		"npm": {
			"shell":  fmt.Sprintf("npm config set registry %s/npm/", base),
			"env":    fmt.Sprintf("npm_config_registry=%s/npm/", base),
			"config": fmt.Sprintf("# ~/.npmrc\nregistry=%s/npm/", base),
			"verify": "npm view express version",
		},
		"go": {
			"shell":  fmt.Sprintf("go env -w GOPROXY=%s/go,direct", base),
			"env":    fmt.Sprintf("GOPROXY=%s/go,direct", base),
			"verify": "go mod download -x golang.org/x/sync@latest",
		},
		"cargo": {
			"config": fmt.Sprintf("# ~/.cargo/config.toml\n[source.crates-io]\nreplace-with = \"depsilo\"\n[source.depsilo]\nregistry = \"sparse+%s/crates/\"", base),
			"verify": "cargo search serde",
		},
		"maven": {
			"config": fmt.Sprintf("# ~/.m2/settings.xml mirror\n<mirror>\n  <id>depsilo</id>\n  <mirrorOf>*</mirrorOf>\n  <url>%s/maven/</url>\n</mirror>", base),
		},
		"rubygems": {
			"shell":  fmt.Sprintf("bundle config mirror.https://rubygems.org %s/rubygems/", base),
			"verify": "gem fetch rake --source " + base + "/rubygems/",
		},
		"composer": {
			"shell": fmt.Sprintf("composer config -g repo.packagist composer %s/composer/", base),
		},
		"nuget": {
			"shell": fmt.Sprintf("dotnet nuget add source %s/nuget/v3/index.json -n depsilo", base),
		},
		"conda": {
			"config": fmt.Sprintf("# ~/.condarc\nchannels:\n  - %s/conda/", base),
		},
		"helm": {
			"shell": fmt.Sprintf("helm repo add depsilo %s/helm/", base),
		},
		"huggingface": {
			"env":    fmt.Sprintf("HF_ENDPOINT=%s/huggingface", base),
			"verify": fmt.Sprintf("HF_ENDPOINT=%s/huggingface hf download bert-base-uncased --cache-dir /tmp/hf-test", base),
		},
		"cran": {
			"config": fmt.Sprintf(`# ~/.Rprofile\noptions(repos = c(CRAN = "%s/cran/"))`, base),
		},
		"apt": {
			"note": fmt.Sprintf("APT requires editing /etc/apt/sources.list; replace the host with %s/apt", base),
		},
		"alpine": {
			"shell":  fmt.Sprintf(`(release="v$(cut -d. -f1,2 /etc/alpine-release)"; repos="$(mktemp)"; trap 'rm -f "$repos"' 0; printf '%%s\n' "%s/alpine/${release}/main" "%s/alpine/${release}/community" > "$repos"; apk --repositories-file "$repos" add curl)`, base, base),
			"config": fmt.Sprintf("# Replace /etc/apk/repositories; substitute the v<major.minor> from /etc/alpine-release.\n%s/alpine/v<major.minor>/main\n%s/alpine/v<major.minor>/community", base, base),
			"verify": fmt.Sprintf(`(release="v$(cut -d. -f1,2 /etc/alpine-release)"; repos="$(mktemp)"; trap 'rm -f "$repos"' 0; printf '%%s\n' "%s/alpine/${release}/main" "%s/alpine/${release}/community" > "$repos"; apk --repositories-file "$repos" update)`, base, base),
		},
		"docker": {
			"config": fmt.Sprintf("# Host-level /etc/docker/daemon.json - show to the operator; do not edit automatically.\n# Use the service root; Docker requests /v2/ itself.\n{\"registry-mirrors\": [\"%s\"]}\n# For plain HTTP, also add the host to insecure-registries. Then restart Docker.", base),
		},
	}
	s, ok := snippets[ecosystem]
	if !ok {
		return jsonResult(map[string]any{
			"error":     "unknown ecosystem",
			"requested": ecosystem,
			"available": h.Ecosystems,
		}), nil
	}
	return jsonResult(map[string]any{
		"ecosystem": ecosystem,
		"endpoint":  base,
		"setup":     s,
		"reference": base + "/",
	}), nil
}

func (h *MCPHandler) toolSearch(query, ecosystem string, limit int) (any, error) {
	if query == "" {
		return jsonResult(map[string]any{"error": "query is required"}), nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	q := h.DB.Model(&db.CacheEntry{}).Where("LOWER(package_name) LIKE ?", "%"+strings.ToLower(query)+"%")
	if ecosystem != "" {
		q = q.Where("adapter_type = ?", ecosystem)
	}
	var entries []db.CacheEntry
	if err := q.Order("hit_count desc").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		results = append(results, map[string]any{
			"package_name":  e.PackageName,
			"adapter":       e.AdapterType,
			"size":          e.Size,
			"hit_count":     e.HitCount,
			"last_accessed": e.LastAccessed,
		})
	}
	return jsonResult(map[string]any{
		"query":   query,
		"count":   len(results),
		"results": results,
	}), nil
}

func (h *MCPHandler) toolRecent(limit int, ecosystem string, onlyMiss bool) (any, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	q := h.DB.Model(&db.AccessLog{})
	if ecosystem != "" {
		q = q.Where("adapter_type = ?", ecosystem)
	}
	if onlyMiss {
		q = q.Where("hit = ?", false)
	}
	var logs []db.AccessLog
	if err := q.Order("created_at desc").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		results = append(results, map[string]any{
			"ts":           l.CreatedAt,
			"adapter":      l.AdapterType,
			"package_name": l.PackageName,
			"hit":          l.Hit,
			"upstream":     l.Upstream,
			"latency_ms":   l.LatencyMs,
			"status":       l.StatusCode,
		})
	}
	return jsonResult(map[string]any{
		"count":  len(results),
		"events": results,
	}), nil
}

func (h *MCPHandler) toolWarmup(c *gin.Context, ecosystem string, packages []string) (any, error) {
	if ecosystem == "" || len(packages) == 0 {
		return jsonResult(map[string]any{"error": "ecosystem and packages are required"}), nil
	}
	return jsonResult(map[string]any{
		"executed": false,
		"request": map[string]any{
			"method": "POST",
			"url":    requestBase(c) + "/api/v1/admin/cache/warmup",
			"headers": map[string]string{
				"Authorization": "Bearer <admin-token>",
				"Content-Type":  "application/json",
			},
			"body": map[string]any{
				"ecosystem": ecosystem,
				"packages":  packages,
			},
		},
		"hint": "MCP warmup execution is not wired yet; send this request to the Admin API",
	}), nil
}

// ── Resources ─────────────────────────────────────────────────────────

func (h *MCPHandler) resourceDefinitions() []map[string]any {
	return []map[string]any{
		{
			"uri":         "depsilo://discover",
			"name":        "Service catalog",
			"description": "JSON catalog of the running Depsilo instance: ecosystems, endpoint URLs, version",
			"mimeType":    "application/json",
		},
		{
			"uri":         "depsilo://stats",
			"name":        "Cache stats",
			"description": "Live snapshot of today's request counts, hit rate, cache size, and upstream health",
			"mimeType":    "application/json",
		},
	}
}

func (h *MCPHandler) readResource(c *gin.Context, uri string) (map[string]any, error) {
	base := requestBase(c)
	switch uri {
	case "depsilo://discover":
		body, err := fetchLocal(base + "/api/v1/discover")
		if err != nil {
			return nil, err
		}
		return map[string]any{"uri": uri, "mimeType": "application/json", "text": body}, nil
	case "depsilo://stats":
		body, err := fetchLocal(base + "/api/v1/stats")
		if err != nil {
			return nil, err
		}
		return map[string]any{"uri": uri, "mimeType": "application/json", "text": body}, nil
	}
	return nil, fmt.Errorf("unknown resource URI: %s", uri)
}

// ── Prompts ───────────────────────────────────────────────────────────

func (h *MCPHandler) promptDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "setup",
			"description": "Copy-pasteable setup instructions for an AI coding agent to route the current project through this Depsilo instance.",
			"arguments":   []map[string]any{},
		},
	}
}

func (h *MCPHandler) getPrompt(c *gin.Context, name string) (any, error) {
	if name != "setup" {
		return nil, fmt.Errorf("unknown prompt: %s", name)
	}
	text, err := fetchLocal(requestBase(c) + "/api/v1/agent-prompt")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"description": "Depsilo setup instructions",
		"messages": []map[string]any{{
			"role": "user",
			"content": map[string]any{
				"type": "text",
				"text": text,
			},
		}},
	}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────

func requestBase(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

var localClient = &http.Client{Timeout: 5 * time.Second}

func fetchLocal(url string) (string, error) {
	resp, err := localClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
