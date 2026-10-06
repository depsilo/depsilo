package db

import (
	"testing"
	"time"
)

func TestCascadeUpstreamEgressMigrationAddsViaColumn(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d (%s): %v", schemaMigrations[index].version, schemaMigrations[index].name, err)
		}
	}
	if !database.Migrator().HasColumn(&UpstreamRecord{}, "Via") {
		t.Fatal("upstream via column is missing after the cascade migration")
	}

	// Existing and newly created rows default to direct egress; enabling
	// cascade must never silently rewrite historical routing.
	record := UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	var loaded UpstreamRecord
	if err := database.First(&loaded, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.Via != "" {
		t.Fatalf("new upstream via = %q, want empty", loaded.Via)
	}

	// The migration is idempotent: a repeat run must not fail or clear data.
	if err := migrateCascadeUpstreamEgress(database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := database.Model(&UpstreamRecord{}).Where("id = ?", record.ID).Update("via", "home").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateCascadeUpstreamEgress(database); err != nil {
		t.Fatalf("repeat migration after update: %v", err)
	}
	if err := database.First(&loaded, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.Via != "home" {
		t.Fatalf("via = %q after repeat migration", loaded.Via)
	}
}
