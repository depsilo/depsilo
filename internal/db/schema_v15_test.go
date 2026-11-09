package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestVulnerabilityDetailRevisionMigrationSchedulesRefresh(t *testing.T) {
	database, err := Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, _ := database.DB()
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, migration := range schemaMigrations[:14] {
		if err := applySchemaMigration(database, migration, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Date(2026, 9, 10, 3, 50, 12, 0, time.UTC)
	if err := database.Table("vulnerabilities").Create(map[string]any{
		"osv_id": "GHSA-jjg7-2v4v-x38h", "ecosystem": "pypi", "package_name": "idna",
		"severity": "unknown", "cvss_score": 0, "summary": "stored before the parser fix",
		"modified_at": stamp, "created_at": stamp, "updated_at": stamp,
	}).Error; err != nil {
		t.Fatal(err)
	}
	fresh := time.Now().UTC().Add(24 * time.Hour)
	if err := database.Table("vulnerability_checks").Create(map[string]any{
		"ecosystem": "pypi", "package_name": "idna", "has_vulnerabilities": true, "vulnerability_count": 1,
		"last_fetched_at": stamp, "next_fetch_at": fresh, "created_at": stamp, "updated_at": stamp,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("migrate to v15: %v", err)
	}

	var stored Vulnerability
	if err := database.First(&stored, "osv_id = ?", "GHSA-jjg7-2v4v-x38h").Error; err != nil {
		t.Fatal(err)
	}
	if stored.DetailRevision != 0 || stored.Summary != "stored before the parser fix" || !stored.ModifiedAt.Equal(stamp) {
		t.Fatalf("migration rewrote the stored advisory instead of marking it for refresh: %+v", stored)
	}
	var check VulnerabilityCheck
	if err := database.First(&check, "package_name = ?", "idna").Error; err != nil {
		t.Fatal(err)
	}
	if !check.NextFetchAt.Before(time.Now()) || check.VulnerabilityCount != 1 {
		t.Fatalf("vulnerability check is not due for refresh: %+v", check)
	}
	// New rows record the parser that derived them.
	current := Vulnerability{OSVID: "GHSA-new", Ecosystem: "pypi", PackageName: "idna", DetailRevision: 1}
	if err := database.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	var reread Vulnerability
	if err := database.First(&reread, current.ID).Error; err != nil || reread.DetailRevision != 1 {
		t.Fatalf("detail revision not persisted: %+v %v", reread, err)
	}
}
