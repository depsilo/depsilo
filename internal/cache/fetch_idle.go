package cache

import (
	"context"
	"io"
	"sync"
	"time"
)

type fetchIdleState uint8

const (
	fetchIdleOpen fetchIdleState = iota
	fetchIdleFinished
	fetchIdleExpired
	fetchIdleClosed
)

// fetchIdleReadCloser applies a per-upstream-Read deadline without buffering
// the body. Time spent between Read calls belongs to the downstream/cache sink
// and must not be misclassified as an idle upstream.
type fetchIdleReadCloser struct {
	body      io.ReadCloser
	timeout   time.Duration
	onTimeout context.CancelFunc

	readMu   sync.Mutex
	arm      chan struct{}
	armed    chan struct{}
	disarm   chan struct{}
	disarmed chan struct{}
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	stateMu sync.Mutex
	state   fetchIdleState
	readErr error

	closeOnce sync.Once
	closeErr  error
}

type bodyIdleTimeoutDecorator interface {
	DecorateBodyIdleTimeout(time.Duration, context.CancelCauseFunc) io.ReadCloser
}

// WithBodyIdleTimeout bounds each blocking upstream body Read. Time spent by
// the caller processing or forwarding bytes between Read calls is excluded.
// On expiry, cancel receives
// ErrFetchIdleTimeout before body is closed, so a blocked producer and health
// accounting observe the same terminal cause. Blocked and subsequent reads
// return ErrFetchIdleTimeout.
//
// Close stops the watchdog and waits for it to exit. A non-positive timeout or
// nil body leaves body unchanged. Expiry relies on the io.ReadCloser contract
// provided by net/http response bodies: closing the body must unblock a
// concurrent Read.
func WithBodyIdleTimeout(
	body io.ReadCloser,
	timeout time.Duration,
	cancel context.CancelCauseFunc,
) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	if decorator, ok := body.(bodyIdleTimeoutDecorator); ok {
		return decorator.DecorateBodyIdleTimeout(timeout, cancel)
	}
	var onTimeout context.CancelFunc
	if cancel != nil {
		onTimeout = func() { cancel(ErrFetchIdleTimeout) }
	}
	return withFetchIdleTimeout(body, timeout, onTimeout)
}

func withFetchIdleTimeout(
	body io.ReadCloser,
	timeout time.Duration,
	onTimeout context.CancelFunc,
) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	wrapped := &fetchIdleReadCloser{
		body:      body,
		timeout:   timeout,
		onTimeout: onTimeout,
		arm:       make(chan struct{}),
		armed:     make(chan struct{}),
		disarm:    make(chan struct{}),
		disarmed:  make(chan struct{}),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go wrapped.watch()
	return wrapped
}

func (r *fetchIdleReadCloser) watch() {
	defer close(r.done)
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	stopFetchIdleTimer(timer)
	var deadline <-chan time.Time

	for {
		select {
		case <-r.stop:
			return
		case <-r.arm:
			resetFetchIdleTimer(timer, r.timeout)
			deadline = timer.C
			select {
			case r.armed <- struct{}{}:
			case <-r.stop:
				return
			}
		case <-r.disarm:
			if deadline != nil {
				stopFetchIdleTimer(timer)
				deadline = nil
			}
			select {
			case r.disarmed <- struct{}{}:
			case <-r.stop:
				return
			}
		case <-deadline:
			deadline = nil
			// A Read returning at the deadline boundary wins if its disarm is
			// already waiting to be consumed.
			select {
			case <-r.disarm:
				select {
				case r.disarmed <- struct{}{}:
				case <-r.stop:
				}
				continue
			default:
			}
			if !r.expire() {
				return
			}
			// Unblock the Read-side arm/disarm handshake before closing the
			// underlying body. Close may itself wait for the blocked Read.
			r.stopWatchdog()
			if r.onTimeout != nil {
				r.onTimeout()
			}
			_ = r.closeBody()
			return
		}
	}
}

func resetFetchIdleTimer(timer *time.Timer, timeout time.Duration) {
	stopFetchIdleTimer(timer)
	timer.Reset(timeout)
}

func stopFetchIdleTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (r *fetchIdleReadCloser) expire() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.state != fetchIdleOpen {
		return false
	}
	r.state = fetchIdleExpired
	return true
}

func (r *fetchIdleReadCloser) timedOut() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.state == fetchIdleExpired
}

func (r *fetchIdleReadCloser) finish(readErr error) {
	r.stateMu.Lock()
	if r.state == fetchIdleOpen {
		r.state = fetchIdleFinished
		r.readErr = readErr
	}
	r.stateMu.Unlock()
	r.stopWatchdog()
}

func (r *fetchIdleReadCloser) terminalReadError() (error, bool) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	switch r.state {
	case fetchIdleOpen:
		return nil, false
	case fetchIdleExpired:
		return ErrFetchIdleTimeout, true
	case fetchIdleClosed:
		return io.ErrClosedPipe, true
	case fetchIdleFinished:
		if r.readErr == nil {
			return io.EOF, true
		}
		return r.readErr, true
	default:
		return io.ErrClosedPipe, true
	}
}

func (r *fetchIdleReadCloser) stopWatchdog() {
	r.stopOnce.Do(func() {
		close(r.stop)
	})
}

func (r *fetchIdleReadCloser) closeBody() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.body.Close()
	})
	return r.closeErr
}

func (r *fetchIdleReadCloser) Read(p []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	if terminalErr, terminal := r.terminalReadError(); terminal {
		return 0, terminalErr
	}
	if !r.armReadDeadline() {
		if terminalErr, terminal := r.terminalReadError(); terminal {
			return 0, terminalErr
		}
		return 0, io.ErrClosedPipe
	}
	n, err := r.body.Read(p)
	r.disarmReadDeadline()
	if r.timedOut() {
		err = ErrFetchIdleTimeout
	}
	if err != nil {
		r.finish(err)
	}
	return n, err
}

func (r *fetchIdleReadCloser) armReadDeadline() bool {
	r.stateMu.Lock()
	open := r.state == fetchIdleOpen
	r.stateMu.Unlock()
	if !open {
		return false
	}
	select {
	case r.arm <- struct{}{}:
	case <-r.stop:
		return false
	}
	select {
	case <-r.armed:
		return true
	case <-r.stop:
		return false
	}
}

func (r *fetchIdleReadCloser) disarmReadDeadline() {
	select {
	case r.disarm <- struct{}{}:
	case <-r.stop:
		return
	}
	select {
	case <-r.disarmed:
	case <-r.stop:
	}
}

func (r *fetchIdleReadCloser) Close() error {
	r.stateMu.Lock()
	if r.state != fetchIdleExpired {
		r.state = fetchIdleClosed
	}
	r.stateMu.Unlock()
	r.stopWatchdog()
	err := r.closeBody()
	<-r.done
	return err
}

func fetchBodyContextError(ctx context.Context, body io.ReadCloser) error {
	if wrapped, ok := body.(*fetchIdleReadCloser); ok && wrapped.timedOut() {
		return ErrFetchIdleTimeout
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}
