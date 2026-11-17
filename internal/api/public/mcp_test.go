package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
	"depsilo/internal/prompts"
)

// serveMCP sends one request to a router that mounts the handler the way
// internal/api/router.go does, without the authentication middleware.
func serveMCP(t *testing.T, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serveMCPAt(t, method, "/mcp", body)
}

// serveMCPAt is serveMCP for a target such as "https://depsilo.example/mcp".
func serveMCPAt(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewMCPHandler(nil, []string{"pypi"}, false, nil)
	router := gin.New()
	router.POST("/mcp", handler.Handle)
	router.GET("/mcp", handler.MethodNotAllowed)
	router.DELETE("/mcp", handler.MethodNotAllowed)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func listMCPTools(t *testing.T) []map[string]any {
	t.Helper()
	recorder := serveMCP(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode tools/list: %v; body = %s", err, recorder.Body.String())
	}
	if len(envelope.Result.Tools) == 0 {
		t.Fatalf("tools/list returned no tools: %s", recorder.Body.String())
	}
	return envelope.Result.Tools
}

// configureViaMCP calls depsilo_configure through tools/call and returns the
// decoded tool result.
func configureViaMCP(t *testing.T, ecosystem string) map[string]any {
	t.Helper()
	return configureViaMCPAt(t, "/mcp", ecosystem)
}

func configureViaMCPAt(t *testing.T, target, ecosystem string) map[string]any {
	t.Helper()
	recorder := serveMCPAt(t, http.MethodPost, target, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"depsilo_configure","arguments":{"ecosystem":"`+ecosystem+`"}}}`)
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || len(envelope.Result.Content) != 1 || envelope.Result.IsError {
		t.Fatalf("depsilo_configure %s: status = %d, err = %v, body = %s", ecosystem, recorder.Code, err, recorder.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &result); err != nil {
		t.Fatalf("depsilo_configure %s: result is not JSON: %v; text = %s", ecosystem, err, envelope.Result.Content[0].Text)
	}
	return result
}

// "bundle config NAME VALUE" without a scope flag writes ~/.bundle/config
// with Bundler 1.x and the project's .bundle/config with Bundler 2.1 and
// later inside a project, so the snippet names its scope.
func TestMCPConfigureBundlerSnippetNamesItsScope(t *testing.T) {
	setup := configureViaMCP(t, "rubygems")["setup"].(map[string]any)
	if got, want := setup["shell"], "bundle config set --local mirror.https://rubygems.org http://example.com/rubygems/"; got != want {
		t.Errorf("setup.shell = %q, want %q", got, want)
	}
	note, _ := setup["note"].(string)
	for _, expected := range []string{"--local writes the project's .bundle/config", "--global writes ~/.bundle/config", `Check "bundle --version" first: before Bundler 2.1, leave out "set".`} {
		if !strings.Contains(note, expected) {
			t.Errorf("setup.note omits %q: %q", expected, note)
		}
	}
	if verify, _ := setup["verify"].(string); !strings.Contains(verify, "http://example.com/rubygems/") {
		t.Errorf("setup.verify = %q", verify)
	}
}

// Composer and .NET SDK 9 refuse a plain-HTTP source unless it opts in, so
// the tool gives the opt-ins, the bootstrap prompt's, for an http address only.
func TestMCPConfigureNuGetAndComposerOptInToPlainHTTP(t *testing.T) {
	for _, tc := range []struct {
		target, composer, nuget, nugetNote string
	}{
		{
			"/mcp",
			"composer config -g secure-http false\ncomposer config -g repo.packagist composer http://example.com/composer/",
			"dotnet nuget add source http://example.com/nuget/v3/index.json -n depsilo --allow-insecure-connections",
			".NET SDK 8 and earlier reject --allow-insecure-connections: leave it out there.",
		},
		{
			"https://example.com/mcp",
			"composer config -g repo.packagist composer https://example.com/composer/",
			"dotnet nuget add source https://example.com/nuget/v3/index.json -n depsilo",
			"",
		},
	} {
		composer := configureViaMCPAt(t, tc.target, "composer")["setup"].(map[string]any)
		if got := composer["shell"]; got != tc.composer {
			t.Errorf("%s composer setup.shell = %q, want %q", tc.target, got, tc.composer)
		}
		nuget := configureViaMCPAt(t, tc.target, "nuget")["setup"].(map[string]any)
		if got := nuget["shell"]; got != tc.nuget {
			t.Errorf("%s nuget setup.shell = %q, want %q", tc.target, got, tc.nuget)
		}
		if got, _ := nuget["note"].(string); got != tc.nugetNote {
			t.Errorf("%s nuget setup.note = %q, want %q", tc.target, got, tc.nugetNote)
		}
	}
}

// Depsilo's npm route serves only the signed tarball URLs of its rewritten
// metadata, so an agent that only sets the registry leaves a project with a
// committed lockfile failing with 404. The tool says what the bootstrap
// prompt says, in the same words.
func TestMCPConfigureNpmCarriesTheLockfileNote(t *testing.T) {
	setup := configureViaMCP(t, "npm")["setup"].(map[string]any)
	note, _ := setup["note"].(string)
	for _, expected := range []string{
		"omit-lockfile-registry-resolved=true in its .npmrc",
		`"npm install --package-lock-only --omit-lockfile-registry-resolved"`,
		"404",
		`"npm ci" alone does not help`,
		"ask before rewriting it",
	} {
		if !strings.Contains(note, expected) {
			t.Fatalf("setup.note omits %q: %q", expected, note)
		}
	}
	prompt := strings.Join(strings.Fields(prompts.Bootstrap("http://example.com", []string{"npm"}, nil)), " ")
	if !strings.Contains(prompt, "npm lockfiles: "+strings.ToLower(note[:1])+note[1:]) {
		t.Errorf("setup.note and the bootstrap prompt's lockfile note differ:\nnote: %s\nprompt: %s", note, prompt)
	}

	// The fields the tool already returned are unchanged.
	for field, want := range map[string]string{
		"shell":  "npm config set registry http://example.com/npm/",
		"env":    "npm_config_registry=http://example.com/npm/",
		"config": "# ~/.npmrc\nregistry=http://example.com/npm/",
		"verify": "npm view express version",
	} {
		if got := setup[field]; got != want {
			t.Errorf("setup.%s = %q, want %q", field, got, want)
		}
	}
}

// The MCP schema types inputSchema as {"type":"object","properties"?:{...}}.
// The official TypeScript SDK validates the whole listing against it, so one
// null hides every tool from the client.
func TestMCPToolsListInputSchemasAreObjectsWithoutNulls(t *testing.T) {
	for _, tool := range listMCPTools(t) {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool without a name: %#v", tool)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("%s: inputSchema = %#v, want an object", name, tool["inputSchema"])
		}
		if schema["type"] != "object" {
			t.Errorf("%s: inputSchema.type = %#v, want \"object\"", name, schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Errorf("%s: inputSchema.properties = %#v, want an object", name, schema["properties"])
		}
		if path := firstJSONNull(schema, "inputSchema"); path != "" {
			t.Errorf("%s: null at %s", name, path)
		}
	}
}

// firstJSONNull returns the path of the first null inside a decoded JSON
// value, or "" when there is none.
func firstJSONNull(value any, path string) string {
	switch typed := value.(type) {
	case nil:
		return path
	case map[string]any:
		for key, child := range typed {
			if found := firstJSONNull(child, path+"."+key); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := firstJSONNull(child, path+"[]"); found != "" {
				return found
			}
		}
	}
	return ""
}

// A request without an id is a notification. JSON-RPC forbids a reply and the
// Streamable HTTP transport acknowledges it with 202 and no body.
func TestMCPNotificationsAreAcceptedWithoutBody(t *testing.T) {
	for name, body := range map[string]string{
		"initialized":        `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		"cancelled":          `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		"unknown":            `{"jsonrpc":"2.0","method":"notifications/roots/list_changed"}`,
		"request without id": `{"jsonrpc":"2.0","method":"tools/list"}`,
		"notification batch": `[{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","method":"notifications/cancelled"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := serveMCP(t, http.MethodPost, body)
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusAccepted, recorder.Body.String())
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("notification got a body: %q", recorder.Body.String())
			}
		})
	}
}

func TestMCPRequestsStillAnswerWith200(t *testing.T) {
	recorder := serveMCP(t, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":1`) {
		t.Fatalf("ping: status = %d, body = %q", recorder.Code, recorder.Body.String())
	}

	// An id makes it a request whatever the method is called, and a request
	// is owed a reply: a notification method is not one this server answers.
	recorder = serveMCP(t, http.MethodPost, `{"jsonrpc":"2.0","id":7,"method":"notifications/initialized"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":7`) || !strings.Contains(recorder.Body.String(), `"code":-32601`) {
		t.Fatalf("notification method with an id: status = %d, body = %q", recorder.Code, recorder.Body.String())
	}

	// A batch answers its requests and stays silent about its notifications.
	recorder = serveMCP(t, http.MethodPost, `[{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`)
	var responses []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &responses); err != nil {
		t.Fatalf("decode batch: %v; body = %q", err, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK || len(responses) != 1 || responses[0]["id"] != float64(2) {
		t.Fatalf("mixed batch: status = %d, responses = %#v", recorder.Code, responses)
	}
}

func TestMCPRejectsGetAndDeleteWith405(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := serveMCP(t, method, "")
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusMethodNotAllowed, recorder.Body.String())
			}
			if allow := recorder.Header().Get("Allow"); allow != http.MethodPost {
				t.Fatalf("Allow = %q, want %q", allow, http.MethodPost)
			}
			var body struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != "METHOD_NOT_ALLOWED" || body.Message == "" {
				t.Fatalf("error body = %q (err %v), want {code, message}", recorder.Body.String(), err)
			}
		})
	}
}

// The bootstrap prompt names the MCP tools in prose. It is generated from the
// definitions, so a tool added to tools/list cannot be missing from it.
func TestAgentPromptNamesEveryMCPTool(t *testing.T) {
	prompt := NewDiscoverHandler(nil).agentPrompt("https://depsilo.example")
	for _, tool := range listMCPTools(t) {
		name := tool["name"].(string)
		if !strings.Contains(prompt, name) {
			t.Errorf("agent prompt does not name %s", name)
		}
	}
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, "depsilo_") && len(line) > 80 {
			t.Errorf("tool list is not wrapped: %q", line)
		}
	}
}

// depsilo_status labels its figures last_24h. On the hourly rollup that is
// the hour bucket holding now-24h and everything after it, not every bucket
// of yesterday's date.
func TestMCPStatusLast24hDoesNotReachBackToYesterdayMidnight(t *testing.T) {
	database := newStatsTestDB(t)
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLogHourly{}); err != nil {
		t.Fatal(err)
	}
	// 2026-11-16 23:30 UTC, read from a UTC+8 clock: the window opens at
	// 2026-11-15 23:30 UTC, inside the 23:00 bucket.
	now := time.Date(2026, time.November, 17, 7, 30, 0, 0, time.FixedZone("CST", 8*3600))
	rows := []db.AccessLogHourly{
		{Date: "2026-11-15", Hour: 0, AdapterType: "pypi", Hit: true, RequestCount: 1000},
		{Date: "2026-11-15", Hour: 22, AdapterType: "pypi", Hit: false, RequestCount: 100},
		{Date: "2026-11-15", Hour: 23, AdapterType: "pypi", Hit: true, RequestCount: 3},
		{Date: "2026-11-16", Hour: 0, AdapterType: "pypi", Hit: false, RequestCount: 2},
		{Date: "2026-11-16", Hour: 23, AdapterType: "pypi", Hit: true, RequestCount: 5},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	snapshot := NewMCPHandler(database, nil, true, nil).statusSnapshot("https://depsilo.example", now)
	last24h := snapshot["last_24h"].(map[string]any)
	for field, want := range map[string]int64{"requests": 10, "hits": 8, "misses": 2} {
		if got := last24h[field]; got != want {
			t.Errorf("last_24h.%s = %v, want %d", field, got, want)
		}
	}
}
