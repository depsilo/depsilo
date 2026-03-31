package license

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequirePro returns a Gin middleware that blocks requests unless a valid Pro license is active.
func RequirePro(mgr *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !mgr.IsPro() {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code":    "PRO_REQUIRED",
				"message": "This feature requires Depsilo Pro.",
				"upgrade": "https://depsilo.com/#pricing",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
