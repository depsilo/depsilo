package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func openStreamTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close stream test DB: %v", err)
		}
	})
	return database
}

func waitForNoInflight(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		m.inflightMu.Lock()
		count := len(m.inflight)
		m.inflightMu.Unlock()
		if count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("manager still has %d inflight fetches", count)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type delayedPutStorage struct {
	*memStorage
	release chan struct{}
}

func (s *delayedPutStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.memStorage.Put(ctx, key, r, size, contentType)
}

func TestManager_CacheMissDeliversBytesBeforeCachePutIsReady(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{memStorage: newMemStorage(), release: make(chan struct{})}
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	payload := []byte("python-package-bytes")
	ctx, tracker := WithTrackedForceRefresh(context.Background())
	result, err := m.Get(ctx, "pypi/files/pkg.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(payload)), "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(result.Reader, got); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("got %q, want %q", got, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("cache miss response was blocked by cache persistence")
	}
	close(storage.release)
	_, _ = io.Copy(io.Discard, result.Reader)
	_ = result.Reader.Close()
	if err := tracker.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManager_PrefetchMissWaitsForDurableCommit(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{memStorage: newMemStorage(), release: make(chan struct{})}
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	key := "pypi/simple/requests/index.html"
	payload := []byte("<html>requests versions</html>")

	done := make(chan error, 1)
	go func() {
		done <- m.Prefetch(context.Background(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return io.NopCloser(bytes.NewReader(payload)), "text/html", int64(len(payload)), "mock", nil
			})
	}()

	select {
	case err := <-done:
		t.Fatalf("prefetch returned before storage commit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(storage.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prefetch did not complete after storage was released")
	}

	reader, _, err := storage.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("stored body = %q, want %q", got, payload)
	}
	var entry db.CacheEntry
	if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	waitForNoInflight(t, m)
}

func TestManager_PrefetchHitDoesNotFetch(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/flask/index.html"
	payload := []byte("cached flask index")
	storage.data[key] = payload
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html",
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	fetched := false
	if err := m.Prefetch(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			fetched = true
			return nil, "", 0, "", errors.New("unexpected fetch")
		}); err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("fresh prefetch hit contacted upstream")
	}
	waitForNoInflight(t, m)

	deadline := time.Now().Add(time.Second)
	for {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.HitCount > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cache hit-count update did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManager_PrefetchErrorReleasesInflight(t *testing.T) {
	database := openStreamTestDB(t)
	m := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	wantErr := errors.New("upstream unavailable")
	err := m.Prefetch(context.Background(), "pypi/simple/missing/index.html", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "mock", wantErr
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("prefetch error = %v, want %v", err, wantErr)
	}
	waitForNoInflight(t, m)
}

func TestManager_PrefetchHonorsCancelledContext(t *testing.T) {
	database := openStreamTestDB(t)
	m := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetched := false
	err := m.Prefetch(ctx, "pypi/simple/cancelled/index.html", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			fetched = true
			return nil, "", 0, "", nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prefetch error = %v, want context.Canceled", err)
	}
	if fetched {
		t.Fatal("cancelled prefetch contacted upstream")
	}
	waitForNoInflight(t, m)
}

func TestManager_PrefetchCancellationReleasesActiveMiss(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{memStorage: newMemStorage(), release: make(chan struct{})}
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	ctx, cancel := context.WithCancel(context.Background())
	fetchStarted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- m.Prefetch(ctx, "pypi/simple/cancelled-active/index.html", "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				close(fetchStarted)
				body := []byte("non-empty response")
				return io.NopCloser(bytes.NewReader(body)), "text/html", int64(len(body)), "mock", nil
			})
	}()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("prefetch did not start upstream fetch")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("prefetch error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active prefetch did not stop after cancellation")
	}
	waitForNoInflight(t, m)
}

func TestManager_StaleMetadataRefreshesBeforeServing(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/av/index.html"
	storage.data[key] = []byte("versions: 17.1.0")
	if err := database.Create(&db.CacheEntry{Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	fetched := 0
	result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			fetched++
			body := []byte("versions: 17.1.0, 18.0.0")
			return io.NopCloser(bytes.NewReader(body)), "text/html", int64(len(body)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	got, err := io.ReadAll(result.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 1 {
		t.Fatalf("upstream fetches = %d, want 1", fetched)
	}
	if !bytes.Contains(got, []byte("18.0.0")) {
		t.Fatalf("first stale request got old metadata: %q", got)
	}
	if result.Hit {
		t.Fatal("synchronous metadata refresh must be reported as a miss")
	}
	deadline := time.Now().Add(time.Second)
	for {
		var entry db.CacheEntry
		if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
			t.Fatal(err)
		}
		if entry.CacheKind == db.CacheKindMetadata && entry.ExpiresAt.After(time.Now()) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshed metadata row was not updated: %+v", entry)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManager_StaleMetadataFallsBackWhenRefreshFails(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/av/index.html"
	old := []byte("versions: 17.1.0")
	storage.data[key] = old
	if err := database.Create(&db.CacheEntry{Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(-time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", errors.New("upstream unavailable")
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	got, err := io.ReadAll(result.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("fallback = %q, want %q", got, old)
	}
}

func TestManager_FreshMetadataOnlyRefreshesWhenForced(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/pillow/index.html"
	old := []byte("versions: 12.2.0")
	storage.data[key] = old
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	fetched := 0
	fetch := func(context.Context) (io.ReadCloser, string, int64, string, error) {
		fetched++
		body := []byte("versions: 12.3.0")
		return io.NopCloser(bytes.NewReader(body)), "text/html", int64(len(body)), "mock", nil
	}

	result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute, fetch)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(result.Reader)
	_ = result.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 0 || !result.Hit || !bytes.Equal(got, old) {
		t.Fatalf("fresh read fetched=%d hit=%v body=%q", fetched, result.Hit, got)
	}

	forceCtx, tracker := WithTrackedForceRefresh(context.Background())
	result, err = m.Get(forceCtx, key, "pypi", 5*time.Minute, fetch)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(result.Reader)
	_ = result.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if fetched != 1 || result.Hit || !bytes.Contains(got, []byte("12.3.0")) {
		t.Fatalf("forced read fetched=%d hit=%v body=%q", fetched, result.Hit, got)
	}
	if err := tracker.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManager_ForcedMetadataRefreshReportsFailureAndKeepsCache(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/pillow/index.html"
	old := []byte("versions: 12.2.0")
	storage.data[key] = old
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })

	_, err := m.Get(WithForceRefresh(context.Background()), key, "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "mock", errors.New("upstream unavailable")
		})
	if err == nil || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("forced refresh error = %v", err)
	}

	result, err := m.Get(context.Background(), key, "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			t.Fatal("fresh cache should remain available without another fetch")
			return nil, "", 0, "", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	got, err := io.ReadAll(result.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("cache after failed refresh = %q, want %q", got, old)
	}
}

func TestManager_TrackedForceRefreshWaitsForDurableCommitRegardlessOfTTL(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{memStorage: newMemStorage(), release: make(chan struct{})}
	key := "pypi/simple/pillow/index.html"
	storage.data[key] = []byte("old")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Deliberately use an index TTL above the immutable threshold. Key-based
	// metadata classification must still make the forced refresh effective.
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })
	ctx, tracker := WithTrackedForceRefresh(context.Background())
	newBody := []byte("new-index")
	result, err := m.Get(ctx, key, "pypi", 96*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(newBody)), "text/html", int64(len(newBody)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(newBody))
	if _, err := io.ReadFull(result.Reader, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newBody) || !tracker.Used() {
		t.Fatalf("forced response body=%q tracker used=%v", got, tracker.Used())
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- tracker.Wait(context.Background()) }()
	select {
	case err := <-waitDone:
		t.Fatalf("tracker completed before durable storage commit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(storage.release)
	_, _ = io.Copy(io.Discard, result.Reader)
	_ = result.Reader.Close()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("tracker did not complete after durable storage commit")
	}
	var entry db.CacheEntry
	if err := database.Where("key = ?", key).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.CacheKind != db.CacheKindMetadata {
		t.Fatalf("refreshed index cache kind = %q", entry.CacheKind)
	}
}

func TestManager_TrackedForceRefreshCancellationStopsItsFetch(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/pillow/index.html"
	storage.data[key] = []byte("old")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", CacheKind: db.CacheKindMetadata,
		StoragePath: key, ContentType: "text/html", ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage, database, NewEventBus(), 72*time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })

	requestCtx, cancel := context.WithCancel(context.Background())
	forceCtx, tracker := WithTrackedForceRefresh(requestCtx)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := m.Get(forceCtx, key, "pypi", 5*time.Minute,
			func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
				close(started)
				<-ctx.Done()
				return nil, "", 0, "mock", ctx.Err()
			})
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tracked refresh did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("tracked refresh error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tracked refresh outlived its request context")
	}
	if _, err := tracker.Outcome(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("tracker outcome error = %v, want context cancellation", err)
	}
	waitForNoInflight(t, m)
}
