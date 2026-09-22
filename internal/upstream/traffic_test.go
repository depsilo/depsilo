package upstream

import (
	"testing"
	"time"
)

func TestTrafficRecentWindow(t *testing.T) {
	now := time.Unix(100, 0)
	var counter trafficCounter
	counter.add(now.Add(-59*time.Second), 1, 12)
	counter.add(now.Add(-61*time.Second), 1, 99)
	got := Traffic{}
	counter.mu.Lock()
	for _, slot := range counter.slots {
		if slot.second >= now.Unix()-60 && slot.second < now.Unix() {
			got.Requests += slot.Requests
			got.Bytes += slot.Bytes
		}
	}
	counter.mu.Unlock()
	if got.Requests != 1 || got.Bytes != 12 {
		t.Fatalf("recent traffic = %+v, want one request and 12 bytes", got)
	}
}

func TestTrafficWindowReportsCollectorCoverage(t *testing.T) {
	now := time.Unix(200, 0)
	counter := trafficCounter{startedAt: now.Add(-10 * time.Second)}
	got := counter.window(now)
	if got.CoverageSeconds != 10 || got.Requests != 0 || got.Bytes != 0 {
		t.Fatalf("sampling window = %+v, want 10s zero traffic", got)
	}
	counter.add(now.Add(-time.Second), 1, 7)
	got = counter.window(now)
	if got.CoverageSeconds != 10 || got.Requests != 1 || got.Bytes != 7 {
		t.Fatalf("measured window = %+v, want 10s and one request", got)
	}
}
