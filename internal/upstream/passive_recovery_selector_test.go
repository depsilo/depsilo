package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

type selectorTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *selectorTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *selectorTestClock) Advance(elapsed time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(elapsed)
	c.mu.Unlock()
}

func TestPassiveRecoverySelectorAllowsUnhealthyPassiveUpstream(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: "https://download.pytorch.org/whl/cu128", Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}

	selected, err := NewPassiveRecoverySelector(pool).Select(context.Background())
	if err != nil {
		t.Fatalf("passive recovery selection failed: %v", err)
	}
	if selected.Name != "pytorch" {
		t.Fatalf("selected %q, want pytorch", selected.Name)
	}
}

func TestPassiveRecoverySelectorPrefersHealthyUpstream(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{
			ID: 1, AdapterType: "extra:pytorch-cu128", Name: "unhealthy-primary",
			URL: "https://primary.example", Priority: 1, ProbeMode: "passive",
			ProbeInterval: "30m", Healthy: false,
		},
		{
			ID: 2, AdapterType: "extra:pytorch-cu128", Name: "healthy-secondary",
			URL: "https://secondary.example", Priority: 2, ProbeMode: "passive",
			ProbeInterval: "30m", Healthy: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	selected, err := NewPassiveRecoverySelector(pool).Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != "healthy-secondary" {
		t.Fatalf("selected %q, want healthy-secondary", selected.Name)
	}
}

func TestPassiveRecoverySelectorDoesNotRecoverActiveUpstream(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "active",
		URL: "https://active.example", Priority: 1,
		ProbeMode: "active", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewPassiveRecoverySelector(pool).Select(context.Background()); err == nil {
		t.Fatal("unhealthy active upstream entered passive recovery")
	}
}

func TestPrioritySelectorDoesNotGainPassiveRecovery(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: "https://download.pytorch.org/whl/cu128", Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewPrioritySelector(pool).Select(context.Background()); err == nil {
		t.Fatal("legacy priority selector unexpectedly gained passive recovery")
	}
}

func TestPassiveRecoverySelectorAllowsOneAttemptPerCooldown(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: "https://download.pytorch.org/whl/cu128", Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	clock := &selectorTestClock{now: time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)}
	selector := newPassiveRecoverySelector(pool, time.Minute, clock.Now)

	if _, err := selector.Select(context.Background()); err != nil {
		t.Fatalf("first half-open selection failed: %v", err)
	}
	if _, err := selector.Select(context.Background()); err == nil {
		t.Fatal("second selection penetrated the same cooldown")
	}

	clock.Advance(time.Minute)
	if _, err := selector.Select(context.Background()); err != nil {
		t.Fatalf("selection after cooldown failed: %v", err)
	}
}

func TestPassiveRecoverySelectorAdmitsOneConcurrentHalfOpenSelection(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: "https://download.pytorch.org/whl/cu128", Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	clock := &selectorTestClock{now: time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)}
	selector := newPassiveRecoverySelector(pool, time.Minute, clock.Now)

	const callers = 32
	start := make(chan struct{})
	results := make(chan error, callers)
	var callersDone sync.WaitGroup
	callersDone.Add(callers)
	for range callers {
		go func() {
			defer callersDone.Done()
			<-start
			_, selectErr := selector.Select(context.Background())
			results <- selectErr
		}()
	}
	close(start)
	callersDone.Wait()
	close(results)

	admitted := 0
	for selectErr := range results {
		if selectErr == nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted %d concurrent half-open selections, want 1", admitted)
	}
}

func TestPassiveRecoverySelectorNeverRetriesCriticalFailure(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: "https://download.pytorch.org/whl/cu128", Priority: 1,
		ProbeMode: "passive", ProbeInterval: "30m", Healthy: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	pool.Snapshot()[0].ReportCriticalFailure(time.Millisecond)
	clock := &selectorTestClock{now: time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)}
	selector := newPassiveRecoverySelector(pool, time.Minute, clock.Now)

	if _, err := selector.Select(context.Background()); err == nil {
		t.Fatal("critical failure entered half-open recovery")
	}
	clock.Advance(24 * time.Hour)
	if _, err := selector.Select(context.Background()); err == nil {
		t.Fatal("critical failure entered recovery after cooldown")
	}
}

func TestPassiveRecoverySelectorKeepsHalfOpenRequestExclusivePastCooldown(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		releaseRequest()
		server.Close()
	}()

	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: server.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m",
		Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	clock := &selectorTestClock{now: time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)}
	selector := newPassiveRecoverySelector(pool, time.Minute, clock.Now)
	selected, err := selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	fetched := make(chan error, 1)
	go func() {
		result, fetchErr := selected.Fetch(context.Background(), "/simple/")
		if result != nil {
			_ = result.Body.Close()
		}
		fetched <- fetchErr
	}()
	<-started

	clock.Advance(2 * time.Minute)
	if _, err := selector.Select(context.Background()); err == nil {
		t.Fatal("second request entered while the first half-open request was still in flight")
	}

	releaseRequest()
	if err := <-fetched; err != nil {
		t.Fatalf("half-open fetch failed: %v", err)
	}
	if _, err := selector.Select(context.Background()); err != nil {
		t.Fatalf("successful recovery did not restore normal selection: %v", err)
	}
}
