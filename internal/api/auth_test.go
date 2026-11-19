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
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
		// The password rules are credential.CredentialPolicy's, tested there;
		// "contains username" proves the username reaches the policy.
		{name: "strong password", username: "operator", password: "Tr0ub4dor&Correct"},
		{name: "contains username", username: "operator", password: "Operator-Is-Safe-123!", wantErr: true},
		{name: "bad username", username: "../operator", password: "Tr0ub4dor&Correct", wantErr: true},
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
	observed, logs := observer.New(zap.InfoLevel)
	restore := zap.ReplaceGlobals(zap.New(observed))
	t.Cleanup(restore)
	outcome, err := EnsureInitialAdmin(database, false, config.NewSecureToken)
	if err != nil {
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
	// The generated password is handed to startup for the console summary; it
	// is the administrator's real, lasting credential.
	if outcome.GeneratedUsername != "admin" || outcome.EnvironmentIgnored {
		t.Fatalf("generated administrator outcome = %#v", outcome)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(outcome.GeneratedPassword)); err != nil {
		t.Fatalf("reported generated password does not sign in: %v", err)
	}
	entries := logs.FilterField(zap.String("initial_admin_password", outcome.GeneratedPassword)).All()
	if len(entries) != 1 {
		t.Fatalf("generated password log entries = %d, want one", len(entries))
	}
	message := entries[0].Message
	if strings.Contains(message, "one-time") || !strings.Contains(message, "shown only this once") || !strings.Contains(message, "stays valid until changed") {
		t.Fatalf("generated password log message = %q", message)
	}

	// A later start finds the administrator and has nothing to disclose again.
	again, err := EnsureInitialAdmin(database, false, config.NewSecureToken)
	if err != nil || again != (InitialAdminOutcome{}) {
		t.Fatalf("second EnsureInitialAdmin = (%#v, %v), want nothing to report", again, err)
	}
}

// tokenSequence hands out the given tokens in order and counts the draws.
func tokenSequence(t *testing.T, tokens ...string) (func() (string, error), *int) {
	t.Helper()
	draws := 0
	return func() (string, error) {
		if draws >= len(tokens) {
			t.Fatalf("drew token %d of %d", draws+1, len(tokens))
		}
		draws++
		return tokens[draws-1], nil
	}, &draws
}

// The password policy rejects a password that contains the user name in any
// case. A random token sometimes does, so a rejected draw is replaced instead
// of failing the start.
func TestEnsureInitialAdminDrawsAgainWhenTheGeneratedPasswordContainsTheUsername(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "ops")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	const rejected = "k3Zq-OpS-rejected-draw-0123456789-ABCDEFG"
	const accepted = "k3Zq-accepted-draw-0123456789-ABCDEFGHIJK"
	tokens, draws := tokenSequence(t, rejected, accepted)

	outcome, err := EnsureInitialAdmin(database, false, tokens)
	if err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	if *draws != 2 || outcome.GeneratedUsername != "ops" || outcome.GeneratedPassword != accepted {
		t.Fatalf("draws = %d, outcome = %#v, want the second token as the password", *draws, outcome)
	}
	if err := VerifyExistingAdminCredentials(database, "ops", accepted); err != nil {
		t.Fatalf("the reported password does not sign in: %v", err)
	}
	if err := VerifyExistingAdminCredentials(database, "ops", rejected); err == nil {
		t.Fatal("the rejected draw signs in")
	}
}

func TestEnsureInitialAdminStopsDrawingAfterSixteenRejectedPasswords(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "ops")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	rejected := make([]string, 16)
	for index := range rejected {
		rejected[index] = "k3Zq-oPs-rejected-draw-0123456789-ABCDEFG"
	}
	tokens, draws := tokenSequence(t, rejected...)

	outcome, err := EnsureInitialAdmin(database, false, tokens)
	if err == nil || err.Error() != "password must not contain the username" {
		t.Fatalf("EnsureInitialAdmin = (%#v, %v), want the password policy's error", outcome, err)
	}
	if *draws != 16 || outcome != (InitialAdminOutcome{}) {
		t.Fatalf("draws = %d, outcome = %#v, want 16 draws and nothing to report", *draws, outcome)
	}
	var count int64
	if err := database.Model(&db.User{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("users = %d, err = %v, want no administrator", count, err)
	}
}

func TestEnsureInitialAdminNeverReportsSuppliedPassword(t *testing.T) {
	database := newAPIAuthTestDB(t)
	const supplied = "Headless&Secure123"
	t.Setenv("DEPSILO_ADMIN_USERNAME", "ops")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", supplied)
	observed, logs := observer.New(zap.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(observed))
	t.Cleanup(restore)

	outcome, err := EnsureInitialAdmin(database, false, config.NewSecureToken)
	if err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	if outcome != (InitialAdminOutcome{}) {
		t.Fatalf("supplied credentials were reported for display: %#v", outcome)
	}
	if err := VerifyExistingAdminCredentials(database, "ops", supplied); err != nil {
		t.Fatalf("headless administrator credentials: %v", err)
	}
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, supplied) {
			t.Fatalf("log message contains the supplied password: %q", entry.Message)
		}
		for _, field := range entry.Context {
			if strings.Contains(field.String, supplied) {
				t.Fatalf("log field %q contains the supplied password", field.Key)
			}
		}
	}
}

func TestEnsureInitialAdminSkipsInteractiveSetup(t *testing.T) {
	database := newAPIAuthTestDB(t)
	t.Setenv("DEPSILO_ADMIN_USERNAME", "")
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "")
	observed, logs := observer.New(zap.WarnLevel)
	restore := zap.ReplaceGlobals(zap.New(observed))
	t.Cleanup(restore)
	outcome, err := EnsureInitialAdmin(database, true, config.NewSecureToken)
	if err != nil {
		t.Fatalf("EnsureInitialAdmin: %v", err)
	}
	if outcome != (InitialAdminOutcome{}) || logs.Len() != 0 {
		t.Fatalf("plain pending setup reported %#v with %d warnings, want silence", outcome, logs.Len())
	}
	var count int64
	if err := database.Model(&db.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("user count = %d, want 0 while setup is pending", count)
	}
}

// On a blank instance the administrator variables do nothing. That must be
// said out loud: otherwise the operator believes an administrator exists while
// the instance still waits for setup.
func TestEnsureInitialAdminWarnsWhenEnvironmentIsIgnoredDuringSetup(t *testing.T) {
	const supplied = "Headless&Secure123"
	for _, test := range []struct {
		name     string
		username string
		password string
	}{
		{name: "both variables", username: "ops", password: supplied},
		{name: "username only", username: "ops"},
		{name: "password only", password: supplied},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newAPIAuthTestDB(t)
			t.Setenv("DEPSILO_ADMIN_USERNAME", test.username)
			t.Setenv("DEPSILO_ADMIN_PASSWORD", test.password)
			observed, logs := observer.New(zap.WarnLevel)
			restore := zap.ReplaceGlobals(zap.New(observed))
			t.Cleanup(restore)

			outcome, err := EnsureInitialAdmin(database, true, config.NewSecureToken)
			if err != nil {
				t.Fatalf("EnsureInitialAdmin: %v", err)
			}
			if outcome != (InitialAdminOutcome{EnvironmentIgnored: true}) {
				t.Fatalf("outcome = %#v, want only the ignored-environment notice", outcome)
			}
			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("warnings = %d, want exactly one", len(entries))
			}
			message := entries[0].Message
			for _, fragment := range []string{
				"DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD are ignored",
				"apply only when a configuration file exists",
				"docs/deployment.md#scripted-setup",
			} {
				if !strings.Contains(message, fragment) {
					t.Errorf("warning %q missing %q", message, fragment)
				}
			}
			if strings.Contains(message, supplied) || len(entries[0].Context) != 0 {
				t.Fatalf("warning disclosed the supplied credentials: %q %v", message, entries[0].Context)
			}
			var count int64
			if err := database.Model(&db.User{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("user count = %d; the variables must not take effect on a blank instance", count)
			}
		})
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
	if _, err := EnsureInitialAdmin(database, cfg.IsDefault, config.NewSecureToken); err != nil {
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
	if _, err := EnsureInitialAdmin(database, false, config.NewSecureToken); err != nil {
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
	expectedFields := []string{"id", "username", "role", "enabled", "auth_method", "token_permissions", "can_write", "permission_source", "login_method", "can_manage_credentials"}
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
