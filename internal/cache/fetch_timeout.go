package cache

import (
	"context"
	"errors"
	"time"
)

const defaultFetchTimeout = 10 * time.Minute

type fetchTimeoutContextKey struct{}
type fetchIdleTimeoutContextKey struct{}

type fetchTimeoutHint struct {
	timeout time.Duration
}

type fetchIdleTimeoutHint struct {
	timeout time.Duration
}

// ErrFetchIdleTimeout reports that an opened upstream response body made no
// read progress for the configured rolling idle interval.
var ErrFetchIdleTimeout = errors.New("upstream response body idle timeout")

// WithFetchTimeout overrides Manager's total upstream-transfer timeout for one
// Get/Prefetch operation. A zero timeout disables Manager's additional total
// deadline; cancellation from the operation or Manager lifecycle still applies.
// Without this hint Manager keeps its historical 10-minute ceiling.
func WithFetchTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, fetchTimeoutContextKey{}, fetchTimeoutHint{timeout: timeout})
}

func fetchTimeoutFrom(ctx context.Context) time.Duration {
	if hint, ok := ctx.Value(fetchTimeoutContextKey{}).(fetchTimeoutHint); ok {
		return hint.timeout
	}
	return defaultFetchTimeout
}

// WithFetchIdleTimeout adds a rolling no-progress timeout to an opened
// upstream response body. Every successful Read resets the interval. A
// non-positive timeout disables the watchdog. This is independent of the total
// transfer timeout, so a large body may run indefinitely while bytes continue
// to arrive.
func WithFetchIdleTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, fetchIdleTimeoutContextKey{}, fetchIdleTimeoutHint{timeout: timeout})
}

func fetchIdleTimeoutFrom(ctx context.Context) time.Duration {
	if hint, ok := ctx.Value(fetchIdleTimeoutContextKey{}).(fetchIdleTimeoutHint); ok {
		return hint.timeout
	}
	return 0
}

func newFetchContext(
	parent context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc, context.CancelCauseFunc) {
	var (
		base       context.Context
		cancelBase context.CancelFunc
	)
	if timeout == 0 {
		base, cancelBase = context.WithCancel(parent)
	} else {
		base, cancelBase = context.WithTimeout(parent, timeout)
	}
	ctx, cancelCause := context.WithCancelCause(base)
	cancelWithCause := func(cause error) {
		cancelCause(cause)
		cancelBase()
	}
	cancel := func() {
		cancelWithCause(context.Canceled)
	}
	return ctx, cancel, cancelWithCause
}
