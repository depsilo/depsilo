package quarantine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/db"
	"depsilo/internal/quarantine/resolvers"
)

// Decision is what the adapter handler reads to know whether to
// serve the request. Allowed=true means "go ahead with the fetch";
// Allowed=false means "refuse with 451 and use this Reason for the
// error body." Audit / webhook side-effects are recorded inside
// Check before the Decision is returned, so the caller never has to
// thread an audit hook through every adapter.
// Machine-readable decision codes carried in the 451 response body.
// Distinct so CI logs and tooling can tell "too young, wait or ask
// for approval" from "known malware, do not retry".
const (
	CodeQuarantined      = "QUARANTINED"
	CodeMaliciousBlocked = "MALICIOUS_BLOCKED"
)

type Decision struct {
	Allowed bool

	// Code identifies WHY a blocked decision blocked (CodeQuarantined
	// or CodeMaliciousBlocked). Empty on allowed decisions; the
	// adapter gate defaults it to CodeQuarantined for backward
	// compatibility.
	Code string

	// Reason is the human-readable explanation. For Blocked
	// decisions it goes in the 451 response body so the user
	// running `pip install requests` sees something actionable
	// instead of an opaque rejection. For Allowed decisions it
	// can be empty.
	Reason string

	// Warned is true when the gate would have blocked this request but
	// the configured mode is warn. Allowed is true in that case: the
	// adapter serves normally, and the decision is visible only in the
	// event stream / Admin UI.
	Warned bool

	// AgeAtCall is now - publishAt observed at decision time.
	// Zero when the publish timestamp wasn't determined (e.g.
	// allow-list match short-circuited the lookup).
	AgeAtCall time.Duration

	// Threshold is the per-ecosystem window in effect at the call.
	// Zero when the ecosystem isn't quarantined.
	Threshold time.Duration
}

// Checker is the single entry point adapter handlers call. Built
// once at server startup via NewChecker and shared across goroutines
// — every field it holds is concurrency-safe.
//
// Per the option-B locked-in decision (2026-06-29) only block mode
// is implemented for T1/3. serve_last_eligible is rejected at
// construction time so an operator who sets it sees the error on
// startup, not silently when a quarantine fires.
// OnBlockFn is the optional hook called when Check decides to Block.
// Wires the quarantine subsystem to side-effect channels (notify
// webhooks, metrics counters, future SIEM exporters) without making
// the quarantine package depend on any of those concrete consumers.
// Called inline before Check returns, so the implementation MUST NOT
// block — fire-and-forget into a buffered channel / goroutine.
type OnBlockFn func(ev db.QuarantineEvent)

// BlocklistMatch mirrors blocklist.Match. Defined here (with the
// Blocklist interface below) so this package never imports
// internal/blocklist — the concrete store is wired in by server.go
// through blocklist's own bridge, keeping the dependency one-way.
type BlocklistMatch struct {
	SourceID string
	Summary  string
}

// Blocklist is the known-malicious lookup consulted as Check's very
// first step. Check returns the advisory match (nil = clean), whether
// an unexpired operator override exempts this exact request, and any
// storage error.
type Blocklist interface {
	Check(ctx context.Context, ecosystem, pkg, version string) (*BlocklistMatch, bool, error)
}

type Checker struct {
	policy    *Policy
	lookup    *Lookup
	store     *Store
	now       func() time.Time // injectable for tests
	onBlock   OnBlockFn
	blocklist Blocklist
	// blocklistMode controls the known-malicious step independently
	// from the minimum-release-age policy mode. Defaults to block.
	blocklistMode Mode
}

// SetBlocklist installs the known-malicious lookup. Same lifecycle
// contract as SetOnBlock: single-word pointer write, safe to call
// once during server boot; nil disables the malware gate.
func (c *Checker) SetBlocklist(bl Blocklist) {
	c.blocklist = bl
}

// NewChecker validates the policy and wires up the dependencies.
// Returns an error if the policy is in a mode this build doesn't
// support yet — caller is expected to surface that at server boot
// so the operator sees it before any request is served.
func NewChecker(p *Policy, l *Lookup, s *Store) (*Checker, error) {
	if p == nil {
		return nil, errors.New("quarantine: nil policy")
	}
	switch p.Mode {
	case ModeBlock, ModeWarn:
	case ModeServeLastEligible:
		return nil, errors.New("quarantine: serve_last_eligible mode is not yet implemented in this build — set mode = \"block\", \"warn\", or omit the field")
	default:
		return nil, fmt.Errorf("quarantine: unsupported mode %q", p.Mode)
	}
	return &Checker{
		policy:        p,
		lookup:        l,
		store:         s,
		now:           func() time.Time { return time.Now().UTC() },
		blocklistMode: ModeBlock,
	}, nil
}

// SetBlocklistMode sets whether a known-malicious match blocks or
// merely warns. block is the default; warn serves the request while
// recording an event.
func (c *Checker) SetBlocklistMode(m Mode) {
	c.blocklistMode = m
}

// Check returns the gating decision for (ecosystem, pkg, version).
// Audit side-effects (RecordEvent on blocked / bypassed paths) run
// before return so a single Check call leaves a consistent audit
// trail. clientIP is recorded in events but never affects the
// decision — quarantine is a per-version policy, not per-client.
//
// Decision order (matters — each step short-circuits the next):
//
//  1. Policy disabled for this ecosystem (threshold == 0) → Allow.
//  2. Allow-list match → Allow, record ActionBypassed event.
//  3. Operator has manually approved this exact version → Allow.
//     No event written here — the approval itself was recorded
//     when the admin created it (see Store.Approve).
//  4. Lookup publish time:
//     - ErrUpstreamUnavailable → Allow + log warn. We will NOT
//     block a build because some upstream registry is having
//     a bad day. An operator can't distinguish "malicious" from
//     "registry 503'd this hour"; serving stays safer than
//     silently breaking every CI run.
//     - ErrNotFound + FailClosed → Block.
//     - ErrUnsupported + FailClosed → Block (we don't have a
//     resolver, can't make the decision, fail-closed says block).
//     - Either of the above + !FailClosed → Allow + log warn.
//  5. now - publishAt < threshold → Block + record ActionBlocked.
//  6. Otherwise → Allow.
func (c *Checker) Check(ctx context.Context, ecosystem, pkg, version, clientIP string) Decision {
	if c == nil || c.policy == nil {
		return Decision{Allowed: true}
	}

	// Step 0: known-malicious blocklist — the hardest gate, evaluated
	// before EVERYTHING else. Threshold-0 ecosystems (go/apt) and the
	// quarantine allow-list deliberately cannot bypass it; the only
	// exemption is an unexpired, audited operator override. Error
	// posture is asymmetric on purpose: a failed override lookup on a
	// confirmed match blocks (the safe direction for malware), while a
	// failed blocklist lookup on an unknown package logs and continues
	// (a DB hiccup must not take the proxy down).
	if c.blocklist != nil && version != "" {
		match, overridden, err := c.blocklist.Check(ctx, ecosystem, pkg, version)
		if match != nil && err != nil {
			zap.L().Warn("quarantine: blocklist override lookup failed — treating the match as unoverridden",
				zap.String("ecosystem", ecosystem), zap.String("package", pkg),
				zap.String("version", version), zap.Error(err))
			overridden = false
		}
		if match != nil {
			if overridden {
				c.recordEvent(ctx, db.QuarantineEvent{
					Ecosystem: ecosystem,
					Package:   pkg,
					Version:   version,
					Action:    ActionMalwareBypassed,
					Reason:    fmt.Sprintf("operator override active for %s", match.SourceID),
					ClientIP:  clientIP,
				})
				// Fall through to the age checks below — an override
				// exempts from the malware block, not from quarantine.
			} else {
				base := fmt.Sprintf(
					"%s@%s is a known-malicious version (%s): %s",
					pkg, version, match.SourceID, match.Summary,
				)
				if c.blocklistMode == ModeWarn {
					reason := base + " — observe mode, serving anyway"
					c.recordEvent(ctx, db.QuarantineEvent{
						Ecosystem: ecosystem,
						Package:   pkg,
						Version:   version,
						Action:    ActionMalwareWarned,
						Reason:    reason,
						ClientIP:  clientIP,
					})
					return Decision{Allowed: true, Warned: true, Code: CodeMaliciousBlocked, Reason: reason}
				}
				reason := base + " — refusing to serve"
				c.recordEvent(ctx, db.QuarantineEvent{
					Ecosystem: ecosystem,
					Package:   pkg,
					Version:   version,
					Action:    ActionMalwareBlocked,
					Reason:    reason,
					ClientIP:  clientIP,
				})
				return Decision{Allowed: false, Code: CodeMaliciousBlocked, Reason: reason}
			}
		} else if err != nil {
			zap.L().Warn("quarantine: blocklist lookup failed — continuing without it",
				zap.String("ecosystem", ecosystem), zap.String("package", pkg), zap.Error(err))
		}
	}

	// Step 1: ecosystem disabled.
	threshold := c.policy.Threshold(ecosystem)
	if threshold <= 0 {
		return Decision{Allowed: true}
	}
	// Without a version we can't make a per-version decision —
	// the adapter handler should be calling Check only on artifact
	// fetches that resolve to a specific version. Allow + log so
	// any caller that gets this wrong is visible.
	if version == "" {
		zap.L().Debug("quarantine: skipping check with empty version",
			zap.String("ecosystem", ecosystem), zap.String("package", pkg))
		return Decision{Allowed: true, Threshold: threshold}
	}

	// Step 2: allow-list (glob / exact / range).
	if c.policy.Allow.Match(ecosystem, pkg, version) {
		c.recordEvent(ctx, db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   pkg,
			Version:   version,
			Action:    ActionBypassed,
			Reason:    "allow-list rule matched",
			Threshold: int64(threshold.Seconds()),
			ClientIP:  clientIP,
		})
		return Decision{
			Allowed:   true,
			Reason:    "allow-list bypass",
			Threshold: threshold,
		}
	}

	// Step 3: admin approval. No event — the approval already
	// recorded one when the admin created it.
	if c.store != nil {
		approved, err := c.store.IsApproved(ctx, ecosystem, pkg, version)
		if err != nil {
			zap.L().Warn("quarantine: IsApproved",
				zap.String("ecosystem", ecosystem),
				zap.String("package", pkg),
				zap.String("version", version),
				zap.Error(err))
			// IsApproved failures don't block — DB hiccups shouldn't
			// turn an approved version into a blocked one.
		}
		if approved {
			return Decision{
				Allowed:   true,
				Reason:    "approved by operator",
				Threshold: threshold,
			}
		}
	}

	// Step 4: lookup publish time.
	publishAt, err := c.lookup.Get(ctx, ecosystem, pkg, version)
	if err != nil {
		return c.handleLookupError(ctx, ecosystem, pkg, version, clientIP, threshold, err)
	}

	// Step 5 & 6: threshold check.
	age := c.now().Sub(publishAt)
	if age < threshold {
		reason := fmt.Sprintf(
			"version %s of %s was published %s ago, which is younger than the configured %s minimum release age for %s",
			version, pkg, formatAge(age), formatAge(threshold), ecosystem,
		)
		if c.policy.Mode == ModeWarn {
			c.recordEvent(ctx, db.QuarantineEvent{
				Ecosystem: ecosystem,
				Package:   pkg,
				Version:   version,
				Action:    ActionWarned,
				Reason:    reason + " — observe mode, serving anyway",
				Threshold: int64(threshold.Seconds()),
				AgeAtCall: int64(age.Seconds()),
				ClientIP:  clientIP,
			})
			return Decision{
				Allowed:   true,
				Warned:    true,
				Code:      CodeQuarantined,
				Reason:    reason + " — observe mode, serving anyway",
				AgeAtCall: age,
				Threshold: threshold,
			}
		}
		c.recordEvent(ctx, db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   pkg,
			Version:   version,
			Action:    ActionBlocked,
			Reason:    reason,
			Threshold: int64(threshold.Seconds()),
			AgeAtCall: int64(age.Seconds()),
			ClientIP:  clientIP,
		})
		return Decision{
			Allowed:   false,
			Code:      CodeQuarantined,
			Reason:    reason,
			AgeAtCall: age,
			Threshold: threshold,
		}
	}

	return Decision{
		Allowed:   true,
		AgeAtCall: age,
		Threshold: threshold,
	}
}

// handleLookupError encapsulates the fail-closed/open policy on
// resolver errors. Pulled out of Check for readability — the matrix
// of sentinel errors × FailClosed × audit is a lot to read inline.
func (c *Checker) handleLookupError(
	ctx context.Context,
	ecosystem, pkg, version, clientIP string,
	threshold time.Duration,
	err error,
) Decision {
	// Upstream-unavailable is ALWAYS allow regardless of FailClosed.
	// An operator setting FailClosed=true means "block when uncertain
	// the version is safe," not "break my build whenever pypi.org
	// 503's." We log so the operator can monitor upstream-health-
	// driven serves, but the decision stays allow.
	if errors.Is(err, resolvers.ErrUpstreamUnavailable) {
		zap.L().Warn("quarantine: upstream registry unavailable, serving anyway",
			zap.String("ecosystem", ecosystem),
			zap.String("package", pkg),
			zap.String("version", version),
			zap.Error(err))
		return Decision{Allowed: true, Threshold: threshold}
	}

	// NotFound + Unsupported follow FailClosed.
	if !c.policy.FailClosed {
		zap.L().Warn("quarantine: resolver returned terminal error, serving (fail-open)",
			zap.String("ecosystem", ecosystem),
			zap.String("package", pkg),
			zap.String("version", version),
			zap.Error(err))
		return Decision{Allowed: true, Threshold: threshold}
	}

	// FailClosed: block, but with a clear human reason so the
	// operator sees what the resolver couldn't do.
	var reason string
	switch {
	case errors.Is(err, resolvers.ErrNotFound):
		reason = fmt.Sprintf(
			"version %s of %s was not found on the upstream %s registry; quarantine is configured to fail-closed",
			version, pkg, ecosystem,
		)
	case errors.Is(err, resolvers.ErrUnsupported):
		reason = fmt.Sprintf(
			"ecosystem %s does not have a quarantine resolver in this build; configured to fail-closed",
			ecosystem,
		)
	default:
		reason = fmt.Sprintf("quarantine lookup failed: %v", err)
	}
	if c.policy.Mode == ModeWarn {
		c.recordEvent(ctx, db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   pkg,
			Version:   version,
			Action:    ActionWarned,
			Reason:    reason + " — observe mode, serving anyway",
			Threshold: int64(threshold.Seconds()),
			ClientIP:  clientIP,
		})
		return Decision{
			Allowed:   true,
			Warned:    true,
			Code:      CodeQuarantined,
			Reason:    reason + " — observe mode, serving anyway",
			Threshold: threshold,
		}
	}
	c.recordEvent(ctx, db.QuarantineEvent{
		Ecosystem: ecosystem,
		Package:   pkg,
		Version:   version,
		Action:    ActionBlocked,
		Reason:    reason,
		Threshold: int64(threshold.Seconds()),
		ClientIP:  clientIP,
	})
	return Decision{
		Allowed:   false,
		Code:      CodeQuarantined,
		Reason:    reason,
		Threshold: threshold,
	}
}

// SetOnBlock installs (or replaces) the block hook. Idempotent;
// passing nil disables further callbacks. Safe to call after the
// Checker is already serving — assigning a function pointer is a
// single-word write on every architecture Go targets, and the
// Check fast path reads via the same pointer.
func (c *Checker) SetOnBlock(fn OnBlockFn) {
	c.onBlock = fn
}

// recordEvent persists a QuarantineEvent best-effort and fires the
// OnBlock hook (if registered) for ActionBlocked events. Both side-
// effects are protected from each other: a store failure logs a
// warning but does NOT prevent the hook firing, and a hook panic
// does NOT prevent future RecordEvent calls (recovered via the
// deferred recover below).
//
// Failures never escape to the caller — failing a quarantine
// decision because the audit log or a webhook hiccupped would be
// the wrong tradeoff. The audit trail is for after-the-fact review;
// the gating decision happens regardless.
func (c *Checker) recordEvent(ctx context.Context, ev db.QuarantineEvent) {
	// Stamp the event before it reaches the store copy AND the hook —
	// the OnBlock webhook formats ev.CreatedAt, and the zero value
	// renders as year 0001 in every chat channel (v0.8.0 review).
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = c.now()
	}
	if c.store != nil {
		if err := c.store.RecordEvent(ctx, ev); err != nil {
			zap.L().Warn("quarantine: RecordEvent",
				zap.String("ecosystem", ev.Ecosystem),
				zap.String("package", ev.Package),
				zap.String("version", ev.Version),
				zap.String("action", ev.Action),
				zap.Error(err))
		}
	}
	if c.onBlock != nil && (ev.Action == ActionBlocked || ev.Action == ActionMalwareBlocked) {
		// Defer-recover so a panicking webhook implementation can't
		// break the gating decision. The OnBlock hook is meant to be
		// fire-and-forget; misbehaving callers shouldn't be able to
		// cascade into request failures.
		func() {
			defer func() {
				if r := recover(); r != nil {
					zap.L().Error("quarantine: OnBlock hook panicked",
						zap.String("ecosystem", ev.Ecosystem),
						zap.Any("recover", r))
				}
			}()
			c.onBlock(ev)
		}()
	}
}

// formatAge formats a duration in human-readable form suitable for
// error-body / audit-event consumption. "3 days 2 hours" instead of
// "74h12m" because the audience is operators, not Go programmers.
func formatAge(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
