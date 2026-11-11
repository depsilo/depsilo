package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

// seasoned models a long-running upstream whose cumulative success rate no
// longer reacts to a handful of failures.
func seasoned(u *Upstream) *Upstream {
	for range 50 {
		u.Report(100*time.Millisecond, true)
	}
	return u
}

func TestConsecutiveSlowFailuresTakeAnUpstreamOutOfSelection(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, AdapterType: "pypi", Name: "active", URL: "https://active.example", Priority: 1, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
		{ID: 2, AdapterType: "pypi", Name: "passive", URL: "https://passive.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range pool.Snapshot() {
		seasoned(u)
		// Failures that return at once never trip the breaker: a stale cache
		// already answers them without waiting.
		for range 5 {
			u.Report(time.Second, false)
		}
		// A success between slow failures resets the count.
		u.Report(slowFailureLatency, false)
		u.Report(100*time.Millisecond, true)
		u.Report(slowFailureLatency, false)
		if !u.IsHealthy() {
			t.Fatalf("%s left selection before consecutive slow failures", u.Name)
		}
		u.Report(slowFailureLatency, false)
		if u.IsHealthy() {
			t.Fatalf("%s stayed in selection after consecutive slow failures", u.Name)
		}
		select {
		case <-u.recheck:
		default:
			t.Fatalf("%s: tripping the breaker did not ask for a recheck", u.Name)
		}
	}

	active := orderedUpstreams(pool)[0]
	active.applyProbe(ProbeResult{Healthy: true, CheckedAt: time.Now()})
	active.Report(slowFailureLatency, false)
	if !active.IsHealthy() {
		t.Fatal("a passing probe did not readmit the upstream and reset its slow failures")
	}
}

func TestTrippedPassiveUpstreamGetsHalfOpenRequestsWhileAnotherMemberServes(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, AdapterType: "pypi", Name: "mirror", URL: "https://mirror.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
		{ID: 2, AdapterType: "pypi", Name: "official", URL: "https://official.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	ordered := orderedUpstreams(pool)
	mirror, official := seasoned(ordered[0]), seasoned(ordered[1])
	now := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	selector := newPassiveRecoverySelector(pool, time.Minute, func() time.Time { return now })
	selected := func() string {
		t.Helper()
		u, err := selector.Select(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return u.Name
	}

	mirror.Report(slowFailureLatency, false)
	mirror.Report(slowFailureLatency, false)
	// One half-open request per cooldown; everything else uses the member that answers.
	if got := []string{selected(), selected()}; got[0] != "mirror" || got[1] != "official" {
		t.Fatalf("selections after the trip = %v, want one half-open request then official", got)
	}
	mirror.Report(slowFailureLatency, false)
	now = now.Add(30 * time.Second)
	if got := selected(); got != "official" {
		t.Fatalf("selected %s before the cooldown elapsed", got)
	}
	now = now.Add(31 * time.Second)
	if got := selected(); got != "mirror" {
		t.Fatalf("selected %s; the cooldown elapsed, want another half-open request", got)
	}
	mirror.Report(100*time.Millisecond, true)
	if got := []string{selected(), selected()}; got[0] != "mirror" || got[1] != "mirror" {
		t.Fatalf("selections after a successful half-open request = %v, want the mirror back", got)
	}
	_ = official

	// An active upstream waits for its probe instead.
	activePool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, AdapterType: "pypi", Name: "mirror", URL: "https://mirror.example", Priority: 1, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
		{ID: 2, AdapterType: "pypi", Name: "official", URL: "https://official.example", Priority: 2, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	activeMirror := seasoned(orderedUpstreams(activePool)[0])
	seasoned(orderedUpstreams(activePool)[1])
	activeMirror.Report(slowFailureLatency, false)
	activeMirror.Report(slowFailureLatency, false)
	if u, err := newPassiveRecoverySelector(activePool, time.Minute, func() time.Time { return now }).Select(context.Background()); err != nil || u.Name != "official" {
		t.Fatalf("active pool selected %v, %v; want official until a probe readmits the mirror", u, err)
	}
}

func TestTrippedUpstreamRejoinsOnTheRecheckProbeNotTheInterval(t *testing.T) {
	var down atomic.Bool
	var probes atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer origin.Close()
	u := seasoned(newTestUpstream(t, origin.URL))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		runUpstreamHealthCheckWithRecheck(ctx, u, nil, time.Hour, 10*time.Millisecond, 40*time.Millisecond)
	}()
	waitFor(t, "initial probe", func() bool { return probes.Load() >= 1 })

	down.Store(true)
	u.Report(slowFailureLatency, false)
	u.Report(slowFailureLatency, false)
	if u.IsHealthy() {
		t.Fatal("consecutive slow failures did not trip the breaker")
	}
	waitFor(t, "rechecks while the link is down", func() bool { return probes.Load() >= 3 })
	if u.IsHealthy() {
		t.Fatal("a failing recheck readmitted the upstream")
	}

	down.Store(false)
	waitFor(t, "readmission after the link recovers", u.IsHealthy)
	cancel()
	<-stopped
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
