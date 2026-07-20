// Package asyncruntime owns fire-and-forget tasks that must be cancelled and
// joined before their server-owned dependencies are closed.
package asyncruntime

import (
	"context"
	"errors"
	"sync"
)

// ErrClosed means a task was rejected and will never execute.
var ErrClosed = errors.New("async runtime is closed")

// Task is asynchronous work bound to a Runtime's lifecycle.
//
// Implementations should stop promptly after ctx is cancelled. A Task must not
// call Close on the Runtime that is executing it, because Close waits for every
// accepted Task, including the caller.
type Task func(ctx context.Context)

// Submitter is the narrow task-admission seam used by runtime consumers.
// A nil error means task was accepted for exactly one execution; a non-nil
// error guarantees task was not accepted and will never execute.
type Submitter interface {
	Submit(Task) error
}

// Runtime admits asynchronous tasks until Close begins, then cancels and joins
// every task it accepted. Its zero value is not usable; construct one with New.
type Runtime struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	wait   sync.WaitGroup
	done   chan struct{}
}

// New constructs a Runtime whose tasks are also cancelled when parent is
// cancelled. A nil parent is treated as context.Background.
func New(parent context.Context) *Runtime {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &Runtime{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

// Submit accepts task for exactly one asynchronous execution. ErrClosed
// guarantees that task was not accepted and will never execute.
//
// Admission and shutdown are linearized under the same mutex: the WaitGroup is
// incremented before an accepted Submit releases the mutex, while Close seals
// admission before it starts waiting. This prevents Add from racing with a
// Wait that observed a zero counter.
func (runtime *Runtime) Submit(task Task) error {
	if task == nil {
		panic("asyncruntime: nil Task")
	}

	runtime.mu.Lock()
	if runtime.closed || runtime.ctx.Err() != nil {
		runtime.mu.Unlock()
		return ErrClosed
	}
	runtime.wait.Add(1)
	runtime.mu.Unlock()

	go func() {
		defer runtime.wait.Done()
		task(runtime.ctx)
	}()
	return nil
}

// Close permanently rejects new tasks, cancels the shared task context, and
// waits for every accepted task. It is safe to call Close repeatedly or from
// multiple goroutines.
//
// If waitCtx expires, Close returns waitCtx.Err but the Runtime remains closed
// and its tasks remain cancelled. A later Close call can continue waiting for
// the same tasks.
func (runtime *Runtime) Close(waitCtx context.Context) error {
	if waitCtx == nil {
		waitCtx = context.Background()
	}

	runtime.mu.Lock()
	if !runtime.closed {
		runtime.closed = true
		runtime.cancel()
		go func() {
			runtime.wait.Wait()
			close(runtime.done)
		}()
	}
	done := runtime.done
	runtime.mu.Unlock()

	// Prefer successful completion when the tasks were already joined, even if
	// the caller supplied an already-cancelled wait context.
	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}
