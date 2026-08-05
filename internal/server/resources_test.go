package server

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type resourceCloserFunc func(context.Context) error

func (close resourceCloserFunc) Close(ctx context.Context) error {
	return close(ctx)
}

type resourceEventLog struct {
	mu     sync.Mutex
	events []string
}

func (log *resourceEventLog) add(event string) {
	log.mu.Lock()
	log.events = append(log.events, event)
	log.mu.Unlock()
}

func (log *resourceEventLog) snapshot() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.events...)
}

func TestServerResourcesPreservesOrderAndDatabaseSafetyAcrossRetry(t *testing.T) {
	var events resourceEventLog
	cacheFailure := errors.New("cache still draining")
	var cacheAttempts atomic.Int32

	resources := newServerResources(
		func() { events.add("cancel") },
		func() error { events.add("listener"); return nil },
	)
	resources.accessRecorder = resourceCloserFunc(func(context.Context) error {
		events.add("access")
		return nil
	})
	resources.cacheManager = resourceCloserFunc(func(context.Context) error {
		events.add("cache")
		if cacheAttempts.Add(1) == 1 {
			return cacheFailure
		}
		return nil
	})
	resources.securityScanner = resourceCloserFunc(func(context.Context) error {
		events.add("scanner")
		return nil
	})
	resources.background = resourceCloserFunc(func(context.Context) error {
		events.add("background")
		return nil
	})
	resources.closeFetcher = func() { events.add("fetcher") }
	resources.closeRegistry = resourceCloserFunc(func(context.Context) error {
		events.add("registry")
		return nil
	})
	resources.closeDatabase = resourceCloserFunc(func(context.Context) error {
		events.add("database")
		return nil
	})

	err := resources.Close(context.Background())
	if !errors.Is(err, cacheFailure) {
		t.Fatalf("first Close error = %v, want cache failure", err)
	}
	if !strings.Contains(err.Error(), "close cache manager") {
		t.Fatalf("first Close error = %q, want cache stage", err)
	}
	wantFirst := []string{"cancel", "listener", "access", "cache", "background"}
	if got := events.snapshot(); !reflect.DeepEqual(got, wantFirst) {
		t.Fatalf("events after first Close = %v, want %v", got, wantFirst)
	}

	if err := resources.Close(context.Background()); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	wantAll := append(wantFirst, "cache", "scanner", "fetcher", "registry", "database")
	if got := events.snapshot(); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("events after retry = %v, want %v", got, wantAll)
	}

	if err := resources.Close(context.Background()); err != nil {
		t.Fatalf("completed Close: %v", err)
	}
	if got := events.snapshot(); !reflect.DeepEqual(got, wantAll) {
		t.Fatalf("completed Close repeated stages: got %v, want %v", got, wantAll)
	}
}

func TestServerResourcesRetriesCompileCacheBeforeDatabase(t *testing.T) {
	var events resourceEventLog
	compileFailure := errors.New("compile cache still flushing")
	var attempts atomic.Int32
	resources := newServerResources(func() { events.add("cancel") }, nil)
	resources.background = resourceCloserFunc(func(context.Context) error {
		events.add("background")
		return nil
	})
	resources.compileCache = resourceCloserFunc(func(context.Context) error {
		events.add("compile")
		if attempts.Add(1) == 1 {
			return compileFailure
		}
		return nil
	})
	resources.closeRegistry = resourceCloserFunc(func(context.Context) error {
		events.add("registry")
		return nil
	})
	resources.closeDatabase = resourceCloserFunc(func(context.Context) error {
		events.add("database")
		return nil
	})

	if err := resources.Close(context.Background()); !errors.Is(err, compileFailure) {
		t.Fatalf("first close error = %v, want compiler-cache failure", err)
	}
	if got, want := events.snapshot(), []string{"cancel", "background", "compile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first close events = %v, want %v", got, want)
	}
	if err := resources.Close(context.Background()); err != nil {
		t.Fatalf("retry close: %v", err)
	}
	if got, want := events.snapshot(), []string{"cancel", "background", "compile", "compile", "registry", "database"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retried close events = %v, want %v", got, want)
	}
}

func TestServerResourcesTimeoutCanBeRetried(t *testing.T) {
	var attempts atomic.Int32
	var databaseCloses atomic.Int32
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloserFunc(func(ctx context.Context) error {
		if attempts.Add(1) == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	resources.closeDatabase = resourceCloserFunc(func(context.Context) error {
		databaseCloses.Add(1)
		return nil
	})

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancelShort()
	if err := resources.Close(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Close error = %v, want context.DeadlineExceeded", err)
	}
	if got := databaseCloses.Load(); got != 0 {
		t.Fatalf("database closes after timeout = %d, want 0", got)
	}

	retryCtx, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := resources.Close(retryCtx); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("cache attempts = %d, want 2", got)
	}
	if got := databaseCloses.Load(); got != 1 {
		t.Fatalf("database closes = %d, want 1", got)
	}
}

func TestServerResourcesLongWaiterTakesOverAfterShortAttemptCancellation(t *testing.T) {
	firstStarted := make(chan struct{})
	var attempts atomic.Int32
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloserFunc(func(ctx context.Context) error {
		if attempts.Add(1) == 1 {
			close(firstStarted)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})

	shortCtx, cancelShort := context.WithCancel(context.Background())
	shortResult := make(chan error, 1)
	go func() { shortResult <- resources.Close(shortCtx) }()
	awaitResourceSignal(t, firstStarted, "first close attempt")

	longBase, cancelLong := context.WithTimeout(context.Background(), time.Second)
	defer cancelLong()
	longWaiting := make(chan struct{})
	longCtx := &doneObservedContext{Context: longBase, observed: longWaiting}
	longResult := make(chan error, 1)
	go func() { longResult <- resources.Close(longCtx) }()
	awaitResourceSignal(t, longWaiting, "long caller joining first attempt")

	cancelShort()
	if err := awaitResourceResult(t, shortResult, "short Close"); !errors.Is(err, context.Canceled) {
		t.Fatalf("short Close error = %v, want context.Canceled", err)
	}
	if err := awaitResourceResult(t, longResult, "long Close"); err != nil {
		t.Fatalf("long Close: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want automatic retry by long waiter", got)
	}
}

type errGatedContext struct {
	context.Context
	doneObserved chan struct{}
	errObserved  chan struct{}
	allowErr     chan struct{}
	doneOnce     sync.Once
	errOnce      sync.Once
}

func (ctx *errGatedContext) Done() <-chan struct{} {
	ctx.doneOnce.Do(func() { close(ctx.doneObserved) })
	return ctx.Context.Done()
}

func (ctx *errGatedContext) Err() error {
	ctx.errOnce.Do(func() { close(ctx.errObserved) })
	<-ctx.allowErr
	return ctx.Context.Err()
}

func TestServerResourcesJoinedContextErrorUsesWaiterCancellation(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloserFunc(func(context.Context) error {
		close(firstStarted)
		<-releaseFirst
		return context.DeadlineExceeded
	})

	leaderResult := make(chan error, 1)
	go func() { leaderResult <- resources.Close(context.Background()) }()
	awaitResourceSignal(t, firstStarted, "leader close attempt")

	waiterBase, cancelWaiter := context.WithCancel(context.Background())
	defer cancelWaiter()
	waiterCtx := &errGatedContext{
		Context:      waiterBase,
		doneObserved: make(chan struct{}),
		errObserved:  make(chan struct{}),
		allowErr:     make(chan struct{}),
	}
	waiterResult := make(chan error, 1)
	go func() { waiterResult <- resources.Close(waiterCtx) }()
	awaitResourceSignal(t, waiterCtx.doneObserved, "waiter joining leader attempt")

	close(releaseFirst)
	awaitResourceSignal(t, waiterCtx.errObserved, "waiter inspecting its context")
	cancelWaiter()
	close(waiterCtx.allowErr)

	if err := awaitResourceResult(t, leaderResult, "leader Close"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("leader Close error = %v, want deadline exceeded", err)
	}
	if err := awaitResourceResult(t, waiterResult, "waiter Close"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter Close error = %v, want its own cancellation", err)
	}
}

func TestServerResourcesJoinedNonContextErrorNeedsExplicitRetry(t *testing.T) {
	transient := errors.New("transient close failure")
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var attempts atomic.Int32
	resources := newServerResources(nil, nil)
	resources.cacheManager = resourceCloserFunc(func(context.Context) error {
		if attempts.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return transient
		}
		return nil
	})

	leaderResult := make(chan error, 1)
	go func() { leaderResult <- resources.Close(context.Background()) }()
	awaitResourceSignal(t, firstStarted, "transient attempt")

	waiterJoined := make(chan struct{})
	waiterCtx := &doneObservedContext{Context: context.Background(), observed: waiterJoined}
	waiterResult := make(chan error, 1)
	go func() { waiterResult <- resources.Close(waiterCtx) }()
	awaitResourceSignal(t, waiterJoined, "waiter joining transient attempt")
	close(releaseFirst)

	if err := awaitResourceResult(t, leaderResult, "leader Close"); !errors.Is(err, transient) {
		t.Fatalf("leader error = %v, want transient error", err)
	}
	if err := awaitResourceResult(t, waiterResult, "joined Close"); !errors.Is(err, transient) {
		t.Fatalf("waiter error = %v, want same transient error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts after joined non-context error = %d, want 1", got)
	}

	if err := resources.Close(context.Background()); err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts after explicit retry = %d, want 2", got)
	}
}

func TestServerResourcesQuiescesOnceAndHandlesPartialStartup(t *testing.T) {
	var cancels atomic.Int32
	var listeners atomic.Int32
	databaseFailure := errors.New("database busy")
	var databaseAttempts atomic.Int32

	resources := newServerResources(
		func() { cancels.Add(1) },
		func() error { listeners.Add(1); return nil },
	)
	resources.closeDatabase = resourceCloserFunc(func(context.Context) error {
		if databaseAttempts.Add(1) == 1 {
			return databaseFailure
		}
		return nil
	})

	if err := resources.Close(nil); !errors.Is(err, databaseFailure) {
		t.Fatalf("first Close error = %v, want database failure", err)
	}
	if err := resources.Close(nil); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if err := resources.Close(nil); err != nil {
		t.Fatalf("completed Close: %v", err)
	}

	if got := cancels.Load(); got != 1 {
		t.Fatalf("cancel calls = %d, want 1", got)
	}
	if got := listeners.Load(); got != 1 {
		t.Fatalf("listener closes = %d, want 1", got)
	}
	if got := databaseAttempts.Load(); got != 2 {
		t.Fatalf("database attempts = %d, want 2", got)
	}

	// A completely empty owner represents failure before any dependency was
	// constructed and must still be safely closable.
	if err := newServerResources(nil, nil).Close(nil); err != nil {
		t.Fatalf("Close empty resources: %v", err)
	}
}

func TestServerResourcesAsyncDatabaseCloseTimeoutThenLaterWaitObservesSuccess(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	adapter := newAsyncCloseAdapter(func() error {
		calls.Add(1)
		close(started)
		<-release
		return nil
	})
	resources := newServerResources(nil, nil)
	resources.closeDatabase = adapter

	shortCtx, cancelShort := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancelShort()
	shortResult := make(chan error, 1)
	go func() { shortResult <- resources.Close(shortCtx) }()
	awaitResourceSignal(t, started, "underlying async close")
	if err := awaitResourceResult(t, shortResult, "timed-out resource Close"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Close error = %v, want deadline exceeded", err)
	}

	longResult := make(chan error, 1)
	go func() { longResult <- resources.Close(context.Background()) }()
	close(release)
	if err := awaitResourceResult(t, longResult, "later resource Close"); err != nil {
		t.Fatalf("later Close error = %v, want nil", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resources.Close(canceledCtx); err != nil {
		t.Fatalf("completed Close with canceled waiter = %v, want stable nil", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying close calls = %d, want 1", got)
	}
}

func TestAsyncCloseAdapterSharesFailureThenRetriesExplicitly(t *testing.T) {
	sentinel := errors.New("close failed")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	adapter := newAsyncCloseAdapter(func() error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return sentinel
		}
		return nil
	})

	leaderResult := make(chan error, 1)
	waiterResult := make(chan error, 1)
	go func() { leaderResult <- adapter.Close(context.Background()) }()
	awaitResourceSignal(t, started, "first async close attempt")
	waiterJoined := make(chan struct{})
	waiterCtx := &doneObservedContext{Context: context.Background(), observed: waiterJoined}
	go func() { waiterResult <- adapter.Close(waiterCtx) }()
	awaitResourceSignal(t, waiterJoined, "waiter joining first async close attempt")
	close(release)
	for description, result := range map[string]<-chan error{
		"leader": leaderResult,
		"waiter": waiterResult,
	} {
		if err := awaitResourceResult(t, result, description+" Close"); !errors.Is(err, sentinel) {
			t.Fatalf("%s Close error = %v, want sentinel", description, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying close calls after joined failure = %d, want 1", got)
	}

	if err := adapter.Close(context.Background()); err != nil {
		t.Fatalf("explicit retry Close = %v, want nil", err)
	}
	if err := adapter.Close(context.Background()); err != nil {
		t.Fatalf("completed Close = %v, want stable nil", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("underlying close calls after retry = %d, want 2", got)
	}
}

type doneObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (ctx *doneObservedContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func awaitResourceSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func awaitResourceResult(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}
