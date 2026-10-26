package upstream

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"depsilo/internal/traffic"
)

// TrafficMeter receives Depsilo→upstream exchange and byte observations.
// It is deliberately a package-level seam: upstreams are constructed by the
// pool, the registry, and tests, and a process-wide origin meter should not
// change every constructor signature.
type TrafficMeter interface {
	ObserveOriginExchange()
	ObserveOriginBytes(n int64)
}

// routedTrafficMeter adds transport attribution without requiring legacy
// origin-only observers to change. Historical origin remains an aggregate of
// direct and cascade fetches, while these counters distinguish the two paths.
type routedTrafficMeter interface {
	ObserveUpstreamExchange(traffic.Route, bool)
	ObserveUpstreamBytes(traffic.Route, int64, bool)
}

var trafficMeter atomic.Pointer[TrafficMeter]

// SetTrafficMeter installs the process-wide origin meter. Passing nil disables
// origin metering (isolated tests, CLI paths without a server).
func SetTrafficMeter(meter TrafficMeter) {
	if meter == nil {
		trafficMeter.Store(nil)
		return
	}
	trafficMeter.Store(&meter)
}

func currentTrafficMeter() TrafficMeter {
	if pointer := trafficMeter.Load(); pointer != nil {
		return *pointer
	}
	return nil
}

// originBody counts bytes actually read from an upstream response body. The
// count is a real read count, not the declared Content-Length, so a truncated
// or abandoned transfer is not reported as a full download.
type originBody struct {
	io.ReadCloser
	meter    TrafficMeter
	routed   routedTrafficMeter
	route    traffic.Route
	redirect bool
	attr     *traffic.Attribution
}

func (b *originBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		if b.meter != nil {
			b.meter.ObserveOriginBytes(int64(n))
		}
		if b.routed != nil {
			b.routed.ObserveUpstreamBytes(b.route, int64(n), b.redirect)
		}
		b.attr.AddOriginBytes(int64(n))
	}
	return n, err
}

// wrapOriginBody records one upstream exchange plus the bytes read through it.
// It only wraps successful (<400) responses: error responses and health probes
// are not origin traffic. The per-request attribution is optional and lets the
// access-log row carry the same real totals for later range aggregation.
func wrapOriginBody(ctx context.Context, body io.ReadCloser, route traffic.Route, redirect bool) io.ReadCloser {
	if body == nil {
		return body
	}
	meter := currentTrafficMeter()
	attr := traffic.AttributionFromContext(ctx)
	if meter == nil && attr == nil {
		return body
	}
	if meter != nil {
		meter.ObserveOriginExchange()
	}
	routed, _ := meter.(routedTrafficMeter)
	if routed != nil {
		routed.ObserveUpstreamExchange(route, redirect)
	}
	attr.AddOriginExchange()
	return &originBody{ReadCloser: body, meter: meter, routed: routed, route: route, redirect: redirect, attr: attr}
}

// MeterResponse instruments an eligible GET response at the upstream read
// seam. Relay uses the same seam as ordinary fetches because it calls a
// resolved HTTP client directly. HEAD probes and HTTP errors remain excluded.
// A peer is identified by its configured transport, never by response headers
// or a hostname guess. The caller retains ownership of the response body.
func MeterResponse(client *http.Client, request *http.Request, response *http.Response) {
	if request == nil || request.Method != http.MethodGet || response == nil || response.StatusCode >= 400 {
		return
	}
	route := traffic.RouteDirect
	if client != nil {
		if _, peer := client.Transport.(*peerTransport); peer {
			route = traffic.RoutePeer
		}
	}
	redirect := response.StatusCode >= 300 && response.StatusCode != http.StatusNotModified
	response.Body = wrapOriginBody(request.Context(), response.Body, route, redirect)
}
