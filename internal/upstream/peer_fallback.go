package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/cascade"
)

// peerRetryAfter is how long an upstream bound to a failing parent goes
// straight to its origin before it tries the parent again. It keeps a parent
// that drops packets from stalling every request for a full dial timeout.
const peerRetryAfter = 30 * time.Second

// PeerStatus tracks whether exchanges through one cascade parent currently
// work. One tracker is shared by every upstream bound to that parent.
type PeerStatus struct {
	mu              sync.Mutex
	lastSuccessAt   time.Time
	lastFallbackAt  time.Time
	lastFailure     string
	skipUntil       time.Time
	fallbackCount   uint64
	now             func() time.Time
	lastLoggedAt    time.Time
	lastLoggedCause string
}

// PeerStatusSnapshot is one consistent sample of a PeerStatus.
type PeerStatusSnapshot struct {
	LastSuccessAt  time.Time
	LastFallbackAt time.Time
	// LastFailure says why the newest fallback happened; empty once an
	// exchange through the parent succeeds again.
	LastFailure   string
	FallbackCount uint64
}

func newPeerStatus() *PeerStatus { return &PeerStatus{now: time.Now} }

// Snapshot returns the tracker's current state.
func (s *PeerStatus) Snapshot() PeerStatusSnapshot {
	if s == nil {
		return PeerStatusSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return PeerStatusSnapshot{
		LastSuccessAt:  s.lastSuccessAt,
		LastFallbackAt: s.lastFallbackAt,
		LastFailure:    s.lastFailure,
		FallbackCount:  s.fallbackCount,
	}
}

func (s *PeerStatus) skipping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Before(s.skipUntil)
}

func (s *PeerStatus) succeeded() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSuccessAt = s.now()
	s.lastFailure = ""
	s.skipUntil = time.Time{}
}

// failed records a parent failure and reports whether it should be logged:
// once per cause and minute, so a down parent does not flood the log.
func (s *PeerStatus) failed(cause string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.lastFallbackAt = now
	s.lastFailure = cause
	s.skipUntil = now.Add(peerRetryAfter)
	s.fallbackCount++
	if cause == s.lastLoggedCause && now.Sub(s.lastLoggedAt) < time.Minute {
		return false
	}
	s.lastLoggedAt, s.lastLoggedCause = now, cause
	return true
}

func (s *PeerStatus) fellBack() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFallbackAt = s.now()
	s.fallbackCount++
}

// relayFailureCodes are the relay's own errors after which the origin itself
// may still answer: the parent refused or could not serve the exchange.
// UPSTREAM_ERROR is absent because it carries the origin's answer.
var relayFailureCodes = map[string]bool{
	"CASCADE_UNAUTHORIZED":       true,
	"INVALID_RELAY_REQUEST":      true,
	"CASCADE_LOOP":               true,
	"CASCADE_HOP_LIMIT":          true,
	"RELAY_UPSTREAM_UNAVAILABLE": true,
	"METHOD_NOT_ALLOWED":         true,
}

// relayUnavailable explains why response did not come from a working relay,
// or returns "" when it did.
func relayUnavailable(response *http.Response) string {
	if response.Header.Get(cascade.HeaderInstance) == "" {
		return fmt.Sprintf("parent answered %d without the cascade relay; is cascade enabled on it?", response.StatusCode)
	}
	if code := response.Header.Get(cascade.HeaderRelayError); relayFailureCodes[code] {
		return "parent relay answered " + code
	}
	return ""
}

type directRouteKey struct{}

// routedDirect reports whether response came through the direct fallback of
// a parent-bound upstream.
func routedDirect(response *http.Response) bool {
	return response != nil && response.Request != nil &&
		response.Request.Context().Value(directRouteKey{}) != nil
}

// peerFallbackTransport sends an exchange through the cascade parent and,
// when the parent cannot serve it, to the origin directly. Only failures of
// the parent itself trigger the fallback: an origin answer relayed by the
// parent, an error status included, is returned as it is.
type peerFallbackTransport struct {
	peer   *peerTransport
	direct http.RoundTripper
	status *PeerStatus
}

func (t *peerFallbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.status.skipping() {
		t.status.fellBack()
		return t.roundTripDirect(request)
	}
	response, err := t.peer.RoundTrip(request)
	cause := ""
	switch {
	case err != nil:
		if request.Context().Err() != nil {
			return nil, err
		}
		cause = err.Error()
	default:
		cause = relayUnavailable(response)
		if cause == "" {
			t.status.succeeded()
			return response, nil
		}
		_, _ = io.CopyN(io.Discard, response.Body, 64<<10)
		_ = response.Body.Close()
	}
	if t.status.failed(cause) {
		zap.L().Warn("cascade parent unavailable; fetching from the origin directly",
			zap.String("peer", t.peer.peer.Name),
			zap.String("cause", cause),
		)
	}
	return t.roundTripDirect(request)
}

func (t *peerFallbackTransport) roundTripDirect(request *http.Request) (*http.Response, error) {
	direct := request.Clone(context.WithValue(request.Context(), directRouteKey{}, true))
	return t.direct.RoundTrip(direct)
}

// CloseIdleConnections lets http.Client.CloseIdleConnections reach both legs.
func (t *peerFallbackTransport) CloseIdleConnections() {
	for _, transport := range []http.RoundTripper{t.peer.base, t.direct} {
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}
