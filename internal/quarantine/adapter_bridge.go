package quarantine

import (
	"context"

	"depsilo/internal/adapter"
)

// AdapterChecker is the bridge between *Checker and the
// adapter.QuarantineChecker interface. It lets server.go bind the checker to
// an adapter.RequestScope without making the adapter package
// import internal/quarantine (which would create a cycle:
// adapter → quarantine → db ← adapter via models).
//
// Wrap returns a value usable directly with adapter.NewRequestScope.
type AdapterChecker struct {
	inner *Checker
}

func Wrap(c *Checker) adapter.QuarantineChecker {
	return AdapterChecker{inner: c}
}

func (a AdapterChecker) Check(ctx context.Context, ecosystem, pkg, version, clientIP string) adapter.QuarantineDecision {
	if a.inner == nil {
		return adapter.QuarantineDecision{Allowed: true}
	}
	d := a.inner.Check(ctx, ecosystem, pkg, version, clientIP)
	return adapter.QuarantineDecision{
		Allowed: d.Allowed,
		Code:    d.Code,
		Reason:  d.Reason,
		Warned:  d.Warned,
	}
}
