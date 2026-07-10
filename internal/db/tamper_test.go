package db

import (
	"testing"
	"time"
)

func TestTamperRecord_Migrate(t *testing.T) {
	d, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rec := TamperRecord{
		Key: "pypi/files/requests-2.32.3.tar.gz", Ecosystem: "pypi",
		Package: "requests", Version: "2.32.3", SHA256: "abc", Size: 10,
		FirstSeenAt: now, LastVerifiedAt: now,
	}
	if err := d.Create(&rec).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got TamperRecord
	if err := d.First(&got, "key = ?", rec.Key).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.SHA256 != "abc" || got.Package != "requests" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
