package adapter

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// PackageRuleOutcome is the complete result an Adapter needs from package
// policy. Storage-error classification and rule precedence remain hidden
// behind PackageRuleChecker in the rules module.
type PackageRuleOutcome uint8

const (
	// PackageRuleAllow is also the safe zero value: a missing/transient policy
	// backend must not turn the proxy into a package outage.
	PackageRuleAllow PackageRuleOutcome = iota
	PackageRuleDeny
	PackageRuleUnevaluable
)

// PackageRuleDecision is the adapter-facing policy result. Reason is used only
// for an explicit deny; unsafe evaluation deliberately returns a stable public
// message instead of exposing persisted policy details.
type PackageRuleDecision struct {
	Outcome PackageRuleOutcome
	Reason  string
}

// PackageRuleChecker is the narrow seam implemented by internal/rules. An
// Adapter calls it only after establishing an authoritative package/version
// coordinate for the artifact request.
type PackageRuleChecker interface {
	EvaluatePackageRule(
		ctx context.Context,
		ecosystem, packageName, version string,
	) PackageRuleDecision
}

// PackageRuleGate evaluates an adapter-proven artifact coordinate and writes
// the authoritative HTTP response when policy blocks or cannot be evaluated
// safely. It returns true when the Adapter must stop processing the request.
func PackageRuleGate(c *gin.Context, ecosystem, packageName, version string) bool {
	if c == nil || c.Request == nil || ecosystem == "" || packageName == "" || version == "" {
		return false
	}
	scope, ok := requestScopeFromContext(c.Request.Context())
	if !ok || scope.packageRules == nil {
		return false
	}

	decision := scope.packageRules.EvaluatePackageRule(
		c.Request.Context(), ecosystem, packageName, version,
	)
	switch decision.Outcome {
	case PackageRuleAllow:
		return false
	case PackageRuleDeny:
		reason := decision.Reason
		if reason == "" {
			reason = "Package blocked by policy"
		}
		LogPolicyBlock(
			c.Request.Context(), ecosystem, packageName, version,
			http.StatusForbidden, c.ClientIP(),
		)
		c.JSON(http.StatusForbidden, gin.H{
			"code":    "PACKAGE_DENIED",
			"message": reason,
			"package": packageName,
			"version": version,
		})
		c.Abort()
		return true
	case PackageRuleUnevaluable:
		fallthrough
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    "PACKAGE_POLICY_UNEVALUABLE",
			"message": "Package policy could not be evaluated safely",
		})
		c.Abort()
		return true
	}
}
