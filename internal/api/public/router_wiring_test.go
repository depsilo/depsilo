package public_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/api"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

const wiringJWTSecret = "0123456789abcdef0123456789abcdef"

// newWiredRouter builds the real route table, so these tests cover how
// internal/api/router.go mounts this package and not only the handlers.
func newWiredRouter(t *testing.T, cfg *config.Config) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wiring.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}, &db.CacheEntry{}, &db.AccessLog{}, &db.AuditLog{}, &db.ControlPlaneState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := db.User{Username: "operator", PasswordHash: "unused", Role: "admin", Enabled: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg.Auth = config.AuthConfig{JWTSecret: wiringJWTSecret, TokenTTL: time.Hour}
	token, err := middleware.GenerateJWT(wiringJWTSecret, user, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}

	router := gin.New()
	api.RegisterRoutes(router, api.Deps{
		LifecycleContext: context.Background(),
		DB:               database,
		Config:           cfg,
		ConfigStore:      config.NewStore(filepath.Join(t.TempDir(), "config.toml"), cfg, zap.NewAtomicLevel()),
		EventBus:         cache.NewEventBus(),
	})
	return router, token
}

func serveWired(router *gin.Engine, method, target, token, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestMCPRouteAnswersUnsupportedMethodsWith405BehindAuthentication(t *testing.T) {
	router, token := newWiredRouter(t, &config.Config{})

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			anonymous := serveWired(router, method, "/mcp", "", "")
			if anonymous.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status = %d, want %d; body = %q", anonymous.Code, http.StatusUnauthorized, anonymous.Body.String())
			}
			authenticated := serveWired(router, method, "/mcp", token, "")
			if authenticated.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d; body = %q", authenticated.Code, http.StatusMethodNotAllowed, authenticated.Body.String())
			}
			if allow := authenticated.Header().Get("Allow"); allow != http.MethodPost {
				t.Fatalf("Allow = %q, want %q", allow, http.MethodPost)
			}
		})
	}

	// POST keeps its contract: authentication first, then JSON-RPC.
	if anonymous := serveWired(router, http.MethodPost, "/mcp", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST status = %d, want %d", anonymous.Code, http.StatusUnauthorized)
	}
	ping := serveWired(router, http.MethodPost, "/mcp", token, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	var envelope struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(ping.Body.Bytes(), &envelope); err != nil || ping.Code != http.StatusOK || envelope.ID != 1 || envelope.Result == nil {
		t.Fatalf("ping: status = %d, body = %q, err = %v", ping.Code, ping.Body.String(), err)
	}
	notification := serveWired(router, http.MethodPost, "/mcp", token, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if notification.Code != http.StatusAccepted || notification.Body.Len() != 0 {
		t.Fatalf("notification: status = %d, body = %q", notification.Code, notification.Body.String())
	}
}

// Discovery learns the setup state from the composition in router.go, and the
// MCP discover resource is the same catalog.
func TestDiscoveryFollowsFirstRunSetupState(t *testing.T) {
	for _, pending := range []bool{true, false} {
		router, token := newWiredRouter(t, &config.Config{IsDefault: pending})

		var catalog struct {
			SetupRequired *bool             `json:"setup_required"`
			Endpoints     map[string]string `json:"endpoints"`
		}
		discover := serveWired(router, http.MethodGet, "/api/v1/discover", "", "")
		if err := json.Unmarshal(discover.Body.Bytes(), &catalog); err != nil || discover.Code != http.StatusOK {
			t.Fatalf("discover: status = %d, err = %v", discover.Code, err)
		}
		if catalog.SetupRequired == nil || *catalog.SetupRequired != pending {
			t.Fatalf("setup_required = %v, want %t", catalog.SetupRequired, pending)
		}
		// The catalog points at a route that exists.
		if status := serveWired(router, http.MethodGet, catalog.Endpoints["setup_status"], "", ""); status.Code != http.StatusOK || !strings.Contains(status.Body.String(), "needs_setup") {
			t.Fatalf("endpoints.setup_status = %q answered %d %q", catalog.Endpoints["setup_status"], status.Code, status.Body.String())
		}

		prompt := serveWired(router, http.MethodGet, "/api/v1/agent-prompt", "", "").Body.String()
		if !strings.Contains(prompt, "This workspace has a dependency cache called Depsilo") {
			t.Fatalf("pending = %t, agent prompt = %q", pending, prompt)
		}

		resource := serveWired(router, http.MethodPost, "/mcp", token, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depsilo://discover"}}`)
		if want := `\"setup_required\":` + map[bool]string{true: "true", false: "false"}[pending]; !strings.Contains(resource.Body.String(), want) {
			t.Fatalf("pending = %t, MCP discover resource = %q", pending, resource.Body.String())
		}
	}
}
