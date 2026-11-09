package db

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDistributionProvenanceMigrationKeepsCatalogAndRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "depsilo.db")
	database, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, _ := database.DB()
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, migration := range schemaMigrations[:13] {
		if err := applySchemaMigration(database, migration, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	manifest := []byte("{\n  \"fixture\": true\n}\n")
	set := DistributionSet{ID: strings.Repeat("b", 64), Name: "linux-python", Revision: "v1", Manifest: manifest, CreatedBy: "existing-admin"}
	if err := database.Create(&set).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`CREATE TRIGGER fail_provenance_ledger
		BEFORE INSERT ON schema_migrations WHEN NEW.version = 14
		BEGIN SELECT RAISE(ABORT, 'injected provenance ledger failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err == nil || !strings.Contains(err.Error(), "injected provenance ledger failure") {
		t.Fatalf("migration error = %v", err)
	}
	if database.Migrator().HasTable(&DistributionSetProvenance{}) {
		t.Fatal("failed migration retained the provenance table")
	}
	if err := database.Exec("DROP TRIGGER fail_provenance_ledger").Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatalf("retry v14: %v", err)
	}
	var count int64
	if err := database.Model(&DistributionSetProvenance{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("migration invented provenance: count=%d err=%v", count, err)
	}
	var kept DistributionSet
	if err := database.First(&kept, "id = ?", set.ID).Error; err != nil || !bytes.Equal(kept.Manifest, manifest) {
		t.Fatalf("migration changed existing set: %v", err)
	}
}
