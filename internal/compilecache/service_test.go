package compilecache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
)

const (
	testNamespace = "team-a"
	testKeyA      = "0123456789abcdef0123456789abcdef01234567"
	testKeyB      = "1123456789abcdef0123456789abcdef01234567"
	testKeyC      = "2123456789abcdef0123456789abcdef01234567"
	testKeyD      = "3123456789abcdef0123456789abcdef01234567"
)

func testCCacheID(namespace, key string) ArtifactID {
	id, err := ParseCCacheArtifact(namespace, key)
	if err != nil {
		panic("invalid ccache test identity: " + err.Error())
	}
	return id
}

func newServiceFixture(t *testing.T, maxBytes int64) (*Service, *cache.LocalStorage) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "compile-cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheEntry{}, &db.CompileCacheDeletion{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(storage, database, Limits{
		MaxBytes: maxBytes, MaxEntries: 1000, MaxEntryBytes: maxBytes,
		NamespaceMaxBytes: maxBytes, NamespaceMaxEntries: 1000,
		MaxConcurrentUploads: 4, MaxInflightUploadBytes: maxBytes,
		UploadTimeout: time.Minute, MaxConcurrentDownloads: 4, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, storage
}

func newServiceWithLimits(t *testing.T, limits Limits) (*Service, *cache.LocalStorage) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "compile-cache-limits.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheEntry{}, &db.CompileCacheDeletion{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	if limits.UploadTimeout == 0 {
		limits.UploadTimeout = time.Minute
	}
	if limits.MaxConcurrentDownloads == 0 {
		limits.MaxConcurrentDownloads = 4
	}
	if limits.DownloadTimeout == 0 {
		limits.DownloadTimeout = time.Minute
	}
	service, err := NewService(storage, database, limits)
	if err != nil {
		t.Fatal(err)
	}
	return service, storage
}

func TestServiceRoundTripOverwriteAndDelete(t *testing.T) {
	service, _ := newServiceFixture(t, 1024)
	ctx := context.Background()
	first := []byte("first")
	id := testCCacheID(testNamespace, testKeyA)
	result, err := service.Put(ctx, id, bytes.NewReader(first), int64(len(first)))
	if err != nil || !result.Created {
		t.Fatalf("first Put = %+v, %v", result, err)
	}
	if size, err := service.Stat(ctx, id); err != nil || size != int64(len(first)) {
		t.Fatalf("Stat = %d, %v", size, err)
	}
	entry, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(entry.Body)
	entry.Body.Close()
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("Open body = %q, %v", got, err)
	}
	if err := service.FlushTouches(ctx); err != nil {
		t.Fatal(err)
	}
	second := []byte("second-version")
	result, err = service.Put(ctx, id, bytes.NewReader(second), int64(len(second)))
	if err != nil || result.Created {
		t.Fatalf("overwrite Put = %+v, %v", result, err)
	}
	stats, err := service.Stats(ctx)
	if err != nil || stats.Entries != 1 || stats.SizeBytes != int64(len(second)) || stats.Hits != 1 {
		t.Fatalf("Stats after overwrite = %+v, %v", stats, err)
	}
	deleted, err := service.Delete(ctx, id)
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	if deleted, err := service.Delete(ctx, id); err != nil || deleted {
		t.Fatalf("idempotent Delete = %v, %v", deleted, err)
	}
	if _, err := service.Open(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open deleted error = %v, want ErrNotFound", err)
	}
}

func TestServiceIsolatesProtocolsWithinNamespace(t *testing.T) {
	service, _ := newServiceFixture(t, 1024)
	ctx := context.Background()
	ccacheID := testCCacheID(testNamespace, testKeyA)
	const sccacheKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sccacheID, err := ParseSCCacheArtifact(testNamespace, "0/1/2/"+sccacheKey)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Put(ctx, ccacheID, bytes.NewReader([]byte("ccache")), 6); err != nil {
		t.Fatalf("put ccache artifact: %v", err)
	}
	if _, err := service.Put(ctx, sccacheID, bytes.NewReader([]byte("sccache")), 7); err != nil {
		t.Fatalf("put sccache artifact: %v", err)
	}

	var ccacheEntry, sccacheEntry db.CompileCacheEntry
	if err := service.db.Where(
		"protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA,
	).First(&ccacheEntry).Error; err != nil {
		t.Fatalf("read ccache metadata: %v", err)
	}
	if err := service.db.Where(
		"protocol = ? AND namespace = ? AND key = ?", ProtocolSCCache, testNamespace, sccacheKey,
	).First(&sccacheEntry).Error; err != nil {
		t.Fatalf("read sccache metadata: %v", err)
	}
	if !strings.HasPrefix(ccacheEntry.StoragePath, "v1/ccache/"+testNamespace+"/") {
		t.Errorf("ccache storage path = %q", ccacheEntry.StoragePath)
	}
	if !strings.HasPrefix(sccacheEntry.StoragePath, "v1/sccache/"+testNamespace+"/") {
		t.Errorf("sccache storage path = %q", sccacheEntry.StoragePath)
	}
	if ccacheEntry.StoragePath == sccacheEntry.StoragePath {
		t.Fatal("protocol artifacts share a storage object")
	}

	if deleted, err := service.Delete(ctx, ccacheID); err != nil || !deleted {
		t.Fatalf("delete ccache artifact = %v, %v", deleted, err)
	}
	sccacheObject, err := service.Open(ctx, sccacheID)
	if err != nil {
		t.Fatalf("ccache delete removed sccache artifact: %v", err)
	}
	body, readErr := io.ReadAll(sccacheObject.Body)
	closeErr := sccacheObject.Body.Close()
	if readErr != nil || closeErr != nil || string(body) != "sccache" {
		t.Fatalf("sccache body after ccache delete = %q, read=%v close=%v", body, readErr, closeErr)
	}
}

type existsCountingStorage struct {
	cache.Storage
	existsCalls atomic.Int64
}

func (storage *existsCountingStorage) Exists(ctx context.Context, key string) (bool, error) {
	storage.existsCalls.Add(1)
	return storage.Storage.Exists(ctx, key)
}

func TestServiceBoundsConcurrentDownloadsUntilBodyClose(t *testing.T) {
	service, storage := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 64,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 2, MaxInflightUploadBytes: 128,
		UploadTimeout: time.Minute, MaxConcurrentDownloads: 1, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	ctx := context.Background()
	for _, key := range []string{testKeyA, testKeyB} {
		if _, err := service.Put(ctx, testCCacheID(testNamespace, key), bytes.NewReader([]byte("value")), 5); err != nil {
			t.Fatal(err)
		}
	}
	counting := &existsCountingStorage{Storage: storage}
	service.storage = counting
	first, err := service.Open(ctx, testCCacheID(testNamespace, testKeyA))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Open(ctx, testCCacheID(testNamespace, testKeyB)); !errors.Is(err, ErrDownloadBusy) {
		t.Fatalf("second Open error = %v, want ErrDownloadBusy", err)
	}
	if _, err := service.Stat(ctx, testCCacheID(testNamespace, testKeyB)); !errors.Is(err, ErrDownloadBusy) {
		t.Fatalf("Stat while download slot is occupied = %v, want ErrDownloadBusy", err)
	}
	if got := counting.existsCalls.Load(); got != 1 {
		t.Fatalf("busy Open/Stat reached storage.Exists %d times, want only the first Open", got)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := service.Open(ctx, testCCacheID(testNamespace, testKeyB))
	if err != nil {
		t.Fatalf("Open after body close: %v", err)
	}
	second.Body.Close()
}

func TestServiceRejectsMismatchedAndOversizedBodies(t *testing.T) {
	service, storage := newServiceFixture(t, 8)
	ctx := context.Background()
	if _, err := service.Put(ctx, testCCacheID(testNamespace, testKeyA), bytes.NewReader([]byte("short")), 6); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("short body error = %v, want ErrSizeMismatch", err)
	}
	if _, err := service.Put(ctx, testCCacheID(testNamespace, testKeyA), bytes.NewReader([]byte("toolong")), 6); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("long body error = %v, want ErrSizeMismatch", err)
	}
	objects, err := storage.List(ctx, "v1/ccache")
	if err != nil || len(objects) != 0 {
		t.Fatalf("partial objects = %v, %v", objects, err)
	}
	if _, err := service.Put(ctx, testCCacheID(testNamespace, testKeyA), bytes.NewReader(make([]byte, 9)), 9); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized error = %v, want ErrTooLarge", err)
	}
}

func TestIncompleteUploadCannotEvictCommittedEntries(t *testing.T) {
	service, _ := newServiceFixture(t, 8)
	ctx := context.Background()
	for _, key := range []string{testKeyA, testKeyB} {
		if _, err := service.Put(ctx, testCCacheID(testNamespace, key), bytes.NewReader([]byte("four")), 4); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Put(ctx, testCCacheID(testNamespace, testKeyC), bytes.NewReader([]byte("short")), 8); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("incomplete upload error = %v, want ErrSizeMismatch", err)
	}
	for _, key := range []string{testKeyA, testKeyB} {
		if _, err := service.Stat(ctx, testCCacheID(testNamespace, key)); err != nil {
			t.Fatalf("committed key %s was evicted: %v", key, err)
		}
	}
}

func TestServiceReclaimsOldEntriesForCapacity(t *testing.T) {
	service, _ := newServiceFixture(t, 8)
	ctx := context.Background()
	for _, key := range []string{testKeyA, testKeyB, testKeyC} {
		if _, err := service.Put(ctx, testCCacheID(testNamespace, key), bytes.NewReader([]byte("four")), 4); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	stats, err := service.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SizeBytes > 8 || stats.Entries != 2 {
		t.Fatalf("Stats after capacity reclaim = %+v", stats)
	}
	if _, err := service.Open(ctx, testCCacheID(testNamespace, testKeyA)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest entry error = %v, want ErrNotFound", err)
	}
}

func TestServiceEnforcesNamespaceAndGlobalEntryQuotas(t *testing.T) {
	service, _ := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 2, MaxEntryBytes: 64,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 1,
		MaxConcurrentUploads: 4, MaxInflightUploadBytes: 128,
		HighWatermarkPercent: 90,
	})
	ctx := context.Background()
	put := func(namespace, key string) {
		t.Helper()
		if _, err := service.Put(ctx, testCCacheID(namespace, key), bytes.NewReader([]byte("x")), 1); err != nil {
			t.Fatalf("Put %s/%s: %v", namespace, key, err)
		}
	}
	put("team-a", testKeyA)
	put("team-b", testKeyB)
	put("team-a", testKeyC)
	if _, err := service.Stat(ctx, testCCacheID("team-a", testKeyA)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("namespace LRU kept old key: %v", err)
	}
	if _, err := service.Stat(ctx, testCCacheID("team-b", testKeyB)); err != nil {
		t.Fatalf("namespace cleanup evicted another namespace: %v", err)
	}
	put("team-c", testKeyD)
	stats, err := service.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 2 || stats.NamespaceCount != 2 || stats.MaxEntries != 2 {
		t.Fatalf("stats after quota cleanup = %+v", stats)
	}
}

type controlledStorage struct {
	cache.Storage
	failDelete  atomic.Bool
	existsCalls atomic.Int64
	putStarted  chan struct{}
	putRelease  chan struct{}
	activePuts  atomic.Int64
	maxPuts     atomic.Int64
}

func (s *controlledStorage) Exists(ctx context.Context, key string) (bool, error) {
	s.existsCalls.Add(1)
	return s.Storage.Exists(ctx, key)
}

func (s *controlledStorage) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	active := s.activePuts.Add(1)
	defer s.activePuts.Add(-1)
	for {
		maximum := s.maxPuts.Load()
		if active <= maximum || s.maxPuts.CompareAndSwap(maximum, active) {
			break
		}
	}
	if s.putStarted != nil {
		s.putStarted <- struct{}{}
	}
	if s.putRelease != nil {
		select {
		case <-s.putRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Storage.Put(ctx, key, reader, size, contentType)
}

func (s *controlledStorage) Delete(ctx context.Context, key string) error {
	if s.failDelete.Load() {
		return errors.New("injected delete failure")
	}
	return s.Storage.Delete(ctx, key)
}

func TestServiceBoundsConcurrentStagingBytes(t *testing.T) {
	service, storage := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 4,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 2, MaxInflightUploadBytes: 4,
		HighWatermarkPercent: 90,
	})
	controlled := &controlledStorage{
		Storage: storage, putStarted: make(chan struct{}, 2), putRelease: make(chan struct{}, 2),
	}
	service.storage = controlled
	errorsSeen := make(chan error, 2)
	for _, key := range []string{testKeyA, testKeyB} {
		id := testCCacheID(testNamespace, key)
		go func() {
			_, err := service.Put(context.Background(), id, bytes.NewReader([]byte("four")), 4)
			errorsSeen <- err
		}()
	}
	select {
	case <-controlled.putStarted:
	case <-time.After(time.Second):
		t.Fatal("first upload did not start")
	}
	select {
	case <-controlled.putStarted:
		t.Fatal("second upload bypassed in-flight byte limit")
	case <-time.After(50 * time.Millisecond):
	}
	controlled.putRelease <- struct{}{}
	select {
	case <-controlled.putStarted:
	case <-time.After(time.Second):
		t.Fatal("second upload did not start after capacity was released")
	}
	controlled.putRelease <- struct{}{}
	for range 2 {
		if err := <-errorsSeen; err != nil {
			t.Fatal(err)
		}
	}
	if got := controlled.maxPuts.Load(); got != 1 {
		t.Fatalf("max concurrent staged puts = %d, want 1", got)
	}
}

func TestServiceRejectsUploadsBeyondBoundedQueue(t *testing.T) {
	service, storage := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 4,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 1, MaxQueuedUploads: 0, MaxInflightUploadBytes: 4,
		UploadTimeout: time.Minute, HighWatermarkPercent: 90,
	})
	controlled := &controlledStorage{
		Storage: storage, putStarted: make(chan struct{}, 1), putRelease: make(chan struct{}, 1),
	}
	service.storage = controlled
	firstDone := make(chan error, 1)
	firstID := testCCacheID(testNamespace, testKeyA)
	go func() {
		_, err := service.Put(context.Background(), firstID, bytes.NewReader([]byte("four")), 4)
		firstDone <- err
	}()
	select {
	case <-controlled.putStarted:
	case <-time.After(time.Second):
		t.Fatal("first upload did not start")
	}
	started := time.Now()
	if _, err := service.Put(context.Background(), testCCacheID(testNamespace, testKeyB), bytes.NewReader([]byte("four")), 4); !errors.Is(err, ErrUploadBusy) {
		t.Fatalf("overflow upload error = %v, want ErrUploadBusy", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("overflow upload did not fail fast: %s", elapsed)
	}
	controlled.putRelease <- struct{}{}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

type blockingCleanupStorage struct {
	cache.Storage
	deleteStarted chan struct{}
	deleteRelease chan struct{}
	startedOnce   sync.Once
}

func (storage *blockingCleanupStorage) Put(ctx context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func (storage *blockingCleanupStorage) Delete(_ context.Context, _ string) error {
	storage.startedOnce.Do(func() { close(storage.deleteStarted) })
	<-storage.deleteRelease
	return nil
}

func TestUploadTimeoutReleasesActiveSlotBeforeBlockingCleanup(t *testing.T) {
	service, storage := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 4,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 1, MaxQueuedUploads: 0, MaxInflightUploadBytes: 4,
		UploadTimeout: 25 * time.Millisecond, HighWatermarkPercent: 90,
	})
	blocking := &blockingCleanupStorage{
		Storage: storage, deleteStarted: make(chan struct{}), deleteRelease: make(chan struct{}),
	}
	service.storage = blocking
	done := make(chan error, 1)
	id := testCCacheID(testNamespace, testKeyA)
	go func() {
		_, err := service.Put(context.Background(), id, bytes.NewReader([]byte("four")), 4)
		done <- err
	}()
	select {
	case <-blocking.deleteStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out upload did not enter object cleanup")
	}
	if len(service.uploadSlots) != 0 {
		t.Fatal("upload slot remained occupied during object cleanup")
	}
	if service.inflight.TryAcquire(1) {
		service.inflight.Release(1)
		t.Fatal("staging-byte budget was released before failed object cleanup completed")
	}
	close(blocking.deleteRelease)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out Put error = %v, want DeadlineExceeded", err)
	}
	if !service.inflight.TryAcquire(4) {
		t.Fatal("staging-byte budget was not released after failed upload cleanup")
	}
	service.inflight.Release(4)
}

func TestServiceUploadTimeoutReleasesStagingBudget(t *testing.T) {
	service, storage := newServiceWithLimits(t, Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 4,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 1, MaxInflightUploadBytes: 4,
		UploadTimeout: 25 * time.Millisecond, HighWatermarkPercent: 90,
	})
	controlled := &controlledStorage{
		Storage: storage, putStarted: make(chan struct{}, 1), putRelease: make(chan struct{}),
	}
	service.storage = controlled
	started := time.Now()
	_, err := service.Put(context.Background(), testCCacheID(testNamespace, testKeyA), bytes.NewReader([]byte("four")), 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out Put error = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timed out Put took %s", elapsed)
	}
	if service.usage.Load() != 0 || service.entries.Load() != 0 {
		t.Fatalf("timed out Put reserved capacity: bytes=%d entries=%d", service.usage.Load(), service.entries.Load())
	}

	controlled.putStarted = nil
	controlled.putRelease = nil
	// Keep the second operation focused on budget release rather than racing a
	// deliberately tiny SQLite transaction deadline under -race.
	service.limits.UploadTimeout = time.Minute
	if _, err := service.Put(context.Background(), testCCacheID(testNamespace, testKeyB), bytes.NewReader([]byte("four")), 4); err != nil {
		t.Fatalf("Put after timeout did not regain upload budget: %v", err)
	}
}

func TestFailedUploadKeepsDelayedFinalAndTempMarkers(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	fixed := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixed }
	_, err := service.Put(context.Background(), testCCacheID(testNamespace, testKeyA), bytes.NewReader([]byte("short")), 6)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("short Put error = %v, want ErrSizeMismatch", err)
	}
	var pending []db.CompileCacheDeletion
	if err := service.db.Order("storage_path ASC").Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("staging markers = %d, want final and .tmp", len(pending))
	}
	if !strings.HasSuffix(pending[1].StoragePath, ".tmp") && !strings.HasSuffix(pending[0].StoragePath, ".tmp") {
		t.Fatalf("staging markers do not cover local temp path: %+v", pending)
	}
	wantNotBefore := fixed.Add(service.limits.UploadTimeout + stagingCleanupGrace)
	for _, marker := range pending {
		if !marker.NotBefore.Equal(wantNotBefore) {
			t.Fatalf("marker NotBefore = %s, want %s", marker.NotBefore, wantNotBefore)
		}
	}
	objects, err := storage.List(context.Background(), "v1/ccache")
	if err != nil || len(objects) != 0 {
		t.Fatalf("failed upload objects = %v, %v", objects, err)
	}
	service.now = func() time.Time { return wantNotBefore.Add(time.Second) }
	if err := service.ProcessPendingDeletions(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if err := service.db.Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expired staging markers remain: %+v", pending)
	}
}

func TestServicePersistsAndRetriesFailedObjectDeletion(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	controlled := &controlledStorage{Storage: storage}
	service.storage = controlled
	ctx := context.Background()
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("value")), 5); err != nil {
		t.Fatal(err)
	}
	var entry db.CompileCacheEntry
	if err := service.db.Where("protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	controlled.failDelete.Store(true)
	if deleted, err := service.Delete(ctx, id); err != nil || !deleted {
		t.Fatalf("Delete = %v, %v", deleted, err)
	}
	var queued int64
	if err := service.db.Model(&db.CompileCacheDeletion{}).Count(&queued).Error; err != nil || queued != 1 {
		t.Fatalf("queued deletions = %d, %v", queued, err)
	}
	if exists, err := storage.Exists(ctx, entry.StoragePath); err != nil || !exists {
		t.Fatalf("object after injected failure exists=%v err=%v", exists, err)
	}
	controlled.failDelete.Store(false)
	if err := service.ProcessPendingDeletions(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := service.db.Model(&db.CompileCacheDeletion{}).Count(&queued).Error; err != nil || queued != 0 {
		t.Fatalf("queued deletions after retry = %d, %v", queued, err)
	}
	if exists, err := storage.Exists(ctx, entry.StoragePath); err != nil || exists {
		t.Fatalf("object after retry exists=%v err=%v", exists, err)
	}
}

func TestServiceSerializesConcurrentWritesToOneKey(t *testing.T) {
	service, _ := newServiceFixture(t, 1024)
	ctx := context.Background()
	payloads := [][]byte{bytes.Repeat([]byte("a"), 128), bytes.Repeat([]byte("b"), 128)}
	start := make(chan struct{})
	var wait sync.WaitGroup
	errorsSeen := make(chan error, len(payloads))
	id := testCCacheID(testNamespace, testKeyA)
	for _, payload := range payloads {
		payload := payload
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := service.Put(ctx, id, bytes.NewReader(payload), int64(len(payload)))
			errorsSeen <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	entry, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(entry.Body)
	entry.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payloads[0]) && !bytes.Equal(got, payloads[1]) {
		t.Fatalf("stored body is torn: %q", got)
	}
}

func TestFlushTouchesCannotRestoreDeletedOrStaleMetadata(t *testing.T) {
	service, _ := newServiceFixture(t, 1024)
	ctx := context.Background()
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("old")), 3); err != nil {
		t.Fatal(err)
	}
	opened, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	opened.Body.Close()
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("new-value")), 9); err != nil {
		t.Fatal(err)
	}
	if err := service.FlushTouches(ctx); err != nil {
		t.Fatal(err)
	}
	var entry db.CompileCacheEntry
	if err := service.db.Where("protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.Size != 9 || entry.HitCount != 1 {
		t.Fatalf("metadata after overwrite/flush = %+v", entry)
	}

	opened, err = service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	opened.Body.Close()
	if _, err := service.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := service.FlushTouches(ctx); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := service.db.Model(&db.CompileCacheEntry{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 || service.usage.Load() != 0 {
		t.Fatalf("deleted entry was restored: count=%d usage=%d", count, service.usage.Load())
	}
}

func TestFailedOverwritePreservesCommittedGeneration(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	original := []byte("original")
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader(original), int64(len(original))); err != nil {
		t.Fatal(err)
	}
	if err := service.db.Exec(`CREATE TRIGGER fail_compile_cache_update
		BEFORE UPDATE OF storage_path ON compile_cache_entries
		BEGIN SELECT RAISE(ABORT, 'injected update failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("replacement")), 11); err == nil {
		t.Fatal("overwrite unexpectedly succeeded")
	}
	entry, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(entry.Body)
	entry.Body.Close()
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("committed body after failed overwrite = %q, %v", got, err)
	}
	objects, err := storage.List(ctx, "v1/ccache")
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects after failed overwrite = %v, %v", objects, err)
	}
}

func TestShortOverwritePreservesCommittedGeneration(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	original := []byte("original")
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader(original), int64(len(original))); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("short")), 6); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("short overwrite error = %v, want ErrSizeMismatch", err)
	}
	entry, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(entry.Body)
	entry.Body.Close()
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("committed body after short overwrite = %q, %v", got, err)
	}
	objects, err := storage.List(ctx, "v1/ccache")
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects after short overwrite = %v, %v", objects, err)
	}
}

func TestServiceInvalidatesSizeCorruptObject(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("original")), 8); err != nil {
		t.Fatal(err)
	}
	var metadata db.CompileCacheEntry
	if err := service.db.Where("protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA).First(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(ctx, metadata.StoragePath, bytes.NewReader([]byte("bad")), 3, compilerArtifactContentType); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Open(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open corrupt object error = %v, want ErrNotFound", err)
	}
	stats, err := service.Stats(ctx)
	if err != nil || stats.SizeBytes != 0 || stats.Entries != 0 {
		t.Fatalf("Stats after corruption = %+v, %v", stats, err)
	}
	if exists, err := storage.Exists(ctx, metadata.StoragePath); err != nil || exists {
		t.Fatalf("corrupt object still exists=%v err=%v", exists, err)
	}
}

func TestServiceRejectsAndInvalidatesChecksumCorruptObject(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	id := testCCacheID(testNamespace, testKeyA)
	if _, err := service.Put(ctx, id, bytes.NewReader([]byte("original")), 8); err != nil {
		t.Fatal(err)
	}
	var metadata db.CompileCacheEntry
	if err := service.db.Where(
		"protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA,
	).First(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(ctx, metadata.StoragePath, bytes.NewReader([]byte("tampered")), 8, compilerArtifactContentType); err != nil {
		t.Fatal(err)
	}

	entry, err := service.Open(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(entry.Body)
	closeErr := entry.Body.Close()
	if !errors.Is(readErr, ErrChecksumMismatch) || closeErr != nil {
		t.Fatalf("corrupt read = body %q, read %v, close %v", body, readErr, closeErr)
	}
	if len(body) != 0 {
		t.Fatalf("small corrupt object leaked %d bytes before validation", len(body))
	}
	if _, err := service.Open(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("checksum-corrupt entry remained visible: %v", err)
	}
	stats, err := service.Stats(ctx)
	if err != nil || stats.SizeBytes != 0 || stats.Entries != 0 {
		t.Fatalf("Stats after checksum corruption = %+v, %v", stats, err)
	}
	var queued int64
	if err := service.db.Model(&db.CompileCacheDeletion{}).Where("storage_path = ?", metadata.StoragePath).Count(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("checksum-corrupt object queued deletions = %d, want 1", queued)
	}
}

func TestReconcileUsesObjectListingAndRemovesSizeMismatch(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	for _, key := range []string{testKeyA, testKeyB} {
		if _, err := service.Put(ctx, testCCacheID(testNamespace, key), bytes.NewReader([]byte("four")), 4); err != nil {
			t.Fatal(err)
		}
	}
	var corrupt db.CompileCacheEntry
	if err := service.db.Where("protocol = ? AND namespace = ? AND key = ?", ProtocolCCache, testNamespace, testKeyA).First(&corrupt).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.Put(ctx, corrupt.StoragePath, bytes.NewReader([]byte("x")), 1, compilerArtifactContentType); err != nil {
		t.Fatal(err)
	}
	tempPath := "v1/ccache/team-a/objects/ff/orphan/generation.tmp"
	if err := storage.Put(ctx, tempPath, bytes.NewReader([]byte("partial")), 7, compilerArtifactContentType); err != nil {
		t.Fatal(err)
	}
	controlled := &controlledStorage{Storage: storage}
	restarted, err := NewService(controlled, service.db, service.limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Reconcile(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if got := controlled.existsCalls.Load(); got != 0 {
		t.Fatalf("Reconcile issued %d per-object existence checks", got)
	}
	stats, err := restarted.Stats(ctx)
	if err != nil || stats.SizeBytes != 4 || stats.Entries != 1 {
		t.Fatalf("Stats after Reconcile = %+v, %v", stats, err)
	}
	if _, err := restarted.Stat(ctx, testCCacheID(testNamespace, testKeyA)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("corrupt entry survived Reconcile: %v", err)
	}
	if _, err := restarted.Stat(ctx, testCCacheID(testNamespace, testKeyB)); err != nil {
		t.Fatalf("healthy entry was removed by Reconcile: %v", err)
	}
	if exists, err := storage.Exists(ctx, tempPath); err != nil || exists {
		t.Fatalf("recent orphan temp file exists=%v err=%v after startup reconciliation", exists, err)
	}
}

type lexicalPrefixStorage struct {
	cache.Storage
	extra   cache.ObjectMeta
	deleted atomic.Bool
}

func (storage *lexicalPrefixStorage) List(ctx context.Context, prefix string) ([]cache.ObjectMeta, error) {
	objects, err := storage.Storage.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return append(objects, storage.extra), nil
}

func (storage *lexicalPrefixStorage) Delete(ctx context.Context, key string) error {
	if key == storage.extra.Key {
		storage.deleted.Store(true)
		return nil
	}
	return storage.Storage.Delete(ctx, key)
}

func TestReconcileIgnoresNeighboringLexicalPrefixes(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	wrapped := &lexicalPrefixStorage{
		Storage: storage,
		extra: cache.ObjectMeta{
			Key:          "v10/unrelated-object",
			Size:         7,
			LastModified: time.Now().Add(-time.Hour),
		},
	}
	service.storage = wrapped
	if err := service.Reconcile(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if wrapped.deleted.Load() {
		t.Fatal("reconciliation deleted an object outside the v1/ namespace")
	}
	var queued int64
	if err := service.db.Model(&db.CompileCacheDeletion{}).Count(&queued).Error; err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("reconciliation queued %d deletion(s) outside the v1/ namespace", queued)
	}
}

func TestEnforceLimitsConvergesExistingMetadata(t *testing.T) {
	service, storage := newServiceFixture(t, 1024)
	ctx := context.Background()
	for _, item := range []struct {
		namespace string
		key       string
	}{
		{testNamespace, testKeyA},
		{testNamespace, testKeyB},
		{"team-b", testKeyC},
	} {
		if _, err := service.Put(ctx, testCCacheID(item.namespace, item.key), bytes.NewReader([]byte("x")), 1); err != nil {
			t.Fatal(err)
		}
	}

	tight, err := NewService(storage, service.db, Limits{
		MaxBytes: 1024, MaxEntries: 2, MaxEntryBytes: 64,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 1,
		MaxConcurrentUploads: 2, MaxInflightUploadBytes: 128,
		UploadTimeout: time.Minute, MaxConcurrentDownloads: 4, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tight.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	result, err := tight.EnforceLimits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedEntries != 1 || result.Entries != 2 {
		t.Fatalf("EnforceLimits result = %+v", result)
	}
	if _, err := tight.Stat(ctx, testCCacheID(testNamespace, testKeyA)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old namespace entry survived limit convergence: %v", err)
	}
	if _, err := tight.Stat(ctx, testCCacheID(testNamespace, testKeyB)); err != nil {
		t.Fatalf("new namespace entry was removed: %v", err)
	}
	if _, err := tight.Stat(ctx, testCCacheID("team-b", testKeyC)); err != nil {
		t.Fatalf("unrelated namespace entry was removed: %v", err)
	}
}
