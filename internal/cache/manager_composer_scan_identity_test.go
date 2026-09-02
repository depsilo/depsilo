package cache

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"depsilo/internal/db"
)

func prefetchComposerIdentityFixture(t *testing.T, manager *Manager, key string) {
	t.Helper()
	body := []byte("composer-fixture")
	if err := manager.Prefetch(
		context.Background(),
		key,
		"composer",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(body)), "application/json", int64(len(body)), "mock", nil
		},
	); err != nil {
		t.Fatalf("Prefetch(%q): %v", key, err)
	}
}

func TestManagerDoesNotPersistOrEnqueueAmbiguousComposerMetadataIdentity(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := newCapturingPackageScanner()
	manager.SetSecurityScanner(scanner)
	t.Cleanup(func() {
		scanner.unblock()
		closeTestManager(t, manager)
	})

	key := "composer/p2/not-a-package"
	prefetchComposerIdentityFixture(t, manager, key)

	var entry db.CacheEntry
	if err := database.Where("key = ?", key).Take(&entry).Error; err != nil {
		t.Fatalf("read ambiguous Composer cache entry: %v", err)
	}
	if entry.PackageName != "" {
		t.Fatalf("ambiguous Composer cache package = %q, want empty", entry.PackageName)
	}
	manager.scanMu.Lock()
	pending := len(manager.scanPending)
	manager.scanMu.Unlock()
	if pending != 0 {
		t.Fatalf("ambiguous Composer metadata queued %d package scans, want none", pending)
	}
	select {
	case <-scanner.started:
		t.Fatal("ambiguous Composer metadata started a package scan")
	default:
	}
}

func TestManagerPersistsAndEnqueuesTrustedComposerMetadataIdentity(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := newCapturingPackageScanner()
	manager.SetSecurityScanner(scanner)
	t.Cleanup(func() {
		scanner.unblock()
		closeTestManager(t, manager)
	})

	key := "composer/p2/symfony/console.json"
	prefetchComposerIdentityFixture(t, manager, key)
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("trusted Composer metadata identity did not start a package scan")
	}
	ecosystem, packageName := scanner.identity()
	if ecosystem != "composer" || packageName != "symfony/console" {
		t.Fatalf("queued scan identity = %q/%q, want composer/symfony/console", ecosystem, packageName)
	}

	var entry db.CacheEntry
	if err := database.Where("key = ?", key).Take(&entry).Error; err != nil {
		t.Fatalf("read trusted Composer cache entry: %v", err)
	}
	if entry.PackageName != "symfony/console" {
		t.Fatalf("trusted Composer cache package = %q, want symfony/console", entry.PackageName)
	}
}
