package trial_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/trial"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := d.AutoMigrate(&db.TrialRecord{}, &db.User{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

func TestNewManager_NoRecord_AvailableTrue(t *testing.T) {
	d := newTestDB(t)
	m := trial.NewManager(d)
	if !m.Available() {
		t.Error("Available() = false, want true")
	}
	if m.IsActive() {
		t.Error("IsActive() = true, want false")
	}
	if m.IsUsed() {
		t.Error("IsUsed() = true, want false")
	}
}

func TestNewManager_LoadsExistingActiveRecord(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	if err := d.Create(&db.TrialRecord{
		ActivatedAt: now,
		ExpiresAt:   now.Add(14 * 24 * time.Hour),
		ActivatedBy: 1,
	}).Error; err != nil {
		t.Fatalf("seed record: %v", err)
	}

	m := trial.NewManager(d)
	if !m.IsActive() {
		t.Error("IsActive() = false, want true")
	}
	if !m.IsUsed() {
		t.Error("IsUsed() = false, want true")
	}
	if m.Available() {
		t.Error("Available() = true, want false")
	}
}

func TestNewManager_LoadsExistingExpiredRecord(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	if err := d.Create(&db.TrialRecord{
		ActivatedAt: now.Add(-30 * 24 * time.Hour),
		ExpiresAt:   now.Add(-16 * 24 * time.Hour),
		ActivatedBy: 1,
	}).Error; err != nil {
		t.Fatalf("seed record: %v", err)
	}

	m := trial.NewManager(d)
	if m.IsActive() {
		t.Error("IsActive() = true, want false (record expired)")
	}
	if !m.IsUsed() {
		t.Error("IsUsed() = false, want true (record exists)")
	}
	if m.Available() {
		t.Error("Available() = true, want false")
	}
}

func TestActivate_FirstCallSucceeds(t *testing.T) {
	d := newTestDB(t)
	m := trial.NewManager(d)

	rec, err := m.Activate(context.Background(), 42, "127.0.0.1")
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if rec == nil {
		t.Fatal("Activate returned nil record")
	}
	if rec.ActivatedBy != 42 {
		t.Errorf("ActivatedBy = %d, want 42", rec.ActivatedBy)
	}
	if rec.ActivatedFrom != "127.0.0.1" {
		t.Errorf("ActivatedFrom = %q, want \"127.0.0.1\"", rec.ActivatedFrom)
	}
	delta := rec.ExpiresAt.Sub(rec.ActivatedAt.Add(14 * 24 * time.Hour))
	if delta < -time.Second || delta > time.Second {
		t.Errorf("ExpiresAt - (ActivatedAt + 14d) = %v, want within 1s", delta)
	}
	if !m.IsActive() {
		t.Error("IsActive() = false after Activate, want true")
	}
}

func TestActivate_SecondCallReturnsErrTrialAlreadyUsed(t *testing.T) {
	d := newTestDB(t)
	m := trial.NewManager(d)

	if _, err := m.Activate(context.Background(), 1, ""); err != nil {
		t.Fatalf("first Activate: %v", err)
	}

	_, err := m.Activate(context.Background(), 2, "")
	if err != trial.ErrTrialAlreadyUsed {
		t.Errorf("second Activate err = %v, want ErrTrialAlreadyUsed", err)
	}
}

func TestActivate_ConcurrentOnlyOneSucceeds(t *testing.T) {
	d := newTestDB(t)
	m := trial.NewManager(d)

	const N = 16
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			_, err := m.Activate(context.Background(), 1, "")
			errs <- err
		}()
	}

	var successes, alreadyUsed int
	for i := 0; i < N; i++ {
		err := <-errs
		switch err {
		case nil:
			successes++
		case trial.ErrTrialAlreadyUsed:
			alreadyUsed++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("successes = %d, want 1", successes)
	}
	if alreadyUsed != N-1 {
		t.Errorf("alreadyUsed = %d, want %d", alreadyUsed, N-1)
	}
}
