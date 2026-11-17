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
)

// serveMCP sends one request to a router that mounts the handler the way
// internal/api/router.go does, without the authentication middleware.
func serveMCP(t *testing.T, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewMCPHandler(nil, []string{"pypi"}, false, nil)
	router := gin.New()
	router.POST("/mcp", handler.Handle)
	router.GET("/mcp", handler.MethodNotAllowed)
	router.DELETE("/mcp", handler.MethodNotAllowed)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/mcp", strings.NewReader(body))
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
