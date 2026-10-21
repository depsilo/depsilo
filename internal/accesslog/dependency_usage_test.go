package accesslog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"depsilo/internal/db"
)

func completedArtifactEvent() Event {
	return Event{
		AdapterType: "npm", Method: "GET", CacheKey: "npm/pkg/-/pkg-1.0.0.tgz",
		DependencyKind: db.CacheKindArtifact, PackageName: "pkg", StatusCode: 200,
		BytesSent: 1024, CacheResult: "miss", DeliveryResult: "completed",
		At: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestDependencyUsageRecorderClassifiesOnlyCompletedArtifactDownloads(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollups=%t", enabled), func(t *testing.T) {
			d := newTestDB(t)
			// A clean never-used state is established only by a fresh DB migration
			// in production; seed that fixture explicitly for this recorder test.
			if err := d.Create(&db.ControlPlaneState{Key: "dependency_usage_v11", Value: `{"state":"never-used","first_successful_request_at":null,"in_progress":false}`}).Error; err != nil {
				t.Fatal(err)
			}
			r := NewRecorder(d, Config{Enabled: enabled, BatchSize: 1000, BatchInterval: time.Hour})
			t.Cleanup(func() { _ = r.Close(context.Background()) })
			base := completedArtifactEvent()
			ignored := []Event{base, base, base, base, base, base}
			ignored[0].DependencyKind = db.CacheKindMetadata
			ignored[1].Method = "HEAD"
			ignored[2].StatusCode = 403
			ignored[3].DeliveryResult = "failed"
			ignored[4].BytesSent = 0
			ignored[5].DependencyKind, ignored[5].CacheKey = "", "/health"
			for _, event := range ignored {
				r.Record(event)
			}
			if err := r.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			usage, err := r.(UsageReader).DependencyUsage(context.Background())
			if err != nil || usage.State != db.DependencyNeverUsed {
				t.Fatalf("non-downloads changed lifetime usage: %+v, %v", usage, err)
			}
			r.Record(base)
			if err := r.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			usage, err = r.(UsageReader).DependencyUsage(context.Background())
			if err != nil || usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt == nil || !usage.FirstSuccessfulRequestAt.Equal(base.At) {
				t.Fatalf("completed download missing: %+v, %v", usage, err)
			}
			if err := r.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			persisted, err := db.ReadDependencyUsage(context.Background(), d)
			if err != nil || persisted.State != db.DependencyUsed || persisted.FirstSuccessfulRequestAt == nil {
				t.Fatalf("completed download not durable: %+v, %v", persisted, err)
			}
		})
	}
}

func TestDependencyUsageRetriesPersistenceWithoutAnotherRequest(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollups=%t", enabled), func(t *testing.T) {
			d := newTestDB(t)
			r := NewRecorder(d, Config{Enabled: enabled, BatchSize: 1000, BatchInterval: time.Hour})
			t.Cleanup(func() { _ = r.Close(context.Background()) })
			if err := d.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON control_plane_states WHEN NEW.key = 'dependency_usage_v11' BEGIN SELECT RAISE(FAIL, 'unavailable'); END`).Error; err != nil {
				t.Fatal(err)
			}
			r.Record(completedArtifactEvent())
			if err := r.Flush(context.Background()); err == nil {
				t.Fatal("usage persistence failure was hidden")
			}
			if _, err := r.(UsageReader).DependencyUsage(context.Background()); err == nil {
				t.Fatal("live reader hid a failed usage write")
			}
			if err := d.Exec("DROP TRIGGER reject_usage").Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Flush(context.Background()); err != nil {
				t.Fatalf("empty flush did not retry usage: %v", err)
			}
			usage, err := db.ReadDependencyUsage(context.Background(), d)
			if err != nil || usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt != nil {
				t.Fatalf("legacy instance must prove use without inventing first timestamp: %+v, %v", usage, err)
			}
		})
	}
}

func TestDependencyUsageObservationPrecedesLossyQueue(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollups=%t", enabled), func(t *testing.T) {
			d := newTestDB(t)
			tracker := newUsageTracker(d)
			queue := make(chan Event, 1)
			queue <- Event{}
			// A deliberately paused worker exposes a full queue deterministically.
			var recorder Recorder
			if enabled {
				recorder = &batchedRecorder{in: queue, usage: tracker}
			} else {
				recorder = &nullRecorder{in: queue, usage: tracker}
			}
			recorder.Record(completedArtifactEvent())
			if drops := recorder.(interface{ Dropped() uint64 }).Dropped(); drops != 1 {
				t.Fatalf("fixture failed to drop raw event: %d", drops)
			}
			usage, err := recorder.(UsageReader).DependencyUsage(context.Background())
			if err != nil || usage.State != db.DependencyUsed {
				t.Fatalf("dropped raw event lost independent use evidence: %+v, %v", usage, err)
			}
			if err := tracker.flush(context.Background(), true); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDependencyUsageUnknownArtifactCompletionLosesAbsenceCertainty(t *testing.T) {
	d := newTestDB(t)
	if err := d.Create(&db.ControlPlaneState{Key: "dependency_usage_v11", Value: `{"state":"never-used","first_successful_request_at":null,"in_progress":false}`}).Error; err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(d, Config{Enabled: true, BatchInterval: time.Hour})
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	event := completedArtifactEvent()
	event.DeliveryResult = "unknown"
	r.Record(event)
	if err := r.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	usage, err := r.(UsageReader).DependencyUsage(context.Background())
	if err != nil || usage.State != db.DependencyUnknown {
		t.Fatalf("unknown artifact completion must revoke absence certainty: %+v, %v", usage, err)
	}
	r.Record(completedArtifactEvent())
	if err := r.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	usage, err = r.(UsageReader).DependencyUsage(context.Background())
	if err != nil || usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt != nil {
		t.Fatalf("later success must not fabricate lifecycle first time: %+v, %v", usage, err)
	}
}

func TestDependencyUsageEarlierUnknownCompletionRevokesFirstTimestamp(t *testing.T) {
	for _, flushBeforeGap := range []bool{false, true} {
		t.Run(fmt.Sprintf("flush-before-gap=%t", flushBeforeGap), func(t *testing.T) {
			d := newTestDB(t)
			if err := d.Create(&db.ControlPlaneState{Key: "dependency_usage_v11", Value: `{"state":"never-used","first_successful_request_at":null,"in_progress":false}`}).Error; err != nil {
				t.Fatal(err)
			}
			r := NewRecorder(d, Config{Enabled: true, BatchInterval: time.Hour})
			t.Cleanup(func() { _ = r.Close(context.Background()) })
			r.Record(completedArtifactEvent())
			if flushBeforeGap {
				if err := r.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			gap := completedArtifactEvent()
			gap.At = gap.At.Add(-time.Second)
			gap.DeliveryResult = "unknown"
			r.Record(gap)
			if err := r.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Further successful downloads cannot restore the lost certainty.
			r.Record(completedArtifactEvent())
			if err := r.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			usage, err := db.ReadDependencyUsage(context.Background(), d)
			if err != nil || usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt != nil {
				t.Fatalf("earlier unknown completion retained fabricated first timestamp: %+v, %v", usage, err)
			}
		})
	}
}

func TestDependencyUsageInitializationFailureIsObservable(t *testing.T) {
	d := newTestDB(t)
	if err := d.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON control_plane_states WHEN NEW.key = 'dependency_usage_v11' BEGIN SELECT RAISE(FAIL, 'unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(d, Config{Enabled: true, BatchInterval: time.Hour})
	usage, err := r.(UsageReader).DependencyUsage(context.Background())
	if err == nil || usage.State != db.DependencyUnknown {
		t.Fatalf("server must receive initialization error before accepting requests: %+v, %v", usage, err)
	}
	if err := d.Exec("DROP TRIGGER reject_usage").Error; err != nil {
		t.Fatal(err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyUsageCloseRetriesFailedFinalWrite(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollups=%t", enabled), func(t *testing.T) {
			d := newTestDB(t)
			r := NewRecorder(d, Config{Enabled: enabled, BatchInterval: time.Hour})
			if err := d.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON control_plane_states WHEN NEW.key = 'dependency_usage_v11' BEGIN SELECT RAISE(FAIL, 'unavailable'); END`).Error; err != nil {
				t.Fatal(err)
			}
			r.Record(completedArtifactEvent())
			if err := r.Close(context.Background()); err == nil {
				t.Fatal("Close hid final write failure")
			}
			if err := d.Exec("DROP TRIGGER reject_usage").Error; err != nil {
				t.Fatal(err)
			}
			if err := r.Close(context.Background()); err != nil {
				t.Fatalf("second Close did not retry final usage write: %v", err)
			}
			usage, err := db.ReadDependencyUsage(context.Background(), d)
			if err != nil || usage.State != db.DependencyUsed {
				t.Fatalf("successful shutdown lost use evidence: %+v, %v", usage, err)
			}
		})
	}
}
