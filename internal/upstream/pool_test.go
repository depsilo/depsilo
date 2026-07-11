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
