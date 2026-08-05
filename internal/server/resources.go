package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
)

// resourceCloser is the private seam for server-owned resources whose Close
// operation is bounded by the current shutdown attempt.
type resourceCloser interface {
	Close(context.Context) error
}

type resourceCloseAttempt struct {
	done chan struct{}
	err  error
}

// asyncCloseAdapter turns an irreversible, context-free close attempt into a
// context-aware resourceCloser. Concurrent callers join one background
// attempt and keep independent wait contexts. A failed attempt remains
// retryable by a later explicit Close; success is remembered permanently.
type asyncCloseAdapter struct {
	close func() error

	mu       sync.Mutex
	current  *resourceCloseAttempt
	complete bool
}

func newAsyncCloseAdapter(close func() error) *asyncCloseAdapter {
	return &asyncCloseAdapter{close: close}
}

func (adapter *asyncCloseAdapter) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	adapter.mu.Lock()
	if adapter.complete {
		adapter.mu.Unlock()
		return nil
	}
	attempt := adapter.current
	if attempt == nil {
		attempt = &resourceCloseAttempt{done: make(chan struct{})}
		adapter.current = attempt
		go func() {
			var err error
			if adapter.close != nil {
				err = adapter.close()
			}

			adapter.mu.Lock()
			attempt.err = err
			if err == nil {
				adapter.complete = true
			}
			if adapter.current == attempt {
				adapter.current = nil
			}
			close(attempt.done)
			adapter.mu.Unlock()
		}()
	}
	adapter.mu.Unlock()

	return waitForResourceAttempt(ctx, attempt)
}

// serverResources owns the ordered, retryable teardown of dependencies shared
// by a server instance. Successful stages are remembered across Close calls;
// failed stages remain pending for the next attempt.
type serverResources struct {
	cancelServer  func()
	closeListener func() error

	accessRecorder  resourceCloser
	cacheManager    resourceCloser
	compileCache    resourceCloser
	securityScanner resourceCloser
	background      resourceCloser
	closeFetcher    func()
	closeRegistry   resourceCloser
	closeDatabase   resourceCloser

	cancelOnce sync.Once

	mu       sync.Mutex
	current  *resourceCloseAttempt
	complete bool

	listenerClosed bool
	accessClosed   bool
	cacheClosed    bool
	compileClosed  bool
	scannerClosed  bool
	backgroundDone bool
	fetcherClosed  bool
	registryClosed bool
	databaseClosed bool
}

func newServerResources(cancelServer func(), closeListener func() error) *serverResources {
	return &serverResources{
		cancelServer:  cancelServer,
		closeListener: closeListener,
	}
}

// Close quiesces the server and advances its resource teardown as far as ctx
// and the dependency graph allow. It is safe to call concurrently and to retry
// after either a context deadline or a resource error.
//
// Concurrent callers join the active attempt. If that attempt ends because its
// context expired, a waiter whose own context is still live automatically
// starts or joins the next attempt. A non-context resource error is returned to
// every caller that joined that attempt; a later explicit Close retries it.
func (resources *serverResources) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		resources.mu.Lock()
		if resources.complete {
			resources.mu.Unlock()
			return nil
		}
		if resources.current != nil {
			attempt := resources.current
			resources.mu.Unlock()

			err := waitForResourceAttempt(ctx, attempt)
			if err != nil && isContextError(err) {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				continue
			}
			return err
		}

		attempt := &resourceCloseAttempt{done: make(chan struct{})}
		resources.current = attempt
		resources.mu.Unlock()

		err := resources.closeAttempt(ctx)

		resources.mu.Lock()
		if resources.allClosed() {
			resources.complete = true
		}
		attempt.err = err
		resources.current = nil
		close(attempt.done)
		resources.mu.Unlock()
		return err
	}
}

func waitForResourceAttempt(ctx context.Context, attempt *resourceCloseAttempt) error {
	// Prefer an attempt result that is already available over a simultaneous
	// cancellation of this waiter.
	select {
	case <-attempt.done:
		return attempt.err
	default:
	}

	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		select {
		case <-attempt.done:
			return attempt.err
		default:
			return ctx.Err()
		}
	}
}

func (resources *serverResources) closeAttempt(ctx context.Context) error {
	var closeErrors []error

	// Quiescing is irreversible and idempotent. Cancellation happens once; a
	// listener close error remains retryable independently.
	resources.cancelOnce.Do(func() {
		if resources.cancelServer != nil {
			resources.cancelServer()
		}
	})
	if !resources.listenerClosed {
		switch {
		case resources.closeListener == nil:
			resources.listenerClosed = true
		default:
			err := resources.closeListener()
			if err == nil || errors.Is(err, net.ErrClosed) {
				resources.listenerClosed = true
			} else {
				closeErrors = append(closeErrors, fmt.Errorf("close listener: %w", err))
			}
		}
	}
	if err := resources.closeContextResource(ctx, "access recorder", resources.accessRecorder, &resources.accessClosed); err != nil {
		closeErrors = append(closeErrors, err)
		if isContextError(err) {
			return errors.Join(closeErrors...)
		}
	}

	if err := resources.closeContextResource(ctx, "cache manager", resources.cacheManager, &resources.cacheClosed); err != nil {
		closeErrors = append(closeErrors, err)
		if isContextError(err) {
			return errors.Join(closeErrors...)
		}
	}
	// Scanner and fetcher form a strict chain behind the cache. A failed cache
	// attempt leaves both pending without manufacturing extra errors.
	if resources.cacheClosed {
		if err := resources.closeContextResource(ctx, "security scanner", resources.securityScanner, &resources.scannerClosed); err != nil {
			closeErrors = append(closeErrors, err)
			if isContextError(err) {
				return errors.Join(closeErrors...)
			}
		}
	}
	if resources.scannerClosed {
		if err := resources.closeFunction(ctx, "OSV fetcher", resources.closeFetcher, &resources.fetcherClosed); err != nil {
			closeErrors = append(closeErrors, err)
			return errors.Join(closeErrors...)
		}
	}

	// The async runtime is independent, but is attempted after the scanner
	// branch so callbacks already in that branch get their final admission
	// opportunity before the runtime seals itself.
	if err := resources.closeContextResource(ctx, "background runtime", resources.background, &resources.backgroundDone); err != nil {
		closeErrors = append(closeErrors, err)
		if isContextError(err) {
			return errors.Join(closeErrors...)
		}
	}
	// Compiler-cache maintenance previously lived in the shared background
	// runtime. Preserve that shutdown position while giving the data domain its
	// own retryable close state and final metadata flush.
	if err := resources.closeContextResource(ctx, "compiler-cache runtime", resources.compileCache, &resources.compileClosed); err != nil {
		closeErrors = append(closeErrors, err)
		if isContextError(err) {
			return errors.Join(closeErrors...)
		}
	}

	if resources.cacheClosed && resources.scannerClosed && resources.backgroundDone && resources.compileClosed {
		if err := resources.closeContextResource(ctx, "upstream registry", resources.closeRegistry, &resources.registryClosed); err != nil {
			closeErrors = append(closeErrors, err)
			if isContextError(err) {
				return errors.Join(closeErrors...)
			}
		}
	}

	if resources.accessClosed && resources.cacheClosed && resources.compileClosed && resources.scannerClosed &&
		resources.backgroundDone && resources.fetcherClosed && resources.registryClosed {
		if err := resources.closeContextResource(ctx, "database", resources.closeDatabase, &resources.databaseClosed); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}

	return errors.Join(closeErrors...)
}

func (resources *serverResources) closeContextResource(
	ctx context.Context,
	stage string,
	closer resourceCloser,
	done *bool,
) error {
	if *done {
		return nil
	}
	if closer == nil {
		*done = true
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("close %s: %w", stage, err)
	}
	if err := closer.Close(ctx); err != nil {
		return fmt.Errorf("close %s: %w", stage, err)
	}
	*done = true
	return nil
}

func (resources *serverResources) closeFunction(
	ctx context.Context,
	stage string,
	close func(),
	done *bool,
) error {
	if *done {
		return nil
	}
	if close == nil {
		*done = true
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("close %s: %w", stage, err)
	}
	close()
	*done = true
	return nil
}

func (resources *serverResources) allClosed() bool {
	return resources.listenerClosed &&
		resources.accessClosed &&
		resources.cacheClosed &&
		resources.compileClosed &&
		resources.scannerClosed &&
		resources.backgroundDone &&
		resources.fetcherClosed &&
		resources.registryClosed &&
		resources.databaseClosed
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
