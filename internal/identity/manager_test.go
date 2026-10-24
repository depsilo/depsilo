package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

type fakeGrant struct {
	nonce, challenge, subject string
	groups                    any
	defect                    string
}
type fakeProvider struct {
	server          *httptest.Server
	key             *rsa.PrivateKey
	mu              sync.Mutex
	grants          map[string]fakeGrant
	tokens          map[string]fakeGrant
	exchanges       atomic.Int64
	foreignEndpoint string
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProvider{key: key, grants: make(map[string]fakeGrant), tokens: make(map[string]fakeGrant)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeProvider) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		keys := f.server.URL + "/keys"
		if f.foreignEndpoint != "" {
			keys = f.foreignEndpoint
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": keys, "userinfo_endpoint": f.server.URL + "/userinfo", "id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"}})
	case "/keys":
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/token":
		_ = r.ParseForm()
		client, secret, _ := r.BasicAuth()
		if client != "test-client" || secret != "test-secret" {
			http.Error(w, `{"error":"invalid_client"}`, 401)
			return
		}
		f.mu.Lock()
		g, ok := f.grants[r.Form.Get("code")]
		delete(f.grants, r.Form.Get("code"))
		f.mu.Unlock()
		f.exchanges.Add(1)
		if !ok || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != g.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		claims := jwt.MapClaims{"iss": f.server.URL, "aud": "test-client", "sub": g.subject, "nonce": g.nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "groups": g.groups}
		switch g.defect {
		case "nonce":
			claims["nonce"] = "wrong"
		case "issuer":
			claims["iss"] = "https://other.test"
		case "audience":
			claims["aud"] = "other"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Minute).Unix()
		case "authorized-party":
			claims["azp"] = "another-client"
		case "multiple-audiences":
			claims["aud"] = []string{"test-client", "another-client"}
		case "access-token-hash":
			claims["at_hash"] = "wrong"
		}
		signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed.Header["kid"] = "test"
		raw, _ := signed.SignedString(f.key)
		if g.defect == "signature" {
			raw = raw[:len(raw)-10] + "aaaaaaaaaa"
		}
		access := randomToken()
		f.mu.Lock()
		f.tokens[access] = g
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 60, "id_token": raw})
	case "/userinfo":
		f.mu.Lock()
		g, ok := f.tokens[r.Header.Get("Authorization")[len("Bearer "):]]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "{}", 401)
			return
		}
		sub := g.subject
		if g.defect == "userinfo-sub" {
			sub = "someone-else"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": sub, "name": "Same Name", "email": "same@example.test", "groups": g.groups})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeProvider) issue(t *testing.T, start StartResult, subject string, groups any, defect string) (string, string) {
	t.Helper()
	u, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") == "" {
		t.Fatal("missing PKCE or nonce")
	}
	code := randomToken()
	g := fakeGrant{nonce: u.Query().Get("nonce"), challenge: u.Query().Get("code_challenge"), subject: subject, groups: groups, defect: defect}
	if defect == "pkce" {
		g.challenge = "wrong"
	}
	f.mu.Lock()
	f.grants[code] = g
	f.mu.Unlock()
	return u.Query().Get("state"), code
}

func testManager(t *testing.T) (*Manager, *fakeProvider, *gorm.DB) {
	t.Helper()
	f := newFakeProvider(t)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "identity.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(&db.User{}, &db.ExternalIdentity{}, &db.IdentitySession{}, &db.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.OIDCConfig{Enabled: true, DisplayName: "Test login", Issuer: f.server.URL, ClientID: "test-client", ClientSecret: "test-secret", RedirectURL: "http://127.0.0.1:18081/api/v1/auth/oidc/callback", AdminGroups: []string{"admins"}, ReadonlyGroups: []string{"readers"}, GroupsClaim: "groups", SessionTTL: 30 * time.Minute, AllowInsecureLoopback: true}
	m, err := New(database, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return m, f, database
}

func doLogin(t *testing.T, m *Manager, f *fakeProvider, subject string, groups any, defect string) (Result, error) {
	t.Helper()
	start, err := m.Begin(context.Background(), "login", "/admin/cache", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, code := f.issue(t, start, subject, groups, defect)
	key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
	if err != nil {
		return Result{}, err
	}
	result, err := m.Complete(context.Background(), key)
	if err == nil && result.Mode == "choice" {
		return m.ResolveChoice(context.Background(), result.ChoiceKey, "create", "", "")
	}
	return result, err
}

func createLocal(t *testing.T, database *gorm.DB, name, role string) db.User {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	u := db.User{Username: name, PasswordHash: string(hash), Role: role, PermissionSource: "local", Enabled: true, CredentialVersion: 1}
	if err := database.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func TestOIDCLoginAdmissionAndStableIdentity(t *testing.T) {
	m, f, database := testManager(t)
	local := createLocal(t, database, "Same Name", "admin")
	result, err := doLogin(t, m, f, "stable-sub", []string{"readers"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.ID == local.ID || result.User.PermissionSource != "oidc" || result.User.PasswordHash != "" || result.User.Role != "readonly" || result.ReturnTo != "/admin/cache" {
		t.Fatalf("invalid external account: %+v", result.User)
	}
	if result.Session.ExpiresAt.Sub(result.Session.CreatedAt) != 30*time.Minute {
		t.Fatal("wrong absolute session lifetime")
	}
	next, err := doLogin(t, m, f, "stable-sub", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.User.ID != result.User.ID || next.User.Role != "admin" || next.User.CredentialVersion != result.User.CredentialVersion+1 {
		t.Fatal("stable subject or role update lost")
	}
	var old db.IdentitySession
	database.First(&old, "id = ?", result.Session.ID)
	if old.RevokedAt == nil {
		t.Fatal("old role session remains")
	}
	if _, err := doLogin(t, m, f, "stable-sub", []string{"unknown"}, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("unmapped groups admitted: %v", err)
	}
	database.First(&old, "id = ?", next.Session.ID)
	if old.RevokedAt == nil {
		t.Fatal("removed group did not revoke sessions")
	}
	database.Model(&db.User{}).Where("id = ?", next.User.ID).Update("enabled", false)
	if _, err := doLogin(t, m, f, "stable-sub", []string{"admins"}, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("disabled account admitted: %v", err)
	}
	if err := m.Unlink(context.Background(), next.User.ID, next.User.CredentialVersion, next.Session.IdentityID, "test-password"); !errors.Is(err, ErrDenied) {
		t.Fatalf("managed authority unlinked: %v", err)
	}
}

func TestOIDCRejectsInvalidProtocolEvidence(t *testing.T) {
	m, f, _ := testManager(t)
	for _, defect := range []string{"nonce", "issuer", "audience", "expired", "signature", "userinfo-sub", "pkce", "authorized-party", "multiple-audiences", "access-token-hash"} {
		t.Run(defect, func(t *testing.T) {
			if _, err := doLogin(t, m, f, "sub-"+defect, []string{"admins"}, defect); err == nil {
				t.Fatal("invalid OIDC evidence accepted")
			}
		})
	}
	for _, groups := range []any{nil, "admins", []any{123}, []string{}, []string{"unmapped"}} {
		if _, err := doLogin(t, m, f, "invalid-group", groups, ""); !errors.Is(err, ErrDenied) {
			t.Fatalf("invalid groups admitted: %v", err)
		}
	}
}

func TestOIDCBrowserBindingReplayAndCompletionExpiry(t *testing.T) {
	m, f, database := testManager(t)
	start, _ := m.Begin(context.Background(), "login", "https://evil.test", 0, 0)
	state, code := f.issue(t, start, "single", []string{"admins"}, "")
	if _, err := m.Callback(context.Background(), state, "other-browser", code, ""); !errors.Is(err, ErrExpired) {
		t.Fatal("unbound callback accepted")
	}
	var wg sync.WaitGroup
	success := atomic.Int32{}
	var key string
	var keyMu sync.Mutex
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			k, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
			if err == nil {
				success.Add(1)
				keyMu.Lock()
				key = k
				keyMu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || f.exchanges.Load() != 1 {
		t.Fatal("callback replayed")
	}
	r, err := m.Complete(context.Background(), key)
	if err != nil || r.ReturnTo != "/admin" {
		t.Fatalf("completion failed: %v", err)
	}
	if _, err := m.Complete(context.Background(), key); !errors.Is(err, ErrExpired) {
		t.Fatal("completion replayed")
	}
	r, err = m.ResolveChoice(context.Background(), r.ChoiceKey, "create", "", "")
	if err != nil {
		t.Fatal(err)
	}
	start, _ = m.Begin(context.Background(), "login", "/admin", 0, 0)
	state, code = f.issue(t, start, "single", []string{"admins"}, "")
	key, _ = m.Callback(context.Background(), state, start.BrowserToken, code, "")
	database.Model(&db.User{}).Where("id = ?", r.User.ID).Update("enabled", false)
	if _, err := m.Complete(context.Background(), key); !errors.Is(err, ErrDenied) {
		t.Fatal("disabled after callback admitted")
	}
	start, _ = m.Begin(context.Background(), "login", "/admin", 0, 0)
	state, code = f.issue(t, start, "late", []string{"admins"}, "")
	m.mu.Lock()
	txn := m.pending[state]
	txn.ExpiresAt = time.Now().Add(-time.Second)
	m.pending[state] = txn
	m.mu.Unlock()
	if _, err := m.Callback(context.Background(), state, start.BrowserToken, code, ""); !errors.Is(err, ErrExpired) {
		t.Fatal("expired state accepted")
	}
}

func TestOIDCExplicitLinkConflictUnlinkAndDisabledInitiator(t *testing.T) {
	m, f, database := testManager(t)
	user := createLocal(t, database, "local-user", "readonly")
	other := createLocal(t, database, "other-user", "admin")
	if _, err := m.ProvePassword(context.Background(), user.ID, 1, "wrong"); !errors.Is(err, ErrProof) {
		t.Fatal("wrong proof accepted")
	}
	link := func(u db.User, sub string) error {
		start, err := m.Begin(context.Background(), "link", "/admin", u.ID, u.CredentialVersion)
		if err != nil {
			return err
		}
		state, code := f.issue(t, start, sub, []string{"admins"}, "")
		key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
		if err != nil {
			return err
		}
		r, err := m.Complete(context.Background(), key)
		if err == nil && (r.Mode != "link" || r.ReturnTo != "/admin/account") {
			t.Fatal("wrong link completion")
		}
		return err
	}
	if err := link(user, "linked-sub"); err != nil {
		t.Fatal(err)
	}
	if err := link(other, "linked-sub"); !errors.Is(err, ErrBound) {
		t.Fatalf("identity conflict ignored: %v", err)
	}
	if err := link(user, "second-sub"); !errors.Is(err, ErrBound) {
		t.Fatalf("second same provider binding: %v", err)
	}
	r, err := doLogin(t, m, f, "linked-sub", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.User.ID != user.ID || r.User.Role != "readonly" || r.User.PermissionSource != "local" {
		t.Fatal("link escalated local ownership/role")
	}
	if err := m.Unlink(context.Background(), user.ID, 1, r.Session.IdentityID, "test-password"); err != nil {
		t.Fatal(err)
	}
	var old db.IdentitySession
	database.First(&old, "id = ?", r.Session.ID)
	if old.RevokedAt == nil {
		t.Fatal("unlink did not revoke")
	}
	var count int64
	database.Model(&db.ExternalIdentity{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 0 {
		t.Fatal("link remains")
	}
	if _, err := m.ProvePassword(context.Background(), user.ID, 1, "test-password"); err != nil {
		t.Fatal("local password lost")
	}
	start, _ := m.Begin(context.Background(), "link", "/admin", user.ID, 1)
	state, code := f.issue(t, start, "new-sub", []string{"admins"}, "")
	database.Model(&db.User{}).Where("id = ?", user.ID).Update("enabled", false)
	key, _ := m.Callback(context.Background(), state, start.BrowserToken, code, "")
	if _, err := m.Complete(context.Background(), key); !errors.Is(err, ErrDenied) {
		t.Fatal("disabled initiator linked")
	}
}

func TestOIDCConcurrentKnownLoginsAndConfigurationRevocation(t *testing.T) {
	m, f, database := testManager(t)
	if _, err := doLogin(t, m, f, "race-sub", []string{"admins"}, ""); err != nil {
		t.Fatal(err)
	}
	starts := make([]StartResult, 4)
	states, codes := make([]string, 4), make([]string, 4)
	for i := range starts {
		starts[i], _ = m.Begin(context.Background(), "login", "/admin", 0, 0)
		states[i], codes[i] = f.issue(t, starts[i], "race-sub", []string{"admins"}, "")
	}
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := m.Callback(context.Background(), states[i], starts[i].BrowserToken, codes[i], "")
			if err == nil {
				_, err = m.Complete(context.Background(), key)
			}
			if err != nil {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatal("concurrent stable logins failed")
	}
	var count int64
	database.Model(&db.User{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate JIT accounts")
	}
	cfg := m.Config()
	cfg.ClientSecret = "rotated-secret"
	if _, err := New(database, cfg); err != nil {
		t.Fatal(err)
	}
	database.Model(&db.IdentitySession{}).Where("revoked_at IS NULL").Count(&count)
	if count != 0 {
		t.Fatal("secret rotation left sessions active")
	}
	r, err := doLogin(t, m, f, "other", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	if _, err := New(database, cfg); err != nil {
		t.Fatal(err)
	}
	var old db.IdentitySession
	database.First(&old, "id = ?", r.Session.ID)
	if old.RevokedAt == nil {
		t.Fatal("disabled config left active session")
	}
}

func TestOIDCRejectsForeignDiscoveryEndpoint(t *testing.T) {
	m, f, _ := testManager(t)
	var contacted atomic.Bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true) }))
	defer foreign.Close()
	f.foreignEndpoint = foreign.URL + "/keys"
	if _, err := m.Begin(context.Background(), "login", "/admin", 0, 0); !errors.Is(err, ErrUnavailable) || contacted.Load() {
		t.Fatal("foreign metadata endpoint allowed")
	}
}

func TestOIDCCompletionRejectsExpiredOrRevokedSession(t *testing.T) {
	m, f, database := testManager(t)
	for _, reason := range []string{"expired-result", "expired-session", "revoked-session", "credential-changed"} {
		t.Run(reason, func(t *testing.T) {
			if _, err := doLogin(t, m, f, reason, []string{"readers"}, ""); err != nil {
				t.Fatal(err)
			}
			start, _ := m.Begin(context.Background(), "login", "/admin", 0, 0)
			state, code := f.issue(t, start, reason, []string{"readers"}, "")
			key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
			if err != nil {
				t.Fatal(err)
			}
			m.mu.Lock()
			c := m.completed[key]
			m.mu.Unlock()
			switch reason {
			case "expired-result":
				m.mu.Lock()
				c.ExpiresAt = time.Now().Add(-time.Second)
				m.completed[key] = c
				m.mu.Unlock()
			case "expired-session":
				database.Model(&db.IdentitySession{}).Where("id = ?", c.Result.Session.ID).Update("expires_at", time.Now().UTC().Add(-time.Second))
			case "revoked-session":
				database.Model(&db.IdentitySession{}).Where("id = ?", c.Result.Session.ID).Update("revoked_at", time.Now().UTC())
			case "credential-changed":
				database.Model(&db.User{}).Where("id = ?", c.Result.User.ID).Update("credential_version", gorm.Expr("credential_version + 1"))
			}
			if _, err := m.Complete(context.Background(), key); err == nil {
				t.Fatal("stale completion accepted")
			}
		})
	}
}

func TestOIDCConcurrentExplicitLinksHaveOneOwner(t *testing.T) {
	m, f, database := testManager(t)
	users := []db.User{createLocal(t, database, "first", "admin"), createLocal(t, database, "second", "readonly")}
	starts := make([]StartResult, 2)
	states, codes := make([]string, 2), make([]string, 2)
	for i, u := range users {
		starts[i], _ = m.Begin(context.Background(), "link", "/admin/account", u.ID, u.CredentialVersion)
		states[i], codes[i] = f.issue(t, starts[i], "contested-sub", []string{"admins"}, "")
	}
	var wg sync.WaitGroup
	var successes, conflicts atomic.Int32
	for i := range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := m.Callback(context.Background(), states[i], starts[i].BrowserToken, codes[i], "")
			if err == nil {
				_, err = m.Complete(context.Background(), key)
			}
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, ErrBound) {
				conflicts.Add(1)
			} else {
				t.Errorf("unexpected result: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("link owners: success=%d conflict=%d", successes.Load(), conflicts.Load())
	}
	var rows []db.ExternalIdentity
	database.Find(&rows)
	if len(rows) != 1 || (rows[0].UserID != users[0].ID && rows[0].UserID != users[1].ID) {
		t.Fatal("unexpected binding owner")
	}
}

func TestOIDCSafeReturnTo(t *testing.T) {
	for _, target := range []string{
		"/admin", "/admin/", "/admin/cache", "/admin/%63ache",
		"/admin/logs?q=hello%20world", "/admin/logs?q=%E4%B8%AD%E6%96%87",
		"/admin/logs?q=中文", "/admin/logs?q=hello%2520world&next=https%3A%2F%2Fexample.test",
		"/admin/logs?q=hello%20world#%E8%AF%A6%E6%83%85", "/admin/cache#对象",
	} {
		t.Run(target, func(t *testing.T) {
			if got := SafeReturnTo(target); got != target {
				t.Fatalf("safe return path changed: %q", got)
			}
		})
	}
	for _, target := range []string{
		"https://evil.test/admin", "//evil.test/admin", "/administrator", "admin/cache",
		"/admin/../login", "/admin/./cache", "/admin/%2e%2e/login", "/admin/%252e%252e/login",
		"/admin/%2f%2fevil.test", "/admin%2fcache", "/admin/%5cevil.test", "/admin\\evil.test",
		"/admin/%00cache", "/admin/cache\t", "/admin//cache", "/admin/login?next=%2Fadmin",
		"/admin/LOGIN/", "/admin/login ", "/admin/%6cogin", "/admin/auth", "/admin/auth/complete",
		"/admin/Auth/%63omplete", "/admin/cache?q=%", "/admin/cache?q=%0d%0aLocation%3Aevil",
		"/admin/cache#%00", "/admin/cache#%", "/admin/cache?" + strings.Repeat("a", 2048),
	} {
		t.Run(target, func(t *testing.T) {
			if got := SafeReturnTo(target); got != "/admin" {
				t.Fatalf("unsafe return path accepted: %q", got)
			}
		})
	}
}

func TestOIDCReturnToPreservesEscapedQueryThroughLogin(t *testing.T) {
	m, f, _ := testManager(t)
	target := "/admin/logs?q=hello%20world&package=%E4%B8%AD%E6%96%87#%E8%AF%A6%E6%83%85"
	start, err := m.Begin(context.Background(), "login", target, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, code := f.issue(t, start, "return-query", []string{"readers"}, "")
	key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Complete(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReturnTo != target {
		t.Fatalf("return_to lost encoded query/fragment: %q", result.ReturnTo)
	}
}
