package upstream

import (
	"context"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestPoolReplaceChangesExistingSelectorWithoutChangingPoolIdentity(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	if err != nil {
		t.Fatal(err)
	}
	selector := NewPrioritySelector(pool)
	next, err := buildPoolSnapshot([]db.UpstreamRecord{{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 1, Healthy: true}}, pool.load())
	if err != nil {
		t.Fatal(err)
	}
	pool.Replace(next)
	got, err := selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 2 {
		t.Fatalf("selected id=%d want=2", got.ID)
	}
}

func TestPrioritySelectorCanExcludeCurrentHealthySource(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{
		{
			ID: 1, AdapterType: "huggingface", Name: "primary",
			URL: "https://primary.example", Priority: 1, Healthy: true,
		},
		{
			ID: 2, AdapterType: "huggingface", Name: "secondary",
			URL: "https://secondary.example", Priority: 2, Healthy: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	selector := NewPrioritySelector(pool)
	primary, err := selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// A single transient failure does not necessarily trip the cumulative
	// health threshold, but request-local failover must still skip it.
	primary.Report(time.Millisecond, true)
	primary.Report(time.Millisecond, false)
	if !primary.IsHealthy() {
		t.Fatal("test primary unexpectedly became unhealthy")
	}
	alternate, err := selector.SelectExcluding(context.Background(), primary)
	if err != nil {
		t.Fatal(err)
	}
	if alternate.ID != 2 {
		t.Fatalf("alternate id = %d, want 2", alternate.ID)
	}
}

func TestPoolSnapshotCannotBeMutatedByCaller(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	if err != nil {
		t.Fatal(err)
	}
	copy := pool.Snapshot()
	copy[0] = nil
	if pool.Snapshot()[0] == nil {
		t.Fatal("caller mutated live snapshot")
	}
}

func TestBuildPoolSnapshotReusesUnchangedConfigAndHealth(t *testing.T) {
	createdAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example",
		Priority: 1, Healthy: true, SuccessRate: 1, CreatedAt: createdAt,
	}})
	if err != nil {
		t.Fatal(err)
	}
	original, ok := pool.Find(1)
	if !ok {
		t.Fatal("original upstream not found")
	}

	next, err := buildPoolSnapshot([]db.UpstreamRecord{{
		ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example",
		Priority: 1, Healthy: false, AvgLatencyMs: 500, SuccessRate: 0,
		CreatedAt: createdAt.Add(time.Hour), UpdatedAt: createdAt.Add(2 * time.Hour),
	}}, pool.load())
	if err != nil {
		t.Fatal(err)
	}
	pool.Replace(next)
	replaced, ok := pool.Find(1)
	if !ok {
		t.Fatal("replaced upstream not found")
	}
	if replaced != original {
		t.Fatal("mutable health or timestamps changed upstream identity")
	}
	if !replaced.HealthSnapshot().Healthy {
		t.Fatal("replacement discarded live health state")
	}
}

func TestUpstreamHealthStateConcurrentReadWrite(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	if err != nil {
		t.Fatal(err)
	}
	u := pool.Snapshot()[0]
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				u.Report(time.Millisecond, n%2 == 0)
			}
		}()
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				_ = u.HealthSnapshot()
			}
		}()
	}
	wg.Wait()
}

func TestReportCriticalFailureOverridesLongSuccessHistory(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{
		ID: 1, AdapterType: "huggingface", Name: "mirror",
		URL: "https://mirror.example", Priority: 1, Healthy: true, SuccessRate: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	for i := 0; i < 1000; i++ {
		selected.Report(time.Millisecond, true)
	}

	selected.ReportCriticalFailure(2 * time.Millisecond)

	health := selected.HealthSnapshot()
	if health.Healthy {
		t.Fatal("critical protocol failure did not remove historically successful upstream")
	}
	if health.SuccessRate <= 0.99 {
		t.Fatalf("test did not retain a strongly diluted cumulative success rate: %v", health.SuccessRate)
	}
}

func TestCriticalFailureStaysOutOfSelectorUntilConfigObjectReplacement(t *testing.T) {
	records := []db.UpstreamRecord{
		{
			ID: 1, AdapterType: "huggingface", Name: "primary",
			URL: "https://primary.example", Priority: 1, Healthy: true, SuccessRate: 1,
		},
		{
			ID: 2, AdapterType: "huggingface", Name: "fallback",
			URL: "https://fallback.example", Priority: 2, Healthy: true, SuccessRate: 1,
		},
	}
	pool, err := NewPoolFromRecords(records)
	if err != nil {
		t.Fatal(err)
	}
	selector := NewPrioritySelector(pool)
	primary, ok := pool.Find(1)
	if !ok {
		t.Fatal("primary upstream not found")
	}

	primary.ReportCriticalFailure(time.Millisecond)
	for range 10 {
		primary.Report(time.Millisecond, true)
	}
	primary.applyProbe(ProbeResult{
		Healthy:   true,
		Latency:   time.Millisecond,
		CheckedAt: time.Now().UTC(),
	})

	if primary.IsHealthy() {
		t.Fatal("ordinary successes or a healthy probe cleared the critical failure")
	}
	selected, err := selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != 2 {
		t.Fatalf("selector chose critically failed upstream %d, want fallback 2", selected.ID)
	}

	unchanged, err := buildPoolSnapshot(records, pool.load())
	if err != nil {
		t.Fatal(err)
	}
	pool.Replace(unchanged)
	stillLatched, ok := pool.Find(1)
	if !ok {
		t.Fatal("unchanged primary upstream not found")
	}
	if stillLatched != primary || stillLatched.IsHealthy() {
		t.Fatalf(
			"same-config snapshot cleared runtime latch: same=%v healthy=%v",
			stillLatched == primary,
			stillLatched.IsHealthy(),
		)
	}

	replacementRecords := append([]db.UpstreamRecord(nil), records...)
	replacementRecords[0].Name = "primary-reconfigured"
	next, err := buildPoolSnapshot(replacementRecords, pool.load())
	if err != nil {
		t.Fatal(err)
	}
	pool.Replace(next)
	replaced, ok := pool.Find(1)
	if !ok {
		t.Fatal("reconfigured primary upstream not found")
	}
	if replaced == primary {
		t.Fatal("configuration change reused the critically failed object")
	}
	selected, err = selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != 1 {
		t.Fatalf("selector did not admit replacement object: selected %d, want 1", selected.ID)
	}
}

func TestCriticalFailureLatchDoesNotSurvivePoolRestart(t *testing.T) {
	records := []db.UpstreamRecord{{
		ID: 1, AdapterType: "huggingface", Name: "primary",
		URL: "https://primary.example", Priority: 1, Healthy: true, SuccessRate: 1,
	}}
	pool, err := NewPoolFromRecords(records)
	if err != nil {
		t.Fatal(err)
	}
	failed := pool.Snapshot()[0]
	failed.ReportCriticalFailure(time.Millisecond)
	failed.Report(time.Millisecond, true)
	if failed.IsHealthy() {
		t.Fatal("critical failure recovered before restart")
	}

	restarted, err := NewPoolFromRecords(records)
	if err != nil {
		t.Fatal(err)
	}
	reloaded := restarted.Snapshot()[0]
	if reloaded == failed || !reloaded.IsHealthy() {
		t.Fatalf("restart did not clear runtime latch: same=%v healthy=%v", reloaded == failed, reloaded.IsHealthy())
	}
}
