package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestApplySchemaMigrationRollsBackSchemaAndLedgerTogether(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	wantErr := errors.New("forced migration failure")
	migration := schemaMigration{
		version: 99,
		name:    "rollback-probe",
		apply: func(tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE migration_rollback_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				return err
			}
			return wantErr
		},
	}

	err := applySchemaMigration(database, migration, time.Now().UTC())
	if !errors.Is(err, wantErr) {
		t.Fatalf("applySchemaMigration error = %v, want %v", err, wantErr)
	}
	if database.Migrator().HasTable("migration_rollback_probe") {
		t.Fatal("failed migration left its probe table behind")
	}
	var records int64
	if err := database.Model(&schemaMigrationRecord{}).Where("version = ?", migration.version).Count(&records).Error; err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if records != 0 {
		t.Fatalf("failed migration recorded %d ledger rows, want 0", records)
	}
}

func TestAutoMigrateRecordsAndReusesCurrentSchemaVersion(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("first AutoMigrate: %v", err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("repeat AutoMigrate: %v", err)
	}

	var records []schemaMigrationRecord
	if err := database.Order("version ASC").Find(&records).Error; err != nil {
		t.Fatalf("read schema migration records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("schema migration record count = %d, want 1", len(records))
	}
	if records[0].Version != CurrentSchemaVersion || records[0].Name != "baseline" {
		t.Fatalf("schema migration record = %#v, want current baseline", records[0])
	}
}

func TestAutoMigrateRejectsDatabaseFromNewerBinary(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	newer := schemaMigrationRecord{
		Version:   CurrentSchemaVersion + 1,
		Name:      "future",
		AppliedAt: time.Now().UTC(),
	}
	if err := database.Create(&newer).Error; err != nil {
		t.Fatalf("seed future schema version: %v", err)
	}

	err := AutoMigrate(database)
	if err == nil {
		t.Fatal("AutoMigrate accepted a database created by a newer binary")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") {
		t.Fatalf("AutoMigrate error = %q, want actionable downgrade refusal", err)
	}
}

func TestAutoMigrateAddsLedgerToLegacyDatabase(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&legacyCompileCacheEntry{}); err != nil {
		t.Fatalf("create pre-ledger schema: %v", err)
	}
	if database.Migrator().HasTable(&schemaMigrationRecord{}) {
		t.Fatal("legacy fixture unexpectedly has a schema migration ledger")
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("upgrade pre-ledger database: %v", err)
	}
	var version int
	if err := database.Model(&schemaMigrationRecord{}).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error; err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}
}

func TestAutoMigrateDoesNotAdvanceLedgerWhenBaselineInvariantFails(t *testing.T) {
	database := openVulnerabilityMigrationTestDB(t)
	seedLegacyVulnerability(t, database)
	if err := database.Exec(
		"CREATE INDEX " + wantVulnerabilityIdentityIndex +
			" ON vulnerabilities(osv_id, package_name, ecosystem)",
	).Error; err != nil {
		t.Fatalf("seed invalid replacement index: %v", err)
	}

	if err := AutoMigrate(database); err == nil {
		t.Fatal("AutoMigrate accepted an invalid baseline invariant")
	}
	var records int64
	if err := database.Model(&schemaMigrationRecord{}).Count(&records).Error; err != nil {
		t.Fatalf("count failed migration records: %v", err)
	}
	if records != 0 {
		t.Fatalf("failed baseline recorded %d schema versions, want 0", records)
	}

	if err := database.Exec("DROP INDEX " + wantVulnerabilityIdentityIndex).Error; err != nil {
		t.Fatalf("remove conflicting replacement index: %v", err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("retry repaired baseline migration: %v", err)
	}
	assertVulnerabilityIdentityIndexes(t, database)

	var version int
	if err := database.Model(&schemaMigrationRecord{}).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error; err != nil {
		t.Fatalf("read retried schema version: %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("retried schema version = %d, want %d", version, CurrentSchemaVersion)
	}
}
