package public

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

func TestMCPConfigureGuidanceUsesEnforcementSafeRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "https://depsilo.example/mcp", nil)
	handler := &MCPHandler{Ecosystems: []string{"alpine", "docker"}}

	docker, err := handler.toolConfigure(context, "docker")
	if err != nil {
		t.Fatal(err)
	}
	dockerText := toolResultText(t, docker)
	if !strings.Contains(dockerText, "registry-mirrors") || !strings.Contains(dockerText, "https://depsilo.example") || strings.Contains(dockerText, "https://depsilo.example/docker/") {
		t.Fatalf("unsafe Docker guidance: %s", dockerText)
	}

	alpine, err := handler.toolConfigure(context, "alpine")
	if err != nil {
		t.Fatal(err)
	}
	alpineText := toolResultText(t, alpine)
	if !strings.Contains(alpineText, "--repositories-file") || !strings.Contains(alpineText, "/alpine/${release}/main") || strings.Contains(alpineText, "apk add --repository ") {
		t.Fatalf("unsafe Alpine guidance: %s", alpineText)
	}
}

func TestMCPWarmupReturnsAnUnexecutedRequestTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "https://depsilo.example/mcp", nil)

	result, err := (&MCPHandler{}).toolWarmup(context, "pypi", []string{"requests"})
	if err != nil {
		t.Fatal(err)
	}
	text := toolResultText(t, result)
	if !strings.Contains(text, `"executed": false`) || !strings.Contains(text, "https://depsilo.example/api/v1/admin/cache/warmup") || strings.Contains(text, `"queued": true`) {
		t.Fatalf("misleading warmup result: %s", text)
	}
}

func TestMCPRequestFactsAreRecentAndCredentialRedacted(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "mcp-request.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.AccessLog{}, &db.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Create(&db.AccessLog{RequestID: "req-mcp-1", AdapterType: "pypi", PackageName: "requests", CacheResult: "miss", CacheReason: "https://user:secret@example.invalid/path", PolicyDecision: "allow", DeliveryResult: "upstream", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewMCPHandler(database, []string{"pypi"}, false, nil)
	result, err := handler.toolRequest(context.Background(), "req-mcp-1")
	if err != nil {
		t.Fatal(err)
	}
	text := toolResultText(t, result)
	if strings.Contains(text, "user:secret") || strings.Contains(text, "example.invalid/path") || !strings.Contains(text, "example.invalid/***") || !strings.Contains(text, "external_strings_untrusted") {
		t.Fatalf("request facts leaked or lacked trust marker: %s", text)
	}
	missing, err := handler.toolRequest(context.Background(), "old-request")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolResultText(t, missing), "no retained access log") {
		t.Fatal("missing request did not report bounded retention")
	}
	for _, value := range []string{
		"https://token@example.invalid/path?secret=1",
		"https://user:secret@example.invalid/path",
		"https://user%3Asecret@example.invalid/path",
		strings.Repeat("x", 300) + "https://user:secret@example.invalid/path",
	} {
		masked := safeMCPFact(value, 256)
		if strings.Contains(masked, "secret") || strings.Contains(masked, "example.invalid/path") {
			t.Fatalf("unsafe MCP fact %q -> %q", value, masked)
		}
	}
	if _, err := handler.toolRequest(context.Background(), "req mcp"); err == nil {
		t.Fatal("unsafe request ID accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handler.toolRequest(cancelled, "req-mcp-1"); err == nil {
		t.Fatal("cancelled request facts query completed")
	}
}

func TestAgentPromptDoesNotPromiseAutomaticOutageFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "https://depsilo.example/api/v1/agent-prompt", nil)

	(&DiscoverHandler{}).AgentPrompt(context)
	body := recorder.Body.String()
	if !strings.Contains(body, "do not provide reliable outage failover") || !strings.Contains(body, `GOPROXY "|direct"`) || strings.Contains(body, "tools fall back to public registries") {
		t.Fatalf("unsafe outage guidance: %s", body)
	}
}

func toolResultText(t *testing.T, result any) string {
	t.Helper()
	value, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	content, ok := value["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", value["content"])
	}
	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatalf("text = %#v", content[0]["text"])
	}
	return text
}
