package db

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func schemaElevenFixture(t *testing.T) *gorm.DB {
	t.Helper()
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, migration := range schemaMigrations[:11] {
		if err := applySchemaMigration(database, migration, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func TestExternalIdentityMigrationPreservesLocalCredentials(t *testing.T) {
	database := schemaElevenFixture(t)
	if err := database.Exec(`INSERT INTO users (id, username, password_hash, role, enabled, credential_version)
		VALUES (42, 'existing-admin', 'existing-password-hash', 'admin', 1, 7)`).Error; err != nil {
		t.Fatal(err)
	}
	token := APIToken{UserID: 42, Name: "existing token", TokenHash: "existing-token-hash", Permissions: "read_write"}
	if err := database.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	var user User
	if err := database.First(&user, 42).Error; err != nil {
		t.Fatal(err)
	}
	if user.PermissionSource != "local" || user.PasswordHash != "existing-password-hash" || user.CredentialVersion != 7 || user.Role != "admin" || !user.Enabled {
		t.Fatal("migration changed existing local identity or credentials")
	}
	var keptToken APIToken
	if err := database.First(&keptToken, token.ID).Error; err != nil {
		t.Fatal(err)
	}
	if keptToken.UserID != 42 || keptToken.TokenHash != token.TokenHash || keptToken.Permissions != token.Permissions {
		t.Fatal("migration changed existing API credentials")
	}
	for _, model := range []any{&ExternalIdentity{}, &IdentitySession{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("migration invented an external login identity or session")
		}
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
}

func TestExternalIdentityMigrationRollsBackTablesColumnAndLedger(t *testing.T) {
	database := schemaElevenFixture(t)
	if err := database.Exec(`CREATE TRIGGER fail_identity_migration_ledger
		BEFORE INSERT ON schema_migrations WHEN NEW.version = 12
		BEGIN SELECT RAISE(ABORT, 'injected identity ledger failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	err := applySchemaMigration(database, schemaMigrations[11], time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "injected identity ledger failure") {
		t.Fatalf("migration error = %v", err)
	}
	if database.Migrator().HasColumn(&User{}, "PermissionSource") || database.Migrator().HasTable(&ExternalIdentity{}) || database.Migrator().HasTable(&IdentitySession{}) {
		t.Fatal("failed migration left schema changes behind")
	}
	var count int64
	if err := database.Model(&schemaMigrationRecord{}).Where("version = 12").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("failed migration recorded a completed schema version")
	}
	if err := database.Exec("DROP TRIGGER fail_identity_migration_ledger").Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
}

func TestExternalIdentityUniqueBindings(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	original := ExternalIdentity{UserID: 1, Provider: "oidc", Issuer: "https://identity.example.test", Subject: "stable-subject", DisplayName: "Shared display name"}
	if err := database.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	for _, attempted := range []ExternalIdentity{
		{UserID: 2, Provider: "oidc", Issuer: original.Issuer, Subject: original.Subject},
		{UserID: 2, Provider: "another-provider", Issuer: original.Issuer, Subject: original.Subject},
		{UserID: 1, Provider: "oidc", Issuer: original.Issuer, Subject: "other-subject"},
	} {
		if err := database.Create(&attempted).Error; err == nil {
			t.Fatal("duplicate issuer/subject or user/provider binding was accepted")
		}
	}
	// Similar names do not link accounts; a different issuer gives the same
	// opaque subject a different identity.
	for _, distinct := range []ExternalIdentity{
		{UserID: 2, Provider: "oidc", Issuer: original.Issuer, Subject: "other-subject", DisplayName: original.DisplayName},
		{UserID: 3, Provider: "oidc", Issuer: "https://another.example.test", Subject: original.Subject},
	} {
		if err := database.Create(&distinct).Error; err != nil {
			t.Fatalf("distinct identity rejected: %v", err)
		}
	}
	session := IdentitySession{ID: "random-session", UserID: 1, IdentityID: original.ID, Role: "readonly", ConfigFingerprint: "config-fingerprint", ExpiresAt: time.Now().Add(30 * time.Minute)}
	if err := database.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&session).Error; err == nil {
		t.Fatal("duplicate identity session accepted")
	}
	now := time.Now().UTC()
	if err := database.Model(&session).Update("revoked_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&session, "id = ?", session.ID).Error; err != nil || session.RevokedAt == nil {
		t.Fatal("session revocation did not persist")
	}
}
