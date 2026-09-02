package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
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

func TestValidateInitialAdminCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{name: "strong password", username: "operator", password: "Tr0ub4dor&Correct"},
		{name: "long passphrase", username: "operator", password: "correct horse battery staple"},
		{name: "short password", username: "operator", password: "S3cure!", wantErr: true},
		{name: "low diversity", username: "operator", password: "abcdefghijkl", wantErr: true},
		{name: "contains username", username: "operator", password: "Operator-Is-Safe-123!", wantErr: true},
		{name: "bad username", username: "../operator", password: "Tr0ub4dor&Correct", wantErr: true},
		{name: "bcrypt byte limit", username: "operator", password: "密密密密密密密密密密密密密密密密密密密密密密密密密", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateInitialAdminCredentials(test.username, test.password)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateInitialAdminCredentials() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestCreateInitialAdminUsesProvidedCredentialsOnlyOnce(t *testing.T) {
	database := newAPIAuthTestDB(t)
	const username = "operator"
	const password = "Tr0ub4dor&Correct"
	if err := CreateInitialAdmin(database, username, password); err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	var user db.User
	if err := database.Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.Role != "admin" || !user.Enabled {
		t.Fatalf("created user = %#v", user)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		t.Fatalf("provided password does not match stored hash: %v", err)
	}
	if err := CreateInitialAdmin(database, "second", "An0ther&SecurePassword"); !errors.Is(err, ErrInitialAdminExists) {
		t.Fatalf("second CreateInitialAdmin error = %v, want ErrInitialAdminExists", err)
	}
}

func TestCreateInitialAdminCanRecoverWhenOnlyAdministratorIsDisabled(t *testing.T) {
	database := newAPIAuthTestDB(t)
	disabled := db.User{
		Username: "retired", PasswordHash: "unused", Role: "admin", Enabled: false,
	}
	if err := database.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&disabled).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := CreateInitialAdmin(database, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	if err := VerifyExistingAdminCredentials(database, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("new loginable administrator credentials: %v", err)
	}
}

func TestVerifyExistingAdminCredentials(t *testing.T) {
	database := newAPIAuthTestDB(t)
	if err := CreateInitialAdmin(database, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExistingAdminCredentials(database, "operator", "Tr0ub4dor&Correct"); err != nil {
		t.Fatalf("VerifyExistingAdminCredentials: %v", err)
	}
	if err := VerifyExistingAdminCredentials(database, "operator", "Wr0ng&Password!"); !errors.Is(err, ErrExistingAdminCredentialMismatch) {
		t.Fatalf("wrong password error = %v", err)
	}
}

func TestEnsureInitialAdminDoesNotCreateAdminAdmin(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	if err := EnsureInitialAdmin(database, false); err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	var user db.User
	if err := database.Where("role = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("load initial administrator: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("initial username = %q, want admin", user.Username)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("admin")); err == nil {
		t.Fatal("initial administrator still accepts the predictable admin password")
	}
}

func TestEnsureInitialAdminSkipsInteractiveSetup(t *testing.T) {
	database := newAPIAuthTestDB(t)
	if err := EnsureInitialAdmin(database, true); err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	var count int64
	if err := database.Model(&db.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("user count = %d, want 0 while setup is pending", count)
	}
}

func TestReconcileSetupStateReopensConfiguredDatabaseWithoutLoginableAdmin(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "recovery-bootstrap-token-0123456789")
	cfg := &config.Config{ConfigPath: filepath.Join(t.TempDir(), "config.toml")}

	if err := ReconcileSetupState(database, cfg); err != nil {
		t.Fatalf("ReconcileSetupState: %v", err)
	}
	if !cfg.IsDefault || cfg.BootstrapToken != "recovery-bootstrap-token-0123456789" || cfg.BootstrapTokenGenerated {
		t.Fatalf("reconciled setup state = %#v", cfg)
	}
	if err := EnsureInitialAdmin(database, cfg.IsDefault); err != nil {
		t.Fatalf("EnsureInitialAdmin after reconciliation: %v", err)
	}
	var count int64
	if err := database.Model(&db.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("administrator count = %d, want 0 while setup is pending", count)
	}
}

func TestReconcileSetupStateKeepsExplicitHeadlessBootstrap(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "ops")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "Headless&Secure123")
	cfg := &config.Config{}

	if err := ReconcileSetupState(database, cfg); err != nil {
		t.Fatalf("ReconcileSetupState: %v", err)
	}
	if cfg.IsDefault {
		t.Fatal("explicit headless credentials unexpectedly reopened setup")
	}
	if err := EnsureInitialAdmin(database, false); err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	if err := VerifyExistingAdminCredentials(database, "ops", "Headless&Secure123"); err != nil {
		t.Fatalf("headless administrator credentials: %v", err)
	}
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
	token, err := middleware.GenerateJWT(cfg.JWTSecret, user, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
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
	if principal.Role != "admin" || !principal.Enabled || principal.AuthMethod != "jwt" || principal.TokenPermissions != nil || !principal.CanWrite {
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
	if err != nil || !parsed.Valid || parsed.Method.Alg() != jwt.SigningMethodHS256.Alg() || claims.ExpiresAt == nil || claims.Role != "admin" || claims.CredentialVersion != user.CredentialVersion {
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

func loginTestRequest(router http.Handler, remoteIP, username, password string) *httptest.ResponseRecorder {
	body := `{"username":"` + username + `","password":"` + password + `"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteIP + ":43210"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestLoginRateLimitsRepeatedFailuresAndResetsAfterSuccess(t *testing.T) {
	database := newAPIAuthTestDB(t)
	const username = "operator"
	const password = "Tr0ub4dor&Correct"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := database.Create(&db.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         "admin",
		Enabled:      true,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	newRouter := func() *gin.Engine {
		handler := NewAuthHandler(database, config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour})
		router := gin.New()
		router.POST("/auth/login", handler.Login)
		return router
	}

	t.Run("returns 429 for one IP and username tuple", func(t *testing.T) {
		router := newRouter()
		for attempt := 1; attempt <= 5; attempt++ {
			response := loginTestRequest(router, "192.0.2.10", username, "wrong-password")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want %d; body=%q", attempt, response.Code, http.StatusUnauthorized, response.Body.String())
			}
		}

		limited := loginTestRequest(router, "192.0.2.10", username, "wrong-password")
		if limited.Code != http.StatusTooManyRequests {
			t.Fatalf("limited status = %d, want %d; body=%q", limited.Code, http.StatusTooManyRequests, limited.Body.String())
		}
		if retryAfter := limited.Header().Get("Retry-After"); retryAfter == "" || retryAfter == "0" {
			t.Fatalf("Retry-After = %q, want positive delay", retryAfter)
		}

		otherIP := loginTestRequest(router, "192.0.2.11", username, "wrong-password")
		if otherIP.Code != http.StatusUnauthorized {
			t.Fatalf("different IP status = %d, want %d", otherIP.Code, http.StatusUnauthorized)
		}
		otherUsername := loginTestRequest(router, "192.0.2.10", "different-operator", "wrong-password")
		if otherUsername.Code != http.StatusUnauthorized {
			t.Fatalf("different username status = %d, want %d", otherUsername.Code, http.StatusUnauthorized)
		}
	})

	t.Run("successful login clears prior failures", func(t *testing.T) {
		router := newRouter()
		for attempt := 1; attempt <= 4; attempt++ {
			response := loginTestRequest(router, "198.51.100.20", username, "wrong-password")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, http.StatusUnauthorized)
			}
		}
		if response := loginTestRequest(router, "198.51.100.20", username, password); response.Code != http.StatusOK {
			t.Fatalf("successful login status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
		}
		for attempt := 1; attempt <= 5; attempt++ {
			response := loginTestRequest(router, "198.51.100.20", username, "wrong-password")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("post-success attempt %d status = %d, want %d", attempt, response.Code, http.StatusUnauthorized)
			}
		}
	})
}

func TestLoginRejectsOversizedRequestBody(t *testing.T) {
	database := newAPIAuthTestDB(t)
	handler := NewAuthHandler(database, config.AuthConfig{JWTSecret: authTestJWTSecret, TokenTTL: time.Hour})
	router := gin.New()
	router.POST("/auth/login", handler.Login)

	body := `{"username":"operator","password":"` + strings.Repeat("x", 16<<10) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
}
