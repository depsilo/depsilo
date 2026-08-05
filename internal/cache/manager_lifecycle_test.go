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

type controlledProgressBody struct {
	remaining int
	readReady chan struct{}
	release   chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newControlledProgressBody(chunks int) *controlledProgressBody {
	return &controlledProgressBody{
		remaining: chunks,
		readReady: make(chan struct{}),
		release:   make(chan struct{}),
		closed:    make(chan struct{}),
	}
}

func (b *controlledProgressBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	select {
	case b.readReady <- struct{}{}:
	case <-b.closed:
		return 0, context.Canceled
	}
	select {
	case <-b.release:
	case <-b.closed:
		return 0, context.Canceled
	}
	b.remaining--
	p[0] = 'x'
	return 1, nil
}

func (b *controlledProgressBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestManagerIdleTimeoutCancelsStalledBodyAndReleasesPipeline(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newCloseBlockingBody()
	const key = "huggingface/models/acme/large/resolve/0123456789abcdef/model.bin"

	ctx := WithFetchTimeout(context.Background(), 0)
	ctx = WithFetchIdleTimeout(ctx, 20*time.Millisecond)
	result, err := manager.Get(ctx, key, "huggingface", 24*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, result.Reader)
		closeErr := result.Reader.Close()
		readDone <- errors.Join(readErr, closeErr)
	}()

	select {
	case err := <-readDone:
		if !errors.Is(err, ErrFetchIdleTimeout) {
			t.Fatalf("stalled body read error = %v, want ErrFetchIdleTimeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stalled body did not time out")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("idle timeout did not close the upstream body")
	}
	waitForNoInflight(t, manager)

	// The timed-out flight must not occupy the key or leave a partial cache
	// representation. A later request can start a fresh fetch immediately.
	const recovered = "recovered canonical bytes"
	retry, err := manager.Get(context.Background(), key, "huggingface", 24*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewBufferString(recovered)),
				"application/octet-stream", int64(len(recovered)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(retry.Reader)
	closeErr := retry.Reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if string(got) != recovered {
		t.Fatalf("retry body = %q, want %q", got, recovered)
	}
	waitForNoInflight(t, manager)
}

func TestManagerIdleTimeoutRollsForwardWhileBodyMakesProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("idle timeout timing contract")
	}
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	t.Cleanup(func() { closeTestManager(t, manager) })
	body := newControlledProgressBody(5)

	ctx := WithFetchTimeout(context.Background(), 0)
	ctx = WithFetchIdleTimeout(ctx, 250*time.Millisecond)
	start := time.Now()
	result, err := manager.Get(ctx,
		"huggingface/models/acme/large/resolve/0123456789abcdef/progress.bin",
		"huggingface",
		24*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", 5, "mock", nil
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
		got, readErr := io.ReadAll(result.Reader)
		closeErr := result.Reader.Close()
		readDone <- readOutcome{body: got, err: errors.Join(readErr, closeErr)}
	}()

	// Each release arrives comfortably inside the 250ms window, but the five
	// windows make the whole transfer last longer than 250ms. Waiting for
	// readReady proves the upstream Read is active before each controlled delay.
	for range 5 {
		select {
		case <-body.readReady:
		case outcome := <-readDone:
			t.Fatalf("progressing body ended early: bytes=%q err=%v", outcome.body, outcome.err)
		case <-time.After(time.Second):
			t.Fatal("upstream did not start the next controlled Read")
		}
		time.Sleep(70 * time.Millisecond)
		select {
		case body.release <- struct{}{}:
		case outcome := <-readDone:
			t.Fatalf("progressing body timed out before release: bytes=%q err=%v", outcome.body, outcome.err)
		case <-time.After(time.Second):
			t.Fatal("controlled upstream Read was not waiting for release")
		}
	}

	var outcome readOutcome
	select {
	case outcome = <-readDone:
	case <-time.After(time.Second):
		t.Fatal("progressing body did not finish")
	}
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if string(outcome.body) != "xxxxx" {
		t.Fatalf("progressing body = %q, want xxxxx", outcome.body)
	}
	if elapsed := time.Since(start); elapsed <= 250*time.Millisecond {
		t.Fatalf("test transfer finished in %v; it did not outlive one idle interval", elapsed)
	}
	waitForNoInflight(t, manager)
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

func TestManagerPutFailureAndClientDisconnectCancelPumpWithoutDrainingUpstream(t *testing.T) {
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
	var firstByte [1]byte
	if _, err := io.ReadFull(result.Reader, firstByte[:]); err != nil {
		t.Fatalf("read first downstream byte: %v", err)
	}
	if string(firstByte[:]) != "x" {
		t.Fatalf("first downstream byte = %q, want x", firstByte[:])
	}
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("storage Put did not start")
	}
	if err := result.Reader.Close(); err != nil {
		t.Fatalf("close downstream reader: %v", err)
	}
	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("storage failure plus downstream disconnect did not cancel upstream body")
	}
	waitForNoInflight(t, manager)
}

type failAfterReadStorage struct {
	*memStorage
	err error
}

func (s *failAfterReadStorage) Put(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	var firstByte [1]byte
	_, _ = io.ReadFull(r, firstByte[:])
	return s.err
}

type gatedFailStorage struct {
	*memStorage
	err         error
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (s *gatedFailStorage) Put(ctx context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.release:
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *gatedFailStorage) fail() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func TestManagerPutFailureDoesNotInterruptLeaderDelivery(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &failAfterReadStorage{
		memStorage: newMemStorage(),
		err:        errors.New("storage unavailable"),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)

	prefix := []byte("first-upstream-chunk")
	suffix := []byte("remaining-downstream-body")
	payload := append(append([]byte(nil), prefix...), suffix...)
	result, err := manager.Get(context.Background(), "pypi/files/complete.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			body := io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(suffix))
			return io.NopCloser(body), "application/octet-stream", int64(len(payload)), "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Reader.Close()

	got, readErr := io.ReadAll(result.Reader)
	if readErr != nil {
		t.Fatalf("cache persistence failure interrupted downstream delivery after %d/%d bytes: %v", len(got), len(payload), readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downstream received %d/%d bytes after cache persistence failure", len(got), len(payload))
	}
	waitForNoInflight(t, manager)

	var entries int64
	if err := database.Model(&db.CacheEntry{}).Where("key = ?", "pypi/files/complete.whl").Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("cache entries after failed persistence = %d, want 0", entries)
	}
}

func TestManagerPrefetchStillFailsWhenPersistenceFails(t *testing.T) {
	database := openStreamTestDB(t)
	wantErr := errors.New("storage unavailable")
	storage := &failFastStorage{memStorage: newMemStorage(), err: wantErr, started: make(chan struct{})}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)
	body := &chunkThenBlockBody{closeBlockingBody: newCloseBlockingBody()}

	err := manager.Prefetch(context.Background(), "pypi/files/prefetch-fail.whl", "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "mock", nil
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("prefetch error = %v, want storage error %v", err, wantErr)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("failed Prefetch did not cancel its upstream body")
	}
	waitForNoInflight(t, manager)
}

func TestManagerFollowerBypassesCacheAfterPersistenceFailure(t *testing.T) {
	database := openStreamTestDB(t)
	storage := &gatedFailStorage{
		memStorage: newMemStorage(),
		err:        errors.New("storage unavailable"),
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	manager := NewManager(storage, database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)
	defer storage.fail()

	key := "pypi/files/follower.whl"
	leaderPayload := []byte("leader pass-through response")
	leader, err := manager.Get(context.Background(), key, "pypi", time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(bytes.NewReader(leaderPayload)), "application/octet-stream", int64(len(leaderPayload)), "leader", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	type readOutcome struct {
		body []byte
		err  error
	}
	leaderDone := make(chan readOutcome, 1)
	go func() {
		body, err := io.ReadAll(leader.Reader)
		_ = leader.Reader.Close()
		leaderDone <- readOutcome{body: body, err: err}
	}()
	select {
	case <-storage.started:
	case <-time.After(time.Second):
		t.Fatal("storage Put did not start")
	}

	const followerCount = 3
	fallbackPayload := []byte("independent follower pass-through")
	type getOutcome struct {
		result *GetResult
		err    error
	}
	done := make(chan getOutcome, followerCount)
	trackers := make([]*RefreshTracker, 0, followerCount)
	var fallbackFetches atomic.Int64
	for range followerCount {
		tracker := newRefreshTracker()
		trackers = append(trackers, tracker)
		ctx := context.WithValue(context.Background(), forceRefreshContextKey{}, forceRefreshRequest{tracker: tracker})
		go func() {
			result, err := manager.Get(ctx, key, "pypi", time.Hour,
				func(context.Context) (io.ReadCloser, string, int64, string, error) {
					fallbackFetches.Add(1)
					return io.NopCloser(bytes.NewReader(fallbackPayload)), "application/octet-stream", int64(len(fallbackPayload)), "fallback", nil
				})
			done <- getOutcome{result: result, err: err}
		}()
	}

	deadline := time.Now().Add(time.Second)
	allJoined := func() bool {
		for _, tracker := range trackers {
			if !tracker.Used() {
				return false
			}
		}
		return true
	}
	for !allJoined() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !allJoined() {
		t.Fatal("followers did not join the existing inflight fetch")
	}
	storage.fail()

	select {
	case outcome := <-leaderDone:
		if outcome.err != nil {
			t.Fatalf("leader read: %v", outcome.err)
		}
		if !bytes.Equal(outcome.body, leaderPayload) {
			t.Fatalf("leader body = %q, want %q", outcome.body, leaderPayload)
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not finish after storage failure")
	}

	for range followerCount {
		var outcome getOutcome
		select {
		case outcome = <-done:
		case <-time.After(time.Second):
			t.Fatal("follower did not switch to pass-through after persistence failure")
		}
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		body, err := io.ReadAll(outcome.result.Reader)
		_ = outcome.result.Reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, fallbackPayload) {
			t.Fatalf("pass-through body = %q, want %q", body, fallbackPayload)
		}
		if outcome.result.Hit || outcome.result.Upstream != "fallback" {
			t.Fatalf("pass-through result hit=%v upstream=%q", outcome.result.Hit, outcome.result.Upstream)
		}
	}
	if got := fallbackFetches.Load(); got != followerCount {
		t.Fatalf("follower pass-through fetches = %d, want %d", got, followerCount)
	}
	select {
	case <-storage.release:
	default:
		t.Fatal("storage failure gate was not released")
	}
	waitForNoInflight(t, manager)
}

func TestManagerFollowerDoesNotBypassUpstreamFailure(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	defer closeTestManager(t, manager)

	key := "pypi/files/upstream-failure.whl"
	flight := &inflightFetch{done: make(chan struct{})}
	manager.inflightMu.Lock()
	manager.inflight[key] = flight
	manager.inflightMu.Unlock()

	tracker := newRefreshTracker()
	ctx := context.WithValue(context.Background(), forceRefreshContextKey{}, forceRefreshRequest{tracker: tracker})
	wantErr := errors.New("upstream reset")
	done := make(chan error, 1)
	go func() {
		_, err := manager.Get(ctx, key, "pypi", time.Hour,
			func(context.Context) (io.ReadCloser, string, int64, string, error) {
				t.Error("upstream failure follower unexpectedly bypassed the failed flight")
				return nil, "", 0, "", nil
			})
		done <- err
	}()

	deadline := time.Now().Add(time.Second)
	for !tracker.Used() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !tracker.Used() {
		t.Fatal("request did not join the existing inflight fetch")
	}
	manager.releaseInflight(key, flight, wantErr)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("follower error = %v, want %v", err, wantErr)
		}
	case <-time.After(time.Second):
		t.Fatal("follower did not receive the upstream failure")
	}
	waitForNoInflight(t, manager)
}

type blockingCloseReadBody struct {
	closeStarted chan struct{}
	releaseClose chan struct{}
	startOnce    sync.Once
}

func newBlockingCloseReadBody() *blockingCloseReadBody {
	return &blockingCloseReadBody{
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
}

func (b *blockingCloseReadBody) Read([]byte) (int, error) {
	<-b.closeStarted
	return 0, context.Canceled
}

func (b *blockingCloseReadBody) Close() error {
	b.startOnce.Do(func() { close(b.closeStarted) })
	<-b.releaseClose
	return nil
}

func TestManagerCloseWaitsForFollowerPassthroughCloseCallback(t *testing.T) {
	database := openStreamTestDB(t)
	manager := NewManager(newMemStorage(), database, NewEventBus(), time.Hour)
	body := newBlockingCloseReadBody()

	result, err := manager.fetchPassthrough(context.Background(),
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return body, "application/octet-stream", -1, "fallback", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := result.Reader.Read(buf[:])
		readDone <- err
	}()

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		closeDone <- manager.Close(ctx)
	}()
	select {
	case <-body.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("manager close did not start closing the pass-through body")
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("pass-through reader did not observe context cancellation")
	}
	select {
	case err := <-closeDone:
		close(body.releaseClose)
		t.Fatalf("manager close returned before the body close callback finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(body.releaseClose)
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	_ = result.Reader.Close()
}

func TestManagerConcurrentCloseAndHitsAreRaceSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("concurrent shutdown stress contract")
	}
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
