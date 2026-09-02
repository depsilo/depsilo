package rules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"depsilo/internal/db"
	ecosystemcatalog "depsilo/internal/ecosystem"
	"depsilo/internal/entitlement"
	"depsilo/internal/packagepolicy"
	"go.uber.org/zap"
)

var (
	// ErrPolicyIntegrity means persisted rule data is not the output of the
	// current dialect preparation revision. It must never be treated like a
	// transient storage outage.
	ErrPolicyIntegrity = errors.New("package policy integrity failure")
	// ErrPolicyEvaluation means a request coordinate could not be interpreted
	// by its declared ecosystem dialect.
	ErrPolicyEvaluation = errors.New("package policy evaluation failure")
)

// OnLoadErrorPolicy controls what the request path does when a policy
// refresh cannot read the rule store.  The values are intentionally explicit
// configuration contract rather than an implicit middleware fail-open.
type OnLoadErrorPolicy string

const (
	OnLoadErrorUseStaleThenAllow OnLoadErrorPolicy = "use_stale_then_allow"
	OnLoadErrorUseStaleThenDeny  OnLoadErrorPolicy = "use_stale_then_deny"
	OnLoadErrorAllow             OnLoadErrorPolicy = "allow"
	OnLoadErrorDeny              OnLoadErrorPolicy = "deny"
	DefaultOnLoadErrorPolicy     OnLoadErrorPolicy = OnLoadErrorUseStaleThenAllow
)

// ParseOnLoadErrorPolicy validates the operator-facing policy value. An empty
// value means the safe, availability-biased default for backwards
// compatibility; all non-empty values must be one of the documented modes.
func ParseOnLoadErrorPolicy(value string) (OnLoadErrorPolicy, error) {
	normalized := OnLoadErrorPolicy(strings.ToLower(strings.TrimSpace(value)))
	if normalized == "" {
		return DefaultOnLoadErrorPolicy, nil
	}
	switch normalized {
	case OnLoadErrorUseStaleThenAllow, OnLoadErrorUseStaleThenDeny, OnLoadErrorAllow, OnLoadErrorDeny:
		return normalized, nil
	default:
		return "", fmt.Errorf("policy.on_load_error must be one of %q, %q, %q, or %q; got %q",
			OnLoadErrorUseStaleThenAllow, OnLoadErrorUseStaleThenDeny, OnLoadErrorAllow, OnLoadErrorDeny, value)
	}
}

// PolicyTelemetry is the narrow observability seam between the rules package
// and the API metrics registry. Keeping the interface here avoids a rules →
// API import cycle while allowing the server composition root to bind the
// process-wide metrics implementation.
type PolicyTelemetry interface {
	PolicySnapshotLoaded(time.Time)
	PolicyRefreshFailed()
	// PolicyState is emitted after a refresh transition (or failure), not for
	// every cache-hit request. Consumers that need a continuously changing age
	// should derive it from the loaded timestamp at scrape/read time.
	PolicyState(degraded bool, usingStaleSnapshot bool, snapshotAgeSeconds float64)
}

// PolicyStatus is the operator-facing state of the in-memory policy
// snapshot. LastSuccessfulRefresh is deliberately retained across failed
// refreshes so snapshot age remains truthful.
type PolicyStatus struct {
	Status                string
	Degraded              bool
	UsingStaleSnapshot    bool
	LastSuccessfulRefresh time.Time
	// SnapshotLoadedAt is an alias kept for callers that use the metric's
	// terminology; both fields always carry the same instant.
	SnapshotLoadedAt   time.Time
	SnapshotAgeSeconds float64
	RefreshFailures    uint64
	OnLoadError        OnLoadErrorPolicy
}

// PolicyStatusProvider is the read-only seam exposed by an Engine to
// operational surfaces. Implementations must return an in-memory status and
// must not perform a rules-store refresh while answering the query.
type PolicyStatusProvider interface {
	PolicyStatus() PolicyStatus
}

// EngineOption customizes an Engine while preserving the historical
// two-argument NewEngine constructor for callers that do not need options.
type EngineOption func(*Engine) error

// WithOnLoadErrorPolicy selects the explicit storage-failure behavior.
func WithOnLoadErrorPolicy(policy OnLoadErrorPolicy) EngineOption {
	return func(engine *Engine) error {
		parsed, err := ParseOnLoadErrorPolicy(string(policy))
		if err != nil {
			return err
		}
		engine.onLoadError = parsed
		return nil
	}
}

// WithLoadErrorPolicy is a readable alias for WithOnLoadErrorPolicy.
func WithLoadErrorPolicy(policy OnLoadErrorPolicy) EngineOption {
	return WithOnLoadErrorPolicy(policy)
}

// WithLoadErrorMode accepts the TOML/string representation directly.
func WithLoadErrorMode(value string) EngineOption {
	return func(engine *Engine) error {
		policy, err := ParseOnLoadErrorPolicy(value)
		if err != nil {
			return err
		}
		engine.onLoadError = policy
		return nil
	}
}

// WithPolicyTelemetry binds metrics or another observer to the engine.
func WithPolicyTelemetry(telemetry PolicyTelemetry) EngineOption {
	return func(engine *Engine) error {
		engine.telemetry = telemetry
		return nil
	}
}

type selectorKind uint8

const (
	selectorWildcard selectorKind = iota
	selectorPrefix
	selectorRange
	selectorExact
)

type compiledRule struct {
	model        db.PackageRule
	packageKind  selectorKind
	packageValue string
	versionKind  selectorKind
	version      packagepolicy.VersionMatcher
}

// Engine evaluates package rules with an in-memory cache of compiled,
// migration-backed rules.
type Engine struct {
	store     *Store
	checker   *entitlement.Checker
	mu        sync.RWMutex
	refreshMu sync.Mutex
	cache     []compiledRule
	// lastLoad is the instant of the last successful snapshot load. Keep the
	// name for compatibility with package-local tests and tooling.
	lastLoad              time.Time
	lastRefreshAttempt    time.Time
	lastRefreshError      error
	lastRefreshWasFailure bool
	degraded              bool
	usingStaleSnapshot    bool
	refreshFailures       uint64
	forceRefresh          bool
	refreshGeneration     uint64
	cacheTTL              time.Duration
	refreshRetryInterval  time.Duration
	onLoadError           OnLoadErrorPolicy
	telemetry             PolicyTelemetry
	optionErr             error
}

// NewEngine creates a new rules Engine. Existing callers retain the default
// use_stale_then_allow behavior; production wiring passes explicit options
// from config.
func NewEngine(store *Store, checker *entitlement.Checker, options ...EngineOption) *Engine {
	engine := &Engine{
		store:                store,
		checker:              checker,
		cacheTTL:             30 * time.Second,
		refreshRetryInterval: 5 * time.Second,
		onLoadError:          DefaultOnLoadErrorPolicy,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(engine); err != nil {
			// Keep the constructor compatibility contract (it historically could
			// not return an error), but remember invalid options so the first
			// evaluation fails explicitly instead of silently changing policy.
			engine.optionErr = err
			break
		}
	}
	return engine
}

// NewEngineWithOptions is the error-returning constructor for startup code
// that wants invalid policy configuration to fail before serving traffic.
func NewEngineWithOptions(store *Store, checker *entitlement.Checker, options ...EngineOption) (*Engine, error) {
	engine := NewEngine(store, checker, options...)
	if engine.optionErr != nil {
		return nil, engine.optionErr
	}
	return engine, nil
}

// Check returns (allowed, matchedRule, error).
// As of the 2026-06-28 pricing reset the rules engine runs in open-source.
func (e *Engine) Check(ctx context.Context, ecosystem, packageName, version string) (bool, *db.PackageRule, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return true, nil, err
		}
	}
	rules, outcome, err := e.loadRulesWithState()
	if err != nil {
		return true, nil, err
	}
	if outcome == loadOutcomeDeny {
		return false, nil, nil
	}

	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	definition, supported := ecosystemcatalog.Lookup(ecosystem)
	if !supported || !definition.RuleEnforcement {
		return false, nil, fmt.Errorf("%w: package rules are unavailable for ecosystem %q", ErrPolicyEvaluation, ecosystem)
	}
	dialect, err := packagepolicy.DialectFor(ecosystem)
	if err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrPolicyEvaluation, err)
	}
	normalizedPackage, err := dialect.NormalizePackageName(packageName)
	if err != nil {
		return false, nil, fmt.Errorf("%w: normalize %s package %q: %v", ErrPolicyEvaluation, ecosystem, packageName, err)
	}

	var bestRule *db.PackageRule
	bestScore := -1
	for index := range rules {
		rule := &rules[index]
		score, err := rule.match(ecosystem, normalizedPackage, version)
		if err != nil {
			return false, nil, fmt.Errorf("%w: evaluate rule %d: %v", ErrPolicyEvaluation, rule.model.ID, err)
		}
		if score > bestScore {
			bestScore = score
			bestRule = &rule.model
		}
	}
	if bestRule == nil {
		return true, nil, nil
	}
	return bestRule.Action == "allow", bestRule, nil
}

// CheckIncompleteArtifact evaluates an artifact whose package/version cannot
// be proven from the request path. Unconditional ecosystem/global wildcards
// still have a deterministic result. More specific rules are safe only when
// their action cannot change that fallback; otherwise evaluation fails closed.
func (e *Engine) CheckIncompleteArtifact(
	ctx context.Context,
	ecosystem string,
) (bool, *db.PackageRule, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return true, nil, err
		}
	}
	rules, outcome, err := e.loadRulesWithState()
	if err != nil {
		return true, nil, err
	}
	if outcome == loadOutcomeDeny {
		return false, nil, nil
	}
	ecosystem = strings.ToLower(strings.TrimSpace(ecosystem))
	definition, supported := ecosystemcatalog.Lookup(ecosystem)
	if !supported || !definition.RuleEnforcement {
		return false, nil, fmt.Errorf("%w: package rules are unavailable for ecosystem %q", ErrPolicyEvaluation, ecosystem)
	}

	var fallback *db.PackageRule
	fallbackScore := -1
	for index := range rules {
		rule := &rules[index]
		if rule.model.Ecosystem != "*" && rule.model.Ecosystem != ecosystem {
			continue
		}
		if rule.packageKind != selectorWildcard || rule.versionKind != selectorWildcard {
			continue
		}
		score := 1
		if rule.model.Ecosystem == ecosystem {
			score = 2
		}
		if score > fallbackScore {
			fallbackScore = score
			fallback = &rule.model
		}
	}

	fallbackAction := "allow"
	if fallback != nil {
		fallbackAction = fallback.Action
	}
	for index := range rules {
		rule := &rules[index]
		if rule.model.Ecosystem != "*" && rule.model.Ecosystem != ecosystem {
			continue
		}
		if rule.packageKind == selectorWildcard && rule.versionKind == selectorWildcard {
			continue
		}
		if rule.model.Action != fallbackAction {
			return false, nil, fmt.Errorf(
				"%w: incomplete %s artifact identity may select rule %d with action %s instead of %s",
				ErrPolicyEvaluation, ecosystem, rule.model.ID, rule.model.Action, fallbackAction,
			)
		}
	}
	return fallbackAction == "allow", fallback, nil
}

// InvalidateCache forces a reload on next Check.
func (e *Engine) InvalidateCache() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// A successful snapshot remains available as a last-known-good fallback;
	// forceRefresh bypasses the normal TTL without erasing its timestamp, so
	// stale age remains truthful for operators.
	e.forceRefresh = true
	e.refreshGeneration++
	e.lastRefreshAttempt = time.Time{}
	e.lastRefreshError = nil
	e.lastRefreshWasFailure = false
	// Preserve degraded state until the forced refresh succeeds; an operator
	// looking at status during the brief transition should not see a false
	// healthy signal.
}

type loadOutcome uint8

const (
	loadOutcomeFresh loadOutcome = iota
	loadOutcomeStale
	loadOutcomeAllow
	loadOutcomeDeny
)

// loadRules preserves the package-local helper's historical signature. The
// request evaluators use loadRulesWithState so an explicit deny fallback can
// be distinguished from an empty, successfully loaded rule set.
func (e *Engine) loadRules() ([]compiledRule, error) {
	rules, _, err := e.loadRulesWithState()
	return rules, err
}

func (e *Engine) loadRulesWithState() ([]compiledRule, loadOutcome, error) {
	if e == nil {
		return nil, loadOutcomeAllow, nil
	}
	now := time.Now()

	// Fast path for a fresh snapshot and for a recent availability failure.
	e.mu.RLock()
	if e.optionErr != nil {
		err := e.optionErr
		e.mu.RUnlock()
		return nil, loadOutcomeAllow, fmt.Errorf("invalid rules engine configuration: %w", err)
	}
	if e.snapshotFreshLocked(now) {
		rules := e.cache
		e.mu.RUnlock()
		return rules, loadOutcomeFresh, nil
	}
	if e.retryBackoffActiveLocked(now) {
		rules, outcome := e.fallbackReadOnlyLocked()
		e.mu.RUnlock()
		return rules, outcome, nil
	}
	e.mu.RUnlock()

	// Serialize refreshes independently from the state lock. Store.List may
	// spend several seconds in SQLite's busy timeout during an outage; status
	// and readiness readers must still be able to take the short state lock and
	// report the last-known-good snapshot while that I/O is in flight.
	e.refreshMu.Lock()
	defer e.refreshMu.Unlock()
	e.mu.Lock()
	if e.optionErr != nil {
		err := e.optionErr
		e.mu.Unlock()
		return nil, loadOutcomeAllow, fmt.Errorf("invalid rules engine configuration: %w", err)
	}
	now = time.Now()
	if e.snapshotFreshLocked(now) {
		rules := e.cache
		e.mu.Unlock()
		return rules, loadOutcomeFresh, nil
	}
	if e.retryBackoffActiveLocked(now) {
		rules, outcome := e.fallbackLocked()
		e.mu.Unlock()
		return rules, outcome, nil
	}

	e.lastRefreshAttempt = now
	generation := e.refreshGeneration
	store := e.store
	e.mu.Unlock()
	if store == nil {
		e.mu.Lock()
		return e.finishRefreshFailureLocked(time.Now(), fmt.Errorf("%w: rule store is nil", ErrRuleStoreUnavailable), true)
	}
	models, err := store.List()
	if err != nil {
		e.mu.Lock()
		if errors.Is(err, ErrRuleDataIntegrity) || errors.Is(err, ErrPolicyIntegrity) {
			wrapped := fmt.Errorf("%w: %v", ErrPolicyIntegrity, err)
			return e.finishRefreshFailureLocked(time.Now(), wrapped, false)
		}
		// Store.List normally classifies transient database failures. Preserve
		// that explicit availability sentinel (and the low-level transient
		// classes for defensive/alternate stores), but never turn an unknown
		// read or scan error into a silent allow. Unknown errors are treated as
		// policy-integrity failures and therefore fail closed.
		if errors.Is(err, ErrRuleStoreUnavailable) || isRuleStoreAvailabilityError(err) {
			wrapped := err
			if !errors.Is(wrapped, ErrRuleStoreUnavailable) {
				wrapped = fmt.Errorf("%w: %v", ErrRuleStoreUnavailable, wrapped)
			}
			return e.finishRefreshFailureLocked(time.Now(), wrapped, true)
		}
		wrapped := fmt.Errorf("%w: %v", ErrPolicyIntegrity, err)
		return e.finishRefreshFailureLocked(time.Now(), wrapped, false)
	}
	compiled := make([]compiledRule, 0, len(models))
	for index := range models {
		rule, compileErr := compilePersistedRule(models[index])
		if compileErr != nil {
			wrapped := fmt.Errorf("%w: rule %d: %v", ErrPolicyIntegrity, models[index].ID, compileErr)
			e.mu.Lock()
			return e.finishRefreshFailureLocked(time.Now(), wrapped, false)
		}
		compiled = append(compiled, rule)
	}

	// Publish the complete immutable slice in one assignment. Never mutate a
	// previously published snapshot in place.
	loadedAt := time.Now()
	e.mu.Lock()
	e.cache = compiled
	e.lastLoad = loadedAt
	e.lastRefreshAttempt = now
	e.lastRefreshError = nil
	e.lastRefreshWasFailure = false
	// An invalidation racing this refresh must not be lost. In that case keep
	// forceRefresh set so the next request performs one more read; otherwise a
	// successful publish satisfies the pending refresh and clears the flag.
	if e.refreshGeneration == generation {
		e.forceRefresh = false
	}
	e.degraded = false
	e.usingStaleSnapshot = false
	status := e.policyStatusLocked(loadedAt)
	telemetry := e.telemetry
	e.mu.Unlock()
	if telemetry != nil {
		telemetry.PolicySnapshotLoaded(loadedAt)
	}
	e.emitPolicyState(telemetry, status)
	return compiled, loadOutcomeFresh, nil
}

// finishRefreshFailureLocked records one refresh failure and selects the
// configured outcome. The caller must hold e.mu; the function always unlocks
// before returning so telemetry callbacks cannot deadlock the engine.
func (e *Engine) finishRefreshFailureLocked(now time.Time, err error, unavailable bool) ([]compiledRule, loadOutcome, error) {
	if now.IsZero() {
		now = time.Now()
	}
	// Record the completion instant rather than only the attempt start. A slow
	// SQLite timeout should still receive the full retry backoff, and the
	// status/age reported below should describe the state at failure time.
	e.lastRefreshAttempt = now
	e.lastRefreshError = err
	e.lastRefreshWasFailure = unavailable
	e.refreshFailures++
	e.degraded = true

	var rules []compiledRule
	outcome := loadOutcomeAllow
	if unavailable {
		rules, outcome = e.fallbackLocked()
		// Record which fallback was selected while holding the write lock. The
		// retry fast path may call fallbackLocked under RLock, so that helper must
		// remain side-effect free.
		e.usingStaleSnapshot = outcome == loadOutcomeStale
	} else {
		// Integrity failures are unsafe policy data, not a transient outage: the
		// request path fails closed and does not evaluate the old snapshot. Do not
		// advertise that snapshot as actively in use.
		e.usingStaleSnapshot = false
	}
	status := e.policyStatusLocked(now)
	telemetry := e.telemetry
	e.mu.Unlock()

	if telemetry != nil {
		telemetry.PolicyRefreshFailed()
	}
	e.emitPolicyState(telemetry, status)
	fields := []zap.Field{
		zap.Error(err),
		zap.Bool("degraded", status.Degraded),
		zap.String("on_load_error", string(status.OnLoadError)),
		zap.Bool("using_stale_snapshot", status.UsingStaleSnapshot),
		zap.Float64("snapshot_age_seconds", status.SnapshotAgeSeconds),
	}
	if status.UsingStaleSnapshot {
		zap.L().Error("package policy refresh failed; using last-known-good snapshot", fields...)
	} else {
		zap.L().Error("package policy refresh failed; no usable snapshot", fields...)
	}

	if unavailable {
		return rules, outcome, nil
	}
	// Integrity failures are unsafe even when an older snapshot exists. They
	// remain visible to Middleware/AdapterChecker as an unevaluable error.
	return nil, loadOutcomeAllow, err
}

func (e *Engine) snapshotFreshLocked(now time.Time) bool {
	return !e.forceRefresh && e.cache != nil && !e.lastLoad.IsZero() && e.cacheTTL > 0 && now.Sub(e.lastLoad) < e.cacheTTL
}

func (e *Engine) retryBackoffActiveLocked(now time.Time) bool {
	return e.lastRefreshWasFailure && errors.Is(e.lastRefreshError, ErrRuleStoreUnavailable) &&
		!e.lastRefreshAttempt.IsZero() && e.refreshRetryInterval > 0 &&
		now.Sub(e.lastRefreshAttempt) < e.refreshRetryInterval
}

func (e *Engine) fallbackLocked() ([]compiledRule, loadOutcome) {
	policy := e.onLoadError
	if policy == "" {
		policy = DefaultOnLoadErrorPolicy
	}
	useStale := policy == OnLoadErrorUseStaleThenAllow || policy == OnLoadErrorUseStaleThenDeny
	if useStale && e.cache != nil {
		// Do not mutate engine state here. This helper is also called while a
		// read lock is held on the fast retry-backoff path; mutating
		// usingStaleSnapshot under RLock races concurrent readers. The state is
		// set by finishRefreshFailureLocked (under the write lock) when the
		// refresh first fails and cleared by a successful publish.
		return e.cache, loadOutcomeStale
	}
	if policy == OnLoadErrorUseStaleThenDeny || policy == OnLoadErrorDeny {
		return nil, loadOutcomeDeny
	}
	return nil, loadOutcomeAllow
}

// fallbackReadOnlyLocked is the RLock-safe counterpart used while serving a
// retry-backoff request. The first failed refresh already recorded
// usingStaleSnapshot under the write lock; this helper must not mutate that
// state while many requests concurrently read the same snapshot.
func (e *Engine) fallbackReadOnlyLocked() ([]compiledRule, loadOutcome) {
	policy := e.onLoadError
	if policy == "" {
		policy = DefaultOnLoadErrorPolicy
	}
	useStale := policy == OnLoadErrorUseStaleThenAllow || policy == OnLoadErrorUseStaleThenDeny
	if useStale && e.cache != nil {
		return e.cache, loadOutcomeStale
	}
	if policy == OnLoadErrorUseStaleThenDeny || policy == OnLoadErrorDeny {
		return nil, loadOutcomeDeny
	}
	return nil, loadOutcomeAllow
}

// PolicyStatus returns a point-in-time, lock-consistent view of policy
// freshness. It never performs I/O, so readiness and Admin status endpoints
// cannot themselves make a database outage worse.
func (e *Engine) PolicyStatus() PolicyStatus {
	if e == nil {
		return PolicyStatus{
			Status:      "unavailable",
			OnLoadError: DefaultOnLoadErrorPolicy,
		}
	}
	now := time.Now()
	e.mu.RLock()
	status := e.policyStatusLocked(now)
	e.mu.RUnlock()
	return status
}

// OnLoadErrorPolicy returns the currently selected fallback mode. It is used
// only for defensive handling of errors outside Store.List's classified
// availability path; normal refresh failures are resolved inside the Engine
// and never reach request middleware as a silent error.
func (e *Engine) OnLoadErrorPolicy() OnLoadErrorPolicy {
	if e == nil {
		return DefaultOnLoadErrorPolicy
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.onLoadError == "" {
		return DefaultOnLoadErrorPolicy
	}
	return e.onLoadError
}

func (e *Engine) fallbackAllowsWithoutSnapshot() bool {
	policy := e.OnLoadErrorPolicy()
	return policy != OnLoadErrorUseStaleThenDeny && policy != OnLoadErrorDeny
}

func (e *Engine) policyStatusLocked(now time.Time) PolicyStatus {
	loadedAt := e.lastLoad
	age := 0.0
	if !loadedAt.IsZero() {
		age = now.Sub(loadedAt).Seconds()
		if age < 0 {
			age = 0
		}
	}
	statusName := "healthy"
	if loadedAt.IsZero() {
		// No successful snapshot exists yet. This is distinct from a healthy
		// empty rule set: operators must be able to tell that policy has never
		// loaded.
		statusName = "unavailable"
	} else if e.degraded {
		statusName = "degraded"
	}
	policy := e.onLoadError
	if policy == "" {
		policy = DefaultOnLoadErrorPolicy
	}
	return PolicyStatus{
		Status:                statusName,
		Degraded:              e.degraded,
		UsingStaleSnapshot:    e.usingStaleSnapshot,
		LastSuccessfulRefresh: loadedAt,
		SnapshotLoadedAt:      loadedAt,
		SnapshotAgeSeconds:    age,
		RefreshFailures:       e.refreshFailures,
		OnLoadError:           policy,
	}
}

func (e *Engine) emitPolicyState(telemetry PolicyTelemetry, status PolicyStatus) {
	if telemetry == nil {
		return
	}
	telemetry.PolicyState(status.Degraded, status.UsingStaleSnapshot, status.SnapshotAgeSeconds)
}

func compilePersistedRule(model db.PackageRule) (compiledRule, error) {
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem:   model.Ecosystem,
		PackageName: model.PackageName,
		Version:     model.Version,
	})
	if err != nil {
		return compiledRule{}, err
	}
	if model.Ecosystem != prepared.Ecosystem ||
		model.PackageName != prepared.PackageName ||
		model.Version != prepared.Version ||
		model.NormalizedPackageName != prepared.NormalizedPackageName ||
		model.NormalizedVersion != prepared.NormalizedVersion ||
		model.DialectRevision != prepared.DialectRevision {
		return compiledRule{}, fmt.Errorf("persisted raw/normalized values do not match dialect revision %d", packagepolicy.CurrentDialectRevision)
	}
	if model.Action != "allow" && model.Action != "deny" {
		return compiledRule{}, fmt.Errorf("unsupported action %q", model.Action)
	}

	compiled := compiledRule{
		model:        model,
		packageValue: model.NormalizedPackageName,
	}
	switch {
	case model.NormalizedPackageName == "*":
		compiled.packageKind = selectorWildcard
	case strings.HasSuffix(model.NormalizedPackageName, "*"):
		compiled.packageKind = selectorPrefix
		compiled.packageValue = strings.TrimSuffix(model.NormalizedPackageName, "*")
	default:
		compiled.packageKind = selectorExact
	}
	switch {
	case model.NormalizedVersion == "*":
		compiled.versionKind = selectorWildcard
	case hasComparisonOperator(model.NormalizedVersion):
		compiled.versionKind = selectorRange
	default:
		compiled.versionKind = selectorExact
	}
	if model.Ecosystem != "*" {
		compiled.version, err = packagepolicy.CompileVersionMatcher(model.Ecosystem, model.NormalizedVersion)
		if err != nil {
			return compiledRule{}, err
		}
	}
	return compiled, nil
}

func (r *compiledRule) match(ecosystem, normalizedPackage, actualVersion string) (int, error) {
	score := 0
	if r.model.Ecosystem == "*" {
		score++
	} else if r.model.Ecosystem == ecosystem {
		score += 2
	} else {
		return -1, nil
	}

	switch r.packageKind {
	case selectorWildcard:
	case selectorPrefix:
		if !strings.HasPrefix(normalizedPackage, r.packageValue) {
			return -1, nil
		}
		score++
	case selectorExact:
		if normalizedPackage != r.packageValue {
			return -1, nil
		}
		score += 2
	}

	if r.versionKind == selectorWildcard {
		return score, nil
	}
	if actualVersion == "" {
		return -1, nil
	}
	matched, err := r.version.Match(actualVersion)
	if err != nil {
		return -1, err
	}
	if !matched {
		return -1, nil
	}
	if r.versionKind == selectorExact {
		score += 2
	} else {
		score++
	}
	return score, nil
}

func hasComparisonOperator(value string) bool {
	for _, operator := range []string{"<=", ">=", "<", ">"} {
		if strings.HasPrefix(value, operator) {
			return true
		}
	}
	return false
}

// matchScore remains a focused test helper for the rule-specificity contract.
// Production evaluation only calls compilePersistedRule on prepared rows.
func matchScore(model *db.PackageRule, ecosystem, packageName, version string) int {
	if model == nil {
		return -1
	}
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem: model.Ecosystem, PackageName: model.PackageName, Version: model.Version,
	})
	if err != nil {
		return -1
	}
	copy := *model
	copy.Ecosystem = prepared.Ecosystem
	copy.PackageName = prepared.PackageName
	copy.Version = prepared.Version
	copy.NormalizedPackageName = prepared.NormalizedPackageName
	copy.NormalizedVersion = prepared.NormalizedVersion
	copy.DialectRevision = prepared.DialectRevision
	copy.Action = "deny"
	compiled, err := compilePersistedRule(copy)
	if err != nil {
		return -1
	}
	dialect, err := packagepolicy.DialectFor(strings.ToLower(strings.TrimSpace(ecosystem)))
	if err != nil {
		return -1
	}
	normalizedPackage, err := dialect.NormalizePackageName(packageName)
	if err != nil {
		return -1
	}
	score, err := compiled.match(strings.ToLower(strings.TrimSpace(ecosystem)), normalizedPackage, version)
	if err != nil {
		return -1
	}
	return score
}
