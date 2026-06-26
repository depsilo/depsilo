package unit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	// `:memory:` SQLite gives each pool connection its own database. The
	// streaming tests do Create from a background goroutine and First from the
	// test thread; without pinning to a single conn the schema migrated above
	// is invisible on whichever conn the goroutine grabs.
	sqlDB, err := d.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := d.AutoMigrate(&db.CacheEntry{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

// trackingStorage wraps fakeStorage with hooks for observing Put lifecycle and
// optional slow-write simulation.
type trackingStorage struct {
	mu        sync.Mutex
	data      map[string][]byte
	putCalls  int32
	putErrors int32
	// putDone is closed by Put after it finishes for the given key.
	putDone map[string]chan struct{}
}

func newTrackingStorage() *trackingStorage {
	return &trackingStorage{
		data:    make(map[string][]byte),
		putDone: make(map[string]chan struct{}),
	}
}

func (s *trackingStorage) waitForPut(t *testing.T, key string, timeout time.Duration) {
	t.Helper()
	s.mu.Lock()
	ch, ok := s.putDone[key]
	if !ok {
		ch = make(chan struct{})
		s.putDone[key] = ch
	}
	s.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("Put(%q) did not complete within %v", key, timeout)
	}
}

func (s *trackingStorage) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *trackingStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.data[key]
	if !ok {
		return nil, 0, io.EOF
	}
	return io.NopCloser(bytes.NewReader(d)), int64(len(d)), nil
}

func (s *trackingStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	atomic.AddInt32(&s.putCalls, 1)
	d, err := io.ReadAll(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		ch, ok := s.putDone[key]
		if !ok {
			ch = make(chan struct{})
			s.putDone[key] = ch
		}
		// close-once
		select {
		case <-ch:
		default:
			close(ch)
		}
	}()
	if err != nil {
		atomic.AddInt32(&s.putErrors, 1)
		return err
	}
	s.data[key] = d
	return nil
}

func (s *trackingStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *trackingStorage) Stat(_ context.Context, _ string) (*cache.ObjectMeta, error) {
	return nil, nil
}

func (s *trackingStorage) List(_ context.Context, _ string) ([]cache.ObjectMeta, error) {
	return nil, nil
}

func (s *trackingStorage) TotalSize(_ context.Context) (int64, error) {
	return 0, nil
}

// slowReader emits bytes at a controlled rate so the test can observe
// first-byte latency from the client's perspective.
type slowReader struct {
	data       []byte
	off        int
	chunk      int
	pause      time.Duration
	firstByte  *time.Time // captured by the test
	firstMu    sync.Mutex
	errAfter   int      // if > 0, return errAfter bytes then an error
	errToThrow error    // optional error returned after errAfter bytes
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	if r.errAfter > 0 && r.off >= r.errAfter {
		return 0, r.errToThrow
	}
	if r.off > 0 {
		time.Sleep(r.pause)
	}
	end := r.off + r.chunk
	if end > len(r.data) {
		end = len(r.data)
	}
	if r.errAfter > 0 && end > r.errAfter {
		end = r.errAfter
	}
	n := copy(p, r.data[r.off:end])
	r.off += n
	return n, nil
}

func (r *slowReader) Close() error { return nil }

// makeFetchFn returns a FetchFunc that emits `body` via slowReader semantics.
func makeFetchFn(body []byte, chunk int, pause time.Duration) cache.FetchFunc {
	return func(_ context.Context) (io.ReadCloser, string, int64, string, error) {
		return &slowReader{data: body, chunk: chunk, pause: pause},
			"application/octet-stream", int64(len(body)), "test-upstream", nil
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMissPath_FirstByteIsFast verifies the client receives the first byte from
// the upstream before the entire body is buffered through storage. Today the
// implementation drains upstream → storage fully BEFORE returning a reader, so
// first-byte latency ≈ total download time. After the fix it should be ≈ the
// upstream's own first-byte latency.
func TestMissPath_FirstByteIsFast(t *testing.T) {
	d := newTestDB(t)
	store := newTrackingStorage()
	mgr := cache.NewManager(store, d, nil)

	// 1 MB body in 16 KB chunks, 50 ms between chunks → ~3.2 s total.
	const total = 1 << 20
	const chunk = 16 << 10
	const pause = 50 * time.Millisecond
	body := bytes.Repeat([]byte{0xAB}, total)

	start := time.Now()
	res, err := mgr.Get(context.Background(), "npm/lodash/-/lodash-4.17.21.tgz",
		"npm", time.Hour, makeFetchFn(body, chunk, pause))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer res.Reader.Close()

	buf := make([]byte, 1)
	if _, err := io.ReadFull(res.Reader, buf); err != nil {
		t.Fatalf("read first byte: %v", err)
	}
	firstByteLatency := time.Since(start)

	if firstByteLatency > 500*time.Millisecond {
		t.Errorf("first-byte latency = %v, want < 500ms (currently buffers whole body before returning)",
			firstByteLatency)
	}
}

// TestMissPath_ClientDisconnect_StorageStillCompletes verifies that when the
// client stops reading mid-stream, the upstream→storage pump keeps going and
// the cache entry lands in DB + storage.
func TestMissPath_ClientDisconnect_StorageStillCompletes(t *testing.T) {
	d := newTestDB(t)
	store := newTrackingStorage()
	mgr := cache.NewManager(store, d, nil)

	const total = 256 << 10 // 256 KB
	const chunk = 16 << 10
	const pause = 20 * time.Millisecond
	body := bytes.Repeat([]byte{0xCD}, total)
	key := "npm/abandoned/-/abandoned-1.0.0.tgz"

	res, err := mgr.Get(context.Background(), key, "npm", time.Hour,
		makeFetchFn(body, chunk, pause))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Read only the first chunk, then bail.
	buf := make([]byte, chunk)
	if _, err := io.ReadFull(res.Reader, buf); err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	if err := res.Reader.Close(); err != nil {
		t.Fatalf("close reader early: %v", err)
	}

	// Storage Put should still finish.
	store.waitForPut(t, key, 5*time.Second)

	// Stored bytes should equal full body.
	stored := store.data[key]
	if len(stored) != total {
		t.Fatalf("stored %d bytes after client disconnect, want %d", len(stored), total)
	}
	if !bytes.Equal(stored, body) {
		t.Error("stored data does not match upstream body after client disconnect")
	}

	// DB entry should be present.
	var entry db.CacheEntry
	if err := d.Where("key = ?", key).First(&entry).Error; err != nil {
		t.Fatalf("DB entry missing after client disconnect: %v", err)
	}
	if entry.Size != int64(total) {
		t.Errorf("DB entry size = %d, want %d", entry.Size, total)
	}
}

// TestMissPath_UpstreamErrorMidStream_NoOrphan verifies that when upstream
// errors after partial bytes, no orphan storage file remains AND no DB entry is
// committed. The next request should re-fetch from upstream.
func TestMissPath_UpstreamErrorMidStream_NoOrphan(t *testing.T) {
	d := newTestDB(t)
	store := newTrackingStorage()
	mgr := cache.NewManager(store, d, nil)

	const total = 128 << 10
	const chunk = 8 << 10
	body := bytes.Repeat([]byte{0xEF}, total)
	key := "npm/broken/-/broken-2.0.0.tgz"

	fetchFn := func(_ context.Context) (io.ReadCloser, string, int64, string, error) {
		return &slowReader{
				data:       body,
				chunk:      chunk,
				pause:      5 * time.Millisecond,
				errAfter:   32 << 10,
				errToThrow: errors.New("upstream RST"),
			},
			"application/octet-stream", int64(total), "test-upstream", nil
	}

	res, err := mgr.Get(context.Background(), key, "npm", time.Hour, fetchFn)
	if err == nil {
		// Drain to surface the error.
		_, copyErr := io.Copy(io.Discard, res.Reader)
		_ = res.Reader.Close()
		if copyErr == nil {
			t.Fatal("expected upstream error to surface, got nil")
		}
	}

	// Give any background commit a tick to settle.
	time.Sleep(100 * time.Millisecond)

	// No DB entry should be present.
	var entry db.CacheEntry
	dbErr := d.Where("key = ?", key).First(&entry).Error
	if dbErr == nil {
		t.Errorf("DB entry exists for failed upstream fetch (got size=%d), want none", entry.Size)
	}

	// No storage entry should be present.
	exists, _ := store.Exists(context.Background(), key)
	if exists {
		t.Errorf("storage has orphan file for failed upstream fetch (%d bytes), want none",
			len(store.data[key]))
	}
}

// TestMissPath_Singleflight_ConcurrentSameKey verifies that two concurrent
// requests for the same key result in exactly one upstream fetch. Per the
// design decision, the second request waits for the first to complete and then
// reads from the cache (it does NOT tee into the live stream).
func TestMissPath_Singleflight_ConcurrentSameKey(t *testing.T) {
	d := newTestDB(t)
	store := newTrackingStorage()
	mgr := cache.NewManager(store, d, nil)

	const total = 64 << 10
	const chunk = 8 << 10
	const pause = 30 * time.Millisecond
	body := bytes.Repeat([]byte{0x42}, total)
	key := "npm/sf/-/sf-1.0.0.tgz"

	var fetchCount int32
	fetchFn := func(_ context.Context) (io.ReadCloser, string, int64, string, error) {
		atomic.AddInt32(&fetchCount, 1)
		return &slowReader{data: body, chunk: chunk, pause: pause},
			"application/octet-stream", int64(total), "test-upstream", nil
	}

	var wg sync.WaitGroup
	results := make([][]byte, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := mgr.Get(context.Background(), key, "npm", time.Hour, fetchFn)
			if err != nil {
				errs[i] = err
				return
			}
			defer res.Reader.Close()
			b, rerr := io.ReadAll(res.Reader)
			if rerr != nil {
				errs[i] = rerr
				return
			}
			results[i] = b
		}()
		// Stagger so the second request arrives during the first's fetch.
		if i == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&fetchCount); got != 1 {
		t.Errorf("upstream fetched %d times for same key, want 1 (singleflight broken)", got)
	}
	for i, r := range results {
		if !bytes.Equal(r, body) {
			t.Errorf("goroutine %d: body mismatch (got %d bytes, want %d)", i, len(r), total)
		}
	}
}

// TestMissPath_FullBodyRelayedToClient sanity-checks that the streaming reader
// delivers the complete body byte-for-byte to a normal client.
func TestMissPath_FullBodyRelayedToClient(t *testing.T) {
	d := newTestDB(t)
	store := newTrackingStorage()
	mgr := cache.NewManager(store, d, nil)

	const total = 200 << 10
	const chunk = 4 << 10
	body := make([]byte, total)
	for i := range body {
		body[i] = byte(i % 211)
	}
	key := "npm/full/-/full-1.0.0.tgz"

	res, err := mgr.Get(context.Background(), key, "npm", time.Hour,
		makeFetchFn(body, chunk, 1*time.Millisecond))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer res.Reader.Close()
	got, err := io.ReadAll(res.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("client received %d bytes != upstream %d", len(got), total)
	}
	store.waitForPut(t, key, 5*time.Second)
	if !bytes.Equal(store.data[key], body) {
		t.Error("storage content differs from upstream body")
	}

	// Re-request should be a HIT (no further upstream fetch — the Get path
	// reads from storage directly).
	res2, err := mgr.Get(context.Background(), key, "npm", time.Hour,
		func(_ context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", fmt.Errorf("upstream should not be called on hit")
		})
	if err != nil {
		t.Fatalf("Get (hit): %v", err)
	}
	defer res2.Reader.Close()
	if !res2.Hit {
		t.Error("expected Hit=true on second request")
	}
}
