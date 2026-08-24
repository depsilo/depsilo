// Package quarantine implements the Minimum Release Age governance
// primitive: refuse to serve any package version whose upstream
// publish time is younger than a configurable per-ecosystem threshold,
// so a freshly-poisoned version cannot enter a build before the
// community catches it.
//
// Spec: docs/DIRECTION.md "Task 1 — Minimum release age (quarantine)".
// Strategic rationale: docs/adr/0003-supply-chain-control-point.md.
//
// Package layout:
//
//	policy.go      — Config struct, mode enum, threshold defaults
//	allowlist.go   — three-syntax pin matcher (glob / exact / range)
//	store.go       — GORM models + persistence helpers
//	timestamps.go  — per-package timestamp lookup cache
//	resolvers/     — per-ecosystem upstream publish-time fetchers
//	checker.go     — Decision engine: combines all of the above
package quarantine

import (
	"fmt"
	"strings"
	"time"
)

// Mode controls what the checker does when a version is younger than
// its ecosystem's threshold.
type Mode string

const (
	// ModeBlock returns a 451 with a clear error body. Default.
	// Spec: docs/DIRECTION.md §Task 1 "block mode".
	ModeBlock Mode = "block"

	// ModeWarn records the decision and serves the request anyway.
	// Operators use it to observe what the gate would have blocked
	// before switching to ModeBlock.
	ModeWarn Mode = "warn"

	// ModeServeLastEligible resolves the request to the newest version
	// that IS older than the threshold and serves that instead. Useful
	// when CI must keep flowing during a quarantine event but the
	// operator accepts the risk of a stale dependency.
	ModeServeLastEligible Mode = "serve_last_eligible"
)

// Policy is the resolved, normalized config the checker consumes.
// Built by NewPolicy from a config.SupplyChainConfig — never
// constructed by hand outside tests.
type Policy struct {
	// ageGateEnabled is the resolved global switch. Thresholds retain the
	// recommended profile even while disabled so an explicit true can enable
	// the profile without duplicating every ecosystem in configuration.
	ageGateEnabled bool

	// Per-ecosystem threshold lookup. Keys are lowercase ecosystem
	// names matching internal/adapter directory names ("pypi", "npm",
	// "cargo", ...). Missing key → fall back to Default.
	Thresholds map[string]time.Duration

	// Default threshold for ecosystems without an explicit entry.
	// Zero means "no quarantine" — the checker short-circuits and
	// returns Allowed=true without even calling the timestamp
	// resolver. This is the Go-modules default per the
	// locked-in decisions (Go's checksum DB + version immutability
	// already mitigate Shai-Hulud-class poisoning).
	Default time.Duration

	// Block and warn are the runtime-supported modes. The legacy
	// serve_last_eligible value is parsed so startup can reject it clearly.
	Mode Mode

	// Allow rules, parsed once into matcher entries. Applied
	// after the threshold check — a matching rule lets a young
	// version through without going through the approval flow.
	Allow *AllowList

	// FailClosed controls what happens when the timestamp resolver
	// cannot determine the package's publish time because it was not found
	// or the resolver is unsupported. True = treat as blocked; false = allow.
	// A genuine upstream outage always allows with a warning.
	FailClosed bool
}

// Config is the user-facing TOML/YAML shape. Mirrors the
// docs/DIRECTION.md §Task 1 example and binds via mapstructure inside
// config.Config.SupplyChain.
type Config struct {
	// MinReleaseAgeEnabled is tri-state for backward compatibility. Nil with
	// no threshold table means off; nil with an explicit table preserves the
	// pre-switch behavior; an explicit bool always wins.
	MinReleaseAgeEnabled *bool `mapstructure:"min_release_age_enabled"`

	// MinReleaseAge holds the per-ecosystem threshold strings. Keys
	// are ecosystem names; values are Go-style duration strings
	// ("3d", "7d", "0", "24h", "30m"). The special key "default"
	// sets the fallback. The "d" suffix isn't standard time.Duration
	// — see ParseDuration below.
	MinReleaseAge map[string]string `mapstructure:"min_release_age"`

	// Mode: "block" and "warn" are supported. "serve_last_eligible" is
	// retained as a recognized legacy value so the checker can return an
	// explicit error.
	Mode string `mapstructure:"mode"`

	// Allow rules — see AllowList for syntax. Each entry is a
	// "<ecosystem>:<pattern>" string.
	Allow []string `mapstructure:"allow"`

	// FailClosed: true (default) blocks not-found/unsupported timestamps;
	// false allows them. Upstream outages always allow with a warning.
	FailClosed *bool `mapstructure:"fail_closed"`
}

// DefaultThresholds returns the recommended per-ecosystem profile used when
// the minimum-release-age gate is enabled. The gate itself defaults off for
// new and empty configurations.
//
// Rationale per ecosystem:
//   - pip / cargo / maven / rubygems / nuget / composer: 3d window
//     covers the typical 24-48h "community catches it and yanks"
//     timeline for Shai-Hulud-class malware with a margin of safety.
//   - npm: 7d because npm sees an order-of-magnitude more publishes
//     than the others; community noticing and yanking lags more.
//   - go: 0 — Go modules are immutable on proxy.golang.org and the
//     checksum DB cross-validates content. Re-tagging is impossible
//     without changing the hash, which the toolchain rejects.
//   - conda / cran / helm / alpine / docker / huggingface: 3d
//     as a conservative middle ground; can be tuned per-deployment.
func DefaultThresholds() map[string]time.Duration {
	return map[string]time.Duration{
		"pypi":        3 * 24 * time.Hour,
		"npm":         7 * 24 * time.Hour,
		"go":          0,
		"cargo":       3 * 24 * time.Hour,
		"maven":       3 * 24 * time.Hour,
		"rubygems":    3 * 24 * time.Hour,
		"composer":    3 * 24 * time.Hour,
		"nuget":       3 * 24 * time.Hour,
		"conda":       3 * 24 * time.Hour,
		"cran":        3 * 24 * time.Hour,
		"helm":        3 * 24 * time.Hour,
		"alpine":      3 * 24 * time.Hour,
		"docker":      3 * 24 * time.Hour,
		"huggingface": 3 * 24 * time.Hour,
		"apt":         0, // apt repos are highly curated upstream; quarantine adds noise here.
	}
}

// NewPolicy normalizes a Config into a Policy ready for the checker.
// Returns an error if any duration string fails to parse — the
// operator hears about config typos at startup, not at request time.
func NewPolicy(cfg Config) (*Policy, error) {
	thresholds := DefaultThresholds()
	ageGateEnabled := len(cfg.MinReleaseAge) > 0
	if cfg.MinReleaseAgeEnabled != nil {
		ageGateEnabled = *cfg.MinReleaseAgeEnabled
	}

	var defaultThreshold time.Duration

	for key, value := range cfg.MinReleaseAge {
		dur, err := ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("quarantine: invalid duration for %q: %w", key, err)
		}
		if key == "default" {
			defaultThreshold = dur
			continue
		}
		thresholds[strings.ToLower(key)] = dur
	}

	mode := ModeBlock
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "block":
		mode = ModeBlock
	case "warn":
		mode = ModeWarn
	case "serve_last_eligible":
		mode = ModeServeLastEligible
	default:
		return nil, fmt.Errorf("quarantine: unknown mode %q (want block | warn | serve_last_eligible)", cfg.Mode)
	}

	allow, err := ParseAllowList(cfg.Allow)
	if err != nil {
		return nil, fmt.Errorf("quarantine: %w", err)
	}

	failClosed := true
	if cfg.FailClosed != nil {
		failClosed = *cfg.FailClosed
	}

	return &Policy{
		ageGateEnabled: ageGateEnabled,
		Thresholds:     thresholds,
		Default:        defaultThreshold,
		Mode:           mode,
		Allow:          allow,
		FailClosed:     failClosed,
	}, nil
}

// Threshold returns the threshold for a given ecosystem, falling
// back to Default. Ecosystem names are normalized to lowercase
// before lookup so callers don't need to remember the case.
func (p *Policy) Threshold(ecosystem string) time.Duration {
	if p == nil || !p.ageGateEnabled {
		return 0
	}
	if d, ok := p.Thresholds[strings.ToLower(ecosystem)]; ok {
		return d
	}
	return p.Default
}

// Enabled reports whether the policy has any effect for an ecosystem.
// When threshold is zero AND there are no allow rules, the checker
// can short-circuit without even reaching the timestamp resolver.
func (p *Policy) Enabled(ecosystem string) bool {
	return p.Threshold(ecosystem) > 0
}

// IsAgeGateEnabled reports the resolved global switch before per-ecosystem
// zero-threshold exemptions are applied.
func (p *Policy) IsAgeGateEnabled() bool {
	return p != nil && p.ageGateEnabled
}

// ParseDuration extends time.ParseDuration with "d" (days) and "w"
// (weeks) suffixes that the spec uses ("3d", "7d", "2w"). Go's
// stdlib intentionally omits these because of leap-day / DST
// ambiguity, but quarantine windows are coarse enough that day ==
// 24h is the natural reading and the locked-in defaults all use
// "d" anyway. Plain time.Duration syntax ("72h", "504h") still
// works.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	// Handle "d" / "w" suffix by converting to hours, then defer
	// to stdlib for any remaining h/m/s components. We only support
	// integer day/week values — "1.5d" is not a thing the spec calls
	// out and the precision isn't useful here.
	multiplier := time.Duration(0)
	switch s[len(s)-1] {
	case 'd':
		multiplier = 24 * time.Hour
	case 'w':
		multiplier = 7 * 24 * time.Hour
	}
	if multiplier > 0 {
		body := s[:len(s)-1]
		var n int
		if _, err := fmt.Sscanf(body, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("invalid duration %q: negative", s)
		}
		return time.Duration(n) * multiplier, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid duration %q: negative", s)
	}
	return d, nil
}
