package rules

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/adapter"
	"depsilo/internal/db"
)

// Middleware returns a Gin middleware that checks package rules before proxying.
// Extra PyPI-compatible routes must be supplied as validated, configuration-
// owned descriptors; omitting them preserves the original constructor contract.
func Middleware(engine *Engine, extraPyPIRoutes ...PyPIRouteDescriptor) gin.HandlerFunc {
	routes := append([]PyPIRouteDescriptor(nil), extraPyPIRoutes...)
	return func(c *gin.Context) {
		target, ok := extractRequestTarget(c.Request.URL.Path, routes...)
		if !ok {
			c.Next()
			return
		}
		var allowed bool
		var rule *db.PackageRule
		var err error
		if target.AmbiguousArtifact {
			allowed, rule, err = engine.CheckIncompleteArtifact(
				c.Request.Context(), target.Ecosystem,
			)
		} else {
			allowed, rule, err = engine.Check(
				c.Request.Context(),
				target.Ecosystem,
				target.PackageName,
				target.Version,
			)
		}
		if err != nil {
			if isUnsafePolicyError(err) {
				abortPolicyUnevaluable(c)
				return
			}
			// Store.List classifies availability failures and Engine resolves them
			// through the configured last-known-good/fallback policy before this
			// point. Keep a defensive branch for cancellation or an unexpected
			// implementation error, but make the decision explicit and observable.
			if errors.Is(err, ErrRuleStoreUnavailable) {
				if engine.fallbackAllowsWithoutSnapshot() {
					zap.L().Error("package policy evaluation failed; applying configured fallback", zap.Error(err), zap.String("on_load_error", string(engine.OnLoadErrorPolicy())))
					c.Next()
					return
				}
				zap.L().Error("package policy evaluation failed; denying request", zap.Error(err), zap.String("on_load_error", string(engine.OnLoadErrorPolicy())))
				abortPolicyUnavailable(c)
				return
			}
			// A client cancellation is not a policy decision; let Gin unwind the
			// request normally. It must not, however, turn an explicitly denied
			// store-unavailable result into an allow (those are handled above).
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				c.Next()
				return
			}
			zap.L().Error("package policy evaluation failed; denying request", zap.Error(err), zap.String("on_load_error", string(engine.OnLoadErrorPolicy())))
			abortPolicyUnavailable(c)
			return
		}
		if !allowed {
			reason := "Package blocked by policy"
			if rule != nil && rule.Reason != "" {
				reason = rule.Reason
			}
			adapter.LogPolicyBlock(
				c.Request.Context(),
				target.Ecosystem,
				target.PackageName,
				target.Version,
				http.StatusForbidden,
				c.ClientIP(),
			)
			c.JSON(http.StatusForbidden, gin.H{
				"code":    "PACKAGE_DENIED",
				"message": reason,
				"package": target.PackageName,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func isUnsafePolicyError(err error) bool {
	// Keep the lower-level store sentinel in this guard as well. Engine refresh
	// paths wrap it as ErrPolicyIntegrity, but retaining the raw classification
	// here protects alternate/test stores and future call sites from accidentally
	// treating malformed policy data as an availability outage.
	return errors.Is(err, ErrPolicyIntegrity) ||
		errors.Is(err, ErrRuleDataIntegrity) ||
		errors.Is(err, ErrPolicyEvaluation)
}

func abortPolicyUnevaluable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"code":    "PACKAGE_POLICY_UNEVALUABLE",
		"message": "Package policy could not be evaluated safely",
	})
	c.Abort()
}

func abortPolicyUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"code":    "PACKAGE_POLICY_UNAVAILABLE",
		"message": "Package policy is unavailable and the configured fallback denies this request",
	})
	c.Abort()
}
