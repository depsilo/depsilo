package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// resourceCloseFunc adapts a function to resourceCloser. It is intentionally
// private; tests and small server compositions can use it without weakening the
// lifecycle's context-aware cleanup contract.
type resourceCloseFunc func(context.Context) error

func (closeFunc resourceCloseFunc) Close(ctx context.Context) error {
	if closeFunc == nil {
		return nil
	}
	return closeFunc(ctx)
}

type serverLifecycle struct {
	resources resourceCloser

	// Starting the HTTP transport shutdown is irreversible, so it happens at
	// most once. Waiting for it is deliberately not guarded by this once: every
	// Shutdown caller keeps its own context and can time out independently.
	transportOnce sync.Once
	transportDone chan struct{}
	transportErr  error

	backgroundFinishOnce sync.Once
	backgroundFinishDone chan struct{}

	resourceMu      sync.Mutex
	resourcesClosed bool

	mu          sync.Mutex
	closing     bool
	helperOwned bool
	active      int
	drained     chan struct{}
	drainedOnce sync.Once
}

var serverLifecycles sync.Map

const (
	backgroundCleanupInitialBackoff = 5 * time.Millisecond
	backgroundCleanupMaxBackoff     = 250 * time.Millisecond
)

type lifecycleHandler struct {
	lifecycle *serverLifecycle
	next      http.Handler
}

func registerServerLifecycle(srv *http.Server, resources resourceCloser) *serverLifecycle {
	lifecycle := &serverLifecycle{
		resources:            resources,
		transportDone:        make(chan struct{}),
		backgroundFinishDone: make(chan struct{}),
		drained:              make(chan struct{}),
	}
	serverLifecycles.Store(srv, lifecycle)
	return lifecycle
}

func (lifecycle *serverLifecycle) track(next http.Handler) http.Handler {
	return &lifecycleHandler{lifecycle: lifecycle, next: next}
}

func (handler *lifecycleHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	lifecycle := handler.lifecycle
	lifecycle.mu.Lock()
	if lifecycle.closing {
		lifecycle.mu.Unlock()
		http.Error(writer, "server is shutting down", http.StatusServiceUnavailable)
		return
	}
	lifecycle.active++
	lifecycle.mu.Unlock()

	defer func() {
		lifecycle.mu.Lock()
		lifecycle.active--
		if lifecycle.closing && lifecycle.active == 0 {
			lifecycle.drainedOnce.Do(func() { close(lifecycle.drained) })
		}
		lifecycle.mu.Unlock()
	}()
	handler.next.ServeHTTP(writer, request)
}

func lookupServerLifecycle(srv *http.Server) (*serverLifecycle, bool) {
	if value, ok := serverLifecycles.Load(srv); ok {
		return value.(*serverLifecycle), true
	}
	handler, ok := srv.Handler.(*lifecycleHandler)
	if !ok || handler.lifecycle == nil {
		return nil, false
	}
	return handler.lifecycle, true
}

func (lifecycle *serverLifecycle) beginClosing(helperOwned bool) {
	lifecycle.mu.Lock()
	if helperOwned {
		lifecycle.helperOwned = true
	}
	lifecycle.closing = true
	if lifecycle.active == 0 {
		lifecycle.drainedOnce.Do(func() { close(lifecycle.drained) })
	}
	lifecycle.mu.Unlock()
}

func (lifecycle *serverLifecycle) isHelperOwned() bool {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.helperOwned
}

func (lifecycle *serverLifecycle) startTransportShutdown(srv *http.Server, helperOwned bool) {
	// helperOwned is monotonic and must be recorded even when another path won
	// the transport once immediately beforehand.
	lifecycle.beginClosing(helperOwned)
	lifecycle.transportOnce.Do(func() {
		go func() {
			lifecycle.transportErr = srv.Shutdown(context.Background())
			close(lifecycle.transportDone)
		}()
	})
}

func waitForLifecycle(ctx context.Context, done <-chan struct{}) error {
	// Prefer completed work when completion and cancellation become observable
	// together. A caller should not receive a stale deadline after the operation
	// has already finished.
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

// closeResources delegates concurrent-attempt coordination to resourceCloser.
// The lifecycle remembers only successful completion so every in-flight caller
// can pass its own context through to the resource owner.
func (lifecycle *serverLifecycle) closeResources(ctx context.Context, srv *http.Server) error {
	lifecycle.resourceMu.Lock()
	if lifecycle.resourcesClosed {
		lifecycle.resourceMu.Unlock()
		return nil
	}
	resources := lifecycle.resources
	if resources == nil {
		lifecycle.resourcesClosed = true
		serverLifecycles.CompareAndDelete(srv, lifecycle)
		lifecycle.resourceMu.Unlock()
		return nil
	}
	lifecycle.resourceMu.Unlock()

	if err := resources.Close(ctx); err != nil {
		return err
	}

	lifecycle.resourceMu.Lock()
	if !lifecycle.resourcesClosed {
		lifecycle.resourcesClosed = true
		lifecycle.resources = nil
		serverLifecycles.CompareAndDelete(srv, lifecycle)
	}
	lifecycle.resourceMu.Unlock()
	return nil
}

func (lifecycle *serverLifecycle) finish(ctx context.Context, srv *http.Server) error {
	if err := waitForLifecycle(ctx, lifecycle.transportDone); err != nil {
		return err
	}
	if err := waitForLifecycle(ctx, lifecycle.drained); err != nil {
		return err
	}
	resourceErr := lifecycle.closeResources(ctx, srv)
	if resourceErr == nil {
		return lifecycle.transportErr
	}
	return errors.Join(lifecycle.transportErr, resourceErr)
}

func (lifecycle *serverLifecycle) resourceCleanupComplete() bool {
	lifecycle.resourceMu.Lock()
	defer lifecycle.resourceMu.Unlock()
	return lifecycle.resourcesClosed
}

// retryCleanupUntilDone retries one cleanup owner with exponential backoff.
// The delay is capped so a long-running process continues making progress
// without either spinning or becoming arbitrarily slow to recover. A non-nil
// error returned alongside done=true is terminal (for example, an HTTP
// transport error after every resource has nevertheless closed).
func retryCleanupUntilDone(
	attempt func() (done bool, err error),
	report func(error),
) error {
	backoff := backgroundCleanupInitialBackoff
	for {
		done, err := attempt()
		if done {
			return err
		}
		if err != nil && report != nil {
			report(err)
		}

		timer := time.NewTimer(backoff)
		<-timer.C
		if backoff < backgroundCleanupMaxBackoff {
			backoff *= 2
			if backoff > backgroundCleanupMaxBackoff {
				backoff = backgroundCleanupMaxBackoff
			}
		}
	}
}

// closeResourcesWithRetry is shared by lifecycle-owned background cleanup and
// startup-failure teardown. It does not return until resources are proven
// closed; report observes transient failures without taking ownership of retry
// policy. A nil resource owner is already complete.
func closeResourcesWithRetry(resources resourceCloser, report func(error)) {
	if resources == nil {
		return
	}
	_ = retryCleanupUntilDone(func() (bool, error) {
		err := resources.Close(context.Background())
		return err == nil, err
	}, report)
}

func (lifecycle *serverLifecycle) scheduleBackgroundFinish(srv *http.Server) <-chan struct{} {
	lifecycle.backgroundFinishOnce.Do(func() {
		go func() {
			defer close(lifecycle.backgroundFinishDone)
			err := retryCleanupUntilDone(func() (bool, error) {
				err := lifecycle.finish(context.Background(), srv)
				return lifecycle.resourceCleanupComplete(), err
			}, func(err error) {
				zap.L().Warn("server background cleanup attempt failed; retrying", zap.Error(err))
			})
			if err != nil {
				// Resources are closed at this point. A remaining error belongs to
				// the already-finished HTTP transport and must not trigger retries.
				zap.L().Error("server transport shutdown completed with error", zap.Error(err))
			}
		}()
	})
	return lifecycle.backgroundFinishDone
}

func serveHTTP(srv *http.Server, listener net.Listener) {
	serveErr := srv.Serve(listener)
	lifecycle, registered := lookupServerLifecycle(srv)
	if !registered {
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			zap.L().Error("server failed", zap.Error(serveErr))
		}
		return
	}
	if errors.Is(serveErr, http.ErrServerClosed) && lifecycle.isHelperOwned() {
		return
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		zap.L().Error("server failed", zap.Error(serveErr))
	}

	// Unexpected Serve exits and callers that bypass Shutdown use the same
	// transport/drain/resource seam. Close forces any remaining connections to
	// release their handlers; resource cleanup still waits for the handlers.
	lifecycle.startTransportShutdown(srv, false)
	if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
		zap.L().Error("force close server failed", zap.Error(closeErr))
	}
	// Unexpected transport exit has no caller left to retry cleanup. Use the
	// same single background owner as timeout shutdown, but do not return from
	// serveHTTP until that owner has proved every resource closed.
	<-lifecycle.scheduleBackgroundFinish(srv)
}

// Shutdown drains HTTP requests and then waits for all server-owned background
// work to stop. The transport shutdown starts only once, while each caller
// retains an independent context for waiting and resource cleanup. Incomplete
// resource cleanup leaves one retrying background owner behind; a timed-out
// caller also forces active connections closed. Caller errors and deadlines are
// never cached for later callers.
func Shutdown(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return errors.New("shutdown nil server")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lifecycle, registered := lookupServerLifecycle(srv)
	if !registered {
		return srv.Shutdown(ctx)
	}

	lifecycle.startTransportShutdown(srv, true)
	err := lifecycle.finish(ctx, srv)
	if err == nil {
		return nil
	}
	// A resource failure must not depend on another caller arriving to make
	// progress. Schedule the single retry owner for every incomplete cleanup,
	// including non-context errors observed by a parent watcher using a
	// background context. When resources are already closed, err belongs to the
	// terminal transport and must not start an infinite retry loop.
	if !lifecycle.resourceCleanupComplete() {
		lifecycle.scheduleBackgroundFinish(srv)
	}
	ctxErr := ctx.Err()
	if ctxErr == nil || !errors.Is(err, ctxErr) {
		return err
	}

	if closeErr := srv.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
		zap.L().Error("force close server after shutdown timeout failed", zap.Error(closeErr))
	}
	return ctxErr
}
