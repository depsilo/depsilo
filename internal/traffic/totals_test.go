package traffic

import (
	"sync"
	"testing"
	"time"
)

func TestTotalsRetainRoutesAfterRollingWindowExpires(t *testing.T) {
	meter := NewMeter()
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	meter.SetClock(func() time.Time { return now })
	meter.ObserveServiceRequest(100)
	meter.ObserveOriginExchange()
	meter.ObserveOriginBytes(70)
	meter.ObserveUpstreamExchange(RouteDirect, false)
	meter.ObserveUpstreamBytes(RouteDirect, 50, false)
	meter.ObserveUpstreamExchange(RoutePeer, true)
	meter.ObserveUpstreamBytes(RoutePeer, 20, true)
	meter.ObserveUpstreamBytes(RouteDirect, -10, false)
	meter.ObserveUpstreamExchange(Route("unknown"), false)
	meter.ObserveUpstreamBytes(Route("unknown"), 99, false)

	want := Totals{
		ServiceRequests: 1, ServiceBytes: 100,
		Direct: UpstreamTotals{Exchanges: 1, Bytes: 50},
		Peer:   UpstreamTotals{Exchanges: 1, Bytes: 20, RedirectExchanges: 1, RedirectBytes: 20},
	}
	if got := meter.Totals(); got != want {
		t.Fatalf("totals = %+v, want %+v", got, want)
	}
	if got := meter.Snapshot(60, now); got.OriginBytes != 70 || got.OriginRequests != 1 {
		t.Fatalf("new transport observations changed legacy origin aggregate: %+v", got)
	}
	now = now.Add(2 * WindowSeconds * time.Second)
	if rate := meter.Snapshot(60, now); rate.OriginBytes != 0 || rate.ServiceBytes != 0 {
		t.Fatalf("old rolling observations retained: %+v", rate)
	}
	if got := meter.Totals(); got != want {
		t.Fatalf("process totals changed after rate expiry: %+v", got)
	}
}

func TestTotalsConcurrentObserversAndSnapshots(t *testing.T) {
	meter := NewMeter()
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for index := range 100 {
				meter.ObserveServiceRequest(10)
				meter.ObserveUpstreamExchange(RouteDirect, index%2 == 0)
				meter.ObserveUpstreamBytes(RouteDirect, 3, index%2 == 0)
				meter.ObserveUpstreamExchange(RoutePeer, false)
				meter.ObserveUpstreamBytes(RoutePeer, 2, false)
				if got := meter.Totals(); got.Direct.RedirectExchanges > got.Direct.Exchanges || got.Direct.RedirectBytes > got.Direct.Bytes {
					t.Errorf("redirect subset exceeds totals: %+v", got)
				}
			}
		})
	}
	workers.Wait()
	want := Totals{
		ServiceRequests: 1600, ServiceBytes: 16000,
		Direct: UpstreamTotals{Exchanges: 1600, Bytes: 4800, RedirectExchanges: 800, RedirectBytes: 2400},
		Peer:   UpstreamTotals{Exchanges: 1600, Bytes: 3200},
	}
	if got := meter.Totals(); got != want {
		t.Fatalf("totals = %+v, want %+v", got, want)
	}
}
