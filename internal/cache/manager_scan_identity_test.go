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

// A cached object records and scans a package identity only when its key
// names one unambiguously; an ambiguous key persists no identity and queues
// no scan, so the scanner can never record a false clean result for it.
func TestManagerPersistsAndEnqueuesOnlyTrustedScanIdentity(t *testing.T) {
	for _, test := range []struct {
		name        string
		ecosystem   string
		key         string
		contentType string
		wantPackage string // "" means the identity is ambiguous
	}{
		{"ambiguous PyPI artifact", "pypi", "pypi/files/packages/aa/foo-bar-1.0.zip", "application/octet-stream", ""},
		{"trusted PyPI simple index", "pypi", "pypi/simple/Friendly_Bard/index.html", "application/octet-stream", "Friendly_Bard"},
		{"ambiguous Composer metadata", "composer", "composer/p2/not-a-package", "application/json", ""},
		{"trusted Composer metadata", "composer", "composer/p2/symfony/console.json", "application/json", "symfony/console"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openStreamTestDB(t)
			manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
			scanner := newCapturingPackageScanner()
			manager.SetSecurityScanner(scanner)
			t.Cleanup(func() {
				scanner.unblock()
				closeTestManager(t, manager)
			})

			body := []byte(test.ecosystem + "-fixture")
			if err := manager.Prefetch(context.Background(), test.key, test.ecosystem, time.Hour,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					return io.NopCloser(bytes.NewReader(body)), test.contentType, int64(len(body)), "mock", nil
				},
			); err != nil {
				t.Fatalf("Prefetch(%q): %v", test.key, err)
			}

			if test.wantPackage != "" {
				select {
				case <-scanner.started:
				case <-time.After(time.Second):
					t.Fatal("trusted identity did not start a package scan")
				}
				if ecosystem, packageName := scanner.identity(); ecosystem != test.ecosystem || packageName != test.wantPackage {
					t.Fatalf("queued scan identity = %q/%q, want %s/%s", ecosystem, packageName, test.ecosystem, test.wantPackage)
				}
			}

			var entry db.CacheEntry
			if err := database.Where("key = ?", test.key).Take(&entry).Error; err != nil {
				t.Fatalf("read cache entry: %v", err)
			}
			if entry.PackageName != test.wantPackage {
				t.Fatalf("cache package = %q, want %q", entry.PackageName, test.wantPackage)
			}
			if test.wantPackage != "" {
				return
			}
			manager.scanMu.Lock()
			pending := len(manager.scanPending)
			manager.scanMu.Unlock()
			if pending != 0 {
				t.Fatalf("ambiguous identity queued %d package scans, want none", pending)
			}
			select {
			case <-scanner.started:
				t.Fatal("ambiguous identity started a package scan")
			default:
			}
		})
	}
}
