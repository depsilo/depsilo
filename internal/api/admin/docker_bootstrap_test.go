package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

func TestDockerBootstrapHandlerExposesOnlySafeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.toml")
	secret := "private-registry-password"
	document := `[docker]
[[docker.registries]]
name = "private"
url = "https://registry.private.example"
password = "` + secret + `"
`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewDockerBootstrapHandler(config.NewStore(path, &config.Config{}, zap.NewAtomicLevel()))
	router := gin.New()
	router.GET("/docker/bootstrap", handler.Get)
	router.POST("/docker/bootstrap", handler.Enable)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/docker/bootstrap", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"configured":true`) || strings.Contains(get.Body.String(), secret) || strings.Contains(get.Body.String(), "registry.private.example") {
		t.Fatalf("GET = %d %s", get.Code, get.Body.String())
	}
	post := httptest.NewRecorder()
	router.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/docker/bootstrap", nil))
	if post.Code != http.StatusUnprocessableEntity || !strings.Contains(post.Body.String(), "INVALID_SETTING") || strings.Contains(post.Body.String(), secret) {
		t.Fatalf("POST = %d %s", post.Code, post.Body.String())
	}
	written, _ := os.ReadFile(path)
	if string(written) != document {
		t.Fatal("existing private registry changed")
	}
}
