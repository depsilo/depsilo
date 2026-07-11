package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

type serverLifecycle struct {
	cleanupOnce sync.Once
	cleanupDone chan struct{}
	cleanup     func() error
	cleanupErr  error

	shutdownOnce sync.Once
	shutdownDone chan struct{}
	shutdownErr  error

	mu          sync.Mutex
	closing     bool
	helperOwned bool
	active      int
	drained     chan struct{}
	drainedOnce sync.Once
}

var serverLifecycles sync.Map

type lifecycleHandler struct {
	lifecycle *serverLifecycle
	next      http.Handler
}

func registerServerLifecycle(srv *http.Server, cleanup func() error) *serverLifecycle {
	lifecycle := &serverLifecycle{
		cleanupDone:  make(chan struct{}),
		cleanup:      cleanup,
		shutdownDone: make(chan struct{}),
		drained:      make(chan struct{}),
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

func (lifecycle *serverLifecycle) runCleanup() error {
	lifecycle.cleanupOnce.Do(func() {
		defer close(lifecycle.cleanupDone)
		cleanup := lifecycle.cleanup
		if cleanup != nil {
			lifecycle.cleanupErr = cleanup()
		}
		lifecycle.cleanup = nil
	})
	<-lifecycle.cleanupDone
	return lifecycle.cleanupErr
}

func (lifecycle *serverLifecycle) finishAfterDrain(srv *http.Server, deleteEntry bool) error {
	<-lifecycle.drained
	err := lifecycle.runCleanup()
	if deleteEntry {
		serverLifecycles.Delete(srv)
	}
	return err
}

func (lifecycle *serverLifecycle) scheduleCleanupAfterDrain(srv *http.Server, deleteEntry bool) {
	go func() {
		if err := lifecycle.finishAfterDrain(srv, deleteEntry); err != nil {
			zap.L().Error("server cleanup failed", zap.Error(err))
		}
	}()
}

func serveHTTP(srv *http.Server, listener net.Listener) {
	err := srv.Serve(listener)
	lifecycle, registered := lookupServerLifecycle(srv)
	if !registered {
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			zap.L().Error("server failed", zap.Error(err))
		}
		return
	}
	if errors.Is(err, http.ErrServerClosed) && lifecycle.isHelperOwned() {
		return
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		zap.L().Error("server failed", zap.Error(err))
	}
	lifecycle.beginClosing(false)
	_ = srv.Close()
	if err := lifecycle.finishAfterDrain(srv, true); err != nil {
		zap.L().Error("server cleanup failed", zap.Error(err))
	}
}

// Shutdown drains HTTP requests and then waits for all server-owned background
// work to stop. If the HTTP drain times out, active connections are forced
// closed; dependencies remain alive until every entered handler has exited.
func Shutdown(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return errors.New("shutdown nil server")
	}
	lifecycle, registered := lookupServerLifecycle(srv)
	if !registered {
		return srv.Shutdown(ctx)
	}
	lifecycle.shutdownOnce.Do(func() {
		defer close(lifecycle.shutdownDone)
		lifecycle.beginClosing(true)
		shutdownErr := srv.Shutdown(ctx)
		if shutdownErr == nil {
			cleanupErr := lifecycle.finishAfterDrain(srv, true)
			lifecycle.shutdownErr = errors.Join(shutdownErr, cleanupErr)
			return
		}

		closeErr := srv.Close()
		lifecycle.shutdownErr = errors.Join(shutdownErr, closeErr)
		select {
		case <-lifecycle.drained:
			lifecycle.shutdownErr = errors.Join(lifecycle.shutdownErr, lifecycle.runCleanup())
			serverLifecycles.Delete(srv)
		default:
			lifecycle.scheduleCleanupAfterDrain(srv, true)
		}
	})
	<-lifecycle.shutdownDone
	return lifecycle.shutdownErr
}
