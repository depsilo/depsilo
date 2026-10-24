package identity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func pendingChoice(t *testing.T, m *Manager, f *fakeProvider, subject string, groups []string) Result {
	t.Helper()
	start, err := m.Begin(context.Background(), "login", "/admin/cache", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, code := f.issue(t, start, subject, groups, "")
	key, err := m.Callback(context.Background(), state, start.BrowserToken, code, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Complete(context.Background(), key)
	if err != nil || result.Mode != "choice" || result.ChoiceKey == "" || result.Choice == nil || result.User.ID != 0 || result.Session.ID != "" {
		t.Fatalf("expected pending account choice: %s %v", result.Mode, err)
	}
	return result
}

func assertAccountCounts(t *testing.T, database *gorm.DB, users, bindings, sessions int64) {
	t.Helper()
	for _, row := range []struct {
		model any
		want  int64
	}{{&db.User{}, users}, {&db.ExternalIdentity{}, bindings}, {&db.IdentitySession{}, sessions}} {
		var count int64
		if err := database.Model(row.model).Count(&count).Error; err != nil || count != row.want {
			t.Fatalf("%T count = %d, want %d; %v", row.model, count, row.want, err)
		}
	}
}

func TestUnassociatedLoginRequiresExplicitChoiceAndCanBeCancelled(t *testing.T) {
	m, f, database := testManager(t)
	choice := pendingChoice(t, m, f, "new", []string{"readers"})
	assertAccountCounts(t, database, 0, 0, 0)
	info, err := m.Choice(choice.ChoiceKey)
	if err != nil || info.Role != "readonly" || info.ReturnTo != "/admin/cache" || !info.ExpiresAt.After(time.Now()) {
		t.Fatal("pending choice cannot be resumed")
	}
	m.CancelChoice(choice.ChoiceKey)
	if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "create", "", ""); !errors.Is(err, ErrExpired) {
		t.Fatal("cancelled choice accepted")
	}
	assertAccountCounts(t, database, 0, 0, 0)
	choice = pendingChoice(t, m, f, "new", []string{"readers"})
	result, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "create", "", "")
	if err != nil || result.Mode != "login" || result.User.PermissionSource != "oidc" || result.User.PasswordHash != "" || result.Session.Role != "readonly" {
		t.Fatalf("managed account creation: %v", err)
	}
	if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "create", "", ""); !errors.Is(err, ErrExpired) {
		t.Fatal("choice replayed")
	}
	assertAccountCounts(t, database, 1, 1, 1)
}

func TestUnassociatedIdentityCanProveAndJoinExistingLocalAccount(t *testing.T) {
	m, f, database := testManager(t)
	local := createLocal(t, database, "existing", "admin")
	choice := pendingChoice(t, m, f, "external", []string{"readers"})
	if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "link", local.Username, "wrong"); !errors.Is(err, ErrProof) {
		t.Fatal("wrong password accepted")
	}
	assertAccountCounts(t, database, 1, 0, 0)
	if _, err := m.Choice(choice.ChoiceKey); err != nil {
		t.Fatal("wrong proof consumed choice")
	}
	result, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "link", local.Username, "test-password")
	if err != nil || result.User.ID != local.ID || result.User.Role != "admin" || result.User.PermissionSource != "local" || result.Session.Role != "readonly" {
		t.Fatalf("local binding lost owner/role cap: %v", err)
	}
	assertAccountCounts(t, database, 1, 1, 1)
	if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "link", local.Username, "test-password"); !errors.Is(err, ErrExpired) {
		t.Fatal("local binding replay accepted")
	}
	var audits int64
	database.Model(&db.AuditLog{}).Where("action IN ?", []string{"identity.link", "identity.login"}).Count(&audits)
	if audits != 2 {
		t.Fatal("binding/login audit missing")
	}
}

func TestAccountChoiceExpiresAndCannotTransferAnIdentity(t *testing.T) {
	m, f, database := testManager(t)
	local := createLocal(t, database, "existing", "admin")
	expired := pendingChoice(t, m, f, "late", []string{"readers"})
	m.mu.Lock()
	txn := m.choices[expired.ChoiceKey]
	txn.Info.ExpiresAt = time.Now().Add(-time.Second)
	m.choices[expired.ChoiceKey] = txn
	m.mu.Unlock()
	if _, err := m.ResolveChoice(context.Background(), expired.ChoiceKey, "create", "", ""); !errors.Is(err, ErrExpired) {
		t.Fatal("expired choice accepted")
	}
	a := pendingChoice(t, m, f, "one-owner", []string{"readers"})
	b := pendingChoice(t, m, f, "one-owner", []string{"readers"})
	created, err := m.ResolveChoice(context.Background(), a.ChoiceKey, "create", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveChoice(context.Background(), b.ChoiceKey, "link", local.Username, "test-password"); !errors.Is(err, ErrBound) {
		t.Fatal("existing managed identity transferred")
	}
	var linked db.ExternalIdentity
	if err := database.First(&linked).Error; err != nil || linked.UserID != created.User.ID {
		t.Fatal("original identity owner changed")
	}
	assertAccountCounts(t, database, 2, 1, 1)
}

func TestAccountChoiceConcurrentResolutionHasOneResult(t *testing.T) {
	m, f, database := testManager(t)
	choice := pendingChoice(t, m, f, "concurrent", []string{"admins"})
	var wg sync.WaitGroup
	var success atomic.Int32
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "create", "", ""); err == nil {
				success.Add(1)
			} else if !errors.Is(err, ErrExpired) {
				t.Errorf("unexpected concurrent result: %v", err)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("choice not single use")
	}
	assertAccountCounts(t, database, 1, 1, 1)
}

func TestAccountChoiceRejectsDisabledOrExternallyManagedLocalProof(t *testing.T) {
	for _, kind := range []string{"disabled", "managed"} {
		t.Run(kind, func(t *testing.T) {
			m, f, database := testManager(t)
			local := createLocal(t, database, "existing", "admin")
			if kind == "disabled" {
				database.Model(&local).Update("enabled", false)
			} else {
				database.Model(&local).Update("permission_source", "oidc")
			}
			choice := pendingChoice(t, m, f, "new", []string{"admins"})
			if _, err := m.ResolveChoice(context.Background(), choice.ChoiceKey, "link", local.Username, "test-password"); !errors.Is(err, ErrProof) {
				t.Fatal("invalid local authority accepted")
			}
			assertAccountCounts(t, database, 1, 0, 0)
		})
	}
}

func TestTrialAuthenticatesWithoutCreatingOrRevokingDurableState(t *testing.T) {
	m, f, database := testManager(t)
	existing, err := doLogin(t, m, f, "existing", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, subject, defect, outcome string
		groups                         []string
		authenticated                  bool
		providerError, code            string
	}{
		{"new", "new-sub", "", "success", []string{"readers"}, true, "", ""},
		{"unmapped-existing", "existing", "", "denied", []string{"unmapped"}, true, "", "ACCESS_DENIED"},
		{"signature", "existing", "signature", "failed", []string{"admins"}, false, "", "OIDC_UNAVAILABLE"},
		{"not authorized by provider", "existing", "", "failed", nil, false, "access_denied", "AUTHENTICATION_NOT_COMPLETED"},
		{"provider error", "existing", "", "failed", nil, false, "server_error", "OIDC_UNAVAILABLE"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			trial, err := NewTrial(ProviderID, m.Config())
			if err != nil {
				t.Fatal(err)
			}
			if err := trial.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
			start, err := trial.Begin(context.Background(), "test", "/admin/authentication?source=oidc", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			state, code := f.issue(t, start, scenario.subject, scenario.groups, scenario.defect)
			if scenario.providerError != "" {
				code = ""
			}
			key, err := trial.Callback(context.Background(), state, start.BrowserToken, code, scenario.providerError)
			if err != nil {
				t.Fatal(err)
			}
			result, err := trial.Complete(context.Background(), key)
			if err != nil || result.Mode != "test" || result.Trial.Outcome != scenario.outcome || result.Trial.Code != scenario.code || result.Trial.Authenticated != scenario.authenticated || result.User.ID != 0 || result.Session.ID != "" {
				t.Fatalf("trial result: %v %+v", err, result.Trial)
			}
			if _, err := trial.Complete(context.Background(), key); !errors.Is(err, ErrExpired) {
				t.Fatal("trial completion replayed")
			}
			assertAccountCounts(t, database, 1, 1, 1)
			var active db.IdentitySession
			if err := database.First(&active, "id = ?", existing.Session.ID).Error; err != nil || active.RevokedAt != nil {
				t.Fatal("trial revoked a real session")
			}
		})
	}
}
