package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

func closeTestManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("close cache manager: %v", err)
	}
}

type closeBlockingBody struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newCloseBlockingBody() *closeBlockingBody {
	return &closeBlockingBody{started: make(chan struct{}), closed: make(chan struct{})}
}

func (b *closeBlockingBody) Read([]byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-b.closed
	return 0, context.Canceled
}

func (b *closeBlockingBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestManagerCloseCancelsSlowMissAndReleasesInflight(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	body := newCloseBlockingBody()
	key := "pypi/files/slow-1.0.0.whl"

	result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("upstream pump did not start")
	}

	closeTestManager(t, manager)
	select {
	case <-body.closed:
	default:
		t.Fatal("manager close did not close the upstream body")
	}
	waitForNoInflight(t, manager)
	if _, err := manager.Get(context.Background(), "pypi/files/after-close.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			t.Fatal("closed manager contacted upstream")
			return nil, "", 0, "", nil
		}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("post-close miss error = %v, want ErrManagerClosed", err)
	}
}

func TestManagerCloseCancelsSlowUpstreamOpen(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	fetchStarted := make(chan struct{})
	getDone := make(chan error, 1)
	go func() {
		_, err := manager.Get(context.Background(), "pypi/files/slow-open.whl", "pypi", time.Hour,
			func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
				close(fetchStarted)
				<-ctx.Done()
				return nil, "", 0, "", ctx.Err()
			})
		getDone <- err
	}()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("upstream open did not start")
	}

	closeTestManager(t, manager)
	select {
	case err := <-getDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Get error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get did not return after manager close")
	}
	waitForNoInflight(t, manager)
}

func TestManagerCloseCancelsContextBoundPrefetch(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	body := newCloseBlockingBody()
	prefetchDone := make(chan error, 1)
	go func() {
		prefetchDone <- manager.Prefetch(context.Background(), "pypi/simple/slow/index.html", "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				return body, "text/html", -1, "mock", nil
			})
	}()
	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("prefetch upstream pump did not start")
	}

	closeTestManager(t, manager)
	select {
	case err := <-prefetchDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("prefetch error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prefetch did not return after manager close")
	}
	waitForNoInflight(t, manager)
}

type blockingScanner struct {
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingScanner) ScanPackage(ctx context.Context, _, _ string) error {
	s.calls.Add(1)
	s.once.Do(func() { close(s.started) })
	<-s.release // Deliberately ignore cancellation: Close must still wait.
	return ctx.Err()
}

func TestManagerCloseWaitsForActiveScanAndIsIdempotent(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := &blockingScanner{started: make(chan struct{}), release: make(chan struct{})}
	manager.SetSecurityScanner(scanner)

	body := []byte("wheel")
	result, err := manager.Get(context.Background(), "pypi/files/demo-1.0.0.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(body)), "application/octet-stream", int64(len(body)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, result.Reader)
	_ = result.Reader.Close()
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("security scan did not start")
	}

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		closeDone <- manager.Close(ctx)
	}()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before active scan finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(scanner.release)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after scan was released")
	}
	closeTestManager(t, manager)
	closeTestManager(t, manager)
}

func TestManagerCloseRejectsRefreshAndScanAdmission(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	staleKey := "pypi/files/stale-1.0.0.whl"
	storage.data[staleKey] = []byte("trusted")
	if err := database.Create(&db.CacheEntry{
		Key: staleKey, AdapterType: "pypi", StoragePath: staleKey, ContentType: "application/octet-stream",
		ExpiresAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	scanner := &blockingScanner{started: make(chan struct{}), release: make(chan struct{})}
	manager.SetSecurityScanner(scanner)
	closeTestManager(t, manager)

	var refreshCalls atomic.Int64
	result, err := manager.Get(context.Background(), staleKey, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			refreshCalls.Add(1)
			return io.NopCloser(bytes.NewReader(nil)), "", 0, "", nil
		})
	if err != nil {
		t.Fatalf("serve stale cache after close: %v", err)
	}
	_ = result.Reader.Close()
	if !result.Hit {
		t.Fatal("closed manager did not serve existing stale cache")
	}
	if manager.scheduleBackgroundRefresh("pypi/files/closed.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			refreshCalls.Add(1)
			return io.NopCloser(bytes.NewReader(nil)), "", 0, "", nil
		}) {
		t.Fatal("closed manager admitted background refresh")
	}
	if manager.enqueueScan("pypi", "closed") {
		t.Fatal("closed manager admitted security scan")
	}
	if refreshCalls.Load() != 0 || scanner.calls.Load() != 0 {
		t.Fatalf("work started after close: refresh=%d scan=%d", refreshCalls.Load(), scanner.calls.Load())
	}
}

func TestManagerHitUpdatesAreMergedAndFlushedOnClose(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/hot/index.html"
	storage.data[key] = []byte("cached")
	entry := db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)

	const hits = 1000
	for range hits {
		result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				t.Fatal("fresh hit contacted upstream")
				return nil, "", 0, "", nil
			})
		if err != nil {
			t.Fatal(err)
		}
		_ = result.Reader.Close()
	}
	closeTestManager(t, manager)

	var updated db.CacheEntry
	if err := database.First(&updated, entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.HitCount != hits {
		t.Fatalf("hit count = %d, want %d", updated.HitCount, hits)
	}
}

func TestManagerBackgroundRefreshReservationDeduplicatesBeforeLaunch(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	var startOnce sync.Once
	fetch := func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		calls.Add(1)
		startOnce.Do(func() { close(started) })
		select {
		case <-release:
			return io.NopCloser(bytes.NewReader([]byte("fresh"))), "application/octet-stream", 5, "mock", nil
		case <-ctx.Done():
			return nil, "", 0, "", ctx.Err()
		}
	}

	const attempts = 500
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if manager.scheduleBackgroundRefresh("pypi/files/shared.whl", "pypi", time.Hour, fetch) {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not start")
	}
	if admitted.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("refresh admission=%d fetches=%d, want 1/1", admitted.Load(), calls.Load())
	}
	close(release)
}

func TestManagerBackgroundRefreshConcurrencyIsBounded(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	release := make(chan struct{})
	var calls atomic.Int64
	fetch := func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		calls.Add(1)
		select {
		case <-release:
			return io.NopCloser(bytes.NewReader(nil)), "application/octet-stream", 0, "mock", nil
		case <-ctx.Done():
			return nil, "", 0, "", ctx.Err()
		}
	}

	admitted := 0
	for i := 0; i < refreshWorkerLimit*4; i++ {
		key := "pypi/files/bounded-" + string(rune('a'+i)) + ".whl"
		if manager.scheduleBackgroundRefresh(key, "pypi", time.Hour, fetch) {
			admitted++
		}
	}
	if admitted != refreshWorkerLimit {
		t.Fatalf("admitted refreshes = %d, want limit %d", admitted, refreshWorkerLimit)
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() != refreshWorkerLimit && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != refreshWorkerLimit {
		t.Fatalf("running refreshes = %d, want %d", calls.Load(), refreshWorkerLimit)
	}
	close(release)
	closeTestManager(t, manager)
}

func TestManagerScanQueueDeduplicatesPackage(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := &blockingScanner{started: make(chan struct{}), release: make(chan struct{})}
	manager.SetSecurityScanner(scanner)

	for range 500 {
		manager.enqueueScan("pypi", "same-package")
	}
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("security scan did not start")
	}
	if calls := scanner.calls.Load(); calls != 1 {
		t.Fatalf("duplicate package scans = %d, want 1", calls)
	}
	close(scanner.release)
	closeTestManager(t, manager)
	if calls := scanner.calls.Load(); calls != 1 {
		t.Fatalf("duplicate package scans after close = %d, want 1", calls)
	}
}

func TestManagerScanQueueIsBoundedAndNonBlocking(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	scanner := &blockingScanner{started: make(chan struct{}), release: make(chan struct{})}
	manager.SetSecurityScanner(scanner)

	start := time.Now()
	accepted := 0
	for i := 0; i < scanQueueSize*4; i++ {
		packageName := "package-" + string(rune(i+1))
		if manager.enqueueScan("pypi", packageName) {
			accepted++
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("scan queue admission blocked for %v", elapsed)
	}
	if accepted == 0 || accepted > scanQueueSize+scanWorkerCount {
		t.Fatalf("accepted scans = %d, want 1..%d", accepted, scanQueueSize+scanWorkerCount)
	}
	close(scanner.release)
	closeTestManager(t, manager)
}

type failFastStorage struct {
	*memStorage
	err     error
	started chan struct{}
	once    sync.Once
}

func (s *failFastStorage) Put(context.Context, string, io.Reader, int64, string) error {
	s.once.Do(func() { close(s.started) })
	return s.err
}

type chunkThenBlockBody struct {
	*closeBlockingBody
	first atomic.Bool
}

func (b *chunkThenBlockBody) Read(p []byte) (int, error) {
	if b.first.CompareAndSwap(false, true) {
		copy(p, "x")
		return 1, nil
	}
	return b.closeBlockingBody.Read(p)
}

func TestManagerPutFailureCancelsPumpWithoutDrainingUpstream(t *testing.T) {
	database := openStreamTestDB(t)
	wantErr := errors.New("storage unavailable")
	storage := &failFastStorage{memStorage: newMemStorage(), err: wantErr, started: make(chan struct{})}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)
	body := &chunkThenBlockBody{closeBlockingBody: newCloseBlockingBody()}

	result, err := manager.Get(context.Background(), "pypi/files/fail.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("storage Put did not start")
	}
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("storage failure did not cancel upstream body")
	}
	waitForNoInflight(t, manager)
}

func TestManagerConcurrentCloseAndHitsAreRaceSafe(t *testing.T) {
	database := openStreamTestDB(t)
	storage := newMemStorage()
	key := "pypi/simple/race/index.html"
	storage.data[key] = []byte("cached")
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "pypi", StoragePath: key, ContentType: "text/html",
		ExpiresAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	start := make(chan struct{})
	errCh := make(chan error, 128)
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 20 {
				result, err := manager.Get(context.Background(), key, "pypi", time.Hour,
					func(context.Context) (io.ReadCloser, string, int64, string, error) {
						return nil, "", 0, "", errors.New("unexpected fetch")
					})
				if err != nil {
					errCh <- err
					return
				}
				_ = result.Reader.Close()
			}
		}()
	}
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := manager.Close(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent operation: %v", err)
	}
	closeTestManager(t, manager)
}
