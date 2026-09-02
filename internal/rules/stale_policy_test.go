package rules

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/adapter"
	"depsilo/internal/db"
	"gorm.io/gorm"
)

// enginePolicyTelemetryCapture is deliberately a test double at the
// observability boundary. It does not replace the Store or the Engine: each
// test still exercises the real SQLite-backed refresh path.
type enginePolicyTelemetryCapture struct {
	mu       sync.Mutex
	loaded   []time.Time
	failures int
	states   []enginePolicyStateEvent
}

type enginePolicyStateEvent struct {
	degraded   bool
	usingStale bool
	ageSeconds float64
}

func (capture *enginePolicyTelemetryCapture) PolicySnapshotLoaded(at time.Time) {
	capture.mu.Lock()
	capture.loaded = append(capture.loaded, at)
	capture.mu.Unlock()
}

func (capture *enginePolicyTelemetryCapture) PolicyRefreshFailed() {
	capture.mu.Lock()
	capture.failures++
	capture.mu.Unlock()
}

func (capture *enginePolicyTelemetryCapture) PolicyState(degraded, usingStale bool, ageSeconds float64) {
	capture.mu.Lock()
	capture.states = append(capture.states, enginePolicyStateEvent{
		degraded:   degraded,
		usingStale: usingStale,
		ageSeconds: ageSeconds,
	})
	capture.mu.Unlock()
}

func (capture *enginePolicyTelemetryCapture) snapshot() (loaded []time.Time, failures int, states []enginePolicyStateEvent) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	loaded = append([]time.Time(nil), capture.loaded...)
	failures = capture.failures
	states = append([]enginePolicyStateEvent(nil), capture.states...)
	return loaded, failures, states
}

func TestAdapterCheckerUsesLastKnownGoodSnapshotAfterRefreshFailure(t *testing.T) {
	database, store := openPolicyRulesFixture(t)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	telemetry := &enginePolicyTelemetryCapture{}
	engine := NewEngine(store, nil, WithPolicyTelemetry(telemetry))
	checker := Wrap(engine)
	first := checker.EvaluatePackageRule(context.Background(), "npm", "fixture", "1.0.0")
	if first.Outcome != adapter.PackageRuleDeny {
		t.Fatalf("initial adapter decision = %#v, want deny", first)
	}

	closePolicyDatabase(t, database)
	// InvalidateCache is the deterministic public refresh boundary used by
	// Admin mutations; it models an expired snapshot without an arbitrary
	// sleep in the regression test.
	engine.InvalidateCache()
	stale := checker.EvaluatePackageRule(context.Background(), "npm", "fixture", "1.0.0")
	if stale.Outcome != adapter.PackageRuleDeny {
		t.Fatalf("stale adapter decision = %#v, want deny from last-known-good snapshot", stale)
	}

	status := engine.PolicyStatus()
	if !status.Degraded || !status.UsingStaleSnapshot {
		t.Fatalf("stale policy status = %#v, want degraded + stale", status)
	}
	if status.LastSuccessfulRefresh.IsZero() || status.SnapshotAgeSeconds < 0 || status.RefreshFailures != 1 {
		t.Fatalf("stale policy freshness = %#v, want successful timestamp, non-negative age, one failure", status)
	}
	loaded, failures, states := telemetry.snapshot()
	if len(loaded) != 1 || failures != 1 {
		t.Fatalf("telemetry loaded=%d failures=%d, want 1/1", len(loaded), failures)
	}
	if len(states) == 0 || !states[len(states)-1].degraded || !states[len(states)-1].usingStale {
		t.Fatalf("telemetry states = %#v, want final degraded stale state", states)
	}
}

func TestEngineUsesLastKnownGoodSnapshotAfterCacheExpiryAndStoreFailure(t *testing.T) {
	database, store := openPolicyRulesFixture(t)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	engine := NewEngine(store, nil)
	if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
		t.Fatalf("initial result = allowed %v err %v, want deny", allowed, err)
	}
	// Model the real 30-second TTL boundary directly instead of sleeping. The
	// cache remains populated, but its successful load is older than cacheTTL.
	engine.mu.Lock()
	engine.lastLoad = time.Now().Add(-engine.cacheTTL - time.Second)
	engine.mu.Unlock()
	closePolicyDatabase(t, database)

	allowed, matched, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0")
	if err != nil || allowed || matched == nil || matched.Action != "deny" {
		t.Fatalf("expired-cache result = allowed %v matched %#v err %v, want stale deny", allowed, matched, err)
	}
	status := engine.PolicyStatus()
	if !status.Degraded || !status.UsingStaleSnapshot || status.RefreshFailures != 1 {
		t.Fatalf("expired-cache status = %#v, want degraded/stale with one refresh failure", status)
	}
}

func TestEngineOnLoadErrorPoliciesWithoutLastKnownGoodSnapshot(t *testing.T) {
	tests := []struct {
		name      string
		policy    OnLoadErrorPolicy
		wantAllow bool
	}{
		{name: "stale then allow", policy: OnLoadErrorUseStaleThenAllow, wantAllow: true},
		{name: "stale then deny", policy: OnLoadErrorUseStaleThenDeny, wantAllow: false},
		{name: "allow", policy: OnLoadErrorAllow, wantAllow: true},
		{name: "deny", policy: OnLoadErrorDeny, wantAllow: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, store := openPolicyRulesFixture(t)
			engine := NewEngine(store, nil, WithOnLoadErrorPolicy(test.policy))
			closePolicyDatabase(t, database)

			allowed, matched, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0")
			if err != nil {
				t.Fatalf("Check error = %v, want configured fallback decision", err)
			}
			if allowed != test.wantAllow || matched != nil {
				t.Fatalf("fallback result = allowed %v matched %#v, want allowed %v and no rule", allowed, matched, test.wantAllow)
			}
			status := engine.PolicyStatus()
			if status.Status != "unavailable" || !status.Degraded || status.UsingStaleSnapshot || !status.LastSuccessfulRefresh.IsZero() || status.RefreshFailures != 1 {
				t.Fatalf("no-LKG policy status = %#v, want unavailable + degraded/no timestamp/one failure", status)
			}
		})
	}
}

func TestEngineUsesConfiguredFallbackOnlyWhenNoSnapshotExists(t *testing.T) {
	tests := []struct {
		name      string
		policy    OnLoadErrorPolicy
		wantAllow bool
	}{
		{name: "allow ignores stale snapshot", policy: OnLoadErrorAllow, wantAllow: true},
		{name: "deny ignores stale snapshot", policy: OnLoadErrorDeny, wantAllow: false},
		{name: "stale then allow evaluates stale snapshot", policy: OnLoadErrorUseStaleThenAllow, wantAllow: false},
		{name: "stale then deny evaluates stale snapshot", policy: OnLoadErrorUseStaleThenDeny, wantAllow: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, store := openPolicyRulesFixture(t)
			if err := store.Create(&db.PackageRule{
				Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
			}); err != nil {
				t.Fatalf("create rule: %v", err)
			}
			engine := NewEngine(store, nil, WithOnLoadErrorPolicy(test.policy))
			if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
				t.Fatalf("initial result = allowed %v err %v, want deny", allowed, err)
			}
			closePolicyDatabase(t, database)
			engine.InvalidateCache()

			allowed, matched, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0")
			if err != nil {
				t.Fatalf("stale/fallback error = %v", err)
			}
			if allowed != test.wantAllow {
				t.Fatalf("stale/fallback allowed = %v, want %v (matched=%#v)", allowed, test.wantAllow, matched)
			}
			if test.policy == OnLoadErrorUseStaleThenAllow || test.policy == OnLoadErrorUseStaleThenDeny {
				if matched == nil || matched.Action != "deny" {
					t.Fatalf("stale mode matched = %#v, want the persisted deny rule", matched)
				}
			} else if matched != nil {
				t.Fatalf("explicit %s fallback matched stale rule %#v, want synthetic fallback", test.policy, matched)
			}
		})
	}
}

func TestEnginePolicyStatusRecoversAfterStoreReturns(t *testing.T) {
	database, store, path := openPolicyRulesFixtureAtPath(t)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	telemetry := &enginePolicyTelemetryCapture{}
	engine := NewEngine(store, nil, WithPolicyTelemetry(telemetry))
	if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
		t.Fatalf("initial result = allowed %v err %v, want deny", allowed, err)
	}
	initial := engine.PolicyStatus()
	if initial.Status != "healthy" || initial.Degraded || initial.UsingStaleSnapshot || initial.LastSuccessfulRefresh.IsZero() {
		t.Fatalf("initial policy status = %#v, want healthy fresh snapshot", initial)
	}

	closePolicyDatabase(t, database)
	engine.InvalidateCache()
	if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
		t.Fatalf("stale result = allowed %v err %v, want deny", allowed, err)
	}
	degraded := engine.PolicyStatus()
	if degraded.Status != "degraded" || !degraded.Degraded || !degraded.UsingStaleSnapshot || degraded.RefreshFailures != 1 {
		t.Fatalf("degraded policy status = %#v, want one stale failure", degraded)
	}

	reopened, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen rules database: %v", err)
	}
	defer closePolicyDatabase(t, reopened)
	store.db = reopened
	engine.InvalidateCache()
	if allowed, matched, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed || matched == nil {
		t.Fatalf("recovered result = allowed %v matched %#v err %v, want persisted deny", allowed, matched, err)
	}
	recovered := engine.PolicyStatus()
	if recovered.Status != "healthy" || recovered.Degraded || recovered.UsingStaleSnapshot || recovered.LastSuccessfulRefresh.IsZero() {
		t.Fatalf("recovered policy status = %#v, want healthy fresh snapshot", recovered)
	}
	if recovered.RefreshFailures != 1 {
		t.Fatalf("recovered refresh failures = %d, want monotonic count 1", recovered.RefreshFailures)
	}
	loaded, failures, states := telemetry.snapshot()
	if len(loaded) != 2 || failures != 1 {
		t.Fatalf("recovery telemetry loaded=%d failures=%d, want 2/1", len(loaded), failures)
	}
	if len(states) == 0 || states[len(states)-1].degraded || states[len(states)-1].usingStale {
		t.Fatalf("recovery telemetry states = %#v, want final healthy/fresh", states)
	}
}

func TestEngineConcurrentStaleReadsShareOneRefreshFailure(t *testing.T) {
	database, store := openPolicyRulesFixture(t)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	engine := NewEngine(store, nil)
	if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
		t.Fatalf("initial result = allowed %v err %v, want deny", allowed, err)
	}
	closePolicyDatabase(t, database)
	engine.InvalidateCache()

	const workers = 32
	var wg sync.WaitGroup
	errorsSeen := make(chan error, workers)
	allowedSeen := make(chan bool, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0")
			allowedSeen <- allowed
			errorsSeen <- err
		}()
	}
	wg.Wait()
	close(errorsSeen)
	close(allowedSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Errorf("concurrent stale Check error = %v, want nil", err)
		}
	}
	for allowed := range allowedSeen {
		if allowed {
			t.Errorf("concurrent stale Check allowed request, want deny")
		}
	}
	status := engine.PolicyStatus()
	if !status.Degraded || !status.UsingStaleSnapshot || status.RefreshFailures != 1 {
		t.Fatalf("concurrent stale status = %#v, want one degraded refresh", status)
	}
}

func TestEngineIntegrityRefreshFailureDoesNotAdvertiseStaleUse(t *testing.T) {
	database, store := openPolicyRulesFixture(t)
	rule := &db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}
	if err := store.Create(rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	engine := NewEngine(store, nil)
	if allowed, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0"); err != nil || allowed {
		t.Fatalf("initial result = allowed %v err %v, want deny", allowed, err)
	}
	// Leave the LKG in memory, but make the next persisted snapshot invalid.
	if err := database.Model(&db.PackageRule{}).Where("id = ?", rule.ID).Update("normalized_version", "corrupt").Error; err != nil {
		t.Fatalf("corrupt persisted rule: %v", err)
	}
	engine.InvalidateCache()
	_, _, err := engine.Check(context.Background(), "pypi", "requests", "1.0.0")
	if !errors.Is(err, ErrPolicyIntegrity) {
		t.Fatalf("integrity refresh error = %v, want ErrPolicyIntegrity", err)
	}
	status := engine.PolicyStatus()
	if !status.Degraded || status.UsingStaleSnapshot || status.LastSuccessfulRefresh.IsZero() || status.RefreshFailures != 1 {
		t.Fatalf("integrity failure status = %#v, want degraded/non-stale with retained timestamp", status)
	}
}

func TestParseOnLoadErrorPolicyValidatesExplicitValues(t *testing.T) {
	tests := []struct {
		input string
		want  OnLoadErrorPolicy
	}{
		{input: "", want: DefaultOnLoadErrorPolicy},
		{input: " use_stale_then_allow ", want: OnLoadErrorUseStaleThenAllow},
		{input: "USE_STALE_THEN_DENY", want: OnLoadErrorUseStaleThenDeny},
		{input: "allow", want: OnLoadErrorAllow},
		{input: "deny", want: OnLoadErrorDeny},
	}
	for _, test := range tests {
		got, err := ParseOnLoadErrorPolicy(test.input)
		if err != nil || got != test.want {
			t.Errorf("ParseOnLoadErrorPolicy(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
	if _, err := ParseOnLoadErrorPolicy("sometimes"); err == nil || !strings.Contains(err.Error(), "policy.on_load_error") {
		t.Fatalf("invalid policy error = %v, want actionable policy.on_load_error error", err)
	}
	if _, err := NewEngineWithOptions(nil, nil, WithOnLoadErrorPolicy(OnLoadErrorPolicy("invalid"))); err == nil {
		t.Fatal("NewEngineWithOptions accepted an invalid on-load-error policy")
	}
}

func openPolicyRulesFixture(t *testing.T) (*gorm.DB, *Store) {
	t.Helper()
	database, store, _ := openPolicyRulesFixtureAtPath(t)
	return database, store
}

func openPolicyRulesFixtureAtPath(t *testing.T) (*gorm.DB, *Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.db")
	database, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open rules database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("migrate rules database: %v", err)
	}
	return database, NewStore(database), path
}

func closePolicyDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()
	if database == nil {
		return
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("get rules SQL database: %v", err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatalf("close rules SQL database: %v", err)
	}
}
