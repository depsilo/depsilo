package rules

import (
	"context"
	"errors"

	"depsilo/internal/adapter"
	"go.uber.org/zap"
)

// AdapterChecker maps the rules Engine's rich result and error taxonomy onto
// the small interface consumed by protocol Adapters. Classified transient
// storage errors are resolved by Engine using its last-known-good snapshot or
// explicit fallback mode; only unexpected errors reach this defensive branch.
type AdapterChecker struct {
	inner *Engine
}

// Wrap returns a package-rule checker suitable for adapter.NewRequestScope.
func Wrap(engine *Engine) adapter.PackageRuleChecker {
	return AdapterChecker{inner: engine}
}

func (checker AdapterChecker) EvaluatePackageRule(
	ctx context.Context,
	ecosystem, packageName, version string,
) adapter.PackageRuleDecision {
	if checker.inner == nil {
		return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow}
	}
	allowed, matched, err := checker.inner.Check(ctx, ecosystem, packageName, version)
	if err != nil {
		if isUnsafePolicyError(err) {
			return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleUnevaluable}
		}
		if errors.Is(err, ErrRuleStoreUnavailable) {
			if checker.inner.fallbackAllowsWithoutSnapshot() {
				zap.L().Error("package policy adapter evaluation failed; applying configured fallback", zap.Error(err), zap.String("on_load_error", string(checker.inner.OnLoadErrorPolicy())))
				return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow}
			}
			zap.L().Error("package policy adapter evaluation failed; denying", zap.Error(err), zap.String("on_load_error", string(checker.inner.OnLoadErrorPolicy())))
			return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleDeny, Reason: "Package policy unavailable"}
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow}
		}
		zap.L().Error("package policy adapter evaluation failed; denying", zap.Error(err), zap.String("on_load_error", string(checker.inner.OnLoadErrorPolicy())))
		return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleDeny, Reason: "Package policy unavailable"}
	}
	if allowed {
		return adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow}
	}
	reason := ""
	if matched != nil {
		reason = matched.Reason
	}
	return adapter.PackageRuleDecision{
		Outcome: adapter.PackageRuleDeny,
		Reason:  reason,
	}
}
