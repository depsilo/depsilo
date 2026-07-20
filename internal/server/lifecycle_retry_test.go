package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type retryLifecycleTestServer struct {
	server *http.Server
	url    string
}

func startRetryLifecycleTestServer(t *testing.T, handler http.Handler, resources resourceCloser) retryLifecycleTestServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &http.Server{}
	lifecycle := registerServerLifecycle(srv, resources)
	srv.Handler = lifecycle.track(handler)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		serveHTTP(srv, listener)
	}()

	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Errorf("server did not stop")
		}
	})
	return retryLifecycleTestServer{
		server: srv,
		url:    "http://" + listener.Addr().String(),
	}
}

func waitRetryLifecycleTest(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestShutdownDeadlineDoesNotPoisonLaterRetry(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	var closeCalls atomic.Int32
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloseFunc(func(context.Context) error {
		closeCalls.Add(1)
		return nil
	})

	testServer := startRetryLifecycleTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		enterOnce.Do(func() { close(entered) })
		<-release
	}), resources)

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := http.Get(testServer.url)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	waitRetryLifecycleTest(t, entered, "handler entry")

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 40*time.Millisecond)
	err := Shutdown(shortCtx, testServer.server)
	cancelShort()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Shutdown error = %v, want deadline exceeded", err)
	}
	if got := closeCalls.Load(); got != 0 {
		t.Fatalf("resource Close calls before handler drain = %d, want 0", got)
	}

	close(release)
	waitRetryLifecycleTest(t, requestDone, "request completion")

	retryCtx, cancelRetry := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRetry()
	if err := Shutdown(retryCtx, testServer.server); err != nil {
		t.Fatalf("retry Shutdown error = %v, want nil", err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("resource Close calls after retry = %d, want 1", got)
	}
	if err := Shutdown(retryCtx, testServer.server); err != nil {
		t.Fatalf("repeated successful Shutdown error = %v, want nil", err)
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("resource Close calls after repeated success = %d, want 1", got)
	}
}

func TestShutdownRetriesTransientResourceError(t *testing.T) {
	transientErr := errors.New("transient resource close failure")
	var closeCalls atomic.Int32
	cleanupComplete := make(chan struct{})
	var completeOnce sync.Once
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloseFunc(func(context.Context) error {
		if closeCalls.Add(1) <= 2 {
			return transientErr
		}
		completeOnce.Do(func() { close(cleanupComplete) })
		return nil
	})
	testServer := startRetryLifecycleTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), resources)

	response, err := http.Get(testServer.url)
	if err != nil {
		t.Fatalf("prime server: %v", err)
	}
	_ = response.Body.Close()

	if err := Shutdown(context.Background(), testServer.server); !errors.Is(err, transientErr) {
		t.Fatalf("first Shutdown error = %v, want transient error", err)
	}
	waitRetryLifecycleTest(t, cleanupComplete, "automatic non-context retry")
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := serverLifecycles.Load(testServer.server); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic non-context retry retained lifecycle entry")
		}
		time.Sleep(time.Millisecond)
	}
	if got := closeCalls.Load(); got != 3 {
		t.Fatalf("resource Close calls = %d, want two failures and one automatic success", got)
	}
	if err := Shutdown(context.Background(), testServer.server); err != nil {
		t.Fatalf("repeated successful Shutdown error = %v, want nil", err)
	}
	if got := closeCalls.Load(); got != 3 {
		t.Fatalf("resource Close calls after repeated success = %d, want 3", got)
	}
}

type blockingRetryLifecycleCloser struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	calls       atomic.Int32
}

func (closer *blockingRetryLifecycleCloser) Close(ctx context.Context) error {
	closer.calls.Add(1)
	closer.startedOnce.Do(func() { close(closer.started) })
	select {
	case <-closer.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestConcurrentShutdownCallersKeepIndependentContexts(t *testing.T) {
	closer := &blockingRetryLifecycleCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	resources := newServerResources(nil, nil)
	resources.cacheManager = closer
	testServer := startRetryLifecycleTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), resources)

	response, err := http.Get(testServer.url)
	if err != nil {
		t.Fatalf("prime server: %v", err)
	}
	_ = response.Body.Close()

	longResult := make(chan error, 1)
	longCtx, cancelLong := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelLong()
	go func() {
		longResult <- Shutdown(longCtx, testServer.server)
	}()
	waitRetryLifecycleTest(t, closer.started, "resource close attempt")

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 40*time.Millisecond)
	shortErr := Shutdown(shortCtx, testServer.server)
	cancelShort()
	if !errors.Is(shortErr, context.DeadlineExceeded) {
		t.Fatalf("short Shutdown error = %v, want deadline exceeded", shortErr)
	}

	close(closer.release)
	select {
	case err := <-longResult:
		if err != nil {
			t.Fatalf("long Shutdown error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for long Shutdown")
	}
	if got := closer.calls.Load(); got != 1 {
		t.Fatalf("resource Close calls = %d, want one shared successful attempt", got)
	}
	if err := Shutdown(context.Background(), testServer.server); err != nil {
		t.Fatalf("Shutdown after concurrent completion = %v, want nil", err)
	}
}

type transientBackgroundLifecycleCloser struct {
	failures  int32
	calls     atomic.Int32
	active    atomic.Int32
	maxActive atomic.Int32
	completed chan struct{}
	doneOnce  sync.Once
}

func (closer *transientBackgroundLifecycleCloser) Close(context.Context) error {
	active := closer.active.Add(1)
	defer closer.active.Add(-1)
	for {
		maximum := closer.maxActive.Load()
		if active <= maximum || closer.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}

	call := closer.calls.Add(1)
	if call <= closer.failures {
		return errors.New("transient background cleanup failure")
	}
	closer.doneOnce.Do(func() { close(closer.completed) })
	return nil
}

func TestBackgroundFinishRetriesTransientResourcesWithSingleOwner(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	closer := &transientBackgroundLifecycleCloser{
		failures:  2,
		completed: make(chan struct{}),
	}
	testServer := startRetryLifecycleTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		enterOnce.Do(func() { close(entered) })
		<-release
	}), closer)

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := http.Get(testServer.url)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	waitRetryLifecycleTest(t, entered, "handler entry")

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 30*time.Millisecond)
	shutdownErr := Shutdown(shortCtx, testServer.server)
	cancelShort()
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want deadline exceeded", shutdownErr)
	}

	value, ok := serverLifecycles.Load(testServer.server)
	if !ok {
		t.Fatal("lifecycle disappeared before background resource cleanup")
	}
	lifecycle := value.(*serverLifecycle)
	// Repeated scheduling requests must share the owner already started by the
	// timed-out Shutdown call.
	var schedulers sync.WaitGroup
	for range 32 {
		schedulers.Add(1)
		go func() {
			defer schedulers.Done()
			lifecycle.scheduleBackgroundFinish(testServer.server)
		}()
	}
	schedulers.Wait()

	close(release)
	waitRetryLifecycleTest(t, requestDone, "request completion")
	waitRetryLifecycleTest(t, closer.completed, "automatic resource retry")

	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := serverLifecycles.Load(testServer.server); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("successful background cleanup retained lifecycle map entry")
		}
		time.Sleep(time.Millisecond)
	}
	if got := closer.calls.Load(); got != 3 {
		t.Fatalf("resource Close calls = %d, want two failures and one success", got)
	}
	if got := closer.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent background owners = %d, want 1", got)
	}
}

type retryFailingListener struct{ err error }

func (listener retryFailingListener) Accept() (net.Conn, error) { return nil, listener.err }
func (retryFailingListener) Close() error                       { return nil }
func (retryFailingListener) Addr() net.Addr                     { return retryStaticAddr("retry-failing") }

type retryStaticAddr string

func (addr retryStaticAddr) Network() string { return "test" }
func (addr retryStaticAddr) String() string  { return string(addr) }

func TestUnexpectedServeExitRetriesResourcesUntilComplete(t *testing.T) {
	closer := &transientBackgroundLifecycleCloser{
		failures:  2,
		completed: make(chan struct{}),
	}
	srv := &http.Server{}
	registerServerLifecycle(srv, closer)

	serveHTTP(srv, retryFailingListener{err: errors.New("accept failed")})
	waitRetryLifecycleTest(t, closer.completed, "unexpected-exit cleanup retry")

	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := serverLifecycles.Load(srv); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unexpected-exit cleanup retained lifecycle map entry")
		}
		time.Sleep(time.Millisecond)
	}
	if got := closer.calls.Load(); got != 3 {
		t.Fatalf("resource Close calls = %d, want 3", got)
	}
}

func TestCloseResourcesWithRetrySupportsStartupFailureOwner(t *testing.T) {
	closer := &transientBackgroundLifecycleCloser{
		failures:  2,
		completed: make(chan struct{}),
	}
	var reports atomic.Int32
	closeResourcesWithRetry(closer, func(error) { reports.Add(1) })

	if got := closer.calls.Load(); got != 3 {
		t.Fatalf("resource Close calls = %d, want 3", got)
	}
	if got := reports.Load(); got != 2 {
		t.Fatalf("transient error reports = %d, want 2", got)
	}
	if got := closer.maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent startup cleanup calls = %d, want 1", got)
	}
}

func TestRetryCleanupStopsOnTerminalTransportError(t *testing.T) {
	transportErr := errors.New("terminal transport error")
	var attempts atomic.Int32
	var transientReports atomic.Int32

	err := retryCleanupUntilDone(func() (bool, error) {
		attempts.Add(1)
		// done=true models resources having closed even though the transport
		// retained an error from its already-finished shutdown.
		return true, transportErr
	}, func(error) { transientReports.Add(1) })

	if !errors.Is(err, transportErr) {
		t.Fatalf("retry result = %v, want terminal transport error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	if got := transientReports.Load(); got != 0 {
		t.Fatalf("transient reports = %d, want 0", got)
	}
}

func TestShutdownDoesNotScheduleRetryForTerminalTransportError(t *testing.T) {
	transportErr := errors.New("terminal transport error")
	var resourceCloses atomic.Int32
	srv := &http.Server{}
	lifecycle := registerServerLifecycle(srv, resourceCloseFunc(func(context.Context) error {
		resourceCloses.Add(1)
		return nil
	}))

	// Pre-complete the transport once with an error so Shutdown exercises the
	// resource-complete/transport-error branch deterministically.
	lifecycle.transportOnce.Do(func() {
		lifecycle.transportErr = transportErr
		close(lifecycle.transportDone)
	})

	err := Shutdown(context.Background(), srv)
	if !errors.Is(err, transportErr) {
		t.Fatalf("Shutdown error = %v, want terminal transport error", err)
	}
	if got := resourceCloses.Load(); got != 1 {
		t.Fatalf("resource Close calls = %d, want 1", got)
	}
	select {
	case <-lifecycle.backgroundFinishDone:
		t.Fatal("terminal transport error scheduled background resource retry")
	default:
	}
	if _, ok := serverLifecycles.Load(srv); ok {
		t.Fatal("resource-complete terminal transport error retained lifecycle entry")
	}
}

func TestShutdownNilContextForUnregisteredServer(t *testing.T) {
	if err := Shutdown(nil, &http.Server{}); err != nil {
		t.Fatalf("Shutdown(nil, unregistered server): %v", err)
	}
}
