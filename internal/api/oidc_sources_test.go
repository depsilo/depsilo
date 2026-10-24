package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
)

func multipleOIDCAPI(t *testing.T) (*gin.Engine, *gorm.DB, string, *OIDCHandler) {
	t.Helper()
	r, database, auth, token, h := newOIDCAPI(t, true)
	sources := make(map[string]config.AuthenticationSourceConfig)
	for _, id := range []string{"company", "community"} {
		var provider *httptest.Server
		provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gin.H{"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize", "token_endpoint": provider.URL + "/token", "jwks_uri": provider.URL + "/keys", "userinfo_endpoint": provider.URL + "/userinfo"})
		}))
		t.Cleanup(provider.Close)
		cfg := auth.OIDC
		cfg.DisplayName, cfg.Issuer = id+" login", provider.URL
		cfg.RedirectURL = "http://127.0.0.1:18081/api/v1/auth/oidc/" + id + "/callback"
		sources[id] = config.AuthenticationSourceConfig{Protocol: "oidc", OIDCConfig: cfg}
	}
	auth.OIDC, auth.Sources = config.OIDCConfig{}, sources
	registry, err := identity.NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	h.auth, h.registry = auth, registry
	return r, database, token, h
}

func oidcCookieRequest(r *gin.Engine, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1:18081"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:18081")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func sourceStart(t *testing.T, r *gin.Engine, id string) (*http.Cookie, url.Values) {
	t.Helper()
	response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/start", fmt.Sprintf(`{"provider_id":%q}`, id), "", "http://127.0.0.1:18081")
	if response.Code != http.StatusOK {
		t.Fatalf("start: %d %s", response.Code, response.Body)
	}
	var body struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(body.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing source flow cookie")
	}
	return cookies[0], u.Query()
}

func TestOIDCMultipleSourceSelectionAndBrowserFlows(t *testing.T) {
	r, _, _, _ := multipleOIDCAPI(t)
	list := oidcRequest(r, http.MethodGet, "/api/v1/auth/providers", "", "", "")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"id":"company"`) || !strings.Contains(list.Body.String(), `"id":"community"`) || !strings.Contains(list.Body.String(), `"protocol":"oidc"`) || strings.Contains(list.Body.String(), "never-return") {
		t.Fatalf("provider list: %s", list.Body)
	}
	for _, body := range []string{`{}`, `{"provider_id":"missing"}`} {
		response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/start", body, "", "http://127.0.0.1:18081")
		if response.Code == 200 {
			t.Fatal("ambiguous or missing source selected")
		}
	}
	first, firstQuery := sourceStart(t, r, "company")
	second, secondQuery := sourceStart(t, r, "community")
	if first.Name == second.Name || firstQuery.Get("redirect_uri") == secondQuery.Get("redirect_uri") {
		t.Fatal("source flows share cookies or callback URI")
	}
	// Copying both the state and browser cookie into a different source's
	// endpoint must not create a completion result or consume the first flow.
	wrong := oidcCookieRequest(r, http.MethodGet, "/api/v1/auth/oidc/community/callback?state="+firstQuery.Get("state")+"&error=access_denied", "", first, second)
	for _, cookie := range wrong.Result().Cookies() {
		if cookie.MaxAge > 0 {
			t.Fatal("cross-source callback produced completion")
		}
	}
	correct := oidcCookieRequest(r, http.MethodGet, "/api/v1/auth/oidc/company/callback?state="+firstQuery.Get("state")+"&error=access_denied", "", first)
	var completion *http.Cookie
	for _, cookie := range correct.Result().Cookies() {
		if strings.HasSuffix(cookie.Name, "_complete") && cookie.MaxAge > 0 {
			completion = cookie
		}
	}
	if completion == nil || !strings.HasPrefix(completion.Value, "company.") || !completion.HttpOnly {
		t.Fatal("source-bound completion missing")
	}
	forged := *completion
	forged.Value = strings.Replace(completion.Value, "company.", "community.", 1)
	if response := oidcCookieRequest(r, http.MethodPost, "/api/v1/auth/oidc/complete", `{}`, &forged); response.Code != 400 {
		t.Fatalf("foreign completion accepted: %d", response.Code)
	}
	response := oidcCookieRequest(r, http.MethodPost, "/api/v1/auth/oidc/complete", `{}`, completion)
	if response.Code != 403 || !strings.Contains(response.Body.String(), "ACCESS_DENIED") {
		t.Fatalf("correct source result lost: %d %s", response.Code, response.Body)
	}
	if response := oidcCookieRequest(r, http.MethodPost, "/api/v1/auth/oidc/complete", `{}`, completion); response.Code != 400 {
		t.Fatal("completion replay allowed")
	}
}

func TestOIDCMultipleSourceProofAndMetadata(t *testing.T) {
	r, database, token, h := multipleOIDCAPI(t)
	var user db.User
	if err := database.Where("username = ?", "local-admin").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	linked := db.ExternalIdentity{UserID: user.ID, Provider: "company", Issuer: h.auth.Sources["company"].Issuer, Subject: "private-subject"}
	if err := database.Create(&linked).Error; err != nil {
		t.Fatal(err)
	}
	response := oidcRequest(r, http.MethodGet, "/api/v1/auth/identities", "", token, "")
	var body struct {
		CanLink   bool `json:"can_link"`
		Provider  any  `json:"provider"`
		Providers []struct {
			ID      string `json:"id"`
			CanLink bool   `json:"can_link"`
		} `json:"providers"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil || !body.CanLink || body.Provider != nil || len(body.Providers) != 2 {
		t.Fatalf("sources metadata: %s", response.Body)
	}
	for _, source := range body.Providers {
		if source.CanLink != (source.ID == "community") {
			t.Fatal("link availability ignores per-source binding")
		}
	}
	if strings.Contains(response.Body.String(), "private-subject") || strings.Contains(response.Body.String(), "never-return") {
		t.Fatal("source metadata leaked credentials")
	}
	for _, id := range []string{"company", "community"} {
		response := oidcRequest(r, http.MethodPost, "/api/v1/auth/identities/link", fmt.Sprintf(`{"provider_id":%q,"password":"wrong"}`, id), token, "http://127.0.0.1:18081")
		if response.Code != 403 || !strings.Contains(response.Body.String(), "REAUTHENTICATION_FAILED") {
			t.Fatalf("fresh local proof bypassed: %d", response.Code)
		}
	}
	response = oidcRequest(r, http.MethodPost, "/api/v1/auth/identities/link", `{"provider_id":"community","password":"correct-password"}`, token, "http://127.0.0.1:18081")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "community") {
		t.Fatalf("selected binding flow failed: %d %s", response.Code, response.Body)
	}
}

func TestOIDCMultipleSourcesShareRateLimit(t *testing.T) {
	r, _, _, _ := multipleOIDCAPI(t)
	for i := 0; i < 6; i++ {
		id := "company"
		if i%2 == 1 {
			id = "community"
		}
		response := oidcRequest(r, http.MethodPost, "/api/v1/auth/oidc/start", fmt.Sprintf(`{"provider_id":%q}`, id), "", "http://127.0.0.1:18081")
		want := http.StatusOK
		if i == 5 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("shared limit attempt %d: %d want %d", i, response.Code, want)
		}
	}
}

func TestOIDCLegacyCallbackDoesNotAcceptSourcePathAlias(t *testing.T) {
	r, _, _, _, _ := newOIDCAPI(t, true)
	flow, query := sourceStart(t, r, "oidc")
	for _, path := range []string{"/api/v1/auth/oidc/oidc/callback", "/api/v1/auth/oidc/callback"} {
		response := oidcCookieRequest(r, http.MethodGet, path+"?state="+query.Get("state")+"&error=access_denied", "", flow)
		completed := false
		for _, cookie := range response.Result().Cookies() {
			completed = completed || strings.HasSuffix(cookie.Name, "_complete") && cookie.MaxAge > 0
		}
		if completed != (path == "/api/v1/auth/oidc/callback") {
			t.Fatal("callback accepted an unconfigured path or consumed the legacy flow")
		}
	}
}
