package adapter

import (
	"context"
	"net/http"
	"time"

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
	// "MALICIOUS_BLOCKED" (known malware, do not retry). A checker must
	// set it whenever Allowed is false; the gate sends it verbatim.
	Code   string
	Reason string
	// Warned is true when the checker observed a policy violation but
	// the configured mode serves anyway. The gate still returns false
	// (proceed) because Allowed is true; Warned is informational for
	// any future response-header / observability work.
	Warned bool
}

// QuarantineProvenance is source-bound release evidence an adapter can present
// when it has already authenticated the artifact coordinate. SourceID names
// the exact upstream that will serve the bytes; PublishAt is the version's
// publish time taken from that same source's metadata document. A zero
// PublishAt means the metadata carried no usable timestamp and an enabled gate
// must fail closed.
type QuarantineProvenance struct {
	SourceID  string
	PublishAt time.Time
	// Kind is empty for a registry publish time and
	// quarantine.ProvenanceKindFirstObserved for first-observation age.
	Kind string
}

// provenanceQuarantineChecker is the optional capability a checker implements
// when it can consume source-bound evidence instead of a registry lookup.
type provenanceQuarantineChecker interface {
	CheckWithProvenance(
		ctx context.Context,
		ecosystem, pkg, version, clientIP string,
		provenance QuarantineProvenance,
	) QuarantineDecision
}

// QuarantineGate evaluates the configured policy for
// (ecosystem, pkg, version) and writes a 451 response when the
// decision is Block. Returns true when the handler must stop
// (response already written), false when the handler should
// proceed with its normal fetch flow.
//
// Semantics:
//   - No checker configured → return false (allow). The checker comes only
//     from the request's RequestScope; a request without one (an isolated
//     handler test, or an in-process call that bypasses the server's scope
//     wrapper) is never quarantine-checked, same as a scoped nil checker.
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
	return quarantineGate(c, ecosystem, pkg, version, nil)
}

// QuarantineGateWithProvenance evaluates the policy with source-bound release
// evidence. Checkers that do not implement the provenance path fall back to
// the coordinate-only check so existing test doubles keep working.
func QuarantineGateWithProvenance(
	c *gin.Context,
	ecosystem, pkg, version string,
	provenance QuarantineProvenance,
) bool {
	return quarantineGate(c, ecosystem, pkg, version, &provenance)
}

func quarantineGate(
	c *gin.Context,
	ecosystem, pkg, version string,
	provenance *QuarantineProvenance,
) bool {
	var checker QuarantineChecker
	if scope, ok := requestScopeFromContext(c.Request.Context()); ok {
		checker = scope.checker
	}
	if checker == nil {
		return false
	}
	if pkg == "" || version == "" {
		return false
	}
	var d QuarantineDecision
	if provenance != nil {
		if provenanceChecker, ok := checker.(provenanceQuarantineChecker); ok {
			d = provenanceChecker.CheckWithProvenance(
				c.Request.Context(),
				ecosystem,
				pkg,
				version,
				c.ClientIP(),
				*provenance,
			)
		} else {
			d = checker.Check(c.Request.Context(), ecosystem, pkg, version, c.ClientIP())
		}
	} else {
		d = checker.Check(
			c.Request.Context(),
			ecosystem,
			pkg,
			version,
			c.ClientIP(),
		)
	}
	if d.Allowed {
		return false
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
		"code":      d.Code,
		"message":   d.Reason,
		"ecosystem": ecosystem,
		"package":   pkg,
		"version":   version,
	})
	return true
}
