package quarantine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/quarantine/resolvers"
)

type fakeSnapshotGate struct {
	match SnapshotMatch
	err   error
}

func (g fakeSnapshotGate) Check(context.Context, string, string, string) (SnapshotMatch, error) {
	return g.match, g.err
}

// newSnapshotGateChecker wires a checker whose age gate has a canned old
// timestamp, so only the snapshot decision can change the outcome.
func newSnapshotGateChecker(t *testing.T, gate SnapshotGate) (*Checker, func() []db.QuarantineEvent) {
	t.Helper()
	database := newLookupDB(t)
	store := NewStore(database)
	enabled := true
	policy, err := NewPolicyWithProvenance(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"npm": "168h"},
	}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	checker, err := NewChecker(policy, NewLookup(store, resolvers.Registry{
		"npm": canned{t: time.Now().Add(-30 * 24 * time.Hour)},
	}), store)
	if err != nil {
		t.Fatal(err)
	}
	checker.SetSnapshotGate(gate)
	listEvents := func() []db.QuarantineEvent {
		var events []db.QuarantineEvent
		if err := database.Where("action = ?", ActionSnapshotBlocked).Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		return events
	}
	return checker, listEvents
}

func TestSnapshotGateBlocksArtifactsOutsideTheActiveSnapshot(t *testing.T) {
	checker, listEvents := newSnapshotGateChecker(t, fakeSnapshotGate{
		match: SnapshotMatch{Allowed: false, SnapshotID: 7, SnapshotName: "golden"},
	})
	decision := checker.Check(context.Background(), "npm", "left-pad", "1.3.0", "10.0.0.1")
	if decision.Allowed || decision.Code != CodeSnapshotBlocked {
		t.Fatalf("decision = %+v", decision)
	}
	if !strings.Contains(decision.Reason, "golden") || !strings.Contains(decision.Reason, "snapshot-only") {
		t.Fatalf("reason = %q", decision.Reason)
	}
	events := listEvents()
	if len(events) != 1 || events[0].Package != "left-pad" || events[0].Version != "1.3.0" {
		t.Fatalf("snapshot block events = %+v", events)
	}
}

func TestSnapshotGateAllowsPinnedArtifacts(t *testing.T) {
	checker, listEvents := newSnapshotGateChecker(t, fakeSnapshotGate{
		match: SnapshotMatch{Allowed: true, SnapshotID: 7, SnapshotName: "golden"},
	})
	decision := checker.Check(context.Background(), "npm", "left-pad", "1.3.0", "10.0.0.1")
	if !decision.Allowed {
		t.Fatalf("pinned decision = %+v", decision)
	}
	if events := listEvents(); len(events) != 0 {
		t.Fatalf("pinned artifact recorded snapshot events = %+v", events)
	}
}

func TestSnapshotGateFailsClosedOnLookupError(t *testing.T) {
	checker, listEvents := newSnapshotGateChecker(t, fakeSnapshotGate{err: errors.New("db down")})
	decision := checker.Check(context.Background(), "npm", "left-pad", "1.3.0", "10.0.0.1")
	if decision.Allowed || decision.Code != CodeSnapshotBlocked {
		t.Fatalf("fail-closed decision = %+v", decision)
	}
	if events := listEvents(); len(events) != 1 {
		t.Fatalf("fail-closed events = %+v", events)
	}
}
