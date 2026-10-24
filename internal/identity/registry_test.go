package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

func testRegistry(t *testing.T) (*Registry, *fakeProvider, *fakeProvider, *gorm.DB, config.AuthConfig) {
	t.Helper()
	legacy, first, database := testManager(t)
	second := newFakeProvider(t)
	a, b := legacy.Config(), legacy.Config()
	a.DisplayName, a.RedirectURL = "Company", "http://127.0.0.1:18081/api/v1/auth/oidc/company/callback"
	b.DisplayName, b.Issuer, b.RedirectURL = "Community", second.server.URL, "http://127.0.0.1:18081/api/v1/auth/oidc/community/callback"
	auth := config.AuthConfig{Sources: map[string]config.AuthenticationSourceConfig{
		"company":   {Protocol: "oidc", OIDCConfig: a},
		"community": {Protocol: "oidc", OIDCConfig: b},
	}}
	r, err := NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	return r, first, second, database, auth
}

func bindSource(t *testing.T, m *Manager, provider *fakeProvider, user db.User, subject string) error {
	t.Helper()
	if _, err := m.ProvePassword(context.Background(), user.ID, user.CredentialVersion, "test-password"); err != nil {
		return err
	}
	start, err := m.Begin(context.Background(), "link", "/admin/account", user.ID, user.CredentialVersion)
	if err != nil {
		return err
	}
	state, code := provider.issue(t, start, subject, []string{"admins"}, "")
	key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
	if err != nil {
		return err
	}
	_, err = m.Complete(context.Background(), key)
	return err
}

func assertSessionRevoked(t *testing.T, database *gorm.DB, id string, revoked bool) {
	t.Helper()
	var row db.IdentitySession
	if err := database.First(&row, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if (row.RevokedAt != nil) != revoked {
		t.Fatalf("session revocation: got %t, want %t", row.RevokedAt != nil, revoked)
	}
}

func TestRegistryLocalAccountBindsMultipleSourcesIndependently(t *testing.T) {
	r, first, second, database, _ := testRegistry(t)
	user := createLocal(t, database, "local", "admin")
	a, b := r.Manager("company"), r.Manager("community")
	// Two independent browsers may bind sources to the same local account.
	var wg sync.WaitGroup
	for _, source := range []struct {
		manager  *Manager
		provider *fakeProvider
	}{{a, first}, {b, second}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bindSource(t, source.manager, source.provider, user, "same-sub"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	admin, err := doLogin(t, a, first, "same-sub", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := doLogin(t, b, second, "same-sub", []string{"readers"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if admin.User.ID != user.ID || reader.User.ID != user.ID || reader.User.Role != "admin" || reader.Session.Role != "readonly" || reader.User.PermissionSource != "local" {
		t.Fatal("source mapping changed local authority")
	}
	if err := bindSource(t, a, first, user, "another-sub"); !errors.Is(err, ErrBound) {
		t.Fatalf("duplicate source bound: %v", err)
	}
	if _, err := doLogin(t, a, first, "same-sub", []string{"unmapped"}, ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("unmapped source admitted: %v", err)
	}
	assertSessionRevoked(t, database, admin.Session.ID, true)
	assertSessionRevoked(t, database, reader.Session.ID, false)
	if err := r.Unlink(context.Background(), user.ID, user.CredentialVersion, admin.Session.IdentityID, "test-password"); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, database, reader.Session.ID, false)
	if _, err := r.ProvePassword(context.Background(), user.ID, user.CredentialVersion, "test-password"); err != nil {
		t.Fatal("local recovery password lost")
	}
}

func TestRegistryJITSubjectCollisionKeepsSeparateAuthorities(t *testing.T) {
	r, first, second, database, _ := testRegistry(t)
	a, err := doLogin(t, r.Manager("company"), first, "shared-sub", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := doLogin(t, r.Manager("community"), second, "shared-sub", []string{"readers"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.User.ID == b.User.ID || a.User.Role != "admin" || b.User.Role != "readonly" {
		t.Fatal("issuers merged by subject or display name")
	}
	changed, err := doLogin(t, r.Manager("company"), first, "shared-sub", []string{"readers"}, "")
	if err != nil || changed.User.ID != a.User.ID || changed.User.Role != "readonly" {
		t.Fatalf("authority mapping not updated: %v", err)
	}
	assertSessionRevoked(t, database, a.Session.ID, true)
	assertSessionRevoked(t, database, b.Session.ID, false)
	if err := bindSource(t, r.Manager("community"), second, changed.User, "extra"); !errors.Is(err, ErrDenied) {
		t.Fatalf("managed authority bound another source: %v", err)
	}
	if err := r.Unlink(context.Background(), changed.User.ID, changed.User.CredentialVersion, changed.Session.IdentityID, "test-password"); !errors.Is(err, ErrDenied) {
		t.Fatalf("managed authority detached: %v", err)
	}
}

func TestRegistryStartupRevocationIsSourceScoped(t *testing.T) {
	for _, change := range []string{"display name", "disabled", "removed", "secret", "issuer", "client", "callback", "admin groups", "readonly groups", "groups claim", "session ttl", "all removed"} {
		t.Run(change, func(t *testing.T) {
			r, first, second, database, auth := testRegistry(t)
			a, err := doLogin(t, r.Manager("company"), first, "a", []string{"admins"}, "")
			if err != nil {
				t.Fatal(err)
			}
			b, err := doLogin(t, r.Manager("community"), second, "b", []string{"readers"}, "")
			if err != nil {
				t.Fatal(err)
			}
			cfg := auth.Sources["company"]
			switch change {
			case "display name":
				cfg.DisplayName = "Renamed company"
				auth.Sources["company"] = cfg
			case "disabled":
				cfg.Enabled = false
				auth.Sources["company"] = cfg
			case "removed":
				delete(auth.Sources, "company")
			case "secret":
				cfg.ClientSecret = "rotated"
				auth.Sources["company"] = cfg
			case "issuer":
				cfg.Issuer += "/replacement"
				auth.Sources["company"] = cfg
			case "client":
				cfg.ClientID = "replacement-client"
				auth.Sources["company"] = cfg
			case "callback":
				// Changing browser origins requires the other source to be
				// dormant; both changes must revoke their respective sessions.
				cfg.RedirectURL = "http://127.0.0.1:18082/api/v1/auth/oidc/company/callback"
				auth.Sources["company"] = cfg
				other := auth.Sources["community"]
				other.Enabled = false
				auth.Sources["community"] = other
			case "admin groups":
				cfg.AdminGroups = []string{"operators"}
				auth.Sources["company"] = cfg
			case "readonly groups":
				cfg.ReadonlyGroups = []string{"viewers"}
				auth.Sources["company"] = cfg
			case "groups claim":
				cfg.GroupsClaim = "roles"
				auth.Sources["company"] = cfg
			case "session ttl":
				cfg.SessionTTL = 15 * time.Minute
				auth.Sources["company"] = cfg
			case "all removed":
				auth.Sources = nil
			}
			if _, err := NewRegistry(database, auth); err != nil {
				t.Fatal(err)
			}
			assertSessionRevoked(t, database, a.Session.ID, change != "display name")
			assertSessionRevoked(t, database, b.Session.ID, change == "all removed" || change == "callback")
		})
	}
}

func TestRegistryPreservesLegacySessionsAndSelection(t *testing.T) {
	m, first, database := testManager(t)
	// Existing direct callers may omit one mapping slice. Registry cloning
	// preserves the configuration used by the standalone manager.
	legacyConfig := m.Config()
	legacyConfig.ReadonlyGroups = nil
	var err error
	m, err = New(database, legacyConfig)
	if err != nil {
		t.Fatal(err)
	}
	old, err := doLogin(t, m, first, "legacy", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	second := newFakeProvider(t)
	other := m.Config()
	other.Issuer, other.RedirectURL = second.server.URL, "http://127.0.0.1:18081/api/v1/auth/oidc/company/callback"
	auth := config.AuthConfig{OIDC: m.Config(), Sources: map[string]config.AuthenticationSourceConfig{"company": {Protocol: "oidc", OIDCConfig: other}}}
	r, err := NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, database, old.Session.ID, false)
	if r.Manager(ProviderID).fingerprint != m.fingerprint {
		t.Fatal("registry changed the legacy source's session fingerprint")
	}
	selected, err := r.Select("")
	if err != nil || selected.ID() != ProviderID {
		t.Fatal("legacy default changed")
	}
	current, err := doLogin(t, r.Manager("company"), second, "new", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(database, m.Config()); err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, database, current.Session.ID, false)
	// A sole explicitly named source is backward compatible; multiple named
	// sources require the caller to choose, never silently pick map order.
	delete(auth.Sources, "company")
	auth.OIDC = config.OIDCConfig{}
	auth.Sources = map[string]config.AuthenticationSourceConfig{"company": {Protocol: "oidc", OIDCConfig: other}}
	r, err = NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	selected, err = r.Select("")
	if err != nil || selected.ID() != "company" {
		t.Fatal("sole source not selected")
	}
}

func TestRegistryRejectsCrossSourceCallbackCodeAndCompletion(t *testing.T) {
	r, first, second, database, _ := testRegistry(t)
	if _, err := r.Select(""); !errors.Is(err, ErrSelection) {
		t.Fatalf("ambiguous source selected: %v", err)
	}
	a, b := r.Manager("company"), r.Manager("community")
	startA, err := a.Begin(context.Background(), "login", "/admin", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	stateA, codeA := first.issue(t, startA, "subject", []string{"admins"}, "")
	if _, err := b.Callback(context.Background(), stateA, startA.BrowserToken, codeA, ""); !errors.Is(err, ErrExpired) {
		t.Fatal("state crossed source boundary")
	}
	if first.exchanges.Load() != 0 || second.exchanges.Load() != 0 {
		t.Fatal("foreign flow contacted token endpoint")
	}
	startB, err := b.Begin(context.Background(), "login", "/admin", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	stateB, _ := second.issue(t, startB, "subject", []string{"admins"}, "")
	keyB, err := b.Callback(context.Background(), stateB, startB.BrowserToken, codeA, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(context.Background(), keyB); err == nil {
		t.Fatal("code crossed issuer boundary")
	}
	keyA, err := a.Callback(context.Background(), stateA, startA.BrowserToken, codeA, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(context.Background(), keyA); !errors.Is(err, ErrExpired) {
		t.Fatal("completion crossed source boundary")
	}
	choice, err := a.Complete(context.Background(), keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResolveChoice(context.Background(), choice.ChoiceKey, "create", "", ""); err != nil {
		t.Fatal(err)
	}
	var users int64
	if err := database.Model(&db.User{}).Count(&users).Error; err != nil || users != 1 {
		t.Fatalf("wrong-source login created an account: %d, %v", users, err)
	}
}

func TestRegistryDoesNotAdoptBindingFromRemovedSource(t *testing.T) {
	r, first, _, database, auth := testRegistry(t)
	user := createLocal(t, database, "local", "admin")
	if err := bindSource(t, r.Manager("company"), first, user, "old-sub"); err != nil {
		t.Fatal(err)
	}
	login, err := doLogin(t, r.Manager("company"), first, "old-sub", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	replacement := auth.Sources["company"]
	delete(auth.Sources, "company")
	replacement.RedirectURL = "http://127.0.0.1:18081/api/v1/auth/oidc/replacement/callback"
	auth.Sources["replacement"] = replacement
	r, err = NewRegistry(database, auth)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionRevoked(t, database, login.Session.ID, true)
	if _, err := doLogin(t, r.Manager("replacement"), first, "old-sub", []string{"admins"}, ""); !errors.Is(err, ErrBound) {
		t.Fatalf("old source account adopted: %v", err)
	}
	if err := bindSource(t, r.Manager("replacement"), first, user, "old-sub"); !errors.Is(err, ErrBound) {
		t.Fatalf("old binding silently moved: %v", err)
	}
	if err := r.Unlink(context.Background(), user.ID, user.CredentialVersion, login.Session.IdentityID, "test-password"); err != nil {
		t.Fatal("removed source cannot be explicitly unlinked", err)
	}
	if err := bindSource(t, r.Manager("replacement"), first, user, "old-sub"); err != nil {
		t.Fatal("explicit rebinding failed", err)
	}
}
