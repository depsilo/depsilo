package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

// fakeRecorder captures manager→recorder calls and lets a test force a
// mismatch verdict. Record/Verify run from the manager's background
// goroutines while tests poll the results from the test goroutine, so
// access is guarded by mu (found via `go test -race`).
type fakeRecorder struct {
	mu          sync.Mutex
	recorded    []string // keys passed to Record
	verified    []string // keys passed to Verify
	forceMismat bool
}

func (f *fakeRecorder) Record(_ context.Context, key, _, _, _, _ string, _ int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, key)
}

func (f *fakeRecorder) Verify(_ context.Context, key, _, _, _, _ string, _ int64, _ string) TamperResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified = append(f.verified, key)
	return TamperResult{KnownMismatch: f.forceMismat}
}

func (f *fakeRecorder) recordedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.recorded...)
}

func newTamperTestManager(t *testing.T) (*Manager, *fakeRecorder) {
	t.Helper()
	d, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	// immutableThreshold = 1h; storage is the in-memory test double
	// already used by cache tests.
	m := NewManager(newMemStorage(), d, NewEventBus(), time.Hour)
	rec := &fakeRecorder{}
	m.SetTamperRecorder(rec)
	return m, rec
}

func TestManager_ImmutableMissRecordsBaseline(t *testing.T) {
	m, rec := newTamperTestManager(t)
	// TTL >= threshold ⇒ immutable ⇒ Record called on miss.
	res, err := m.Get(context.Background(), "pypi/files/x-1.0.0.tar.gz", "pypi", 72*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("payload-A")), "application/gzip", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// The manager fans the upstream body out to the client via an
	// unbuffered io.Pipe; the background storage-commit goroutine (which
	// runs Record) can't drain to EOF until the client side is consumed
	// or closed. Close without reading, matching how a disconnecting
	// client is handled elsewhere in this package.
	_ = res.Reader.Close()
	// Give the async storeAndCommit goroutine time to run.
	waitFor(t, func() bool { return len(rec.recordedKeys()) == 1 })
	if keys := rec.recordedKeys(); keys[0] != "pypi/files/x-1.0.0.tar.gz" {
		t.Errorf("Record key = %s", keys[0])
	}
}

func TestManager_MutableMissSkipsRecorder(t *testing.T) {
	m, rec := newTamperTestManager(t)
	// TTL < threshold ⇒ mutable metadata ⇒ recorder untouched.
	res, err := m.Get(context.Background(), "pypi/simple/x/index.html", "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("<html>")), "text/html", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Reader.Close()
	// Nothing should ever be recorded for a mutable key.
	time.Sleep(200 * time.Millisecond)
	if keys := rec.recordedKeys(); len(keys) != 0 {
		t.Errorf("mutable key recorded: %v", keys)
	}
}

// memStorage is a minimal in-memory Storage double for cache manager tests.
type memStorage struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStorage() *memStorage { return &memStorage{data: map[string][]byte{}} }
func (s *memStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.data[key] = b
	s.mu.Unlock()
	return nil
}
func (s *memStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[key]
	if !ok {
		return nil, 0, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}
func (s *memStorage) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}
func (s *memStorage) Delete(_ context.Context, key string) error { return nil }
func (s *memStorage) Stat(_ context.Context, key string) (*ObjectMeta, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *memStorage) List(_ context.Context, prefix string) ([]ObjectMeta, error) { return nil, nil }
func (s *memStorage) TotalSize(_ context.Context) (int64, error)                  { return 0, nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
