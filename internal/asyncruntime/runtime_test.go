package asyncruntime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitRunsAcceptedTaskExactlyOnceAndCloseCancelsIt(t *testing.T) {
	runtime := New(context.Background())
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var calls atomic.Int32

	if err := runtime.Submit(func(ctx context.Context) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(cancelled)
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	awaitSignal(t, started, "task start")

	closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := runtime.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitSignal(t, cancelled, "task cancellation")
	if got := calls.Load(); got != 1 {
		t.Fatalf("task executions = %d, want 1", got)
	}

	var rejectedCalled atomic.Bool
	if err := runtime.Submit(func(context.Context) { rejectedCalled.Store(true) }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after Close error = %v, want ErrClosed", err)
	}
	if rejectedCalled.Load() {
		t.Fatal("task rejected after Close was executed")
	}
}

func TestSubmitRejectsAfterParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	runtime := New(parent)
	cancelParent()

	var called atomic.Bool
	if err := runtime.Submit(func(context.Context) { called.Store(true) }); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after parent cancellation error = %v, want ErrClosed", err)
	}
	if called.Load() {
		t.Fatal("task rejected after parent cancellation was executed")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestConcurrentSubmitAndCloseLinearizeAdmission(t *testing.T) {
	runtime := New(context.Background())

	const taskCount = 1024
	const closerCount = 32
	var executions [taskCount]atomic.Int32
	results := make([]error, taskCount)
	start := make(chan struct{})

	var submitters sync.WaitGroup
	submitters.Add(taskCount)
	for index := range taskCount {
		index := index
		go func() {
			defer submitters.Done()
			<-start
			results[index] = runtime.Submit(func(context.Context) {
				executions[index].Add(1)
			})
		}()
	}

	closeErrors := make(chan error, closerCount)
	var closers sync.WaitGroup
	closers.Add(closerCount)
	for range closerCount {
		go func() {
			defer closers.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			closeErrors <- runtime.Close(ctx)
		}()
	}

	close(start)
	submitters.Wait()
	closers.Wait()
	close(closeErrors)
	for err := range closeErrors {
		if err != nil {
			t.Fatalf("concurrent Close: %v", err)
		}
	}

	for index, err := range results {
		switch {
		case err == nil:
			if got := executions[index].Load(); got != 1 {
				t.Fatalf("accepted task %d executions = %d, want 1", index, got)
			}
		case errors.Is(err, ErrClosed):
			if got := executions[index].Load(); got != 0 {
				t.Fatalf("rejected task %d executions = %d, want 0", index, got)
			}
		default:
			t.Fatalf("task %d Submit error = %v, want nil or ErrClosed", index, err)
		}
	}

	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

func TestCloseTimeoutCanBeRetried(t *testing.T) {
	runtime := New(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseTask := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseTask)

	if err := runtime.Submit(func(context.Context) {
		close(started)
		// Deliberately ignore cancellation to exercise Close's wait deadline.
		<-release
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	awaitSignal(t, started, "blocking task start")

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelShort()
	if err := runtime.Close(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Close error = %v, want context.DeadlineExceeded", err)
	}
	if err := runtime.Submit(func(context.Context) {}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after timed-out Close error = %v, want ErrClosed", err)
	}

	releaseTask()
	retryCtx, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := runtime.Close(retryCtx); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
}

func TestSubmitNilTaskPanics(t *testing.T) {
	runtime := New(context.Background())
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	defer func() {
		if recover() == nil {
			t.Fatal("Submit(nil) did not panic")
		}
	}()
	_ = runtime.Submit(nil)
}

func awaitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
