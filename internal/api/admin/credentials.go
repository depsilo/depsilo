package admin

import (
	"github.com/gin-gonic/gin"

	"depsilo/internal/api/credentialurl"
	"depsilo/internal/middleware"
)

func principalCanViewCredentials(c *gin.Context) bool {
	principal, ok := middleware.PrincipalFromContext(c)
	return ok && principal.CanWrite
}

func maskWebhookURL(raw string) string {
	return credentialurl.Mask(raw)
}

func maskCredentialURL(raw string) string {
	if raw == "" {
		return raw
	}
	return credentialurl.Mask(raw)
}
