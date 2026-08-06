package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func newReadAuthRouter(t *testing.T) (*gin.Engine, string, string) {
	t.Helper()
	database := newAPIAuthTestDB(t)
	if err := database.AutoMigrate(&db.CacheEntry{}, &db.AccessLog{}); err != nil {
		t.Fatalf("migrate package history: %v", err)
	}

	user := db.User{
		Username:     "operator",
		PasswordHash: "unused",
		Role:         "admin",
		Enabled:      true,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	jwtToken, err := middleware.GenerateJWT(authTestJWTSecret, user.ID, user.Username, user.Role, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	apiToken := "read-only-api-token"
	digest := sha256.Sum256([]byte(apiToken))
	if err := database.Create(&db.APIToken{
		UserID:      user.ID,
		Name:        "reader",
		TokenHash:   hex.EncodeToString(digest[:]),
		Permissions: "readonly",
	}).Error; err != nil {
		t.Fatalf("create API token: %v", err)
	}

	now := time.Now().UTC()
	if err := database.Create(&db.CacheEntry{
		Key:          "npm/private-package/-/private-package-1.0.0.tgz",
		AdapterType:  "npm",
		CacheKind:    db.CacheKindArtifact,
		PackageName:  "private-package",
		Size:         42,
		HitCount:     3,
		LastAccessed: now,
		ExpiresAt:    now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("create cache entry: %v", err)
	}
	if err := database.Create(&db.AccessLog{
		AdapterType: "npm",
		PackageName: "private-package",
		StatusCode:  http.StatusOK,
		CreatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create access log: %v", err)
	}

	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}}
	router := gin.New()
	RegisterRoutes(router, Deps{
		LifecycleContext: context.Background(),
		DB:               database,
		Config:           cfg,
		EventBus:         cache.NewEventBus(),
	})
	return router, jwtToken, apiToken
}

type closedNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *closedNotifyRecorder) CloseNotify() <-chan bool {
	closed := make(chan bool)
	close(closed)
	return closed
}

func TestPackageHistoryRoutesRequireReadAuthentication(t *testing.T) {
	router, jwtToken, apiToken := newReadAuthRouter(t)

	for _, requestPath := range []string{
		"/api/v1/packages",
		"/api/v1/packages/npm/private-package",
		"/api/v1/events/stream",
	} {
		t.Run("anonymous "+requestPath, func(t *testing.T) {
			response := &closedNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusUnauthorized, response.Body.String())
			}
		})
	}

	for name, test := range map[string]struct {
		path  string
		token string
	}{
		"JWT":       {path: "/api/v1/packages", token: jwtToken},
		"API token": {path: "/api/v1/packages/npm/private-package", token: apiToken},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "private-package") {
				t.Fatalf("status = %d, body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestNowRouteRequiresReadAuthentication(t *testing.T) {
	router, jwtToken, apiToken := newReadAuthRouter(t)

	for _, test := range []struct {
		name  string
		token string
		want  int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "JWT", token: jwtToken, want: http.StatusOK},
		{name: "readonly API token", token: apiToken, want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/now", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.want, response.Body.String())
			}
			if test.want == http.StatusOK && !strings.Contains(response.Body.String(), "private-package") {
				t.Fatalf("authorized live status omitted request context: %q", response.Body.String())
			}
		})
	}
}

func TestMCPPackageHistoryRequiresReadAuthentication(t *testing.T) {
	router, jwtToken, apiToken := newReadAuthRouter(t)

	requests := []struct {
		name  string
		body  string
		token string
		want  int
	}{
		{
			name: "anonymous search",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"depsilo_search","arguments":{"query":"private"}}}`,
			want: http.StatusUnauthorized,
		},
		{
			name:  "JWT recent",
			body:  `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"depsilo_recent","arguments":{}}}`,
			token: jwtToken,
			want:  http.StatusOK,
		},
		{
			name:  "API token search",
			body:  `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"depsilo_search","arguments":{"query":"private"}}}`,
			token: apiToken,
			want:  http.StatusOK,
		},
	}

	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.want, response.Body.String())
			}
			if test.want == http.StatusOK && !strings.Contains(response.Body.String(), "private-package") {
				t.Fatalf("authorized response omitted package history: %q", response.Body.String())
			}
		})
	}
}

func TestPortalDiscoveryAndAggregateStatusRemainAnonymous(t *testing.T) {
	router, _, _ := newReadAuthRouter(t)

	for _, requestPath := range []string{
		"/api/v1/discover",
		"/api/v1/agent-prompt",
		"/api/v1/integration-prompt",
		"/api/v1/stats",
	} {
		t.Run(requestPath, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, requestPath, nil)
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("anonymous Portal endpoint returned %d, want %d: %q", response.Code, http.StatusOK, response.Body.String())
			}
		})
	}
}
