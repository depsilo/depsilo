package cache

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

type capturingPackageScanner struct {
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	ecosystem   string
	packageName string
}

func newCapturingPackageScanner() *capturingPackageScanner {
	return &capturingPackageScanner{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (scanner *capturingPackageScanner) ScanPackage(ctx context.Context, ecosystem, packageName string) error {
	scanner.mu.Lock()
	scanner.ecosystem = ecosystem
	scanner.packageName = packageName
	scanner.mu.Unlock()
	scanner.startOnce.Do(func() { close(scanner.started) })
	select {
	case <-scanner.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (scanner *capturingPackageScanner) unblock() {
	scanner.releaseOnce.Do(func() { close(scanner.release) })
}

func (scanner *capturingPackageScanner) identity() (string, string) {
	scanner.mu.Lock()
	defer scanner.mu.Unlock()
	return scanner.ecosystem, scanner.packageName
}

func prefetchPyPIIdentityFixture(t *testing.T, manager *Manager, key string) {
	t.Helper()
	body := []byte("fixture")
	if err := manager.Prefetch(
		context.Background(),
		key,
		"pypi",
		time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(body)), "application/octet-stream", int64(len(body)), "mock", nil
		},
	); err != nil {
		t.Fatalf("Prefetch(%q): %v", key, err)
	}
}

func TestManagerDoesNotPersistOrEnqueueAmbiguousPyPIArtifactIdentity(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := newCapturingPackageScanner()
	manager.SetSecurityScanner(scanner)
	t.Cleanup(func() {
		scanner.unblock()
		closeTestManager(t, manager)
	})

	key := "pypi/files/packages/aa/foo-bar-1.0.zip"
	prefetchPyPIIdentityFixture(t, manager, key)

	var entry db.CacheEntry
	if err := database.Where("key = ?", key).Take(&entry).Error; err != nil {
		t.Fatalf("read ambiguous PyPI cache entry: %v", err)
	}
	if entry.PackageName != "" {
		t.Fatalf("ambiguous PyPI cache package = %q, want empty", entry.PackageName)
	}
	manager.scanMu.Lock()
	pending := len(manager.scanPending)
	manager.scanMu.Unlock()
	if pending != 0 {
		t.Fatalf("ambiguous PyPI artifact queued %d package scans, want none", pending)
	}
	select {
	case <-scanner.started:
		t.Fatal("ambiguous PyPI artifact started a package scan")
	default:
	}
}

func TestManagerPersistsAndEnqueuesTrustedPyPISimpleIndexIdentity(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := newCapturingPackageScanner()
	manager.SetSecurityScanner(scanner)
	t.Cleanup(func() {
		scanner.unblock()
		closeTestManager(t, manager)
	})

	key := "pypi/simple/Friendly_Bard/index.html"
	prefetchPyPIIdentityFixture(t, manager, key)
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("trusted PyPI simple-index identity did not start a package scan")
	}
	ecosystem, packageName := scanner.identity()
	if ecosystem != "pypi" || packageName != "Friendly_Bard" {
		t.Fatalf("queued scan identity = %q/%q, want pypi/Friendly_Bard", ecosystem, packageName)
	}

	var entry db.CacheEntry
	if err := database.Where("key = ?", key).Take(&entry).Error; err != nil {
		t.Fatalf("read trusted PyPI cache entry: %v", err)
	}
	if entry.PackageName != "Friendly_Bard" {
		t.Fatalf("trusted PyPI cache package = %q, want Friendly_Bard", entry.PackageName)
	}
}
