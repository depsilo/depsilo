package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"gorm.io/gorm"
)

// This fixture is the pre-v2 algorithm, independent of the production helper.
// Its legacy form deliberately omits the source ID and preserves nil slices.
func oldSessionFingerprint(id string, cfg config.OIDCConfig) string {
	raw, _ := json.Marshal(struct {
		Config config.OIDCConfig
		Secret string
	}{cfg, cfg.ClientSecret})
	if id != "oidc" {
		raw = append([]byte(id+"\x00"), raw...)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func fingerprintStartup(t *testing.T, database *gorm.DB, id string, cfg config.OIDCConfig) (*Manager, error) {
	t.Helper()
	if id == ProviderID {
		return New(database, cfg)
	}
	r, err := NewRegistry(database, config.AuthConfig{Sources: map[string]config.AuthenticationSourceConfig{id: {Protocol: "oidc", OIDCConfig: cfg}}})
	if err != nil {
		return nil, err
	}
	return r.Manager(id), nil
}

func TestSessionFingerprintUpgradeAndRenamePreserveAdmission(t *testing.T) {
	for _, id := range []string{ProviderID, "company"} {
		t.Run(id, func(t *testing.T) {
			m, provider, database := testManager(t)
			cfg := m.Config()
			cfg.ReadonlyGroups = nil // compatibility includes old nil JSON slices
			if id != ProviderID {
				cfg.RedirectURL = strings.Replace(cfg.RedirectURL, "/oidc/callback", "/oidc/"+id+"/callback", 1)
			}
			m, err := fingerprintStartup(t, database, id, cfg)
			if err != nil {
				t.Fatal(err)
			}
			login, err := doLogin(t, m, provider, "existing-account", []string{"admins"}, "")
			if err != nil {
				t.Fatal(err)
			}
			oldFingerprint := oldSessionFingerprint(id, cfg)
			if err := database.Model(&db.IdentitySession{}).Where("id = ?", login.Session.ID).Update("config_fingerprint", oldFingerprint).Error; err != nil {
				t.Fatal(err)
			}
			// Expired and revoked rows must never be migrated or resurrected.
			expired, revoked := login.Session, login.Session
			expired.ID, revoked.ID = randomToken(), randomToken()
			expired.ConfigFingerprint, revoked.ConfigFingerprint = oldFingerprint, oldFingerprint
			expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
			now := time.Now().UTC()
			revoked.RevokedAt = &now
			for _, row := range []db.IdentitySession{expired, revoked} {
				if err := database.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			m, err = fingerprintStartup(t, database, id, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var upgraded db.IdentitySession
			if err := database.First(&upgraded, "id = ?", login.Session.ID).Error; err != nil {
				t.Fatal(err)
			}
			if upgraded.RevokedAt != nil || upgraded.ConfigFingerprint != m.ConfigFingerprint() || upgraded.ConfigFingerprint == oldFingerprint || len(upgraded.ConfigFingerprint) != 64 {
				t.Fatal("existing admission was not upgraded to the compatible fingerprint")
			}
			if upgraded.UserID != login.User.ID || upgraded.IdentityID != login.Session.IdentityID || upgraded.Role != login.Session.Role || !upgraded.CreatedAt.Equal(login.Session.CreatedAt) || !upgraded.ExpiresAt.Equal(login.Session.ExpiresAt) {
				t.Fatal("fingerprint upgrade changed account authority or session lifetime")
			}
			for _, id := range []string{expired.ID, revoked.ID} {
				var row db.IdentitySession
				if err := database.First(&row, "id = ?", id).Error; err != nil {
					t.Fatal(err)
				}
				if row.ConfigFingerprint != oldFingerprint || row.RevokedAt == nil {
					t.Fatal("expired or revoked admission was upgraded")
				}
			}
			cfg.DisplayName = "Renamed provider"
			m, err = fingerprintStartup(t, database, id, cfg)
			if err != nil {
				t.Fatal(err)
			}
			assertSessionRevoked(t, database, login.Session.ID, false)
			if m.ConfigFingerprint() != upgraded.ConfigFingerprint {
				t.Fatal("presentation rename changed session authority")
			}
		})
	}
}

func TestSessionFingerprintLegacyUpgradeFailsClosedForChangedConfiguration(t *testing.T) {
	for _, change := range []string{"display name", "secret", "issuer", "identity issuer", "identity source"} {
		t.Run(change, func(t *testing.T) {
			m, provider, database := testManager(t)
			login, err := doLogin(t, m, provider, "old-account", []string{"admins"}, "")
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.Config()
			if err := database.Model(&db.IdentitySession{}).Where("id = ?", login.Session.ID).Update("config_fingerprint", oldSessionFingerprint(ProviderID, cfg)).Error; err != nil {
				t.Fatal(err)
			}
			switch change {
			case "display name":
				cfg.DisplayName = "First upgrade and rename"
			case "secret":
				cfg.ClientSecret = "replacement"
			case "issuer":
				cfg.Issuer += "/replacement"
			case "identity issuer", "identity source":
				field, value := "issuer", cfg.Issuer+"/foreign"
				if change == "identity source" {
					field, value = "provider", "foreign"
				}
				if err := database.Model(&db.ExternalIdentity{}).Where("id = ?", login.Session.IdentityID).Update(field, value).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewRegistry(database, config.AuthConfig{OIDC: cfg}); err != nil {
				t.Fatal(err)
			}
			assertSessionRevoked(t, database, login.Session.ID, true)
		})
	}
}

func TestSessionFingerprintStartupRollsBackUpgradeOnRevocationFailure(t *testing.T) {
	for _, id := range []string{ProviderID, "company"} {
		t.Run(id, func(t *testing.T) {
			m, provider, database := testManager(t)
			cfg := m.Config()
			if id != ProviderID {
				cfg.RedirectURL = strings.Replace(cfg.RedirectURL, "/oidc/callback", "/oidc/"+id+"/callback", 1)
			}
			m, err := fingerprintStartup(t, database, id, cfg)
			if err != nil {
				t.Fatal(err)
			}
			login, err := doLogin(t, m, provider, "existing-account", []string{"admins"}, "")
			if err != nil {
				t.Fatal(err)
			}
			oldFingerprint := oldSessionFingerprint(id, cfg)
			if err := database.Model(&db.IdentitySession{}).Where("id = ?", login.Session.ID).Update("config_fingerprint", oldFingerprint).Error; err != nil {
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
			if _, err := fingerprintStartup(t, database, id, cfg); err == nil {
				t.Fatal("startup continued after revocation failure")
			}
			var row db.IdentitySession
			if err := database.First(&row, "id = ?", login.Session.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.ConfigFingerprint != oldFingerprint || row.RevokedAt != nil {
				t.Fatal("failed startup partially committed fingerprint upgrade")
			}
		})
	}
}
