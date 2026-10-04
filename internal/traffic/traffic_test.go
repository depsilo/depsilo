package traffic

import (
	"context"
	"testing"
	"time"
)

func TestMeterSeparatesServiceAndOriginPaths(t *testing.T) {
	clock := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	meter := NewMeter()
	meter.SetClock(func() time.Time { return clock })

	meter.ObserveServiceRequest(1024)
	meter.ObserveServiceRequest(0)
	meter.ObserveOriginExchange()
	meter.ObserveOriginBytes(512)
	meter.ObserveOriginBytes(512)
	// Health-probe style bytes are never reported through this meter.

	rate := meter.Snapshot(60, clock)
	if rate.ServiceRequests != 2 {
		t.Fatalf("service requests = %d, want 2", rate.ServiceRequests)
	}
	if rate.ServiceBytes != 1024 {
		t.Fatalf("service bytes = %d, want 1024", rate.ServiceBytes)
	}
	if rate.OriginRequests != 1 {
		t.Fatalf("origin requests = %d, want 1", rate.OriginRequests)
	}
	if rate.OriginBytes != 1024 {
		t.Fatalf("origin bytes = %d, want 1024", rate.OriginBytes)
	}
	if got, want := rate.ServiceRequestsPerSec(), 2.0/60.0; got != want {
		t.Fatalf("service req/s = %v, want %v", got, want)
	}
}

func TestMeterWindowDropsOldSeconds(t *testing.T) {
	start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	now := start
	meter := NewMeter()
	meter.SetClock(func() time.Time { return now })

	meter.ObserveServiceRequest(10)
	now = start.Add(90 * time.Second)
	meter.ObserveServiceRequest(20)

	window := meter.Snapshot(60, now)
	if window.ServiceRequests != 1 || window.ServiceBytes != 20 {
		t.Fatalf("window = %+v, want only the second request inside the 60s window", window)
	}
}

func TestAttributionAccumulatesPerRequest(t *testing.T) {
	attr := NewAttribution()
	ctx := WithAttribution(context.Background(), attr)
	attr.AddOriginExchange()
	attr.AddOriginBytes(2048)

	requests, bytes := AttributionFromContext(ctx).OriginTotals()
	if requests != 1 || bytes != 2048 {
		t.Fatalf("origin totals = (%d, %d), want (1, 2048)", requests, bytes)
	}

	// A second attachment must not replace the accumulator mid-request.
	other := NewAttribution()
	ctx = WithAttribution(ctx, other)
	if AttributionFromContext(ctx) != attr {
		t.Fatal("WithAttribution replaced an existing accumulator")
	}
}

func TestSnapshotsClampWindowToRetention(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	meter := NewMeter()
	meter.SetClock(func() time.Time { return now })
	rate := meter.Snapshot(10_000, now)
	if rate.WindowSeconds != WindowSeconds {
		t.Fatalf("window seconds = %d, want %d", rate.WindowSeconds, WindowSeconds)
	}
}
