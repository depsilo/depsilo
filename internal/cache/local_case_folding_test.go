package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalStorageReportsRootCaseFolding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	// Probe the root the same way independently: a mixed-case file that also
	// answers to its lowercase name means the root ignores case.
	probe := filepath.Join(root, "Probe-Aa")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(filepath.Join(root, "probe-aa"))
	wantFolding := statErr == nil
	if storage.foldsCase != wantFolding {
		t.Fatalf("detected foldsCase = %v, want %v for root %q", storage.foldsCase, wantFolding, root)
	}
}

// Two keys that differ only in case must land in separate objects and must
// each keep serving their own bytes on a case-folding root.
func TestManagerKeepsCaseDistinctKeysInSeparateObjects(t *testing.T) {
	storage, err := NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	// Force the folding branch so the check runs on case-sensitive hosts too.
	storage.foldsCase = true
	manager := NewManager(storage, openStreamTestDB(t), NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	fixtures := []struct {
		key  string
		body string
	}{
		{key: "npm-exact-v1/Express/metadata.json", body: `{"name":"Express"}`},
		{key: "npm-exact-v1/express/metadata.json", body: `{"name":"express"}`},
	}
	for _, fixture := range fixtures {
		body := fixture.body
		ctx, tracker := WithTrackedForceRefresh(context.Background())
		result, err := manager.Get(ctx, fixture.key, "npm", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return io.NopCloser(strings.NewReader(body)), "application/json", int64(len(body)), "mock", nil
			})
		if err != nil {
			t.Fatalf("fill %s: %v", fixture.key, err)
		}
		stored, err := io.ReadAll(result.Reader)
		if err != nil {
			t.Fatal(err)
		}
		_ = result.Reader.Close()
		if err := tracker.Wait(context.Background()); err != nil {
			t.Fatalf("commit %s: %v", fixture.key, err)
		}
		if string(stored) != fixture.body {
			t.Fatalf("%s served %q, want %q", fixture.key, stored, fixture.body)
		}
	}

	for _, fixture := range fixtures {
		// A hit must come from this key's own object: the fetch function fails
		// the test, so nothing can silently re-fetch.
		result, err := manager.Get(context.Background(), fixture.key, "npm", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				t.Errorf("cache miss for %s: the case-distinct object was replaced", fixture.key)
				return nil, "", 0, "", errors.New("unexpected upstream fetch")
			})
		if err != nil {
			t.Fatalf("hit %s: %v", fixture.key, err)
		}
		body, err := io.ReadAll(result.Reader)
		if err != nil {
			t.Fatal(err)
		}
		_ = result.Reader.Close()
		if string(body) != fixture.body {
			t.Fatalf("%s served %q, want its own %q", fixture.key, body, fixture.body)
		}
	}
}
