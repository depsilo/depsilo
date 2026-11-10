package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDistributionRoutesRequireAuthenticationAndWriteCapability(t *testing.T) {
	router, jwtToken, readToken := newReadAuthRouter(t)
	id := strings.Repeat("a", 64)
	for _, route := range []struct {
		method, path string
		write        bool
	}{
		{http.MethodGet, "/sets", false}, {http.MethodGet, "/sets/" + id, false}, {http.MethodGet, "/usage", false},
		{http.MethodPost, "/sets", true}, {http.MethodPut, "/sets/" + id + "/objects/" + id, true},
		{http.MethodPost, "/sets/" + id + "/verify", true}, {http.MethodDelete, "/sets/" + id, true},
		{http.MethodPost, "/prune", true},
		{http.MethodGet, "/sets/" + id + "/bundle", true}, {http.MethodPost, "/import", true},
	} {
		for _, auth := range []struct{ name, token string }{{"anonymous", ""}, {"readonly", readToken}, {"admin", jwtToken}} {
			t.Run(route.method+route.path+auth.name, func(t *testing.T) {
				request := httptest.NewRequest(route.method, "/api/v1/admin/distribution"+route.path, nil)
				if auth.token != "" {
					request.Header.Set("Authorization", "Bearer "+auth.token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusServiceUnavailable // fixture intentionally has no distribution store
				if auth.token == "" {
					want = http.StatusUnauthorized
				} else if route.write && auth.name == "readonly" {
					want = http.StatusForbidden
				}
				if response.Code != want {
					t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
				}
				if want == 503 && !strings.Contains(response.Body.String(), "DISTRIBUTION_DISABLED") {
					t.Fatalf("missing disabled state: %s", response.Body)
				}
			})
		}
	}
}
