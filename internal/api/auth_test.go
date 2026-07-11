package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

const authTestJWTSecret = "0123456789abcdef0123456789abcdef"

func newAPIAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api-auth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func apiAuthRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAuthMeAndRefreshUseCurrentPrincipal(t *testing.T) {
	database := newAPIAuthTestDB(t)
	user := db.User{Username: "operator", PasswordHash: "unused", Role: "admin", Enabled: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}
	h := NewAuthHandler(database, cfg)
	r := gin.New()
	r.GET("/auth/me", middleware.Authenticate(cfg.JWTSecret, database), h.Me)
	r.POST("/auth/refresh", middleware.JWTOnly(cfg.JWTSecret, database), h.Refresh)
	token, err := middleware.GenerateJWT(cfg.JWTSecret, user.ID, user.Username, "admin", time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	if err := database.Model(&user).Update("role", "readonly").Error; err != nil {
		t.Fatalf("downgrade: %v", err)
	}

	me := apiAuthRequest(r, http.MethodGet, "/auth/me", token)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", me.Code, me.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(me.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode principal fields: %v", err)
	}
	expectedFields := []string{"id", "username", "role", "enabled", "auth_method", "token_permissions", "can_write"}
	if len(fields) != len(expectedFields) {
		t.Fatalf("principal fields = %v", fields)
	}
	for _, field := range expectedFields {
		if _, ok := fields[field]; !ok {
			t.Fatalf("principal missing field %q: %v", field, fields)
		}
	}
	var principal middleware.Principal
	if err := json.Unmarshal(me.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.Role != "readonly" || !principal.Enabled || principal.AuthMethod != "jwt" || principal.TokenPermissions != nil || principal.CanWrite {
		t.Fatalf("principal = %#v", principal)
	}

	refresh := apiAuthRequest(r, http.MethodPost, "/auth/refresh", token)
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refresh.Code, refresh.Body.String())
	}
	var refreshed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(refresh.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	claims := &middleware.Claims{}
	parsed, err := jwt.ParseWithClaims(
		refreshed.Token,
		claims,
		func(token *jwt.Token) (any, error) { return []byte(cfg.JWTSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid || parsed.Method.Alg() != jwt.SigningMethodHS256.Alg() || claims.ExpiresAt == nil || claims.Role != "readonly" {
		t.Fatalf("refreshed claims = %#v, err = %v", claims, err)
	}
}

func TestAuthMeReportsAPITokenPermissions(t *testing.T) {
	database := newAPIAuthTestDB(t)
	user := db.User{Username: "admin", PasswordHash: "unused", Role: "admin", Enabled: true}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	rawToken := "readonly-api-token"
	digest := sha256.Sum256([]byte(rawToken))
	apiToken := db.APIToken{
		UserID: user.ID, Name: "reader", TokenHash: hex.EncodeToString(digest[:]), Permissions: "readonly",
	}
	if err := database.Create(&apiToken).Error; err != nil {
		t.Fatalf("create API token: %v", err)
	}
	cfg := config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour}
	h := NewAuthHandler(database, cfg)
	r := gin.New()
	r.GET("/auth/me", middleware.Authenticate(cfg.JWTSecret, database), h.Me)

	rec := apiAuthRequest(r, http.MethodGet, "/auth/me", rawToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var principal middleware.Principal
	if err := json.Unmarshal(rec.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.AuthMethod != middleware.AuthMethodAPIToken || principal.TokenPermissions == nil || *principal.TokenPermissions != "readonly" || principal.CanWrite {
		t.Fatalf("principal = %#v", principal)
	}
}
