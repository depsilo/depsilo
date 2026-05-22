package entitlement

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequirePro returns a Gin middleware that blocks requests when no
// entitlement source grants Pro access. The 402 response body includes
// `trial_available` so the frontend can offer an in-paywall trial CTA.
func RequirePro(checker *Checker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if checker.IsPro() {
			c.Next()
			return
		}
		trialAvailable := false
		if checker.trial != nil {
			trialAvailable = !checker.trial.IsUsed()
		}
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code":            "PRO_REQUIRED",
			"message":         "This feature requires Depsilo Pro.",
			"upgrade":         "https://depsilo.com/#pricing",
			"trial_available": trialAvailable,
		})
		c.Abort()
	}
}
