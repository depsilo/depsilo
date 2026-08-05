package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestEgressSelectorUsesUnhealthyPassiveUpstreamWithoutRecoveryLimits(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "artifact")
	}))
	defer server.Close()

	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "extra:pytorch-cu128", Name: "pytorch",
		URL: server.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m",
		Healthy: false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	upstream := pool.Snapshot()[0]
	upstream.mu.Lock()
	upstream.recovery.inFlight = true
	upstream.recovery.nextAttempt = time.Now().Add(time.Hour)
	upstream.mu.Unlock()

	selector := NewEgressSelector(pool)
	for range 3 {
		selected, selectErr := selector.Select(context.Background())
		if selectErr != nil {
			t.Fatalf("select unhealthy passive upstream: %v", selectErr)
		}
		if selected != upstream {
			t.Fatalf("selected %p, want %p", selected, upstream)
		}
		result, fetchErr := selected.FetchURL(context.Background(), server.URL+"/artifact")
		if fetchErr != nil {
			t.Fatalf("FetchURL while passive recovery is in flight: %v", fetchErr)
		}
		body, readErr := io.ReadAll(result.Body)
		_ = result.Body.Close()
		if readErr != nil || string(body) != "artifact" {
			t.Fatalf("artifact body = %q, read error = %v", body, readErr)
		}
	}

	if got := requests.Load(); got != 3 {
		t.Fatalf("external requests = %d, want 3", got)
	}
	if upstream.IsHealthy() {
		t.Fatal("external artifact success changed metadata-origin health")
	}
	upstream.mu.RLock()
	totalRequests := upstream.health.totalReqs
	recoveryInFlight := upstream.recovery.inFlight
	upstream.mu.RUnlock()
	if totalRequests != 0 {
		t.Fatalf("metadata-origin health request count = %d, want 0", totalRequests)
	}
	if !recoveryInFlight {
		t.Fatal("external artifact fetch changed passive recovery state")
	}
}

func TestEgressSelectorUsesPriorityAndPreservesPoolOrderForTies(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, Name: "later-priority", URL: "https://later.example", Priority: 2, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
		{ID: 2, Name: "first-tie", URL: "https://first.example", Priority: 1, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
		{ID: 3, Name: "second-tie", URL: "https://second.example", Priority: 1, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	selected, err := NewEgressSelector(pool).Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != "first-tie" {
		t.Fatalf("selected %q, want first-tie", selected.Name)
	}
}

func TestEgressSelectorSkipsCriticalFailures(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, Name: "primary", URL: "https://primary.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
		{ID: 2, Name: "secondary", URL: "https://secondary.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m", Healthy: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	upstreams := pool.Snapshot()
	upstreams[0].ReportCriticalFailure(time.Millisecond)

	selector := NewEgressSelector(pool)
	selected, err := selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != "secondary" {
		t.Fatalf("selected %q, want secondary", selected.Name)
	}

	upstreams[1].ReportCriticalFailure(time.Millisecond)
	if _, err := selector.Select(context.Background()); err == nil {
		t.Fatal("all-critical pool produced an egress upstream")
	}
}

func TestEgressSelectorRejectsEmptyPool(t *testing.T) {
	pool, err := NewPoolFromRecords(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEgressSelector(pool).Select(context.Background()); err == nil {
		t.Fatal("empty pool produced an egress upstream")
	}
}

func TestEgressSelectorConcurrentSelection(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{ID: 1, Name: "primary", URL: "https://primary.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: false},
		{ID: 2, Name: "secondary", URL: "https://secondary.example", Priority: 2, ProbeMode: "active", ProbeInterval: "30m", Healthy: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	selector := NewEgressSelector(pool)

	const callers = 64
	start := make(chan struct{})
	errors := make(chan error, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for range callers {
		go func() {
			defer group.Done()
			<-start
			for range 100 {
				selected, selectErr := selector.Select(context.Background())
				if selectErr != nil {
					errors <- selectErr
					return
				}
				if selected.Name != "primary" {
					errors <- &unexpectedEgressSelection{got: selected.Name, want: "primary"}
					return
				}
			}
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

type unexpectedEgressSelection struct {
	got  string
	want string
}

func (e *unexpectedEgressSelection) Error() string {
	return "selected " + e.got + ", want " + e.want
}
