package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/config"
	"depsilo/internal/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func authenticationSourcesFixture(t *testing.T, principal middleware.Principal) (*gin.Engine, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# unrelated configuration\n[server]\nlog_level=\"info\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", strings.Repeat("a", 64))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthenticationSourceHandler(config.NewStore(path, cfg, zap.NewAtomicLevel()))
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.ContextKeyPrincipal, principal) })
	router.GET("/sources", AdminRoleRequired(), handler.List)
	writes := router.Group("", AdminRoleRequired(), middleware.WriteRequired(), middleware.CredentialManagementRequired())
	writes.POST("/sources", handler.Create)
	writes.PUT("/sources/:id", handler.Update)
	writes.DELETE("/sources/:id", handler.Delete)
	writes.POST("/sources/:id/restore", handler.Restore)
	return router, path
}

func sourceRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	req := httptest.NewRequest(method, "http://127.0.0.1:18081"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, req)
	return response
}

func TestAuthenticationSourceHandlersLifecycleAndImmutableIdentity(t *testing.T) {
	router, _ := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
	response := sourceRequest(router, http.MethodGet, "/sources", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"sources":[]`) {
		t.Fatalf("unexpected empty list: %s", response.Body)
	}
	response = sourceRequest(router, http.MethodPost, "/sources", `{"id":"company","protocol":"oidc","display_name":"Company","client_secret":"write-only-secret"}`)
	if response.Code != 201 {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}
	var state config.AuthenticationSourceState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.ID != "company" || state.Configured == nil || !state.Configured.ClientSecretSet || state.Effective != nil || !state.PendingRestart {
		t.Fatal("wrong create state")
	}
	if strings.Contains(response.Body.String(), "write-only-secret") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret leak or caching allowed")
	}
	response = sourceRequest(router, http.MethodPut, "/sources/company", `{"display_name":"Renamed"}`)
	if response.Code != 200 {
		t.Fatalf("update: %s", response.Body)
	}
	for _, body := range []string{`{"id":"new"}`, `{"protocol":"ldap"}`} {
		if response = sourceRequest(router, http.MethodPut, "/sources/company", body); response.Code != 400 {
			t.Fatal("mutable identity accepted")
		}
	}
	response = sourceRequest(router, http.MethodDelete, "/sources/company", "")
	if response.Code != 204 {
		t.Fatalf("delete: %s", response.Body)
	}
	response = sourceRequest(router, http.MethodPut, "/sources/company", `{"display_name":"Gone"}`)
	if response.Code != 404 {
		t.Fatal("missing source not reported")
	}
}

func TestAuthenticationSourceHandlersRejectAmbiguousInput(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"id":"company","protocol":"oidc","id":"other"}`, `{"id":"company","Protocol":"oidc"}`,
		`{"id":"company","protocol":"oidc","client_secret":null}`, `{"id":"company","protocol":"oidc","unknown":"private-value"}`,
		`{"id":"company","protocol":"oidc","client_secret":123}`, `{"id":"company","protocol":"oidc"} {}`,
		`{"id":"company","protocol":"saml"}`, `{"id":"oidc","protocol":"oidc"}`, `{"id":"../bad","protocol":"oidc"}`,
	} {
		t.Run(body[:min(40, len(body))], func(t *testing.T) {
			router, path := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
			before, _ := os.ReadFile(path)
			response := sourceRequest(router, http.MethodPost, "/sources", body)
			if response.Code != 400 && response.Code != 422 {
				t.Fatalf("unexpected status %d: %s", response.Code, response.Body)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) || strings.Contains(response.Body.String(), "private-value") {
				t.Fatal("bad request changed file or echoed secret")
			}
		})
	}
}

func TestAuthenticationSourceHandlersRequireLocalCredentialAuthority(t *testing.T) {
	for _, test := range []struct {
		name        string
		principal   middleware.Principal
		read, write int
	}{
		{"local admin", middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true}, 200, 201},
		{"external admin", middleware.Principal{Role: "admin", CanWrite: true, LoginMethod: "oidc"}, 200, 403},
		{"admin read token", middleware.Principal{Role: "admin", AuthMethod: "api_token"}, 200, 403},
		{"readonly", middleware.Principal{Role: "readonly"}, 403, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, _ := authenticationSourcesFixture(t, test.principal)
			if response := sourceRequest(router, http.MethodGet, "/sources", ""); response.Code != test.read {
				t.Fatalf("read got %d", response.Code)
			}
			if response := sourceRequest(router, http.MethodPost, "/sources", `{"id":"company","protocol":"oidc"}`); response.Code != test.write {
				t.Fatalf("write got %d: %s", response.Code, response.Body)
			}
		})
	}
}

func TestAuthenticationSourceFieldErrorsDoNotReflectValues(t *testing.T) {
	router, _ := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
	response := sourceRequest(router, http.MethodPost, "/sources", `{"id":"company","protocol":"oidc","display_name":"Company","issuer":"https://private-supplied-secret@identity.example"}`)
	if response.Code != 422 {
		t.Fatalf("status %d", response.Code)
	}
	var payload struct {
		FieldErrors map[string]string `json:"field_errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.FieldErrors["issuer"] != "INVALID_URL" || strings.Contains(response.Body.String(), "private-supplied-secret") {
		t.Fatal("missing safe field error")
	}
	for _, body := range []string{`null`, `{"enabled":true}`, `{} {}`, `{"client_secret":"ignored"}`} {
		response = sourceRequest(router, http.MethodPost, "/sources/company/restore", body)
		if response.Code != 400 {
			t.Fatalf("restore accepted nonempty or ambiguous payload: %d", response.Code)
		}
	}
}

func TestLDAPSourceHandlerUsesTypedSettingsAndWriteOnlyBindSecret(t *testing.T) {
	router, path := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
	response := sourceRequest(router, http.MethodPost, "/sources", `{"id":"directory","protocol":"ldap","display_name":"Directory","ldap":{"url":"ldaps://directory.example","bind_dn":"cn=service,dc=example","bind_password":"private-bind-secret","user_base_dn":"ou=people,dc=example"}}`)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "private-bind-secret") || !strings.Contains(response.Body.String(), `"bind_password_set":true`) {
		t.Fatalf("LDAP creation: %d %s", response.Code, response.Body)
	}
	var source config.AuthenticationSourceState
	if err := json.Unmarshal(response.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.Protocol != "ldap" || source.Configured == nil || source.Configured.LDAP == nil || source.Configured.LDAP.URL != "ldaps://directory.example" || source.Effective != nil || !source.PendingRestart {
		t.Fatal("LDAP source state lost its protocol or typed settings")
	}
	response = sourceRequest(router, http.MethodPut, "/sources/directory", `{"display_name":"Renamed directory","ldap":{"user_filter":"(mail={username})"}}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"bind_password_set":true`) || strings.Contains(response.Body.String(), "private-bind-secret") {
		t.Fatalf("LDAP update: %d %s", response.Code, response.Body)
	}
	before, _ := os.ReadFile(path)
	for _, body := range []string{
		`{"issuer":"https://other.example"}`, `{"client_secret":"private-input"}`, `{"ldap":null}`,
		`{"ldap":{"bind_password":null}}`, `{"ldap":{"bind_password":true}}`,
		`{"ldap":{"bind_password":"private-input","bind_password":"other"}}`,
		`{"ldap":{"BindPassword":"private-input"}}`, `{"ldap":{"skip_verify":true}}`,
	} {
		response = sourceRequest(router, http.MethodPut, "/sources/directory", body)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-input") {
			t.Fatalf("LDAP accepted ambiguous or other-protocol settings: %d %s", response.Code, response.Body)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid LDAP input changed persisted configuration")
	}
	response = sourceRequest(router, http.MethodPost, "/sources", `{"id":"mixed","protocol":"oidc","ldap":{"url":"ldaps://directory.example"}}`)
	if response.Code != http.StatusBadRequest {
		t.Fatal("OIDC source accepted directory settings")
	}
	response = sourceRequest(router, http.MethodDelete, "/sources/directory", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("LDAP removal: %d %s", response.Code, response.Body)
	}
}

func TestLDAPSourceSecretsRequireSecureTransportWithoutBlockingOtherEdits(t *testing.T) {
	for _, sample := range []struct {
		name, originURL, origin, forwarded string
		allowed                            bool
	}{
		{"remote HTTP", "http://depsilo.example", "", "", false},
		{"remote HTTP browser", "http://depsilo.example", "http://depsilo.example", "", false},
		{"forwarding header is not proof", "http://depsilo.example", "", "https", false},
		{"foreign HTTPS origin", "http://depsilo.example", "https://foreign.example", "", false},
		{"direct HTTPS API", "https://depsilo.example", "", "", true},
		{"same host TLS-terminated browser", "http://depsilo.example", "https://depsilo.example", "", true},
		{"loopback IPv4", "http://127.0.0.1:18081", "", "", true},
		{"loopback hostname", "http://localhost:18081", "http://localhost:18081", "", true},
		{"loopback IPv6", "http://[::1]:18081", "", "", true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			router, path := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
			request := func(method, route, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, sample.originURL+route, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if sample.origin != "" {
					req.Header.Set("Origin", sample.origin)
				}
				if sample.forwarded != "" {
					req.Header.Set("X-Forwarded-Proto", sample.forwarded)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return response
			}
			before, _ := os.ReadFile(path)
			response := request(http.MethodPost, "/sources", `{"id":"directory","protocol":"ldap","display_name":"Directory","ldap":{"bind_password":"candidate-private-secret"}}`)
			if sample.allowed {
				if response.Code != http.StatusCreated {
					t.Fatalf("secure create: %d %s", response.Code, response.Body)
				}
			} else {
				if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "HTTPS_REQUIRED") {
					t.Fatalf("insecure secret create accepted: %d %s", response.Code, response.Body)
				}
				after, _ := os.ReadFile(path)
				if string(before) != string(after) {
					t.Fatal("rejected secret creation changed configuration")
				}
				response = request(http.MethodPost, "/sources", `{"id":"directory","protocol":"ldap","display_name":"Directory"}`)
				if response.Code != http.StatusCreated {
					t.Fatalf("non-secret draft blocked: %d %s", response.Code, response.Body)
				}
			}
			before, _ = os.ReadFile(path)
			response = request(http.MethodPut, "/sources/directory", `{"ldap":{"bind_password":"replacement-private-secret"}}`)
			if sample.allowed {
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"bind_password_set":true`) {
					t.Fatalf("secure update: %d %s", response.Code, response.Body)
				}
			} else {
				if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "HTTPS_REQUIRED") {
					t.Fatalf("insecure secret update accepted: %d %s", response.Code, response.Body)
				}
				after, _ := os.ReadFile(path)
				if string(before) != string(after) {
					t.Fatal("rejected secret update changed configuration")
				}
			}
			if strings.Contains(response.Body.String(), "private-secret") {
				t.Fatal("response reflected the submitted directory password")
			}
			response = request(http.MethodPut, "/sources/directory", `{"display_name":"Renamed directory"}`)
			if response.Code != http.StatusOK {
				t.Fatalf("non-secret update blocked: %d %s", response.Code, response.Body)
			}
			response = request(http.MethodPost, "/sources", `{"id":"browser-login","protocol":"oidc","client_secret":"existing-oidc-contract"}`)
			if response.Code != http.StatusCreated {
				t.Fatalf("unrelated OIDC policy changed: %d %s", response.Code, response.Body)
			}
		})
	}
}
