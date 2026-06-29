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

// canned is a simple Resolver impl whose return is preset. Lets
// checker tests run without any HTTP plumbing.
type canned struct {
	t   time.Time
	err error
}

func (c canned) Lookup(_ context.Context, _, _ string) (time.Time, error) {
	return c.t, c.err
}

// newChecker builds a Checker wired to an in-memory DB + a canned
// resolver registry. `now` is fixed so age calculations are
// deterministic.
func newChecker(t *testing.T, cfg Config, reg resolvers.Registry, now time.Time) *Checker {
	t.Helper()
	p, err := NewPolicy(cfg)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	store := NewStore(newLookupDB(t))
	c, err := NewChecker(p, NewLookup(store, reg), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	c.now = func() time.Time { return now }
	return c
}

func TestChecker_RejectsServeLastEligible(t *testing.T) {
	// Option B locked-in 2026-06-29: serve_last_eligible isn't
	// implemented; NewChecker must reject it at construction so the
	// operator hears about it at startup, not silently when a
	// quarantine fires.
	p, err := NewPolicy(Config{Mode: "serve_last_eligible"})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	_, err = NewChecker(p, nil, nil)
	if err == nil {
		t.Fatal("NewChecker accepted serve_last_eligible; should reject until implemented")
	}
	if !strings.Contains(err.Error(), "serve_last_eligible") {
		t.Errorf("error message should name the mode; got %v", err)
	}
}

func TestChecker_EcosystemDisabled(t *testing.T) {
	// Threshold 0 → no quarantine → no resolver call ever.
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	c := newChecker(t, Config{}, resolvers.Registry{}, now)
	d := c.Check(context.Background(), "go", "github.com/x/y", "v1.0.0", "127.0.0.1")
	if !d.Allowed {
		t.Errorf("go ecosystem (threshold 0) should always allow; got %+v", d)
	}
	if d.Threshold != 0 {
		t.Errorf("threshold = %v, want 0", d.Threshold)
	}
}

func TestChecker_BlocksUnderThreshold(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// Published 2 days ago, threshold 7d → block.
	publishedAt := now.Add(-2 * 24 * time.Hour)
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
	}, resolvers.Registry{
		"npm": canned{t: publishedAt},
	}, now)

	d := c.Check(context.Background(), "npm", "lodash", "4.17.21", "10.0.0.1")
	if d.Allowed {
		t.Fatalf("expected Block; got %+v", d)
	}
	if d.AgeAtCall != 2*24*time.Hour {
		t.Errorf("AgeAtCall = %v, want 48h", d.AgeAtCall)
	}
	if d.Threshold != 7*24*time.Hour {
		t.Errorf("Threshold = %v, want 168h", d.Threshold)
	}
	if !strings.Contains(d.Reason, "lodash") || !strings.Contains(d.Reason, "4.17.21") {
		t.Errorf("Reason should name package and version; got %q", d.Reason)
	}
}

func TestChecker_AllowsOverThreshold(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// Published 30 days ago, threshold 7d → allow.
	publishedAt := now.Add(-30 * 24 * time.Hour)
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
	}, resolvers.Registry{
		"npm": canned{t: publishedAt},
	}, now)

	d := c.Check(context.Background(), "npm", "react", "18.0.0", "")
	if !d.Allowed {
		t.Fatalf("expected Allow; got %+v", d)
	}
}

func TestChecker_AllowlistBypass(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// Published 1 minute ago — would normally be blocked.
	publishedAt := now.Add(-1 * time.Minute)
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
		Allow:         []string{"npm:@scope/internal-*"},
	}, resolvers.Registry{
		"npm": canned{t: publishedAt},
	}, now)

	d := c.Check(context.Background(), "npm", "@scope/internal-utils", "1.0.0", "")
	if !d.Allowed {
		t.Fatalf("allow-list should bypass; got %+v", d)
	}
	if !strings.Contains(d.Reason, "allow-list") {
		t.Errorf("Reason should mention allow-list; got %q", d.Reason)
	}
	// Allow-list bypass must record an ActionBypassed event so
	// security review can see what got past the wall.
	if !hasEvent(t, c.store, ActionBypassed) {
		t.Error("expected ActionBypassed event recorded")
	}
}

func TestChecker_AdminApprovalBypass(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	publishedAt := now.Add(-1 * time.Hour)
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
	}, resolvers.Registry{
		"npm": canned{t: publishedAt},
	}, now)

	// Pre-approve via the store (simulating an admin action).
	if err := c.store.Approve(context.Background(), "npm", "lodash", "4.17.99", "operator approved for hotfix", 7); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	d := c.Check(context.Background(), "npm", "lodash", "4.17.99", "")
	if !d.Allowed {
		t.Fatalf("admin-approved version should pass; got %+v", d)
	}
	// IsApproved bypass must NOT record another event — the approval
	// itself was recorded when the admin created it (Store.Approve
	// records ActionApproved). A second event per fetch would flood
	// the audit table on every install.
	if hasEventForVersion(t, c.store, "lodash", "4.17.99", ActionBypassed) {
		t.Error("admin-approval bypass should NOT record ActionBypassed (Store.Approve already recorded ActionApproved)")
	}
}

func TestChecker_UpstreamUnavailable_AlwaysAllows(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	// FailClosed=true should NOT block on upstream-unavailable —
	// "block when uncertain it's safe" is not the same as "break my
	// build whenever pypi.org has a bad day."
	failClosed := true
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
		FailClosed:    &failClosed,
	}, resolvers.Registry{
		"npm": canned{err: resolvers.ErrUpstreamUnavailable},
	}, now)

	d := c.Check(context.Background(), "npm", "x", "1.0", "")
	if !d.Allowed {
		t.Errorf("upstream-unavailable should always allow even under FailClosed; got %+v", d)
	}
}

func TestChecker_NotFound_FailClosed_Blocks(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	failClosed := true
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
		FailClosed:    &failClosed,
	}, resolvers.Registry{
		"npm": canned{err: resolvers.ErrNotFound},
	}, now)

	d := c.Check(context.Background(), "npm", "ghostpkg", "1.0", "")
	if d.Allowed {
		t.Fatalf("ErrNotFound + FailClosed should block; got %+v", d)
	}
	if !strings.Contains(d.Reason, "not found") {
		t.Errorf("Reason should explain not-found; got %q", d.Reason)
	}
	if !hasEvent(t, c.store, ActionBlocked) {
		t.Error("expected ActionBlocked event recorded")
	}
}

func TestChecker_NotFound_FailOpen_Allows(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	failOpen := false
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
		FailClosed:    &failOpen,
	}, resolvers.Registry{
		"npm": canned{err: resolvers.ErrNotFound},
	}, now)

	d := c.Check(context.Background(), "npm", "ghostpkg", "1.0", "")
	if !d.Allowed {
		t.Errorf("ErrNotFound + FailClosed=false should allow; got %+v", d)
	}
}

func TestChecker_UnsupportedEcosystem_FailClosed_Blocks(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	failClosed := true
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"exotic": "1d"}, // configured but no resolver
		FailClosed:    &failClosed,
	}, resolvers.Registry{}, now)

	d := c.Check(context.Background(), "exotic", "x", "1.0", "")
	if d.Allowed {
		t.Fatalf("ErrUnsupported + FailClosed should block; got %+v", d)
	}
	if !strings.Contains(d.Reason, "resolver") {
		t.Errorf("Reason should explain missing resolver; got %q", d.Reason)
	}
}

func TestChecker_EmptyVersion_Allows(t *testing.T) {
	// Adapter handlers should only call Check on per-version fetches.
	// Defense-in-depth: if a caller passes an empty version, allow +
	// log rather than block, since blocking would be the user-hostile
	// default for a bug in our caller.
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	c := newChecker(t, Config{
		MinReleaseAge: map[string]string{"npm": "7d"},
	}, resolvers.Registry{
		"npm": canned{err: errors.New("should not be called")},
	}, now)

	d := c.Check(context.Background(), "npm", "lodash", "", "")
	if !d.Allowed {
		t.Errorf("empty version should not block; got %+v", d)
	}
}

func TestChecker_FormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{2 * 24 * time.Hour, "2d"},
		{2*24*time.Hour + 5*time.Hour, "2d 5h"},
		{3 * time.Hour, "3h"},
		{3*time.Hour + 15*time.Minute, "3h 15m"},
		{45 * time.Minute, "45m"},
		{30 * time.Second, "30s"},
		{-2 * 24 * time.Hour, "2d"}, // negative → absolute
	}
	for _, c := range cases {
		if got := formatAge(c.d); got != c.want {
			t.Errorf("formatAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// ── helpers ────────────────────────────────────────────────────────

func hasEvent(t *testing.T, store *Store, action string) bool {
	t.Helper()
	var count int64
	if err := store.db.Model(&db.QuarantineEvent{}).Where("action = ?", action).Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count > 0
}

func hasEventForVersion(t *testing.T, store *Store, pkg, version, action string) bool {
	t.Helper()
	var count int64
	if err := store.db.Model(&db.QuarantineEvent{}).
		Where("package = ? AND version = ? AND action = ?", pkg, version, action).
		Count(&count).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count > 0
}
