package identity

import (
	"testing"

	"depsilo/internal/db"
)

func TestSessionFingerprintRenamePreservesAdmissionAndUnknownFingerprintRevokes(t *testing.T) {
	m, provider, database := testManager(t)
	login, err := doLogin(t, m, provider, "existing-account", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	stale := login.Session
	stale.ID, stale.ConfigFingerprint = randomToken(), "old-security-configuration"
	if err := database.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	cfg := m.Config()
	cfg.DisplayName = "Renamed provider"
	renamed, err := startTestSource(database, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ConfigFingerprint() != m.ConfigFingerprint() || len(renamed.ConfigFingerprint()) != 64 {
		t.Fatal("presentation rename changed session authority")
	}
	assertSessionRevoked(t, database, login.Session.ID, false)
	assertSessionRevoked(t, database, stale.ID, true)
}

func TestSessionFingerprintStartupFailsOnRevocationFailure(t *testing.T) {
	m, provider, database := testManager(t)
	login, err := doLogin(t, m, provider, "existing-account", []string{"admins"}, "")
	if err != nil {
		t.Fatal(err)
	}
	stale := login.Session
	stale.ID, stale.ConfigFingerprint = randomToken(), "old-security-configuration"
	if err := database.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("CREATE TRIGGER fail_session_revocation BEFORE UPDATE OF revoked_at ON identity_sessions BEGIN SELECT RAISE(ABORT, 'revocation unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := startTestSource(database, m.Config()); err == nil {
		t.Fatal("startup continued after revocation failure")
	}
	var row db.IdentitySession
	if err := database.First(&row, "id = ?", stale.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.RevokedAt != nil {
		t.Fatal("failed startup revoked a session")
	}
}
