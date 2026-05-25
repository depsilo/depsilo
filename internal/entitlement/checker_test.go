package entitlement_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/trial"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d.AutoMigrate(&db.TrialRecord{}, &db.LicenseStorage{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

func newTrial(t *testing.T, d *gorm.DB) *trial.Manager {
	t.Helper()
	tm, err := trial.NewManager(d)
	if err != nil {
		t.Fatalf("trial.NewManager: %v", err)
	}
	return tm
}

func TestChecker_NoneWhenBothEmpty(t *testing.T) {
	d := newDB(t)
	c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), newTrial(t, d))
	if c.IsPro() {
		t.Error("IsPro() = true, want false")
	}
	s := c.Status()
	if s.Source != entitlement.SourceNone {
		t.Errorf("Source = %q, want %q", s.Source, entitlement.SourceNone)
	}
	if s.IsPro {
		t.Error("Status.IsPro = true, want false")
	}
	if s.TrialUsed {
		t.Error("TrialUsed = true, want false")
	}
	if !s.TrialAvailable {
		t.Error("TrialAvailable = false, want true")
	}
	if s.DaysLeft != 0 {
		t.Errorf("DaysLeft = %d, want 0", s.DaysLeft)
	}
}

func TestChecker_TrialActiveSourceIsTrial(t *testing.T) {
	d := newDB(t)
	tm := newTrial(t, d)
	if _, err := tm.Activate(context.Background(), 1, ""); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), tm)
	if !c.IsPro() {
		t.Error("IsPro() = false, want true")
	}
	s := c.Status()
	if s.Source != entitlement.SourceTrial {
		t.Errorf("Source = %q, want %q", s.Source, entitlement.SourceTrial)
	}
	if !s.IsPro {
		t.Error("Status.IsPro = false, want true")
	}
	if !s.TrialUsed {
		t.Error("TrialUsed = false, want true")
	}
	if s.TrialAvailable {
		t.Error("TrialAvailable = true, want false (just activated)")
	}
	if s.DaysLeft != 14 {
		t.Errorf("DaysLeft = %d, want 14", s.DaysLeft)
	}
}

func TestChecker_PaidBeatsTrial(t *testing.T) {
	d := newDB(t)
	// Force paid via DEPSILO_DEV_PRO
	t.Setenv("DEPSILO_DEV_PRO", "1")
	lm := license.NewManager(config.LicenseConfig{}, d)

	tm := newTrial(t, d)
	if _, err := tm.Activate(context.Background(), 1, ""); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	c := entitlement.NewChecker(lm, tm)
	if !c.IsPro() {
		t.Error("IsPro() = false, want true")
	}
	s := c.Status()
	if s.Source != entitlement.SourcePaid {
		t.Errorf("Source = %q, want %q (paid should beat trial)", s.Source, entitlement.SourcePaid)
	}
	if !s.TrialUsed {
		t.Error("TrialUsed = false, want true (trial record exists even though paid wins)")
	}
	if s.TrialAvailable {
		t.Error("TrialAvailable = true, want false")
	}
}

func TestChecker_TrialExpiredFallsToNone(t *testing.T) {
	d := newDB(t)
	now := time.Now().UTC()
	if err := d.Create(&db.TrialRecord{
		ActivatedAt: now.Add(-30 * 24 * time.Hour),
		ExpiresAt:   now.Add(-16 * 24 * time.Hour),
		ActivatedBy: 1,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), newTrial(t, d))
	if c.IsPro() {
		t.Error("IsPro() = true, want false (trial expired)")
	}
	s := c.Status()
	if s.Source != entitlement.SourceNone {
		t.Errorf("Source = %q, want %q", s.Source, entitlement.SourceNone)
	}
	if !s.TrialUsed {
		t.Error("TrialUsed = false, want true (record exists)")
	}
	if s.TrialAvailable {
		t.Error("TrialAvailable = true, want false (single-shot)")
	}
	if s.ExpiresAt == nil {
		t.Error("ExpiresAt = nil, want set to historical expiry date")
	}
	if s.ExpiresAt != nil && !s.ExpiresAt.Equal(now.Add(-16 * 24 * time.Hour)) {
		t.Errorf("ExpiresAt = %v, want %v", *s.ExpiresAt, now.Add(-16 * 24 * time.Hour))
	}
}
