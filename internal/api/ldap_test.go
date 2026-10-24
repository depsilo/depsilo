package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"depsilo/internal/api/admin"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func newLDAPAPI(t *testing.T) (*gin.Engine, string, *OIDCHandler) {
	t.Helper()
	router, database, auth, token, h := newOIDCAPI(t, false)
	auth.OIDC = config.OIDCConfig{}
	auth.Sources = map[string]config.AuthenticationSourceConfig{"directory": {
		Protocol: "ldap", OIDCConfig: config.OIDCConfig{Enabled: true, DisplayName: "Company directory", AdminGroups: []string{"cn=admins,ou=groups,dc=example,dc=test"}, SessionTTL: 30 * time.Minute},
		LDAP: &config.LDAPConfig{URL: "ldaps://127.0.0.1:1", BindDN: "cn=service,dc=example,dc=test", BindPassword: "private-bind-secret", UserBaseDN: "ou=users,dc=example,dc=test", UserFilter: "(uid={username})", SubjectAttribute: "entryUUID", DisplayNameAttribute: "cn", GroupsAttribute: "memberOf"},
	}}
	var err error
	h.registry, err = identity.NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	router.POST("/api/v1/auth/ldap/login", h.LDAPLogin)
	router.POST("/api/v1/auth/identities/ldap/link", middleware.JWTOnly(auth.JWTSecret, database), h.LDAPLink)
	router.POST("/api/v1/auth/identity/complete", middleware.OptionalLogoutPrincipal(auth.JWTSecret, database), h.Complete)
	router.POST("/api/v1/auth/identity/resolve", h.Resolve)
	router.POST("/api/v1/auth/identity/cancel", h.Cancel)
	h.ConfigureDraftPreview(ldapDraftFixture{source: auth.Sources["directory"]})
	adminGroup := router.Group("/api/v1/admin", middleware.Authenticate(auth.JWTSecret, database), admin.AdminRoleRequired(), middleware.CredentialManagementRequired())
	adminGroup.POST("/authentication/ldap/check", h.LDAPCheck)
	adminGroup.POST("/authentication/ldap/trial", h.LDAPTrial)
	return router, token, h
}

type ldapDraftFixture struct {
	source config.AuthenticationSourceConfig
}

func (f ldapDraftFixture) PreviewAuthenticationSourceDraft(context.Context, config.AuthenticationSourceDraft) (config.OIDCConfig, error) {
	return config.OIDCConfig{}, identity.ErrUnavailable
}
func (f ldapDraftFixture) PreviewLDAPAuthenticationSourceDraft(context.Context, config.AuthenticationSourceDraft) (config.AuthenticationSourceConfig, error) {
	return f.source, nil
}

func ldapHTTPRequest(router *gin.Engine, host, path, body, token, origin, site string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestLDAPHTTPRequiresSecureSameOriginCredentials(t *testing.T) {
	for _, test := range []struct{ name, host, origin, site string }{
		{"missing origin", "127.0.0.1:18081", "", ""},
		{"cross origin", "127.0.0.1:18081", "https://foreign.example", ""},
		{"plain remote", "depsilo.example", "http://depsilo.example", ""},
		{"origin with path", "depsilo.example", "https://depsilo.example/path", ""},
		{"null origin", "127.0.0.1:18081", "null", ""},
		{"cross-site metadata", "depsilo.example", "https://depsilo.example", "cross-site"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, token, _ := newLDAPAPI(t)
			for _, path := range []string{"/api/v1/auth/ldap/login", "/api/v1/auth/identities/ldap/link", "/api/v1/admin/authentication/ldap/check", "/api/v1/admin/authentication/ldap/trial"} {
				response := ldapHTTPRequest(router, test.host, path, `{"password":"private-directory-secret"}`, token, test.origin, test.site)
				if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private-directory-secret") {
					t.Fatalf("%s: %d %s", path, response.Code, response.Body)
				}
			}
		})
	}
}

func TestLDAPHTTPSAndLoopbackReachCredentialValidation(t *testing.T) {
	for _, test := range []struct{ host, origin string }{
		{"depsilo.example", "https://depsilo.example"},
		{"localhost:18081", "http://localhost:18081"},
		{"[::1]:18081", "http://[::1]:18081"},
	} {
		t.Run(test.origin, func(t *testing.T) {
			router, _, _ := newLDAPAPI(t)
			response := ldapHTTPRequest(router, test.host, "/api/v1/auth/ldap/login", `{"provider_id":"directory","username":"alice","password":""}`, "", test.origin, "same-origin")
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "LDAP_AUTHENTICATION_FAILED") {
				t.Fatalf("valid origin did not reach credential validation: %d %s", response.Code, response.Body)
			}
		})
	}
	router, _, _ := newLDAPAPI(t)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18081/api/v1/auth/ldap/login", strings.NewReader("username=alice&password=private-secret"))
	req.Header.Set("Origin", "http://127.0.0.1:18081")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-secret") {
		t.Fatal("form-based directory login was accepted or reflected credentials")
	}
}

func TestLDAPHTTPRejectsAmbiguousCredentialInput(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"username":null}`, `{"username":"a","username":"b"}`, `{"Username":"a"}`,
		`{"password":"private-secret","extra":true}`, `{"password":1}`, `{} {}`,
		`{"password":"` + strings.Repeat("a", maxLoginRequestBody) + `"}`,
	} {
		t.Run(fmt.Sprintf("body-%d", len(body)), func(t *testing.T) {
			router, token, _ := newLDAPAPI(t)
			for _, path := range []string{"/api/v1/auth/ldap/login", "/api/v1/auth/identities/ldap/link"} {
				response := oidcRequest(router, http.MethodPost, path, body, token, "http://127.0.0.1:18081")
				if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-secret") {
					t.Fatalf("%s: %d %s", path, response.Code, response.Body)
				}
			}
		})
	}
}

func TestLDAPProviderMetadataAndWrongProtocolDoNotExposeSecrets(t *testing.T) {
	router, token, h := newLDAPAPI(t)
	for _, path := range []string{"/api/v1/auth/providers", "/api/v1/auth/identities"} {
		response := oidcRequest(router, http.MethodGet, path, "", token, "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"protocol":"ldap"`) || !strings.Contains(response.Body.String(), "Company directory") || strings.Contains(response.Body.String(), "private-bind-secret") || strings.Contains(response.Body.String(), "cn=service") {
			t.Fatalf("provider metadata: %d %s", response.Code, response.Body)
		}
	}
	for _, path := range []string{"/api/v1/auth/oidc/start", "/api/v1/auth/identities/link"} {
		response := oidcRequest(router, http.MethodPost, path, `{"provider_id":"directory","password":"correct-password"}`, token, "http://127.0.0.1:18081")
		if response.Code != http.StatusServiceUnavailable && response.Code != http.StatusForbidden {
			t.Fatalf("LDAP accepted through browser redirect endpoint: %d %s", response.Code, response.Body)
		}
	}
	var count int64
	h.db.Model(&db.ExternalIdentity{}).Count(&count)
	if count != 0 {
		t.Fatal("wrong-protocol request created a binding")
	}
}

func TestLDAPLinkChecksLocalPasswordBeforeDirectoryCredentials(t *testing.T) {
	router, token, h := newLDAPAPI(t)
	for range 5 {
		response := oidcRequest(router, http.MethodPost, "/api/v1/auth/identities/ldap/link", `{"provider_id":"directory","password":"incorrect","username":"alice","directory_password":"private-directory-secret"}`, token, "http://127.0.0.1:18081")
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "REAUTHENTICATION_FAILED") || strings.Contains(response.Body.String(), "private-directory-secret") {
			t.Fatalf("password proof: %d %s", response.Code, response.Body)
		}
	}
	response := oidcRequest(router, http.MethodPost, "/api/v1/auth/identities/ldap/link", `{"provider_id":"directory","password":"correct-password","username":"alice","directory_password":"private-directory-secret"}`, token, "http://127.0.0.1:18081")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatal("LDAP binding password proof is unbounded")
	}
	if response = oidcRequest(router, http.MethodGet, "/api/v1/auth/identities", "", token, ""); response.Code != http.StatusOK {
		t.Fatal("failed proof invalidated local recovery")
	}
	var identities, sessions int64
	h.db.Model(&db.ExternalIdentity{}).Count(&identities)
	h.db.Model(&db.IdentitySession{}).Count(&sessions)
	if identities != 0 || sessions != 0 {
		t.Fatal("failed binding persisted identity state")
	}
}

func TestLDAPDiagnosticsRejectMixedProtocolAndSecretAliases(t *testing.T) {
	for _, body := range []string{
		`{"id":"directory","protocol":"oidc","create":false,"settings":{}}`,
		`{"id":"directory","protocol":"ldap","settings":{}}`,
		`{"id":"directory","protocol":"ldap","create":false,"settings":{"issuer":"https://other.example"}}`,
		`{"id":"directory","protocol":"ldap","create":false,"settings":{"ldap":{"bind_password":null}}}`,
		`{"id":"directory","protocol":"ldap","create":false,"settings":{"ldap":{"bind_password":"private-secret","bind_password":"other"}}}`,
		`{"id":"directory","protocol":"ldap","create":false,"settings":{"ldap":{"BindPassword":"private-secret"}}}`,
	} {
		t.Run(fmt.Sprintf("body-%d", len(body)), func(t *testing.T) {
			router, token, _ := newLDAPAPI(t)
			for _, endpoint := range []string{"check", "trial"} {
				response := oidcRequest(router, http.MethodPost, "/api/v1/admin/authentication/ldap/"+endpoint, body, token, "http://127.0.0.1:18081")
				if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-secret") {
					t.Fatalf("LDAP draft accepted or echoed: %d %s", response.Code, response.Body)
				}
			}
		})
	}
}

func TestLDAPAuthenticationFailureIsUnavailableWithoutWrites(t *testing.T) {
	router, token, h := newLDAPAPI(t)
	response := oidcRequest(router, http.MethodPost, "/api/v1/auth/ldap/login", `{"provider_id":"directory","username":"alice","password":"private-directory-secret"}`, "", "http://127.0.0.1:18081")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "LDAP_UNAVAILABLE") || strings.Contains(response.Body.String(), "private-directory-secret") {
		t.Fatalf("connection failure: %d %s", response.Code, response.Body)
	}
	for _, path := range []string{"/api/v1/auth/identity/complete", "/api/v1/auth/identity/resolve"} {
		body := `{}`
		if strings.HasSuffix(path, "/resolve") {
			body = `{"action":"create"}`
		}
		if response = oidcRequest(router, http.MethodPost, path, body, "", "http://127.0.0.1:18081"); response.Code != http.StatusBadRequest {
			t.Fatalf("failed login acquired an account choice: %d %s", response.Code, response.Body)
		}
	}
	if response = oidcRequest(router, http.MethodGet, "/api/v1/auth/identities", "", token, ""); response.Code != http.StatusOK {
		t.Fatal("LDAP outage affected local recovery")
	}
	var identities, sessions int64
	h.db.Model(&db.ExternalIdentity{}).Count(&identities)
	h.db.Model(&db.IdentitySession{}).Count(&sessions)
	if identities != 0 || sessions != 0 {
		t.Fatal("LDAP failure wrote authentication state")
	}
}

func TestLDAPManagedUserCannotUseAccidentalLocalPassword(t *testing.T) {
	router, _, h := newLDAPAPI(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	user := db.User{Username: "directory-user", Role: "admin", Enabled: true, PermissionSource: "ldap", PasswordHash: string(hash)}
	if err := h.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAuthHandler(h.db, h.auth)
	router.POST("/api/v1/auth/login", auth.Login)
	response := oidcRequest(router, http.MethodPost, "/api/v1/auth/login", `{"username":"directory-user","password":"correct-password"}`, "", "")
	var body map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != http.StatusUnauthorized || body["token"] != nil {
		t.Fatal("LDAP-managed account escaped directory authority through a local password")
	}
}
