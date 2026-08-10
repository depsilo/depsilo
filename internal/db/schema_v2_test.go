package db

import (
	"strings"
	"testing"
	"time"
)

func TestCredentialVersionMigrationRollsBackColumnAndLedgerTogether(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	if err := applySchemaMigration(database, schemaMigrations[0], time.Now().UTC()); err != nil {
		t.Fatalf("apply baseline migration: %v", err)
	}
	if database.Migrator().HasColumn(&User{}, "CredentialVersion") {
		t.Fatal("baseline unexpectedly contains credential version")
	}
	if err := database.Exec(`
		CREATE TRIGGER fail_credential_version_ledger
		BEFORE INSERT ON schema_migrations
		WHEN NEW.version = 2
		BEGIN
			SELECT RAISE(ABORT, 'injected credential-version ledger failure');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := applySchemaMigration(database, schemaMigrations[1], time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "injected credential-version ledger failure") {
		t.Fatalf("migration error = %v", err)
	}
	if database.Migrator().HasColumn(&User{}, "CredentialVersion") {
		t.Fatal("failed migration left credential_version behind")
	}
	var records int64
	if err := database.Model(&schemaMigrationRecord{}).Where("version = ?", 2).Count(&records).Error; err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if records != 0 {
		t.Fatalf("failed migration recorded %d version-2 rows", records)
	}
}

func TestCredentialVersionInvariantRepairsZeroGeneration(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	user := User{
		Username: "operator", PasswordHash: "unused", Role: "admin", Enabled: true,
		CredentialVersion: InitialCredentialVersion,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := database.Exec("UPDATE users SET credential_version = 0 WHERE id = ?", user.ID).Error; err != nil {
		t.Fatalf("zero credential version: %v", err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := database.First(&user, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if user.CredentialVersion != InitialCredentialVersion {
		t.Fatalf("credential version = %d, want %d", user.CredentialVersion, InitialCredentialVersion)
	}
}
