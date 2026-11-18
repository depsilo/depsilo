package cache

import (
	"context"
	"errors"
	"sync"
)

type forceRefreshContextKey struct{}

type forceRefreshRequest struct {
	forced       bool
	contextBound bool
	tracker      *RefreshTracker
}

// RefreshTracker reports when a refresh or prefetch has durably completed. It
// is attached to the inflight storage operation by Manager.Get, allowing admin
// callers to wait for slow local/S3 writes without polling the database.
type RefreshTracker struct {
	once    sync.Once
	done    chan struct{}
	mu      sync.Mutex
	err     error
	outcome RefreshOutcome
	used    bool
}

// RefreshOutcome identifies the Upstream used by a tracked refresh and
// whether its conditional request returned HTTP 304.
type RefreshOutcome struct {
	Upstream    string
	NotModified bool
}

func newRefreshTracker() *RefreshTracker {
	return &RefreshTracker{done: make(chan struct{})}
}

// WithForceRefresh marks an internal request as an operator-triggered cache
// refresh. Adapters keep their normal fetch/rewrite logic; Manager simply
// bypasses a fresh mutable-metadata hit for this request.
func WithForceRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, forceRefreshContextKey{}, forceRefreshRequest{forced: true})
}

// WithTrackedForceRefresh additionally returns a completion handle for the
// durable cache write performed by the forced request.
func WithTrackedForceRefresh(ctx context.Context) (context.Context, *RefreshTracker) {
	tracker := newRefreshTracker()
	return context.WithValue(ctx, forceRefreshContextKey{}, forceRefreshRequest{forced: true, contextBound: true, tracker: tracker}), tracker
}

func forceRefreshRequested(ctx context.Context) bool {
	return refreshRequestFrom(ctx).forced
}

func refreshTrackerFrom(ctx context.Context) *RefreshTracker {
	return refreshRequestFrom(ctx).tracker
}

func refreshRequestFrom(ctx context.Context) forceRefreshRequest {
	request, _ := ctx.Value(forceRefreshContextKey{}).(forceRefreshRequest)
	return request
}

func persistenceRequired(ctx context.Context) bool {
	request := refreshRequestFrom(ctx)
	return request.contextBound || request.forced
}

func withTrackedPrefetch(ctx context.Context) (context.Context, *RefreshTracker) {
	tracker := newRefreshTracker()
	request := refreshRequestFrom(ctx)
	request.contextBound = true
	request.tracker = tracker
	return context.WithValue(ctx, forceRefreshContextKey{}, request), tracker
}

func (t *RefreshTracker) markUsed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.used = true
	t.mu.Unlock()
}

func (t *RefreshTracker) complete(outcome RefreshOutcome, err error) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.outcome = outcome
		t.mu.Unlock()
		close(t.done)
	})
}

// Used reports whether the request reached a cache refresh operation.
func (t *RefreshTracker) Used() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used
}

// Wait blocks until the forced refresh's cache write and DB update are both
// complete. A conditional HTTP 304 is a successful refresh.
func (t *RefreshTracker) Wait(ctx context.Context) error {
	_, err := t.Outcome(ctx)
	return err
}

// Outcome waits for the durable refresh and returns its selected Upstream and
// conditional-request result. ErrNotModified is represented by NotModified
// and remains a successful outcome.
func (t *RefreshTracker) Outcome(ctx context.Context) (RefreshOutcome, error) {
	if t == nil {
		return RefreshOutcome{}, errors.New("nil refresh tracker")
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return RefreshOutcome{}, ctx.Err()
	}
	t.mu.Lock()
	err := t.err
	outcome := t.outcome
	t.mu.Unlock()
	if errors.Is(err, ErrNotModified) {
		return outcome, nil
	}
	return outcome, err
}
