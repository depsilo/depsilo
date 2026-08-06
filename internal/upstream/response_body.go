package upstream

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Error responses are never cache payloads. Reading an unbounded 4xx body
	// before returning would let a slow or infinite origin pin one goroutine per
	// miss, while a small drain still preserves connection reuse for ordinary
	// registry errors.
	maxErrorResponseDrainBytes = 64 << 10
	errorResponseDrainTimeout  = 2 * time.Second
)

// terminalResponseBody delays exchange accounting until the streamed response
// reaches a trustworthy terminal state. It never buffers response bytes.
type terminalResponseBody struct {
	body           io.ReadCloser
	expectedLength int64
	bytesRead      atomic.Int64
	finishOnce     sync.Once
	finish         func(bool)
}

func (b *terminalResponseBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	read := b.bytesRead.Add(int64(n))

	if b.expectedLength >= 0 && read > b.expectedLength {
		b.finishTerminal(false)
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return n, err
	}

	switch {
	case err == nil:
		return n, nil
	case err != io.EOF:
		b.finishTerminal(false)
		return n, err
	}

	complete := b.expectedLength < 0 || read == b.expectedLength
	b.finishTerminal(complete)
	if !complete {
		return n, io.ErrUnexpectedEOF
	}
	return n, io.EOF
}

func (b *terminalResponseBody) Close() error {
	// Linearize the caller's Close before the transport unblocks any concurrent
	// Read with EOF. Reaching EOF first still wins; Close first is incomplete.
	b.finishTerminal(false)
	return b.body.Close()
}

func (b *terminalResponseBody) finishTerminal(complete bool) {
	b.finishOnce.Do(func() {
		b.finish(complete)
	})
}

func (u *Upstream) observeResponseBody(
	response *http.Response,
	latency time.Duration,
	reportHealth bool,
	recovery bool,
	statusSuccess bool,
) {
	if !reportHealth && !recovery {
		return
	}
	finish := func(bodyComplete bool) {
		if reportHealth {
			u.Report(latency, statusSuccess && bodyComplete)
		}
		if recovery {
			u.finishPassiveRecovery()
		}
	}

	if responseHasNoBody(response) {
		finish(true)
		return
	}
	response.Body = &terminalResponseBody{
		body:           response.Body,
		expectedLength: response.ContentLength,
		finish:         finish,
	}
}

func (u *Upstream) finishExchange(
	latency time.Duration,
	reportHealth bool,
	recovery bool,
	success bool,
) {
	if reportHealth {
		u.Report(latency, success)
	}
	if recovery {
		u.finishPassiveRecovery()
	}
}

// drainErrorResponse gives a small, bounded 4xx response a chance to reach EOF
// so health accounting can treat the origin as reachable. Both time and bytes
// are capped: oversized, stalled, or malformed bodies are closed and therefore
// recorded as incomplete by terminalResponseBody.
func drainErrorResponse(body io.ReadCloser) {
	if body == nil {
		return
	}
	drained := make(chan struct{})
	go func() {
		_, _ = io.CopyN(io.Discard, body, maxErrorResponseDrainBytes+1)
		close(drained)
	}()

	timer := time.NewTimer(errorResponseDrainTimeout)
	select {
	case <-drained:
		// Since Go 1.23, Stop guarantees a subsequent receive cannot observe a
		// stale timer value. Draining timer.C after a false result can instead
		// block forever when the select raced the deadline.
		timer.Stop()
	case <-timer.C:
	}
	_ = body.Close()
}

func responseHasNoBody(response *http.Response) bool {
	if response == nil || response.Body == nil || response.Body == http.NoBody {
		return true
	}
	if response.Request != nil && strings.EqualFold(response.Request.Method, http.MethodHead) {
		return true
	}
	status := response.StatusCode
	return status >= 100 && status <= 199 || status == http.StatusNoContent || status == http.StatusNotModified
}
