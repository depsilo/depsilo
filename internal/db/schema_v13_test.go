package db

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func schemaTwelveDistributionFixture(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "depsilo.db")
	database, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, migration := range schemaMigrations[:12] {
		if err := applySchemaMigration(database, migration, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	return database, path
}

func TestDistributionMigrationPreservesStateAndReopens(t *testing.T) {
	database, path := schemaTwelveDistributionFixture(t)
	user := User{ID: 42, Username: "existing-admin", PasswordHash: "existing-password-hash", Role: "admin", Enabled: true, CredentialVersion: 7, PermissionSource: "local"}
	token := APIToken{ID: 43, UserID: user.ID, Name: "existing-token", TokenHash: "existing-token-hash", Permissions: "read_write"}
	cache := CacheEntry{ID: 44, Key: "pypi/files/existing.whl", AdapterType: "pypi", CacheKind: CacheKindArtifact, PackageName: "existing", StoragePath: "cached-object", Size: 123, HitCount: 9}
	snapshot := Snapshot{ID: 45, Name: "approved-existing", CreatedBy: "existing-admin", ArtifactCount: 1, TotalBytes: 123}
	active := ControlPlaneState{Key: "snapshot_active_id", Value: "45"}
	identity := ExternalIdentity{ID: 46, UserID: user.ID, Provider: "oidc", Issuer: "https://identity.example.test", Subject: "existing-subject"}
	for _, row := range []any{&user, &token, &cache, &snapshot, &active, &identity} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	tables := []string{"users", "api_tokens", "cache_entries", "snapshots", "control_plane_states", "external_identities"}
	before := make(map[string][]map[string]any)
	for _, table := range tables {
		var rows []map[string]any
		if err := database.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		before[table] = rows
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("upgrade v12: %v", err)
	}
	for _, model := range []any{&DistributionSet{}, &DistributionObject{}, &DistributionSetObject{}, &DistributionActiveSet{}} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("migration invented distribution state for %T: count=%d err=%v", model, count, err)
		}
	}

	// Preserve exact manifest bytes; JSON serialization is owned by the
	// distribution module and cannot be reconstructed by database migration.
	manifest := []byte("{\n  \"fixture\": true\n}\n")
	set := DistributionSet{ID: strings.Repeat("a", 64), Name: "linux-python", Revision: "v1", Manifest: manifest, CreatedBy: "existing-admin"}
	if err := database.Create(&set).Error; err != nil {
		t.Fatal(err)
	}
	sqlDatabase, _ := database.DB()
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedSQL, _ := reopened.DB()
	t.Cleanup(func() { _ = reopenedSQL.Close() })
	if err := AutoMigrate(reopened); err != nil {
		t.Fatalf("reopen current schema: %v", err)
	}
	for _, table := range tables {
		var rows []map[string]any
		if err := reopened.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rows, before[table]) {
			t.Errorf("migration or reopen changed existing %s rows", table)
		}
	}
	var kept DistributionSet
	if err := reopened.First(&kept, "id = ?", set.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kept.Manifest, manifest) || kept.Name != set.Name || kept.Revision != set.Revision || kept.VerifiedAt != nil || kept.CheckedAt != nil {
		t.Fatal("reopen changed manifest bytes, identity, or invented verification")
	}
}

func TestDistributionMigrationRollsBackTablesAndLedger(t *testing.T) {
	database, _ := schemaTwelveDistributionFixture(t)
	if err := database.Exec(`CREATE TRIGGER fail_distribution_migration_ledger
		BEFORE INSERT ON schema_migrations WHEN NEW.version = 13
		BEGIN SELECT RAISE(ABORT, 'injected distribution ledger failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	err := AutoMigrate(database)
	if err == nil || !strings.Contains(err.Error(), "injected distribution ledger failure") {
		t.Fatalf("migration error = %v", err)
	}
	for _, model := range []any{&DistributionSet{}, &DistributionObject{}, &DistributionSetObject{}, &DistributionActiveSet{}} {
		if database.Migrator().HasTable(model) {
			t.Errorf("failed migration retained %T table", model)
		}
	}
	var version int
	if err := database.Model(&schemaMigrationRecord{}).Select("MAX(version)").Scan(&version).Error; err != nil || version != 12 {
		t.Fatalf("failed migration advanced ledger: version=%d err=%v", version, err)
	}
	if err := database.Exec("DROP TRIGGER fail_distribution_migration_ledger").Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
}

func TestDistributionCatalogIdentityAndSizeConstraints(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	first := DistributionSet{ID: strings.Repeat("a", 64), Name: "python", Revision: "v1", Manifest: []byte("{}"), CreatedBy: "admin"}
	second := DistributionSet{ID: strings.Repeat("b", 64), Name: first.Name, Revision: "v2", Manifest: []byte("{}"), CreatedBy: "admin"}
	for _, row := range []any{&first, &second} {
		if err := database.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, attempted := range []DistributionSet{
		{ID: strings.Repeat("c", 64), Name: first.Name, Revision: first.Revision, Manifest: []byte("{\"changed\":true}"), CreatedBy: "admin"},
		{ID: first.ID, Name: "another-name", Revision: "v3", Manifest: []byte("{}"), CreatedBy: "admin"},
	} {
		if err := database.Create(&attempted).Error; err == nil {
			t.Fatal("duplicate set identity or name/revision accepted")
		}
	}
	object := DistributionObject{Digest: strings.Repeat("d", 64), Size: 42, StorageKey: "objects/first", VerifiedAt: time.Now().UTC()}
	if err := database.Create(&object).Error; err != nil {
		t.Fatal(err)
	}
	for _, attempted := range []DistributionObject{
		{Digest: strings.Repeat("e", 64), Size: 42, StorageKey: object.StorageKey},
		{Digest: object.Digest, Size: 42, StorageKey: "objects/other"},
		{Digest: strings.Repeat("f", 64), Size: -1, StorageKey: "objects/negative"},
	} {
		if err := database.Create(&attempted).Error; err == nil {
			t.Fatal("duplicate object identity, shared storage key or negative size accepted")
		}
	}
	// Declared references intentionally do not require uploaded bytes yet.
	missingDigest := strings.Repeat("0", 64)
	for _, id := range []string{first.ID, second.ID} {
		ref := DistributionSetObject{SetID: id, Digest: missingDigest, Size: 21}
		if err := database.Create(&ref).Error; err != nil {
			t.Fatalf("staged reference or cross-set content reuse rejected: %v", err)
		}
	}
	for _, attempted := range []DistributionSetObject{
		{SetID: first.ID, Digest: missingDigest, Size: 21},
		{SetID: first.ID, Digest: object.Digest, Size: -1},
	} {
		if err := database.Create(&attempted).Error; err == nil {
			t.Fatal("duplicate reference or negative declared size accepted")
		}
	}
	selected := DistributionActiveSet{Name: first.Name, SetID: first.ID, ActivatedAt: time.Now().UTC()}
	if err := database.Create(&selected).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := DistributionActiveSet{Name: first.Name, SetID: second.ID, ActivatedAt: time.Now().UTC()}
	if err := database.Create(&duplicate).Error; err == nil {
		t.Fatal("two active revisions for one name accepted")
	}
}
