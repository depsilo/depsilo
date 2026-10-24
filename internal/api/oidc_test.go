package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
)

func newOIDCAPI(t *testing.T, enabled bool) (*gin.Engine, *gorm.DB, config.AuthConfig, string, *OIDCHandler) {
	t.Helper()
	database := newAPIAuthTestDB(t)
	if err := database.AutoMigrate(&db.ExternalIdentity{}, &db.IdentitySession{}, &db.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gin.H{"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize", "token_endpoint": provider.URL + "/token", "jwks_uri": provider.URL + "/keys", "userinfo_endpoint": provider.URL + "/userinfo"})
	}))
	t.Cleanup(provider.Close)
	auth := config.AuthConfig{JWTSecret: "test-secret-012345678901234567890123", TokenTTL: time.Hour, OIDC: config.OIDCConfig{Enabled: enabled, DisplayName: "Example Login", Issuer: provider.URL, ClientID: "client", ClientSecret: "never-return-this-secret", RedirectURL: "http://127.0.0.1:18081/api/v1/auth/oidc/callback", AdminGroups: []string{"admins"}, GroupsClaim: "groups", SessionTTL: 30 * time.Minute, AllowInsecureLoopback: true}}
	m, err := identity.NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	h := NewOIDCHandler(m, database, auth)
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	user := db.User{Username: "local-admin", Role: "admin", Enabled: true, PasswordHash: string(hash), CredentialVersion: 1, PermissionSource: "local"}
	if err := database.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token, err := middleware.GenerateJWT(auth.JWTSecret, user, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.GET("/api/v1/auth/providers", h.Providers)
	r.POST("/api/v1/auth/oidc/start", h.Start)
	r.GET("/api/v1/auth/oidc/callback", h.Callback)
	r.GET("/api/v1/auth/oidc/:provider_id/callback", h.Callback)
	r.POST("/api/v1/auth/oidc/complete", middleware.OptionalLogoutPrincipal(auth.JWTSecret, database), h.Complete)
	r.POST("/api/v1/auth/oidc/resolve", h.Resolve)
	r.POST("/api/v1/auth/oidc/cancel", h.Cancel)
	g := r.Group("/api/v1/auth", middleware.JWTOnly(auth.JWTSecret, database))
	g.GET("/identities", h.Identities)
	g.POST("/identities/link", h.Link)
	g.POST("/identities/:id/unlink", h.Unlink)
	return r, database, auth, token, h
}

func oidcRequest(r *gin.Engine, method, path, body, token, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1:18081"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestOIDCAPIProviderAndBrowserBoundStart(t *testing.T) {
	r, _, _, _, _ := newOIDCAPI(t, true)
	providers := oidcRequest(r, "GET", "/api/v1/auth/providers", "", "", "")
	if providers.Code != 200 || !strings.Contains(providers.Body.String(), "Example Login") || strings.Contains(providers.Body.String(), "never-return") {
		t.Fatalf("providers: %s", providers.Body)
	}
	for _, origin := range []string{"https://evil.test", "null", "http://127.0.0.1:18080"} {
		resp := oidcRequest(r, "POST", "/api/v1/auth/oidc/start", `{}`, "", origin)
		if resp.Code != 403 {
			t.Fatal("cross-origin start allowed")
		}
	}
	req := httptest.NewRequest("POST", "/api/v1/auth/oidc/start", strings.NewReader("return_to=/admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal("form login CSRF accepted")
	}
	start := oidcRequest(r, "POST", "/api/v1/auth/oidc/start", `{"return_to":"/admin/cache"}`, "", "http://127.0.0.1:18081")
	if start.Code != 200 {
		t.Fatalf("start: %s", start.Body)
	}
	var payload struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	_ = json.Unmarshal(start.Body.Bytes(), &payload)
	u, _ := url.Parse(payload.AuthorizationURL)
	if u.Query().Get("state") == "" || u.Query().Get("nonce") == "" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("client_secret") != "" {
		t.Fatal("invalid authorization URL")
	}
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("flow cookie lacks browser protections")
	}
	callback := oidcRequest(r, "GET", "/api/v1/auth/oidc/callback?state=invalid&code=secret-code", "", "", "")
	if callback.Code != 303 || callback.Header().Get("Location") != "/admin/auth/complete" || strings.Contains(callback.Body.String(), "secret-code") {
		t.Fatal("callback leaked code or wrong redirect")
	}
	complete := oidcRequest(r, "POST", "/api/v1/auth/oidc/complete", `{}`, "", "http://127.0.0.1:18081")
	if complete.Code != 400 || !strings.Contains(complete.Body.String(), "LOGIN_EXPIRED") {
		t.Fatalf("missing completion: %s", complete.Body)
	}
}

func TestOIDCAPIProofErrorsDoNotLogOutAndAreBounded(t *testing.T) {
	r, database, _, token, _ := newOIDCAPI(t, true)
	for i := 0; i < 5; i++ {
		rec := oidcRequest(r, "POST", "/api/v1/auth/identities/link", `{"password":"wrong"}`, token, "http://127.0.0.1:18081")
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), "REAUTHENTICATION_FAILED") {
			t.Fatalf("attempt %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := oidcRequest(r, "POST", "/api/v1/auth/identities/link", `{"password":"correct-password"}`, token, "http://127.0.0.1:18081")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatal("password proof unbounded")
	}
	rec = oidcRequest(r, "GET", "/api/v1/auth/identities", "", token, "")
	if rec.Code != 200 {
		t.Fatal("failed reauthentication invalidated local session")
	}
	var denials int64
	database.Model(&db.AuditLog{}).Where("action = ?", "identity.denied").Count(&denials)
	if denials != 5 {
		t.Fatalf("denials audit = %d", denials)
	}
}

func TestOIDCAPIUnlinkWorksAfterProviderRemoval(t *testing.T) {
	r, database, _, token, h := newOIDCAPI(t, false)
	var user db.User
	database.Where("username = ?", "local-admin").First(&user)
	linked := db.ExternalIdentity{UserID: user.ID, Provider: "oidc", Issuer: "https://old-provider.test", Subject: "old-sub", DisplayName: "External name"}
	if err := database.Create(&linked).Error; err != nil {
		t.Fatal(err)
	}
	h.auth.OIDC = config.OIDCConfig{} // The operator removed the configuration.
	var err error
	h.registry, err = identity.NewRegistry(database, h.auth)
	if err != nil {
		t.Fatal(err)
	}
	list := oidcRequest(r, "GET", "/api/v1/auth/identities", "", token, "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"can_unlink":true`) || strings.Contains(list.Body.String(), "old-sub") || strings.Contains(list.Body.String(), "never-return") {
		t.Fatalf("identity view: %s", list.Body)
	}
	resp := oidcRequest(r, "POST", fmt.Sprintf("/api/v1/auth/identities/%d/unlink", linked.ID), `{"password":"correct-password"}`, token, "http://127.0.0.1:18081")
	if resp.Code != 204 {
		t.Fatalf("disabled provider unlink: %d %s", resp.Code, resp.Body)
	}
	var count int64
	database.Model(&db.ExternalIdentity{}).Where("id = ?", linked.ID).Count(&count)
	if count != 0 {
		t.Fatal("identity not removed")
	}
	if oidcRequest(r, "GET", "/api/v1/auth/identities", "", token, "").Code != 200 {
		t.Fatal("unlink revoked password session")
	}
}

func TestOIDCAPIManagedIdentityCannotBeUnlinked(t *testing.T) {
	r, database, auth, _, _ := newOIDCAPI(t, true)
	user := db.User{Username: "oidc-account", Role: "admin", PermissionSource: "oidc", Enabled: true, CredentialVersion: 1}
	database.Create(&user)
	linked := db.ExternalIdentity{UserID: user.ID, Provider: "oidc", Issuer: auth.OIDC.Issuer, Subject: "managed"}
	database.Create(&linked)
	session := db.IdentitySession{ID: "managed-session", UserID: user.ID, IdentityID: linked.ID, Role: "admin", ConfigFingerprint: "configured", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(30 * time.Minute)}
	database.Create(&session)
	token, err := middleware.GenerateIdentityJWT(auth.JWTSecret, user, session, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	list := oidcRequest(r, "GET", "/api/v1/auth/identities", "", token, "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"can_unlink":false`) || !strings.Contains(list.Body.String(), `"can_link":false`) {
		t.Fatalf("managed list: %s", list.Body)
	}
	resp := oidcRequest(r, "POST", fmt.Sprintf("/api/v1/auth/identities/%d/unlink", linked.ID), `{"password":"correct-password"}`, token, "http://127.0.0.1:18081")
	if resp.Code != 403 {
		t.Fatalf("managed unlink accepted: %d", resp.Code)
	}
}

func TestOIDCOriginUsesDefaultPortEquivalence(t *testing.T) {
	a, _ := url.Parse("https://depsilo.example:443/api/v1/auth/oidc/callback")
	b, _ := url.Parse("https://depsilo.example")
	if !sameOIDCOrigin(a, b) {
		t.Fatal("default TLS port rejected")
	}
}

func TestOIDCAPIIdentityAvailabilityUsesBoundIssuer(t *testing.T) {
	for _, test := range []struct {
		name                       string
		enabled                    bool
		provider, issuerSuffix     string
		wantMatches, wantAvailable bool
	}{
		{"active current issuer", true, "oidc", "", true, true},
		{"disabled current issuer", false, "oidc", "", true, false},
		{"issuer changed", true, "oidc", "/previous", false, false},
		{"issuer comparison exact", true, "oidc", "/", false, false},
		{"provider changed", true, "previous-provider", "", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, database, auth, token, _ := newOIDCAPI(t, test.enabled)
			var user db.User
			if err := database.Where("username = ?", "local-admin").First(&user).Error; err != nil {
				t.Fatal(err)
			}
			timestamp := time.Now().UTC().Truncate(time.Second)
			row := db.ExternalIdentity{UserID: user.ID, Provider: test.provider, Issuer: auth.OIDC.Issuer + test.issuerSuffix, Subject: "private-subject", DisplayName: "Bound User", CreatedAt: timestamp, LastLoginAt: timestamp}
			if err := database.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			response := oidcRequest(router, http.MethodGet, "/api/v1/auth/identities", "", token, "")
			if response.Code != http.StatusOK {
				t.Fatalf("identities status: %d", response.Code)
			}
			var body struct {
				Identities []struct {
					ID              uint      `json:"id"`
					ProviderName    string    `json:"provider_name"`
					DisplayName     string    `json:"display_name"`
					ProviderMatches bool      `json:"provider_matches"`
					LoginAvailable  bool      `json:"login_available"`
					CanUnlink       bool      `json:"can_unlink"`
					CreatedAt       time.Time `json:"created_at"`
					LastLoginAt     time.Time `json:"last_login_at"`
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Identities) != 1 {
				t.Fatal("identity association was lost")
			}
			got := body.Identities[0]
			if got.ProviderMatches != test.wantMatches || got.LoginAvailable != test.wantAvailable {
				t.Fatal("availability attributed to the wrong issuer")
			}
			wantName := "OIDC"
			if test.wantMatches {
				wantName = auth.OIDC.DisplayName
			}
			if got.ProviderName != wantName || got.ID != row.ID || got.DisplayName != row.DisplayName || !got.CanUnlink || !got.CreatedAt.Equal(timestamp) || !got.LastLoginAt.Equal(timestamp) {
				t.Fatal("historical binding details changed")
			}
			if strings.Contains(response.Body.String(), "private-subject") || strings.Contains(response.Body.String(), auth.OIDC.ClientSecret) {
				t.Fatal("private identity information exposed")
			}
		})
	}
}
