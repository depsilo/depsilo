package license_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/license"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := d.AutoMigrate(&db.LicenseStorage{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

func TestSetKey_PersistsAndUpdatesState(t *testing.T) {
	d := newTestDB(t)
	m := license.NewManager(config.LicenseConfig{}, d)
	if m.IsPro() {
		t.Fatal("IsPro() = true on fresh manager, want false")
	}

	// SetKey will call doValidate which tries to reach Lemon Squeezy. Test
	// environment typically has no internet — doValidate will fail. The key
	// MUST be persisted regardless.
	_ = m.SetKey(context.Background(), "depsilo-test-key-1234", 7)

	var stored db.LicenseStorage
	if err := d.First(&stored).Error; err != nil {
		t.Fatalf("expected LicenseStorage row to exist after SetKey: %v", err)
	}
	if stored.Key != "depsilo-test-key-1234" {
		t.Errorf("stored.Key = %q, want %q", stored.Key, "depsilo-test-key-1234")
	}
	if stored.UpdatedBy != 7 {
		t.Errorf("stored.UpdatedBy = %d, want 7", stored.UpdatedBy)
	}
	if m.Status().KeyMasked != "depsilo-***" {
		t.Errorf("KeyMasked = %q, want %q", m.Status().KeyMasked, "depsilo-***")
	}
}

func TestClearKey_RemovesPersistenceAndResetsStatus(t *testing.T) {
	d := newTestDB(t)
	if err := d.Create(&db.LicenseStorage{ID: 1, Key: "depsilo-stale", UpdatedBy: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := license.NewManager(config.LicenseConfig{}, d)
	if m.Status().KeyMasked != "depsilo-***" {
		t.Fatalf("setup: KeyMasked = %q, want depsilo-*** (key should load from DB)", m.Status().KeyMasked)
	}

	if err := m.ClearKey(context.Background(), 7); err != nil {
		t.Fatalf("ClearKey: %v", err)
	}

	var count int64
	if err := d.Model(&db.LicenseStorage{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("LicenseStorage count = %d, want 0", count)
	}
	if m.Status().KeyMasked != "" {
		t.Errorf("KeyMasked = %q after ClearKey, want empty", m.Status().KeyMasked)
	}
	if m.IsPro() {
		t.Error("IsPro() = true after ClearKey, want false")
	}
}

func TestNewManager_DBKeyOverridesConfigKey(t *testing.T) {
	d := newTestDB(t)
	if err := d.Create(&db.LicenseStorage{ID: 1, Key: "fromdb-key-xxxx", UpdatedBy: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := license.NewManager(config.LicenseConfig{Key: "cfgkey-yyyyyy"}, d)
	// Masks differ by prefix: "fromdb-***" vs "cfgkey-***"
	if got := m.Status().KeyMasked; got != "fromdb-***" {
		t.Errorf("KeyMasked = %q, want %q (DB key should win over config key)", got, "fromdb-***")
	}
}
