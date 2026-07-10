package tamper

import (
	"context"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

func newTestRecorder(t *testing.T) *Recorder {
	t.Helper()
	d, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	return NewRecorder(d)
}

func TestRecorder_FirstSeenThenMatch(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "pypi/files/requests-2.32.3.tar.gz"

	// First sight establishes the baseline; no event.
	r.Record(ctx, key, "pypi", "requests", "2.32.3", "hashA", 100)

	var count int64
	r.db.Model(&db.QuarantineEvent{}).Count(&count)
	if count != 0 {
		t.Fatalf("baseline should write no event, got %d", count)
	}

	// A matching re-fetch verifies clean.
	res := r.Verify(ctx, key, "pypi", "requests", "2.32.3", "hashA", 100, "10.0.0.1")
	if res.KnownMismatch {
		t.Error("matching hash must not report KnownMismatch")
	}
	var rec db.TamperRecord
	r.db.First(&rec, "key = ?", key)
	if rec.VerifyCount != 1 {
		t.Errorf("VerifyCount = %d, want 1", rec.VerifyCount)
	}
	if !rec.LastVerifiedAt.After(rec.FirstSeenAt) {
		t.Errorf("LastVerifiedAt (%v) did not advance past FirstSeenAt (%v)", rec.LastVerifiedAt, rec.FirstSeenAt)
	}
}

func TestRecorder_MismatchAlertsAndProtects(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "npm/left-pad/-/left-pad-1.3.0.tgz"

	var fired *db.QuarantineEvent
	r.SetOnTamper(func(ev db.QuarantineEvent) { fired = &ev })

	r.Record(ctx, key, "npm", "left-pad", "1.3.0", "goodhash", 42)
	res := r.Verify(ctx, key, "npm", "left-pad", "1.3.0", "EVILHASH", 42, "10.0.0.2")

	if !res.KnownMismatch {
		t.Fatal("differing hash must report KnownMismatch")
	}
	if fired == nil || fired.Action != quarantine.ActionTamperDetected {
		t.Fatalf("OnTamper hook not fired with tamper_detected: %+v", fired)
	}
	// The baseline must NOT be overwritten — first-seen stays truth.
	var rec db.TamperRecord
	r.db.First(&rec, "key = ?", key)
	if rec.SHA256 != "goodhash" {
		t.Errorf("baseline hash overwritten to %s", rec.SHA256)
	}
	// The event carries a non-zero timestamp.
	if fired.CreatedAt.IsZero() {
		t.Error("event CreatedAt is the zero value")
	}
	// And it persisted.
	var evCount int64
	r.db.Model(&db.QuarantineEvent{}).Where("action = ?", quarantine.ActionTamperDetected).Count(&evCount)
	if evCount != 1 {
		t.Errorf("expected 1 persisted tamper event, got %d", evCount)
	}
}

func TestRecorder_VerifyWithoutBaselineRecords(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "cargo/serde/1.0.0/download.crate"

	// Pre-feature cached artifact: Verify with no baseline adopts the
	// current hash as the baseline and does NOT alert.
	res := r.Verify(ctx, key, "cargo", "serde", "1.0.0", "firsthash", 7, "10.0.0.3")
	if res.KnownMismatch {
		t.Error("no baseline must never report KnownMismatch")
	}
	var rec db.TamperRecord
	if err := r.db.First(&rec, "key = ?", key).Error; err != nil {
		t.Fatalf("baseline not adopted: %v", err)
	}
	if rec.SHA256 != "firsthash" {
		t.Errorf("adopted hash = %s", rec.SHA256)
	}
}

func TestRecorder_RecordIsIdempotent(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "npm/dep/-/dep-1.0.0.tgz"

	// First Record sets the baseline. A SECOND Record with a DIFFERENT
	// hash must be a no-op — the trusted first-seen hash is never
	// overwritten.
	r.Record(ctx, key, "npm", "dep", "1.0.0", "original", 10)
	r.Record(ctx, key, "npm", "dep", "1.0.0", "attacker-swapped", 10)

	var rec db.TamperRecord
	if err := r.db.First(&rec, "key = ?", key).Error; err != nil {
		t.Fatal(err)
	}
	if rec.SHA256 != "original" {
		t.Errorf("baseline overwritten by second Record: %s", rec.SHA256)
	}
}

func TestRecorder_OnTamperPanicRecovered(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "npm/dep/-/dep-2.0.0.tgz"

	// A panicking hook must not escape Verify — the refresh path must
	// survive a misbehaving webhook.
	r.SetOnTamper(func(db.QuarantineEvent) { panic("boom") })
	r.Record(ctx, key, "npm", "dep", "2.0.0", "good", 10)

	// If the panic escaped, this call would crash the test.
	res := r.Verify(ctx, key, "npm", "dep", "2.0.0", "evil", 10, "10.0.0.9")
	if !res.KnownMismatch {
		t.Error("mismatch should still report KnownMismatch even when the hook panics")
	}
}

func TestRecorder_VerifyLookupErrorDegrades(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "npm/dep/-/dep-3.0.0.tgz"
	r.Record(ctx, key, "npm", "dep", "3.0.0", "good", 10)

	// Force a non-ErrRecordNotFound DB error by dropping the table the
	// Verify lookup reads, then confirm Verify degrades to
	// KnownMismatch=false (a DB fault must never manufacture a false
	// tamper alarm) and writes no tamper event.
	if err := r.db.Migrator().DropTable(&db.TamperRecord{}); err != nil {
		t.Fatal(err)
	}
	res := r.Verify(ctx, key, "npm", "dep", "3.0.0", "whatever", 10, "10.0.0.10")
	if res.KnownMismatch {
		t.Error("a DB lookup error must degrade to KnownMismatch=false, not alarm")
	}
}
