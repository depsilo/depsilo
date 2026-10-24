package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func TestExternalAdminCannotManageDurableCredentialsThroughRouter(t *testing.T) {
	for _, protocol := range []string{"oidc", "ldap"} {
		t.Run(protocol, func(t *testing.T) {
			database := newAPIAuthTestDB(t)
			if err := database.AutoMigrate(&db.ExternalIdentity{}, &db.IdentitySession{}, &db.ControlPlaneState{}); err != nil {
				t.Fatal(err)
			}
			user := db.User{Username: "managed", Role: "admin", Enabled: true, PermissionSource: protocol}
			if err := database.Create(&user).Error; err != nil {
				t.Fatal(err)
			}
			issuer := "https://id.example"
			if protocol == "ldap" {
				issuer = "urn:depsilo:ldap:" + strings.Repeat("a", 64)
			}
			identity := db.ExternalIdentity{UserID: user.ID, Provider: "company", Issuer: issuer, Subject: "managed"}
			if err := database.Create(&identity).Error; err != nil {
				t.Fatal(err)
			}
			session := db.IdentitySession{ID: "router-session", UserID: user.ID, IdentityID: identity.ID, Role: "admin", ConfigFingerprint: "test", ExpiresAt: time.Now().Add(time.Minute)}
			if err := database.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			token, err := middleware.GenerateIdentityJWT(authTestJWTSecret, user, session, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}}
			router := gin.New()
			RegisterRoutes(router, Deps{LifecycleContext: context.Background(), DB: database, Config: cfg, ConfigStore: config.NewStore(filepath.Join(t.TempDir(), "config.toml"), cfg, zap.NewAtomicLevel()), EventBus: cache.NewEventBus()})
			for _, route := range []struct{ method, path string }{
				{http.MethodPost, "/tokens"}, {http.MethodDelete, "/tokens/1"},
				{http.MethodPost, "/users"}, {http.MethodPut, "/users/1"}, {http.MethodDelete, "/users/1"},
				{http.MethodPost, "/compile-cache/credentials"}, {http.MethodDelete, "/compile-cache/credentials/1"},
				{http.MethodPut, "/cascade/config"}, {http.MethodPut, "/authentication"},
				{http.MethodPost, "/authentication/check"}, {http.MethodPost, "/authentication/trial"},
				{http.MethodPost, "/authentication/ldap/check"}, {http.MethodPost, "/authentication/ldap/trial"},
				{http.MethodPost, "/authentication/sources"}, {http.MethodPut, "/authentication/sources/company"}, {http.MethodDelete, "/authentication/sources/company"},
			} {
				response := apiAuthRequest(router, route.method, "/api/v1/admin"+route.path, token)
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusForbidden || body.Code != "LOCAL_CREDENTIALS_REQUIRED" {
					t.Fatalf("%s %s: %d %s", route.method, route.path, response.Code, body.Code)
				}
			}
		})
	}
}

func TestIdentityRefreshAndLogoutPreserveAbsoluteAuthority(t *testing.T) {
	database := newAPIAuthTestDB(t)
	if err := database.AutoMigrate(&db.ExternalIdentity{}, &db.IdentitySession{}); err != nil {
		t.Fatal(err)
	}
	user := db.User{Username: "managed", Role: "admin", Enabled: true, PermissionSource: "oidc"}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	identity := db.ExternalIdentity{UserID: user.ID, Provider: "company", Issuer: "https://id.example", Subject: "one"}
	if err := database.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	session := db.IdentitySession{ID: "session", UserID: user.ID, IdentityID: identity.ID, Role: "admin", ConfigFingerprint: "test", ExpiresAt: time.Now().Add(10 * time.Minute).Truncate(time.Second)}
	if err := database.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: 168 * time.Hour}
	h := NewAuthHandler(database, cfg)
	router := gin.New()
	router.POST("/refresh", middleware.JWTOnly(cfg.JWTSecret, database), h.Refresh)
	router.POST("/logout", middleware.OptionalLogoutPrincipal(cfg.JWTSecret, database), h.Logout)
	router.GET("/me", middleware.JWTOnly(cfg.JWTSecret, database), h.Me)
	token, err := middleware.GenerateIdentityJWT(cfg.JWTSecret, user, session, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		response := apiAuthRequest(router, http.MethodPost, "/refresh", token)
		if response.Code != http.StatusOK {
			t.Fatalf("refresh: %d %s", response.Code, response.Body.String())
		}
		var body struct {
			Token     string `json:"token"`
			ExpiresAt int64  `json:"expires_at"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		claims := &middleware.Claims{}
		if _, err := jwt.ParseWithClaims(body.Token, claims, func(*jwt.Token) (any, error) { return []byte(cfg.JWTSecret), nil }); err != nil {
			t.Fatal(err)
		}
		if claims.SessionID != session.ID || !claims.ExpiresAt.Time.Equal(session.ExpiresAt) || body.ExpiresAt != session.ExpiresAt.Unix() {
			t.Fatal("refresh extended or detached external authority")
		}
		token = body.Token
	}
	if response := apiAuthRequest(router, http.MethodPost, "/logout", token); response.Code != http.StatusOK {
		t.Fatalf("logout: %d", response.Code)
	}
	for _, stale := range []string{token, "", "invalid"} {
		if response := apiAuthRequest(router, http.MethodPost, "/logout", stale); response.Code != http.StatusOK {
			t.Fatalf("idempotent logout: %d", response.Code)
		}
	}
	for _, path := range []string{"/refresh", "/me"} {
		method := http.MethodPost
		if path == "/me" {
			method = http.MethodGet
		}
		if response := apiAuthRequest(router, method, path, token); response.Code != http.StatusUnauthorized {
			t.Fatalf("revoked %s: %d", path, response.Code)
		}
	}
}

func TestLocalSessionCannotOutliveRemovalOfPassword(t *testing.T) {
	database := newAPIAuthTestDB(t)
	user := db.User{Username: "operator", PasswordHash: "previous-hash", Role: "admin", Enabled: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}
	token, err := middleware.GenerateJWT(cfg.JWTSecret, user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&user).Update("password_hash", "").Error; err != nil {
		t.Fatal(err)
	}
	h := NewAuthHandler(database, cfg)
	router := gin.New()
	router.POST("/refresh", middleware.JWTOnly(cfg.JWTSecret, database), h.Refresh)
	if response := apiAuthRequest(router, http.MethodPost, "/refresh", token); response.Code != http.StatusUnauthorized {
		t.Fatalf("removed password refresh: %d", response.Code)
	}
}

func TestExternalAdministratorDoesNotReplaceLocalRecoveryAccount(t *testing.T) {
	database := newAPIAuthTestDB(t)
	if err := database.Create(&db.User{Username: "managed", Role: "admin", PermissionSource: "oidc", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := CreateInitialAdmin(database, "recovery", "Tr0ub4dor&Correct"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExistingAdminCredentials(database, "recovery", "Tr0ub4dor&Correct"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationConfigurationRoleEnforcedThroughRouter(t *testing.T) {
	database := newAPIAuthTestDB(t)
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}}
	router := gin.New()
	RegisterRoutes(router, Deps{LifecycleContext: context.Background(), DB: database, Config: cfg, ConfigStore: config.NewStore(filepath.Join(t.TempDir(), "config.toml"), cfg, zap.NewAtomicLevel()), EventBus: cache.NewEventBus()})
	for _, test := range []struct {
		role string
		want int
	}{{"admin", http.StatusOK}, {"readonly", http.StatusForbidden}} {
		user := db.User{Username: "system-" + test.role, PasswordHash: "existing-local-password", Role: test.role, Enabled: true}
		if err := database.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		token, err := middleware.GenerateJWT(authTestJWTSecret, user, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		response := apiAuthRequest(router, http.MethodGet, "/api/v1/admin/authentication", token)
		if response.Code != test.want {
			t.Fatalf("%s GET: %d", test.role, response.Code)
		}
		if test.role == "readonly" {
			response = apiAuthRequest(router, http.MethodPut, "/api/v1/admin/authentication", token)
			if response.Code != http.StatusForbidden {
				t.Fatalf("readonly PUT: %d", response.Code)
			}
		}
	}
	if response := apiAuthRequest(router, http.MethodGet, "/api/v1/admin/authentication", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET: %d", response.Code)
	}
}
