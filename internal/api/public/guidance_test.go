package public

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
