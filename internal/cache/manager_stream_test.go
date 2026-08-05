package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	started chan struct{}
	once    sync.Once
}

func (s *delayedPutStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.memStorage.Put(ctx, key, r, size, contentType)
}

type countingReadBody struct {
	reader    *bytes.Reader
	bytesRead atomic.Int64
}

func newCountingReadBody(payload []byte) *countingReadBody {
	return &countingReadBody{reader: bytes.NewReader(payload)}
}

func (b *countingReadBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.bytesRead.Add(int64(n))
	return n, err
}

func (b *countingReadBody) Close() error {
	return nil
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

func TestManager_CacheMissLargeBodyDoesNotWaitForCachePutReadiness(t *testing.T) {
	if testing.Short() {
		t.Skip("large streaming pressure contract")
	}
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{memStorage: newMemStorage(), release: make(chan struct{})}
	m := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, m) })

	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	result, err := m.Get(context.Background(), "pypi/files/large.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(payload)), "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	type readOutcome struct {
		body []byte
		err  error
	}
	readDone := make(chan readOutcome, 1)
	go func() {
		body, readErr := io.ReadAll(result.Reader)
		readDone <- readOutcome{body: body, err: readErr}
	}()

	released := false
	releaseStorage := func() {
		if !released {
			close(storage.release)
			released = true
		}
	}
	defer releaseStorage()

	select {
	case outcome := <-readDone:
		if outcome.err != nil {
			t.Fatalf("read large downstream body: %v", outcome.err)
		}
		if !bytes.Equal(outcome.body, payload) {
			t.Fatalf("downstream received %d/%d bytes while cache Put was not ready", len(outcome.body), len(payload))
		}
	case <-time.After(3 * time.Second):
		releaseStorage()
		outcome := <-readDone
		_ = result.Reader.Close()
		t.Fatalf(
			"large downstream body remained blocked behind cache Put for 3 seconds (eventual bytes=%d/%d, err=%v)",
			len(outcome.body),
			len(payload),
			outcome.err,
		)
	}

	if err := result.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	releaseStorage()
	waitForNoInflight(t, m)
}

func TestManager_TransientCachePutStallStillPersistsMiss(t *testing.T) {
	if testing.Short() {
		t.Skip("storage backpressure timing contract")
	}
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{
		memStorage: newMemStorage(),
		release:    make(chan struct{}),
		started:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	var releaseOnce sync.Once
	releaseStorage := func() {
		releaseOnce.Do(func() { close(storage.release) })
	}
	t.Cleanup(func() {
		releaseStorage()
		closeTestManager(t, manager)
	})

	const key = "pypi/files/transient-storage-stall.whl"
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	upstreamBody := newCountingReadBody(payload)
	result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return upstreamBody, "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("cache Put did not start")
	}

	type readOutcome struct {
		body []byte
		err  error
	}
	readDone := make(chan readOutcome, 1)
	go func() {
		body, readErr := io.ReadAll(result.Reader)
		readDone <- readOutcome{body: body, err: readErr}
	}()

	// A remote object store can legitimately spend hundreds of milliseconds
	// establishing a connection before it starts consuming the request body.
	wantRead := int64((missStorageQueueDepth + 2) * missStreamChunkSize)
	deadline := time.Now().Add(time.Second)
	for upstreamBody.bytesRead.Load() < wantRead && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := upstreamBody.bytesRead.Load(); got < wantRead {
		t.Fatalf("upstream read %d bytes before deadline, want at least %d", got, wantRead)
	}
	time.Sleep(200 * time.Millisecond)
	releaseStorage()

	select {
	case outcome := <-readDone:
		if outcome.err != nil {
			t.Fatalf("read downstream body: %v", outcome.err)
		}
		if !bytes.Equal(outcome.body, payload) {
			t.Fatalf("downstream body = %d/%d bytes", len(outcome.body), len(payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("downstream did not resume after transient cache Put stall")
	}
	if err := result.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	waitForNoInflight(t, manager)

	unexpectedFetch := errors.New("cache miss after transient storage stall")
	hit, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", unexpectedFetch
		})
	if errors.Is(err, unexpectedFetch) {
		t.Fatal(unexpectedFetch)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	cached, err := io.ReadAll(hit.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !hit.Hit || !bytes.Equal(cached, payload) {
		t.Fatalf("cache hit=%v body=%d/%d bytes", hit.Hit, len(cached), len(payload))
	}
}

func TestManager_FollowerFailsOpenWhenCachePutRemainsBackpressured(t *testing.T) {
	if testing.Short() {
		t.Skip("backpressure timeout contract")
	}
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{
		memStorage: newMemStorage(),
		release:    make(chan struct{}),
		started:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() {
		select {
		case <-storage.release:
		default:
			close(storage.release)
		}
		closeTestManager(t, manager)
	})

	const key = "pypi/files/backpressured-followers.whl"
	leaderPayload := bytes.Repeat([]byte("leader"), 192*1024)
	leader, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(leaderPayload)), "application/octet-stream", int64(len(leaderPayload)), "leader", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("cache Put did not start")
	}

	fallbackPayload := []byte("follower independent pass-through")
	var fallbackFetches atomic.Int64
	type getOutcome struct {
		result *GetResult
		err    error
	}
	followerDone := make(chan getOutcome, 1)
	go func() {
		result, getErr := manager.Get(context.Background(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				fallbackFetches.Add(1)
				return io.NopCloser(bytes.NewReader(fallbackPayload)), "application/octet-stream", int64(len(fallbackPayload)), "fallback", nil
			})
		followerDone <- getOutcome{result: result, err: getErr}
	}()

	leaderDone := make(chan error, 1)
	go func() {
		body, readErr := io.ReadAll(leader.Reader)
		closeErr := leader.Reader.Close()
		if readErr == nil && !bytes.Equal(body, leaderPayload) {
			readErr = fmt.Errorf("leader body = %d/%d bytes", len(body), len(leaderPayload))
		}
		leaderDone <- errors.Join(readErr, closeErr)
	}()

	select {
	case err := <-leaderDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("leader remained blocked behind cache Put")
	}

	var follower getOutcome
	select {
	case follower = <-followerDone:
	case <-time.After(4 * time.Second):
		t.Fatal("follower did not switch to pass-through after cache backpressure")
	}
	if follower.err != nil {
		t.Fatal(follower.err)
	}
	body, err := io.ReadAll(follower.result.Reader)
	closeErr := follower.result.Reader.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	if !bytes.Equal(body, fallbackPayload) {
		t.Fatalf("follower body = %q, want %q", body, fallbackPayload)
	}
	if follower.result.Hit || follower.result.Upstream != "fallback" {
		t.Fatalf("follower result hit=%v upstream=%q", follower.result.Hit, follower.result.Upstream)
	}
	if got := fallbackFetches.Load(); got != 1 {
		t.Fatalf("follower fallback fetches = %d, want 1", got)
	}
	waitForNoInflight(t, manager)
}

func TestManager_SlowDownstreamAppliesBoundedBackpressureWithoutAbandoningCacheFill(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })

	payload := bytes.Repeat([]byte("bounded-backpressure"), 256*1024)
	upstreamBody := newCountingReadBody(payload)
	const key = "pypi/files/slow-downstream.whl"
	result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return upstreamBody, "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for upstreamBody.bytesRead.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if upstreamBody.bytesRead.Load() == 0 {
		t.Fatal("upstream pump did not start")
	}
	time.Sleep(50 * time.Millisecond)
	if got := upstreamBody.bytesRead.Load(); got > 256*1024 {
		t.Fatalf("idle downstream allowed %d bytes of upstream read-ahead, want a bounded window", got)
	}

	// Abandoning the downstream must release its backpressure and let the cache
	// remain as the sole consumer until the durable fill completes.
	if err := result.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	waitForNoInflight(t, manager)

	hit, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			t.Fatal("completed cache fill contacted upstream")
			return nil, "", 0, "", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	got, err := io.ReadAll(hit.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("cached body after slow downstream disconnect = %d/%d bytes", len(got), len(payload))
	}
}

func TestManager_DownstreamDisconnectDuringStorageBackpressureKeepsCacheFill(t *testing.T) {
	if testing.Short() {
		t.Skip("backpressure timeout contract")
	}
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{
		memStorage: newMemStorage(),
		release:    make(chan struct{}),
		started:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	var releaseOnce sync.Once
	releaseStorage := func() {
		releaseOnce.Do(func() { close(storage.release) })
	}
	t.Cleanup(func() {
		releaseStorage()
		closeTestManager(t, manager)
	})

	const key = "pypi/files/disconnect-under-storage-pressure.whl"
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	upstreamBody := newCountingReadBody(payload)
	result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return upstreamBody, "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("cache Put did not start")
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, result.Reader)
		readDone <- readErr
	}()

	// One chunk is blocked in the storage pipe, the bounded queue holds eight,
	// and the next chunk is waiting to enqueue. Closing the client at that point
	// must turn storage into the required sink rather than detach it at timeout.
	wantRead := int64((missStorageQueueDepth + 2) * missStreamChunkSize)
	deadline := time.Now().Add(time.Second)
	for upstreamBody.bytesRead.Load() < wantRead && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := upstreamBody.bytesRead.Load(); got < wantRead {
		t.Fatalf("upstream read %d bytes before deadline, want at least %d", got, wantRead)
	}
	if err := result.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("downstream reader did not stop after close")
	}

	time.Sleep(missStorageBackpressureTTL + 100*time.Millisecond)
	manager.inflightMu.Lock()
	inflight := len(manager.inflight)
	manager.inflightMu.Unlock()
	if inflight != 1 {
		t.Fatalf("cache fill was abandoned after downstream disconnect; inflight=%d", inflight)
	}

	releaseStorage()
	waitForNoInflight(t, manager)

	unexpectedFetch := errors.New("cache miss after downstream disconnect")
	hit, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return nil, "", 0, "", unexpectedFetch
		})
	if errors.Is(err, unexpectedFetch) {
		t.Fatal(unexpectedFetch)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer hit.Reader.Close()
	cached, err := io.ReadAll(hit.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !hit.Hit || !bytes.Equal(cached, payload) {
		t.Fatalf("cache hit=%v body=%d/%d bytes", hit.Hit, len(cached), len(payload))
	}
}

func TestManager_CloseCancelsCachePutThatHasNotStartedReading(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{
		memStorage: newMemStorage(),
		release:    make(chan struct{}),
		started:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	t.Cleanup(func() {
		select {
		case <-storage.release:
		default:
			close(storage.release)
		}
	})

	payload := []byte("client can finish before cache persistence")
	result, err := manager.Get(context.Background(), "pypi/files/close.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(payload)), "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(result.Reader)
	_ = result.Reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("downstream body = %q, want %q", body, payload)
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("cache Put did not start")
	}

	closeTestManager(t, manager)
	waitForNoInflight(t, manager)
}

func TestManager_CloseCancelsFullStorageBackpressurePipeline(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &delayedPutStorage{
		memStorage: newMemStorage(),
		release:    make(chan struct{}),
		started:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(storage.release) })
	})

	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB
	upstreamBody := newCountingReadBody(payload)
	result, err := manager.Get(context.Background(), "pypi/files/close-full-queue.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return upstreamBody, "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("cache Put did not start")
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, result.Reader)
		readDone <- readErr
	}()
	wantRead := int64((missStorageQueueDepth + 2) * missStreamChunkSize)
	deadline := time.Now().Add(time.Second)
	for upstreamBody.bytesRead.Load() < wantRead && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := upstreamBody.bytesRead.Load(); got < wantRead {
		t.Fatalf("upstream read %d bytes before deadline, want at least %d", got, wantRead)
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Close(closeCtx); err != nil {
		t.Fatalf("close manager with full storage queue: %v", err)
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("downstream reader remained blocked after Manager.Close")
	}
	_ = result.Reader.Close()
	waitForNoInflight(t, manager)
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
