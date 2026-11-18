package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/middleware"
)

func TestAuthenticationSourceUpdateRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"enabled":null}`, `{"client_secret":null}`, `{"enabled":true,"enabled":false}`, `{"enabled":true,"Enabled":false}`, `{"DisplayName":"alias"}`,
		`{"unknown":"private-secret"}`, `{"client_secret_set":true}`, `{"client_secret":123}`, `{"admin_groups":"a"}`,
		`{"admin_groups":[null]}`, `{"enabled":false} {}`, `{"display_name":"` + strings.Repeat("x", 65536) + `"}`,
	} {
		t.Run(body[:min(len(body), 40)], func(t *testing.T) {
			router, path := authenticationSourcesFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
			if response := sourceRequest(router, http.MethodPost, "/sources", `{"id":"company","protocol":"oidc","client_secret":"private-secret"}`); response.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", response.Code, response.Body)
			}
			before, _ := os.ReadFile(path)
			recorder := sourceRequest(router, http.MethodPut, "/sources/company", body)
			if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d", recorder.Code)
			}
			if strings.Contains(recorder.Body.String(), "private-secret") {
				t.Fatal("bad request echoed input")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("bad request modified config")
			}
		})
	}
}

func TestAuthenticationAdminRoleGuard(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal middleware.Principal
		want      int
	}{
		{"local admin", middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true}, http.StatusOK},
		{"external admin", middleware.Principal{Role: "admin", CanWrite: true, LoginMethod: "oidc"}, http.StatusOK},
		{"admin read token", middleware.Principal{Role: "admin", AuthMethod: "api_token"}, http.StatusOK},
		{"readonly", middleware.Principal{Role: "readonly"}, http.StatusForbidden},
		{"missing", middleware.Principal{}, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(middleware.ContextKeyPrincipal, test.principal) })
			router.GET("/", AdminRoleRequired(), func(c *gin.Context) { c.Status(http.StatusOK) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != test.want {
				t.Fatalf("got %d", response.Code)
			}
		})
	}
}
