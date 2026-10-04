// Package traffic owns Depsilo's bounded, in-process request and byte
// metering. It exists so Overview can show two genuinely different request
// paths — clients to Depsilo, and Depsilo to upstreams — without inferring one
// from the other.
//
// The meter keeps a fixed per-second ring buffer. It is intentionally small
// (no unbounded event table, no external time-series store) and
// allocation-free on the hot path: observers only bump counters.
//
// Attribution is per request. Adapters attach an Attribution to the request
// context; the upstream read path accumulates into it, and the access-log
// hook reads the totals back out so a historical range can report real origin
// bytes instead of a client-side proxy for them.
package traffic

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// WindowSeconds is how many trailing seconds the ring buffer retains. It bounds
// memory and lets status endpoints compute rolling rates without DB scans.
const WindowSeconds = 300

type slot struct {
	second          int64
	serviceRequests int64
	serviceBytes    int64
	originRequests  int64
	originBytes     int64
}

// Meter is a process-owned rolling counter. It is safe for concurrent use and
// never blocks on I/O.
type Meter struct {
	mu        sync.Mutex
	now       func() time.Time
	startedAt time.Time
	slots     [WindowSeconds]slot
}

// NewMeter returns a meter anchored at the current instant.
func NewMeter() *Meter {
	now := time.Now().UTC()
	return &Meter{now: time.Now, startedAt: now}
}

// SetClock overrides the time source. Test-only helper.
func (m *Meter) SetClock(fn func() time.Time) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.now = fn
	m.mu.Unlock()
}

// StartedAt reports when the meter (that is, the process) began counting.
func (m *Meter) StartedAt() time.Time {
	if m == nil {
		return time.Time{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startedAt
}

func (m *Meter) slotLocked(t time.Time) *slot {
	sec := t.Unix()
	idx := int(((sec % WindowSeconds) + WindowSeconds) % WindowSeconds)
	s := &m.slots[idx]
	if s.second != sec {
		*s = slot{second: sec}
	}
	return s
}

// ObserveServiceRequest records one completed client→Depsilo request and the
// bytes actually delivered to that client.
func (m *Meter) ObserveServiceRequest(bytesSent int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	s := m.slotLocked(m.now())
	s.serviceRequests++
	if bytesSent > 0 {
		s.serviceBytes += bytesSent
	}
	m.mu.Unlock()
}

// ObserveOriginExchange records one Depsilo→upstream HTTP exchange. Health
// probes and admin-triggered upstream checks do not pass through this seam.
func (m *Meter) ObserveOriginExchange() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.slotLocked(m.now()).originRequests++
	m.mu.Unlock()
}

// ObserveOriginBytes records bytes actually read from an upstream response
// body. Callers report increments as the body streams; the totals are real
// reads, not declared Content-Length.
func (m *Meter) ObserveOriginBytes(n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.mu.Lock()
	m.slotLocked(m.now()).originBytes += n
	m.mu.Unlock()
}

// Rate is the aggregate over one trailing window. Counts are absolute totals
// inside the window; the per-second rates are derived by callers so a window
// that is not fully covered by process uptime is not overstated.
type Rate struct {
	WindowSeconds   int   `json:"window_seconds"`
	ServiceRequests int64 `json:"service_requests"`
	ServiceBytes    int64 `json:"service_bytes"`
	OriginRequests  int64 `json:"origin_requests"`
	OriginBytes     int64 `json:"origin_bytes"`
}

func (r Rate) ServiceRequestsPerSec() float64 {
	if r.WindowSeconds <= 0 {
		return 0
	}
	return float64(r.ServiceRequests) / float64(r.WindowSeconds)
}

func (r Rate) OriginRequestsPerSec() float64 {
	if r.WindowSeconds <= 0 {
		return 0
	}
	return float64(r.OriginRequests) / float64(r.WindowSeconds)
}

func (r Rate) ServiceBytesPerSec() float64 {
	if r.WindowSeconds <= 0 {
		return 0
	}
	return float64(r.ServiceBytes) / float64(r.WindowSeconds)
}

func (r Rate) OriginBytesPerSec() float64 {
	if r.WindowSeconds <= 0 {
		return 0
	}
	return float64(r.OriginBytes) / float64(r.WindowSeconds)
}

// Snapshot sums the trailing window (clamped to the ring size). A window
// larger than the retention returns the retained span and reports it, so a
// caller never presents a partial window as a complete one.
func (m *Meter) Snapshot(windowSeconds int, now time.Time) Rate {
	if m == nil {
		return Rate{}
	}
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	if windowSeconds > WindowSeconds {
		windowSeconds = WindowSeconds
	}
	now = now.UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	latest := now.Unix()
	out := Rate{WindowSeconds: windowSeconds}
	for i := int64(0); i < int64(windowSeconds); i++ {
		sec := latest - i
		idx := int(((sec % WindowSeconds) + WindowSeconds) % WindowSeconds)
		s := m.slots[idx]
		if s.second != sec {
			continue
		}
		out.ServiceRequests += s.serviceRequests
		out.ServiceBytes += s.serviceBytes
		out.OriginRequests += s.originRequests
		out.OriginBytes += s.originBytes
	}
	return out
}

// Attribution is the per-request accumulator. The upstream read path adds
// exchanges and bytes; the access-log hook snapshots it once.
type Attribution struct {
	originRequests atomic.Int64
	originBytes    atomic.Int64
}

// NewAttribution returns an empty request accumulator.
func NewAttribution() *Attribution { return &Attribution{} }

func (a *Attribution) AddOriginExchange() {
	if a != nil {
		a.originRequests.Add(1)
	}
}

func (a *Attribution) AddOriginBytes(n int64) {
	if a != nil && n > 0 {
		a.originBytes.Add(n)
	}
}

// OriginTotals returns the accumulated origin exchanges and bytes.
func (a *Attribution) OriginTotals() (requests int64, bytes int64) {
	if a == nil {
		return 0, 0
	}
	return a.originRequests.Load(), a.originBytes.Load()
}

type attributionKey struct{}

// WithAttribution attaches the accumulator to a request context. It is a no-op
// when the request already carries one so background refreshes and retries
// cannot silently reparent.
func WithAttribution(ctx context.Context, a *Attribution) context.Context {
	if ctx == nil || a == nil {
		return ctx
	}
	if existing, ok := ctx.Value(attributionKey{}).(*Attribution); ok && existing != nil {
		return ctx
	}
	return context.WithValue(ctx, attributionKey{}, a)
}

// AttributionFromContext returns the request accumulator, if any.
func AttributionFromContext(ctx context.Context) *Attribution {
	if ctx == nil {
		return nil
	}
	a, _ := ctx.Value(attributionKey{}).(*Attribution)
	return a
}
