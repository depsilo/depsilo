package cache

import (
	"bytes"
	"context"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestManagerStatsReportsDurableBytesAndEntries(t *testing.T) {
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatalf("migrate cache entries: %v", err)
	}
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	for key, content := range map[string]string{
		"objects/one": "abc",
		"objects/two": "wxyz",
	} {
		if err := storage.Put(context.Background(), key, bytes.NewBufferString(content), int64(len(content)), "application/octet-stream"); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	now := time.Now().UTC()
	entries := []db.CacheEntry{
		{Key: "pypi/files/one.whl", StoragePath: "objects/one", Size: 3, ExpiresAt: now, LastAccessed: now},
		{Key: "npm/two.tgz", StoragePath: "objects/two", Size: 4, ExpiresAt: now, LastAccessed: now},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}

	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	stats, err := manager.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SizeBytes != 7 || stats.Entries != 2 {
		t.Fatalf("Stats = %+v, want 7 bytes and 2 entries", stats)
	}
}

func TestManagerStatsDoesNotScanStorage(t *testing.T) {
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatalf("migrate cache entries: %v", err)
	}
	now := time.Now().UTC()
	if err := database.Create(&db.CacheEntry{
		Key: "npm/example.tgz", StoragePath: "objects/example", Size: 4,
		ExpiresAt: now, LastAccessed: now,
	}).Error; err != nil {
		t.Fatalf("seed cache entry: %v", err)
	}
	storage, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	// The physical object is deliberately larger than the durable metadata.
	// Periodic metrics must not walk storage (especially an S3 bucket).
	if err := storage.Put(context.Background(), "objects/orphan", bytes.NewBufferString("orphaned"), 8, "application/octet-stream"); err != nil {
		t.Fatalf("put orphaned object: %v", err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	stats, err := manager.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.SizeBytes != 4 || stats.Entries != 1 {
		t.Fatalf("Stats = %+v, want durable inventory only", stats)
	}
}
