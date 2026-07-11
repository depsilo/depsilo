package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

const authTestSecret = "0123456789abcdef0123456789abcdef"

func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.AutoMigrate(&db.User{}, &db.APIToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func createAuthTestUser(t *testing.T, database *gorm.DB, username, role string, enabled bool) db.User {
	t.Helper()
	user := db.User{Username: username, PasswordHash: "unused", Role: role, Enabled: enabled}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func createAuthTestAPIToken(t *testing.T, database *gorm.DB, userID uint, raw, permissions string) {
	t.Helper()
	digest := sha256.Sum256([]byte(raw))
	token := db.APIToken{UserID: userID, Name: raw, TokenHash: hex.EncodeToString(digest[:]), Permissions: permissions}
	if err := database.Create(&token).Error; err != nil {
		t.Fatalf("create API token: %v", err)
	}
}

func authRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func assertGenericUnauthorized(t *testing.T, rec *httptest.ResponseRecorder, token string) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if len(response) != 2 || response["code"] != "UNAUTHORIZED" || response["message"] != "invalid or expired token" {
		t.Fatalf("unauthorized response = %#v", response)
	}
	for _, sensitive := range []string{token, "sql", "database", "api_tokens", "users", "injected"} {
		if sensitive != "" && strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(sensitive)) {
			t.Fatalf("unauthorized response leaked %q: %s", sensitive, rec.Body.String())
		}
	}
}

func assertGenericForbidden(t *testing.T, rec *httptest.ResponseRecorder, token string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if len(response) != 2 || response["code"] != "FORBIDDEN" || response["message"] != "write capability required" {
		t.Fatalf("forbidden response = %#v", response)
	}
	for _, sensitive := range []string{token, "sql", "database", "api_tokens", "users", "injected"} {
		if sensitive != "" && strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(sensitive)) {
			t.Fatalf("forbidden response leaked %q: %s", sensitive, rec.Body.String())
		}
	}
}

func TestJWTUsesCurrentRoleAndRejectsDisabledUser(t *testing.T) {
	database := newAuthTestDB(t)
	user := createAuthTestUser(t, database, "operator", "admin", true)
	token, err := GenerateJWT(authTestSecret, user.ID, user.Username, user.Role, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	r := gin.New()
	r.GET("/read", Authenticate(authTestSecret, database), ReadRequired(), func(c *gin.Context) {
		principal, _ := PrincipalFromContext(c)
		c.JSON(http.StatusOK, principal)
	})
	r.POST("/write", Authenticate(authTestSecret, database), WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	if err := database.Model(&user).Update("role", "readonly").Error; err != nil {
		t.Fatalf("downgrade user: %v", err)
	}
	readRec := authRequest(r, http.MethodGet, "/read", token)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", readRec.Code, readRec.Body.String())
	}
	var principal Principal
	if err := json.Unmarshal(readRec.Body.Bytes(), &principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.Role != "readonly" || principal.CanWrite || principal.AuthMethod != AuthMethodJWT || principal.TokenPermissions != nil {
		t.Fatalf("principal = %#v", principal)
	}
	if rec := authRequest(r, http.MethodPost, "/write", token); rec.Code != http.StatusForbidden {
		t.Fatalf("stale admin JWT write status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := database.Model(&user).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if rec := authRequest(r, http.MethodGet, "/read", token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("disabled JWT status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAPITokenPermissionMatrix(t *testing.T) {
	database := newAuthTestDB(t)
	admin := createAuthTestUser(t, database, "admin-owner", "admin", true)
	readonly := createAuthTestUser(t, database, "reader-owner", "readonly", true)
	createAuthTestAPIToken(t, database, admin.ID, "admin-readonly-token", "readonly")
	createAuthTestAPIToken(t, database, admin.ID, "admin-readwrite-token", "readwrite")
	createAuthTestAPIToken(t, database, readonly.ID, "reader-readwrite-token", "readwrite")
	r := gin.New()
	r.GET("/read", Authenticate(authTestSecret, database), ReadRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/write", Authenticate(authTestSecret, database), WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name        string
		token       string
		readStatus  int
		writeStatus int
	}{
		{name: "admin readonly token", token: "admin-readonly-token", readStatus: http.StatusNoContent, writeStatus: http.StatusForbidden},
		{name: "admin readwrite token", token: "admin-readwrite-token", readStatus: http.StatusNoContent, writeStatus: http.StatusNoContent},
		{name: "readonly owner readwrite token", token: "reader-readwrite-token", readStatus: http.StatusNoContent, writeStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := authRequest(r, http.MethodGet, "/read", tt.token); rec.Code != tt.readStatus {
				t.Fatalf("read status = %d, want %d", rec.Code, tt.readStatus)
			}
			if rec := authRequest(r, http.MethodPost, "/write", tt.token); rec.Code != tt.writeStatus {
				t.Fatalf("write status = %d, want %d", rec.Code, tt.writeStatus)
			}
		})
	}
}

func TestJWTOnlyRejectsAPIToken(t *testing.T) {
	database := newAuthTestDB(t)
	admin := createAuthTestUser(t, database, "admin", "admin", true)
	createAuthTestAPIToken(t, database, admin.ID, "readwrite-api-token", "readwrite")
	r := gin.New()
	r.POST("/refresh", JWTOnly(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	if rec := authRequest(r, http.MethodPost, "/refresh", "readwrite-api-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("API token refresh status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticateRejectsInvalidCredentialState(t *testing.T) {
	database := newAuthTestDB(t)
	user := createAuthTestUser(t, database, "operator", "admin", true)
	jwtToken, err := GenerateJWT(authTestSecret, user.ID, user.Username, user.Role, time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	createAuthTestAPIToken(t, database, user.ID, "expired-api-token", "readonly")
	if err := database.Model(&db.APIToken{}).Where("name = ?", "expired-api-token").Update("expires_at", expired).Error; err != nil {
		t.Fatalf("expire API token: %v", err)
	}
	createAuthTestAPIToken(t, database, user.ID, "invalid-permissions-token", "owner")

	r := gin.New()
	r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, token := range []string{"expired-api-token", "invalid-permissions-token"} {
		if rec := authRequest(r, http.MethodGet, "/", token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q status = %d, body = %s", token, rec.Code, rec.Body.String())
		}
	}
	if err := database.Delete(&user).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if rec := authRequest(r, http.MethodGet, "/", jwtToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted JWT user status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticateSetsLegacyKeysFromCurrentUser(t *testing.T) {
	database := newAuthTestDB(t)
	user := createAuthTestUser(t, database, "original", "admin", true)
	token, err := GenerateJWT(authTestSecret, user.ID, "stale-name", "readonly", time.Hour)
	if err != nil {
		t.Fatalf("generate JWT: %v", err)
	}
	if err := database.Model(&user).Updates(map[string]any{"username": "current-name", "role": "admin"}).Error; err != nil {
		t.Fatalf("update user: %v", err)
	}
	r := gin.New()
	r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":  c.MustGet(ContextKeyUserID),
			"username": c.MustGet(ContextKeyUsername),
			"role":     c.MustGet(ContextKeyRole),
		})
	})
	rec := authRequest(r, http.MethodGet, "/", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var legacy struct {
		UserID   uint   `json:"user_id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy keys: %v", err)
	}
	if legacy.UserID != user.ID || legacy.Username != "current-name" || legacy.Role != "admin" {
		t.Fatalf("legacy keys = %#v", legacy)
	}
}

func TestJWTRequiresValidExpiration(t *testing.T) {
	database := newAuthTestDB(t)
	user := createAuthTestUser(t, database, "operator", "admin", true)

	withoutExpiry := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: user.ID, Username: user.Username, Role: user.Role,
	})
	withoutExpiryToken, err := withoutExpiry.SignedString([]byte(authTestSecret))
	if err != nil {
		t.Fatalf("sign JWT without expiry: %v", err)
	}
	expiredToken, err := GenerateJWT(authTestSecret, user.ID, user.Username, user.Role, -time.Minute)
	if err != nil {
		t.Fatalf("generate expired JWT: %v", err)
	}
	r := gin.New()
	r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, token := range []string{withoutExpiryToken, expiredToken} {
		if rec := authRequest(r, http.MethodGet, "/", token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid-expiration JWT status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}
}

func TestAuthenticateRejectsInvalidUsersAndJWTSignatures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, database *gorm.DB, user db.User) string
	}{
		{
			name: "API token disabled owner",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				createAuthTestAPIToken(t, database, user.ID, "disabled-owner-token", "readwrite")
				if err := database.Model(&user).Update("enabled", false).Error; err != nil {
					t.Fatalf("disable owner: %v", err)
				}
				return "disabled-owner-token"
			},
		},
		{
			name: "API token deleted owner",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				createAuthTestAPIToken(t, database, user.ID, "deleted-owner-token", "readwrite")
				if err := database.Delete(&user).Error; err != nil {
					t.Fatalf("delete owner: %v", err)
				}
				return "deleted-owner-token"
			},
		},
		{
			name: "API token unsupported owner role",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				createAuthTestAPIToken(t, database, user.ID, "unsupported-owner-token", "readwrite")
				if err := database.Model(&user).Update("role", "owner").Error; err != nil {
					t.Fatalf("change owner role: %v", err)
				}
				return "unsupported-owner-token"
			},
		},
		{
			name: "JWT unsupported current role",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				token, err := GenerateJWT(authTestSecret, user.ID, user.Username, "admin", time.Hour)
				if err != nil {
					t.Fatalf("generate JWT: %v", err)
				}
				if err := database.Model(&user).Update("role", "owner").Error; err != nil {
					t.Fatalf("change user role: %v", err)
				}
				return token
			},
		},
		{
			name: "JWT wrong algorithm",
			setup: func(t *testing.T, _ *gorm.DB, user db.User) string {
				claims := Claims{UserID: user.ID, Username: user.Username, Role: user.Role, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(authTestSecret))
				if err != nil {
					t.Fatalf("sign HS384 JWT: %v", err)
				}
				return token
			},
		},
		{
			name: "JWT wrong signature",
			setup: func(t *testing.T, _ *gorm.DB, user db.User) string {
				token, err := GenerateJWT("abcdef0123456789abcdef0123456789", user.ID, user.Username, user.Role, time.Hour)
				if err != nil {
					t.Fatalf("generate wrong-signature JWT: %v", err)
				}
				return token
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := newAuthTestDB(t)
			user := createAuthTestUser(t, database, "operator", "admin", true)
			token := tt.setup(t, database, user)
			r := gin.New()
			r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			assertGenericUnauthorized(t, authRequest(r, http.MethodGet, "/", token), token)
		})
	}
}

func TestAPITokenLastUsedAtAndUpdateFailure(t *testing.T) {
	t.Run("updates only after authoritative validation", func(t *testing.T) {
		database := newAuthTestDB(t)
		user := createAuthTestUser(t, database, "admin", "admin", true)
		createAuthTestAPIToken(t, database, user.ID, "valid-token", "readwrite")
		createAuthTestAPIToken(t, database, user.ID, "expired-token", "readwrite")
		createAuthTestAPIToken(t, database, user.ID, "invalid-permission-token", "owner")
		createAuthTestAPIToken(t, database, user.ID, "disabled-owner-token", "readwrite")
		expiredAt := time.Now().Add(-time.Minute)
		if err := database.Model(&db.APIToken{}).Where("name = ?", "expired-token").Update("expires_at", expiredAt).Error; err != nil {
			t.Fatalf("expire token: %v", err)
		}

		r := gin.New()
		r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		if rec := authRequest(r, http.MethodGet, "/", "valid-token"); rec.Code != http.StatusNoContent {
			t.Fatalf("valid token status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if err := database.Model(&user).Update("enabled", false).Error; err != nil {
			t.Fatalf("disable owner: %v", err)
		}
		for _, raw := range []string{"expired-token", "invalid-permission-token", "disabled-owner-token"} {
			assertGenericUnauthorized(t, authRequest(r, http.MethodGet, "/", raw), raw)
		}

		var tokens []db.APIToken
		if err := database.Order("id").Find(&tokens).Error; err != nil {
			t.Fatalf("load tokens: %v", err)
		}
		if len(tokens) != 4 || tokens[0].LastUsedAt == nil {
			t.Fatalf("valid token last_used_at was not updated: %#v", tokens)
		}
		for _, token := range tokens[1:] {
			if token.LastUsedAt != nil {
				t.Fatalf("invalid token %q last_used_at = %v", token.Name, token.LastUsedAt)
			}
		}
	})

	t.Run("last used update failure does not change authority", func(t *testing.T) {
		database := newAuthTestDB(t)
		user := createAuthTestUser(t, database, "admin", "admin", true)
		createAuthTestAPIToken(t, database, user.ID, "update-failure-token", "readwrite")
		if err := database.Exec(`CREATE TRIGGER fail_last_used BEFORE UPDATE OF last_used_at ON api_tokens BEGIN SELECT RAISE(ABORT, 'injected update failure'); END`).Error; err != nil {
			t.Fatalf("create update trigger: %v", err)
		}
		r := gin.New()
		r.POST("/", Authenticate(authTestSecret, database), WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		rec := authRequest(r, http.MethodPost, "/", "update-failure-token")
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var token db.APIToken
		if err := database.Where("name = ?", "update-failure-token").First(&token).Error; err != nil {
			t.Fatalf("reload token: %v", err)
		}
		if token.LastUsedAt != nil {
			t.Fatalf("last_used_at = %v", token.LastUsedAt)
		}
	})
}

func TestAuthenticateDatabaseFailuresAreGeneric(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, database *gorm.DB, user db.User) string
	}{
		{
			name: "API token lookup failure",
			setup: func(t *testing.T, database *gorm.DB, _ db.User) string {
				if err := database.Migrator().DropTable(&db.APIToken{}); err != nil {
					t.Fatalf("drop API token table: %v", err)
				}
				return "lookup-failure-token"
			},
		},
		{
			name: "API token user lookup failure",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				createAuthTestAPIToken(t, database, user.ID, "user-lookup-failure-token", "readwrite")
				if err := database.Migrator().DropTable(&db.User{}); err != nil {
					t.Fatalf("drop user table: %v", err)
				}
				return "user-lookup-failure-token"
			},
		},
		{
			name: "JWT user lookup failure",
			setup: func(t *testing.T, database *gorm.DB, user db.User) string {
				token, err := GenerateJWT(authTestSecret, user.ID, user.Username, user.Role, time.Hour)
				if err != nil {
					t.Fatalf("generate JWT: %v", err)
				}
				if err := database.Migrator().DropTable(&db.User{}); err != nil {
					t.Fatalf("drop user table: %v", err)
				}
				return token
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := newAuthTestDB(t)
			user := createAuthTestUser(t, database, "operator", "admin", true)
			token := tt.setup(t, database, user)
			r := gin.New()
			r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			assertGenericUnauthorized(t, authRequest(r, http.MethodGet, "/", token), token)
		})
	}
}

func TestCompatibilityMiddlewareWrappersAreRemoved(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "auth.go", nil, 0)
	if err != nil {
		t.Fatalf("parse auth.go: %v", err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch function.Name.Name {
		case "JWTAuth", "AdminRequired":
			t.Errorf("obsolete compatibility wrapper %s is still declared", function.Name.Name)
		}
	}
}

func runConcurrentAuthPhase(t *testing.T, r *gin.Engine, token string, count int) []int {
	t.Helper()
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(count)
	done.Add(count)
	statuses := make([]int, count)
	for i := range count {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			statuses[index] = authRequest(r, http.MethodGet, "/", token).Code
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return statuses
}

func TestConcurrentAuthenticationRejectsCommittedMutations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, database *gorm.DB, user db.User, rawToken string)
	}{
		{
			name: "owner disabled",
			mutate: func(t *testing.T, database *gorm.DB, user db.User, _ string) {
				if err := database.Model(&user).Update("enabled", false).Error; err != nil {
					t.Fatalf("disable owner: %v", err)
				}
			},
		},
		{
			name: "token deleted",
			mutate: func(t *testing.T, database *gorm.DB, _ db.User, rawToken string) {
				digest := sha256.Sum256([]byte(rawToken))
				if err := database.Where("token_hash = ?", hex.EncodeToString(digest[:])).Delete(&db.APIToken{}).Error; err != nil {
					t.Fatalf("delete token: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := newAuthTestDB(t)
			user := createAuthTestUser(t, database, "admin", "admin", true)
			rawToken := "concurrent-" + strings.ReplaceAll(tt.name, " ", "-")
			createAuthTestAPIToken(t, database, user.ID, rawToken, "readwrite")
			r := gin.New()
			r.GET("/", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusNoContent) })

			for _, status := range runConcurrentAuthPhase(t, r, rawToken, 16) {
				if status != http.StatusNoContent {
					t.Fatalf("pre-mutation status = %d", status)
				}
			}
			tt.mutate(t, database, user, rawToken)
			for _, status := range runConcurrentAuthPhase(t, r, rawToken, 16) {
				if status != http.StatusUnauthorized {
					t.Fatalf("post-mutation status = %d", status)
				}
			}
			assertGenericUnauthorized(t, authRequest(r, http.MethodGet, "/", rawToken), rawToken)
		})
	}
}
