package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
	"depsilo/internal/middleware"
)

func authenticationFixture(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	document := `[auth.oidc]
enabled = false
client_secret = "private-secret"
`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", strings.Repeat("a", 64))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthenticationHandler(config.NewStore(path, cfg, zap.NewAtomicLevel()))
	router := gin.New()
	router.GET("/authentication", handler.Get)
	router.PUT("/authentication", handler.Update)
	return router, path
}

func TestAuthenticationConfigHandlerRedactsAndUpdates(t *testing.T) {
	router, path := authenticationFixture(t)
	for _, request := range []struct{ method, body string }{{http.MethodGet, ""}, {http.MethodPut, `{"display_name":"Corporate"}`}} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(request.method, "/authentication", strings.NewReader(request.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "no-store" || strings.Contains(recorder.Body.String(), "private-secret") {
			t.Fatal("missing no-store or revealed secret")
		}
		var state config.OIDCSettingsState
		if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if !state.Configured.ClientSecretSet {
			t.Fatal("secret presence missing")
		}
		if request.method == http.MethodPut && (!state.PendingRestart || state.Configured.DisplayName != "Corporate" || state.Effective.DisplayName == "Corporate") {
			t.Fatal("configuration applied without restart")
		}
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "private-secret") {
		t.Fatal("secret was not preserved")
	}
}

func TestAuthenticationConfigHandlerRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"enabled":null}`, `{"client_secret":null}`, `{"enabled":true,"enabled":false}`, `{"enabled":true,"Enabled":false}`, `{"DisplayName":"alias"}`,
		`{"unknown":"private-secret"}`, `{"client_secret_set":true}`, `{"client_secret":123}`, `{"admin_groups":"a"}`,
		`{"admin_groups":[null]}`, `{"enabled":false} {}`, `{"display_name":"` + strings.Repeat("x", 65536) + `"}`,
	} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			router, path := authenticationFixture(t)
			before, _ := os.ReadFile(path)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/authentication", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), "private-secret") {
				t.Fatal("bad request echoed input")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("bad request modified config")
			}
		})
	}
}

func TestAuthenticationAdminRoleGuard(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal middleware.Principal
		want      int
	}{
		{"local admin", middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true}, http.StatusOK},
		{"external admin", middleware.Principal{Role: "admin", CanWrite: true, LoginMethod: "oidc"}, http.StatusOK},
		{"admin read token", middleware.Principal{Role: "admin", AuthMethod: "api_token"}, http.StatusOK},
		{"readonly", middleware.Principal{Role: "readonly"}, http.StatusForbidden},
		{"missing", middleware.Principal{}, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(middleware.ContextKeyPrincipal, test.principal) })
			router.GET("/", AdminRoleRequired(), func(c *gin.Context) { c.Status(http.StatusOK) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != test.want {
				t.Fatalf("got %d", response.Code)
			}
		})
	}
}

// Keep the handler interface exercised without a process or provider network.
var _ authenticationStore = (*config.Store)(nil)
