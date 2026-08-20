package rules

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
)

// Middleware returns a Gin middleware that checks package rules before proxying.
func Middleware(engine *Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := extractRequestTarget(c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}

		allowed, rule, err := engine.Check(
			c.Request.Context(),
			target.Ecosystem,
			target.PackageName,
			target.Version,
		)
		if err != nil {
			// Policy storage is deliberately fail-open: an unavailable rules
			// database must not turn the proxy into a package outage.
			c.Next()
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
