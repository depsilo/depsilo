package db

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestAutoMigrateEmptyDatabaseCreatesFrozenSchemaV1(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}

	const wantFingerprint = "284780462bfb5e3de93b703f78526671955f7852caf112606a047fc77d6231f0"
	canonical := schemaV1Snapshot(t, database, true)
	gotFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	if gotFingerprint != wantFingerprint {
		t.Fatalf("schema v1 fingerprint = %s, want %s\n%s", gotFingerprint, wantFingerprint, canonical)
	}
}

func TestSchemaV1SnapshotMatchesVersionOneDomainModels(t *testing.T) {
	if CurrentSchemaVersion != 1 {
		t.Fatal("replace version-one parity coverage with fresh/install upgrade convergence tests before adding schema v2")
	}

	frozen := openCompileCacheMigrationTestDB(t)
	if err := frozen.AutoMigrate(schemaV1Models()...); err != nil {
		t.Fatalf("create frozen schema v1: %v", err)
	}
	if err := ensureSchemaV1Invariants(frozen); err != nil {
		t.Fatalf("enforce frozen schema v1 invariants: %v", err)
	}

	current := openCompileCacheMigrationTestDB(t)
	if err := current.AutoMigrate(currentSchemaV1ModelsForTest()...); err != nil {
		t.Fatalf("create schema from current domain models: %v", err)
	}
	if err := ensureCurrentSchemaInvariants(current); err != nil {
		t.Fatalf("enforce current schema invariants: %v", err)
	}

	if got, want := schemaV1Snapshot(t, frozen, false), schemaV1Snapshot(t, current, false); got != want {
		t.Fatalf("frozen schema v1 differs from current version-one models\nfrozen:\n%s\ncurrent:\n%s", got, want)
	}

	models := schemaV1Models()
	if len(models) != 35 {
		t.Fatalf("frozen schema model count = %d, want 35", len(models))
	}
	seenTables := make(map[string]struct{}, len(models))
	for _, model := range models {
		modelType := reflect.TypeOf(model)
		if modelType.Kind() != reflect.Pointer || !strings.HasPrefix(modelType.Elem().Name(), "schemaV1") {
			t.Fatalf("migration 1 contains non-frozen model %T", model)
		}
		tableModel, ok := model.(interface{ TableName() string })
		if !ok || tableModel.TableName() == "" {
			t.Fatalf("frozen model %T has no explicit table name", model)
		}
		if _, duplicate := seenTables[tableModel.TableName()]; duplicate {
			t.Fatalf("frozen schema repeats table %q", tableModel.TableName())
		}
		seenTables[tableModel.TableName()] = struct{}{}
	}
}

func TestAutoMigrateUpgradesExistingPreLedgerSchemaV1WithoutChangingData(t *testing.T) {
	if CurrentSchemaVersion != 1 {
		t.Fatal("replace the live-model v1 fixture with a checked-in schema-v1 database before adding schema v2")
	}
	database := openCompileCacheMigrationTestDB(t)
	// This is the exact path used before the numbered migration ledger existed:
	// AutoMigrate the then-current domain models and run the repair steps.
	if err := database.AutoMigrate(currentSchemaV1ModelsForTest()...); err != nil {
		t.Fatalf("create pre-ledger schema v1: %v", err)
	}
	if err := ensureSchemaV1Invariants(database); err != nil {
		t.Fatalf("enforce pre-ledger schema v1 invariants: %v", err)
	}
	if database.Migrator().HasTable(&schemaMigrationRecord{}) {
		t.Fatal("pre-ledger schema v1 fixture unexpectedly has a migration ledger")
	}
	if err := database.Exec(`
		INSERT INTO cache_entries
			(id, key, adapter_type, cache_kind, package_name, storage_path, size, hit_count,
			 content_type, etag, last_modified, response_headers, expires_at, last_accessed,
			 created_at, updated_at)
		VALUES
			(7, 'pypi/files/demo-1.0.whl', 'pypi', 'artifact', 'demo', 'objects/demo', 42, 3,
			 'application/octet-stream', 'etag-1', '', '{}', ?, ?, ?, ?)
	`, time.Unix(10, 0).UTC(), time.Unix(11, 0).UTC(), time.Unix(12, 0).UTC(), time.Unix(13, 0).UTC()).Error; err != nil {
		t.Fatalf("seed schema v1 row: %v", err)
	}
	before := semanticSchemaV1Snapshot(t, database, false)

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("upgrade existing pre-ledger schema v1: %v", err)
	}
	if after := semanticSchemaV1Snapshot(t, database, false); after != before {
		t.Fatalf("reopening schema v1 changed its schema\nbefore:\n%s\nafter:\n%s", before, after)
	}
	var records []schemaMigrationRecord
	if err := database.Find(&records).Error; err != nil {
		t.Fatalf("read schema migration ledger: %v", err)
	}
	if len(records) != 1 || records[0].Version != 1 || records[0].Name != "baseline" {
		t.Fatalf("schema migration ledger = %#v, want one baseline record", records)
	}

	var row struct {
		Key         string
		PackageName string
		HitCount    int64
	}
	if err := database.Table("cache_entries").Where("id = ?", 7).Take(&row).Error; err != nil {
		t.Fatalf("read preserved schema v1 row: %v", err)
	}
	if row.Key != "pypi/files/demo-1.0.whl" || row.PackageName != "demo" || row.HitCount != 3 {
		t.Fatalf("schema v1 row changed: %+v", row)
	}
}

func TestSchemaMigrationSequenceIsContiguous(t *testing.T) {
	if len(schemaMigrations) != CurrentSchemaVersion {
		t.Fatalf("migration count = %d, current version = %d", len(schemaMigrations), CurrentSchemaVersion)
	}
	for index, migration := range schemaMigrations {
		wantVersion := index + 1
		if migration.version != wantVersion {
			t.Fatalf("migration %d version = %d, want %d", index, migration.version, wantVersion)
		}
		if migration.name == "" || migration.apply == nil {
			t.Fatalf("migration %d is incomplete: %+v", migration.version, migration)
		}
	}
}

func schemaV1Snapshot(t *testing.T, database *gorm.DB, includeLedger bool) string {
	t.Helper()
	type schemaObject struct {
		Type      string
		Name      string
		TableName string `gorm:"column:tbl_name"`
		SQL       string
	}
	var objects []schemaObject
	query := `
		SELECT type, name, tbl_name, COALESCE(sql, '') AS sql
		FROM sqlite_master
		WHERE type IN ('table', 'index')
		  AND name NOT LIKE 'sqlite_%'`
	if !includeLedger {
		query += " AND tbl_name <> 'schema_migrations'"
	}
	if err := database.Raw(query).Scan(&objects).Error; err != nil {
		t.Fatalf("read SQLite schema: %v", err)
	}

	lines := make([]string, 0, len(objects))
	for _, object := range objects {
		lines = append(lines, strings.Join([]string{
			object.Type,
			object.Name,
			object.TableName,
			strings.Join(strings.Fields(object.SQL), " "),
		}, "|"))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func semanticSchemaV1Snapshot(t *testing.T, database *gorm.DB, includeLedger bool) string {
	t.Helper()
	// SQLite's table-rebuild path may switch identifier quotes from backticks
	// to double quotes even when columns, constraints, defaults and indexes are
	// unchanged. Ignore that serialization-only difference for compatibility;
	// the immutable fingerprint test above intentionally remains byte-exact.
	return strings.NewReplacer("`", "", `"`, "").Replace(schemaV1Snapshot(t, database, includeLedger))
}

// currentSchemaV1ModelsForTest deliberately mirrors the pre-ledger
// AutoMigrate list. While version 1 is current, the parity test makes any live
// model drift fail until a new numbered migration is added.
func currentSchemaV1ModelsForTest() []any {
	return []any{
		&CacheEntry{},
		&HuggingFaceRefPin{},
		&HuggingFaceRepositoryRevocation{},
		&CompileCacheEntry{},
		&CompileCacheCredential{},
		&CompileCacheDeletion{},
		&UpstreamUpdateEvent{},
		&AccessLog{},
		&AccessLogFiveMinutely{},
		&AccessLogHourly{},
		&AccessLogDaily{},
		&AccessLogPackageDaily{},
		&UpstreamRecord{},
		&ControlPlaneState{},
		&User{},
		&APIToken{},
		&UpstreamLatencyLog{},
		&AuditLog{},
		&PackageRule{},
		&Vulnerability{},
		&VulnerabilityCheck{},
		&SecurityPolicy{},
		&DismissedVuln{},
		&Project{},
		&ProjectPackage{},
		&TrialRecord{},
		&LicenseStorage{},
		&WebhookConfig{},
		&PackageTimestamp{},
		&ApprovedVersion{},
		&QuarantineEvent{},
		&MaliciousPackage{},
		&MalwareOverride{},
		&BlocklistSyncState{},
		&TamperRecord{},
	}
}
