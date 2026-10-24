package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"depsilo/internal/api/admin"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"github.com/gin-gonic/gin"
)

type trialDraftFixture struct{ cfg config.OIDCConfig }

func (s trialDraftFixture) PreviewAuthenticationSourceDraft(context.Context, config.AuthenticationSourceDraft) (config.OIDCConfig, error) {
	return s.cfg, nil
}

func trialAPI(t *testing.T) (*gin.Engine, string, *OIDCHandler) {
	t.Helper()
	r, _, auth, token, h := newOIDCAPI(t, false)
	h.ConfigureDraftPreview(trialDraftFixture{auth.OIDC})
	g := r.Group("/api/v1/admin", middleware.Authenticate(auth.JWTSecret, h.db), admin.AdminRoleRequired(), middleware.CredentialManagementRequired())
	g.POST("/authentication/check", h.Check)
	g.POST("/authentication/trial", h.StartTrial)
	return r, token, h
}

func trialRequest(r *gin.Engine, path, body, token, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18081"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	return recorder
}

func trialCompletionCookie(t *testing.T, r *gin.Engine, token string) *http.Cookie {
	t.Helper()
	start := oidcRequest(r, http.MethodPost, "/api/v1/admin/authentication/trial", `{"id":"oidc","protocol":"oidc","settings":{},"create":false}`, token, "http://127.0.0.1:18081")
	if start.Code != 200 {
		t.Fatalf("start trial: %d %s", start.Code, start.Body)
	}
	var body struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if json.Unmarshal(start.Body.Bytes(), &body) != nil {
		t.Fatal("start response invalid")
	}
	u, _ := url.Parse(body.AuthorizationURL)
	if u.Query().Get("state") == "" || u.Query().Get("nonce") == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("trial missing protocol protection")
	}
	if len(start.Result().Cookies()) != 1 {
		t.Fatal("trial flow cookie missing")
	}
	flow := start.Result().Cookies()[0]
	wrong := oidcCookieRequest(r, http.MethodGet, "/api/v1/auth/oidc/foreign/callback?state="+u.Query().Get("state")+"&error=access_denied", "", flow)
	for _, cookie := range wrong.Result().Cookies() {
		if cookie.MaxAge > 0 {
			t.Fatal("wrong-source trial callback produced completion")
		}
	}
	callback := oidcCookieRequest(r, http.MethodGet, "/api/v1/auth/oidc/callback?state="+u.Query().Get("state")+"&error=access_denied", "", flow)
	for _, cookie := range callback.Result().Cookies() {
		if strings.HasSuffix(cookie.Name, "_complete") && cookie.MaxAge > 0 {
			if !cookie.HttpOnly || !strings.HasPrefix(cookie.Value, "@trial.") {
				t.Fatal("unprotected trial completion cookie")
			}
			return cookie
		}
	}
	t.Fatal("trial callback did not produce completion")
	return nil
}

func TestOIDCAdminTrialIsIsolatedAndBrowserOwnerBound(t *testing.T) {
	r, token, h := trialAPI(t)
	completion := trialCompletionCookie(t, r, token)
	wrongOrigin := trialRequest(r, "/api/v1/auth/oidc/complete", `{}`, token, "https://foreign.test", completion)
	if wrongOrigin.Code != 403 {
		t.Fatal("cross-origin trial completion allowed")
	}
	response := trialRequest(r, "/api/v1/auth/oidc/complete", `{}`, token, "http://127.0.0.1:18081", completion)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"test"`) || !strings.Contains(response.Body.String(), `"outcome":"failed"`) || !strings.Contains(response.Body.String(), `"authenticated":false`) || strings.Contains(response.Body.String(), `"token"`) {
		t.Fatalf("trial result: %d %s", response.Code, response.Body)
	}
	if replay := trialRequest(r, "/api/v1/auth/oidc/complete", `{}`, token, "http://127.0.0.1:18081", completion); replay.Code != 400 {
		t.Fatal("trial completion replayed")
	}
	var users, identities, sessions int64
	h.db.Model(&db.User{}).Count(&users)
	h.db.Model(&db.ExternalIdentity{}).Count(&identities)
	h.db.Model(&db.IdentitySession{}).Count(&sessions)
	if users != 1 || identities != 0 || sessions != 0 {
		t.Fatal("trial created durable authentication state")
	}
	if response := oidcRequest(r, http.MethodGet, "/api/v1/auth/identities", "", token, ""); response.Code != 200 {
		t.Fatal("trial invalidated administrator login")
	}
}

func TestFailedTrialCallbackClearsUnrelatedCompletion(t *testing.T) {
	for _, failure := range []string{"wrong-source", "missing-browser", "wrong-browser"} {
		t.Run(failure, func(t *testing.T) {
			r, token, h := trialAPI(t)
			start := trialRequest(r, "/api/v1/admin/authentication/trial", `{"id":"oidc","protocol":"oidc","settings":{},"create":false}`, token, "http://127.0.0.1:18081")
			if start.Code != http.StatusOK {
				t.Fatalf("start trial: %d", start.Code)
			}
			var body struct {
				AuthorizationURL string `json:"authorization_url"`
			}
			if err := json.Unmarshal(start.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			authorization, err := url.Parse(body.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			flow := start.Result().Cookies()[0]
			path := "/api/v1/auth/oidc/callback"
			switch failure {
			case "wrong-source":
				path = "/api/v1/auth/oidc/foreign/callback"
			case "missing-browser":
				flow.Value = ""
			case "wrong-browser":
				flow.Value = "another-browser"
			}
			name := h.cookieName(h.browserManager(), "complete")
			// A browser may still carry an ordinary login completion from an
			// earlier tab. Failure must expire it before opening AuthComplete.
			old := &http.Cookie{Name: name, Value: "oidc.previous-login-completion", Path: "/"}
			response := oidcCookieRequest(r, http.MethodGet, path+"?state="+authorization.Query().Get("state")+"&error=access_denied", "", flow, old)
			cleared := false
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == name {
					cleared = cookie.MaxAge < 0 && cookie.Value == ""
				}
			}
			if response.Code != http.StatusSeeOther || !cleared {
				t.Fatal("failed trial left an unrelated completion active")
			}
			if got := trialRequest(r, "/api/v1/auth/oidc/complete", `{}`, token, "http://127.0.0.1:18081"); got.Code != http.StatusBadRequest || strings.Contains(got.Body.String(), `"token"`) {
				t.Fatal("failed trial completed an unrelated login")
			}
			if got := oidcRequest(r, http.MethodGet, "/api/v1/auth/identities", "", token, ""); got.Code != http.StatusOK {
				t.Fatal("failed trial replaced administrator login")
			}
		})
	}
}

func TestOIDCTrialCompletionRequiresSameCurrentLocalAdmin(t *testing.T) {
	for _, kind := range []string{"missing", "other-owner", "credential-changed", "disabled", "demoted"} {
		t.Run(kind, func(t *testing.T) {
			r, token, h := trialAPI(t)
			completion := trialCompletionCookie(t, r, token)
			var owner db.User
			if err := h.db.First(&owner).Error; err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				token = ""
			case "other-owner":
				other := db.User{Username: "other", Role: "admin", PermissionSource: "local", PasswordHash: owner.PasswordHash, Enabled: true, CredentialVersion: 1}
				if err := h.db.Create(&other).Error; err != nil {
					t.Fatal(err)
				}
				token, _ = middleware.GenerateJWT(h.auth.JWTSecret, other, time.Hour)
			case "credential-changed":
				h.db.Model(&owner).Update("credential_version", owner.CredentialVersion+1)
			case "disabled":
				h.db.Model(&owner).Update("enabled", false)
			case "demoted":
				h.db.Model(&owner).Update("role", "readonly")
			}
			response := trialRequest(r, "/api/v1/auth/oidc/complete", `{}`, token, "http://127.0.0.1:18081", completion)
			if response.Code != 403 {
				t.Fatalf("stale/mismatched owner completed trial: %d", response.Code)
			}
		})
	}
}

func TestOIDCDraftEndpointsRejectUnknownAndOversizedInput(t *testing.T) {
	for _, endpoint := range []string{"check", "trial"} {
		t.Run(endpoint, func(t *testing.T) {
			for _, body := range []string{
				`null`, `[]`, `{"id":"oidc","protocol":"oidc","create":false,"settings":null}`,
				`{"id":"oidc","protocol":"oidc","create":false,"settings":{"client_secret":null}}`,
				`{"id":"oidc","protocol":"oidc","create":false,"settings":{"enabled":true,"enabled":false}}`,
				`{"id":"oidc","protocol":"oidc","create":false,"settings":{"Enabled":false}}`,
				`{"id":"oidc","ID":"foreign","protocol":"oidc","create":false,"settings":{}}`,
				`{"id":"oidc","id":"foreign","protocol":"oidc","create":false,"settings":{}}`,
				`{"id":"oidc","protocol":"oidc","unexpected":"secret-marker"}`, `{"id":"oidc","settings":{"bad":"secret-marker"}}`, `{} {}`, `{"id":"` + strings.Repeat("x", 65536) + `"}`} {
				r, token, _ := trialAPI(t)
				response := oidcRequest(r, http.MethodPost, "/api/v1/admin/authentication/"+endpoint, body, token, "http://127.0.0.1:18081")
				if response.Code != 400 || strings.Contains(response.Body.String(), "secret-marker") {
					t.Fatalf("draft input accepted or echoed: %d %s", response.Code, response.Body)
				}
			}
		})
	}
}

func TestOIDCChoiceEndpointsRequireBrowserProofAndLimitPasswordAttempts(t *testing.T) {
	r, _, _, _, h := newOIDCAPI(t, true)
	for _, path := range []string{"resolve", "cancel"} {
		response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/"+path, `{}`, "", "https://foreign.test")
		if response.Code != 403 {
			t.Fatal("cross-origin choice operation accepted")
		}
	}
	for i := 0; i < 5; i++ {
		h.ipLimiter = newLoginAttemptLimiter()
		response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/resolve", `{"action":"link","username":"existing","password":"wrong"}`, "", "http://127.0.0.1:18081")
		if response.Code != 400 {
			t.Fatalf("missing choice: %d", response.Code)
		}
	}
	response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/resolve", `{"action":"link","username":"existing","password":"wrong"}`, "", "http://127.0.0.1:18081")
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatal("choice password guesses unbounded")
	}
}

func TestOIDCCheckRequiresJSONAndSameOriginButAllowsNonBrowserClients(t *testing.T) {
	for _, test := range []struct {
		contentType, origin, site string
		status                    int
	}{
		{"application/x-www-form-urlencoded", "", "", 400},
		{"application/json", "https://foreign.test", "", 403},
		{"application/json", "http://127.0.0.1:18081", "cross-site", 403},
		// The fixture discovery is valid but intentionally has no JWKS: 503
		// proves valid browser/non-browser calls reached the network check.
		{"application/json", "http://127.0.0.1:18081", "same-origin", 503},
		{"application/json", "", "", 503},
	} {
		t.Run(test.contentType+test.origin+test.site, func(t *testing.T) {
			r, token, _ := trialAPI(t)
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18081/api/v1/admin/authentication/check", strings.NewReader(`{"id":"oidc","protocol":"oidc","create":false,"settings":{}}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", test.contentType)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Sec-Fetch-Site", test.site)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, test.status, rec.Body)
			}
		})
	}
}

func TestOIDCCheckAllowsLocalWritableAPITokenButTrialDoesNot(t *testing.T) {
	r, _, h := trialAPI(t)
	var owner db.User
	if err := h.db.First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	raw := "test-diagnostic-api-token"
	digest := sha256.Sum256([]byte(raw))
	key := db.APIToken{UserID: owner.ID, Name: "diagnostics", TokenHash: hex.EncodeToString(digest[:]), Permissions: "readwrite"}
	if err := h.db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"id":"oidc","protocol":"oidc","create":false,"settings":{}}`
	check := oidcRequest(r, http.MethodPost, "/api/v1/admin/authentication/check", body, raw, "")
	if check.Code != 503 || !strings.Contains(check.Body.String(), "OIDC_UNAVAILABLE") {
		t.Fatalf("API token cannot reach check: %d %s", check.Code, check.Body)
	}
	trial := oidcRequest(r, http.MethodPost, "/api/v1/admin/authentication/trial", body, raw, "")
	if trial.Code != 403 {
		t.Fatal("API token may not own a browser trial")
	}
}
