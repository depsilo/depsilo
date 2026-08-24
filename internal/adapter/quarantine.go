package adapter

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// QuarantineGate reads its checker from the immutable RequestScope attached by
// the owning server, avoiding constructor-threading through every adapter.
// Adapters call QuarantineGate(c, ecosystem, pkg, version) at the top of their
// artifact-fetch handler; the helper returns true=blocked-and-responded
// (handler must return immediately), false=allowed (handler proceeds).
//
// The interface is minimal so the adapter package never imports
// internal/quarantine — that would create a back-edge in the
// dependency graph. The wiring in server.go knows about both.

// QuarantineChecker is the minimal interface the adapter helper
// needs from internal/quarantine. The real *quarantine.Checker
// implements it; tests can supply a fake.
type QuarantineChecker interface {
	Check(ctx context.Context, ecosystem, pkg, version, clientIP string) QuarantineDecision
}

// QuarantineDecision mirrors quarantine.Decision shape — but lives
// here so adapters consume only this package. internal/quarantine
// provides a wrapper that satisfies QuarantineChecker by widening
// its own Decision into this shape.
type QuarantineDecision struct {
	Allowed bool
	// Code distinguishes WHY the request was refused: "QUARANTINED"
	// (too young, retry later or ask for approval) vs
	// "MALICIOUS_BLOCKED" (known malware, do not retry). Empty
	// defaults to QUARANTINED for backward compatibility.
	Code   string
	Reason string
	// Warned is true when the checker observed a policy violation but
	// the configured mode serves anyway. The gate still returns false
	// (proceed) because Allowed is true; Warned is informational for
	// any future response-header / observability work.
	Warned bool
}

// quarantineCheckerSnapshot is the compatibility representation for unscoped
// callers. Scoped production requests never consult this process-global value.
type quarantineCheckerSnapshot struct {
	checker QuarantineChecker
}

var quarantineHooks atomic.Pointer[quarantineCheckerSnapshot]

// InstallQuarantineChecker atomically installs a compatibility checker for
// unscoped callers and returns an idempotent release function. A replaced
// owner's release cannot clear the checker installed by a newer owner.
func InstallQuarantineChecker(checker QuarantineChecker) func() {
	owned := &quarantineCheckerSnapshot{checker: checker}
	quarantineHooks.Store(owned)
	return func() {
		quarantineHooks.CompareAndSwap(owned, nil)
	}
}

// SetQuarantineChecker installs (or replaces) the checker the
// adapter helper uses. Pass nil to disable quarantine entirely
// (useful in tests; the default state is nil so unit tests that
// don't wire one get pass-through behavior).
// Deprecated: production servers should use NewRequestScope.
func SetQuarantineChecker(c QuarantineChecker) {
	if c == nil {
		quarantineHooks.Store(nil)
		return
	}
	quarantineHooks.Store(&quarantineCheckerSnapshot{checker: c})
}

// QuarantineGate evaluates the configured policy for
// (ecosystem, pkg, version) and writes a 451 response when the
// decision is Block. Returns true when the handler must stop
// (response already written), false when the handler should
// proceed with its normal fetch flow.
//
// Semantics:
//   - No checker configured → return false (allow)
//   - Empty package or version → return false (allow; the checker
//     itself logs the inconsistency but adapters shouldn't be
//     forced to differentiate "metadata request" vs "missing
//     version" here — the handler picks which calls actually need
//     gating)
//   - Decision.Allowed → return false
//   - Decision.Blocked → write 451 with the human-readable Reason
//     in the body, return true
//
// 451 (Unavailable For Legal Reasons) is the closest semantic
// fit in standard HTTP statuses — the version is being withheld
// not because the client did anything wrong but because policy
// says so. It's also visible enough in client error output that
// CI logs make the cause obvious.
func QuarantineGate(c *gin.Context, ecosystem, pkg, version string) bool {
	var checker QuarantineChecker
	if hooks := quarantineHooks.Load(); hooks != nil {
		checker = hooks.checker
	}
	if scope, ok := requestScopeFromContext(c.Request.Context()); ok {
		// A scoped nil checker explicitly disables quarantine for this owner; it
		// must never inherit another server's process-wide compatibility hook.
		checker = scope.checker
	}
	if checker == nil {
		return false
	}
	if pkg == "" || version == "" {
		return false
	}
	d := checker.Check(
		c.Request.Context(),
		ecosystem,
		pkg,
		version,
		c.ClientIP(),
	)
	if d.Allowed {
		return false
	}
	code := d.Code
	if code == "" {
		code = "QUARANTINED"
	}
	LogPolicyBlock(
		c.Request.Context(),
		ecosystem,
		pkg,
		version,
		http.StatusUnavailableForLegalReasons,
		c.ClientIP(),
	)
	c.JSON(http.StatusUnavailableForLegalReasons, gin.H{
		"code":      code,
		"message":   d.Reason,
		"ecosystem": ecosystem,
		"package":   pkg,
		"version":   version,
	})
	return true
}
