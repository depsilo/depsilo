package upstream

import (
	"context"
	"io"
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
	meter TrafficMeter
	attr  *traffic.Attribution
}

func (b *originBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		if b.meter != nil {
			b.meter.ObserveOriginBytes(int64(n))
		}
		b.attr.AddOriginBytes(int64(n))
	}
	return n, err
}

// wrapOriginBody records one upstream exchange plus the bytes read through it.
// It only wraps successful (<400) responses: error responses and health probes
// are not origin traffic. The per-request attribution is optional and lets the
// access-log row carry the same real totals for later range aggregation.
func wrapOriginBody(ctx context.Context, body io.ReadCloser) io.ReadCloser {
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
	attr.AddOriginExchange()
	return &originBody{ReadCloser: body, meter: meter, attr: attr}
}
