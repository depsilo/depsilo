package traffic

// Route describes the transport used for an upstream fetch. Direct includes
// private mirrors and configured HTTP proxies; it does not mean public WAN.
// Peer means the response arrived through the Depsilo cascade protocol.
type Route string

const (
	RouteDirect Route = "direct"
	RoutePeer   Route = "peer"
)

// UpstreamTotals counts eligible responses and body bytes read from them.
// Bytes exclude HTTP headers, TLS/TCP overhead, and unsuccessful responses.
type UpstreamTotals struct {
	Exchanges int64
	Bytes     int64
	// Redirect observations are a subset of the totals above. A 304 is a
	// validation response, not a redirect, and belongs only to the totals.
	RedirectExchanges int64
	RedirectBytes     int64
}

// Totals are process-lifetime counters, independent of the rolling rate
// window. They reset when the process meter is recreated, never on a scrape.
// Service totals cover native dependency-client responses, not relay traffic.
type Totals struct {
	ServiceRequests int64
	ServiceBytes    int64
	Direct          UpstreamTotals
	Peer            UpstreamTotals
}

// Totals returns a consistent copy of the process counters.
func (m *Meter) Totals() Totals {
	if m == nil {
		return Totals{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.totals
}

// ObserveUpstreamExchange records a successful GET response in its transport
// category. Callers exclude health checks and error statuses. This augments
// the existing origin meter; it does not reinterpret historical origin rows.
// redirect marks an observed 3xx response other than 304.
func (m *Meter) ObserveUpstreamExchange(route Route, redirect bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if totals := m.upstreamTotalsLocked(route); totals != nil {
		totals.Exchanges++
		if redirect {
			totals.RedirectExchanges++
		}
	}
}

// ObserveUpstreamBytes counts actual reads, including partial transfers. A
// declared Content-Length is never sufficient evidence of transferred bytes.
// redirect must match the response classification used for its exchange.
func (m *Meter) ObserveUpstreamBytes(route Route, n int64, redirect bool) {
	if m == nil || n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if totals := m.upstreamTotalsLocked(route); totals != nil {
		totals.Bytes += n
		if redirect {
			totals.RedirectBytes += n
		}
	}
}

func (m *Meter) upstreamTotalsLocked(route Route) *UpstreamTotals {
	switch route {
	case RouteDirect:
		return &m.totals.Direct
	case RoutePeer:
		return &m.totals.Peer
	default:
		return nil
	}
}
