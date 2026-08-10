package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const ContextKeyPrincipal = "principal"

const (
	AuthMethodJWT      = "jwt"
	AuthMethodAPIToken = "api_token"
)

type Principal struct {
	ID                uint    `json:"id"`
	Username          string  `json:"username"`
	Role              string  `json:"role"`
	Enabled           bool    `json:"enabled"`
	AuthMethod        string  `json:"auth_method"`
	TokenPermissions  *string `json:"token_permissions"`
	CanWrite          bool    `json:"can_write"`
	CredentialVersion uint64  `json:"-"`
}

func PrincipalFromContext(c *gin.Context) (Principal, bool) {
	value, exists := c.Get(ContextKeyPrincipal)
	if !exists {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	return principal, ok
}

func setPrincipal(c *gin.Context, principal Principal) {
	c.Set(ContextKeyPrincipal, principal)
	c.Set(ContextKeyUserID, principal.ID)
	c.Set(ContextKeyUsername, principal.Username)
	c.Set(ContextKeyRole, principal.Role)
}

func ReadRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || (principal.Role != "admin" && principal.Role != "readonly") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "read capability required"})
			return
		}
		c.Next()
	}
}

func WriteRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || !principal.CanWrite {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write capability required"})
			return
		}
		c.Next()
	}
}
