package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func servePrompt(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/integration-prompt", NewIntegrationPromptHandler().Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestIntegrationPromptScopesToEcosystem(t *testing.T) {
	recorder := servePrompt(t, "http://depsilo.example/api/v1/integration-prompt?ecosystem=node")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "## Scope — Node.js only") || !strings.Contains(body, "`http://depsilo.example/npm/`") {
		t.Fatalf("scoped prompt missing its scope: %s", body[:min(len(body), 600)])
	}
}

func TestIntegrationPromptWithoutEcosystemIsUnscoped(t *testing.T) {
	recorder := servePrompt(t, "http://depsilo.example/api/v1/integration-prompt")
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "## Scope") {
		t.Fatalf("unscoped prompt: status = %d, scoped = %v", recorder.Code, strings.Contains(recorder.Body.String(), "## Scope"))
	}
}

func TestIntegrationPromptRejectsUnknownEcosystem(t *testing.T) {
	recorder := servePrompt(t, "http://depsilo.example/api/v1/integration-prompt?ecosystem=cobol")
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "python") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
