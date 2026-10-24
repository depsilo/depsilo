package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const ContextKeyPrincipal = "principal"

const (
	AuthMethodJWT      = "jwt"
	AuthMethodAPIToken = "api_token"
)

type Principal struct {
	ID                   uint      `json:"id"`
	Username             string    `json:"username"`
	Role                 string    `json:"role"`
	Enabled              bool      `json:"enabled"`
	AuthMethod           string    `json:"auth_method"`
	TokenPermissions     *string   `json:"token_permissions"`
	CanWrite             bool      `json:"can_write"`
	CredentialVersion    uint64    `json:"-"`
	PermissionSource     string    `json:"permission_source"`
	LoginMethod          string    `json:"login_method"`
	CanManageCredentials bool      `json:"can_manage_credentials"`
	SessionID            string    `json:"-"`
	SessionExpiresAt     time.Time `json:"-"`
	CurrentIdentityID    uint      `json:"current_identity_id,omitempty"`
	CurrentProviderID    string    `json:"current_provider_id,omitempty"`
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

// CredentialManagementRequired keeps durable identity and machine credentials
// under locally authenticated operator control. External login may grant operational
// writes without granting a way to outlive the external authorization window.
func CredentialManagementRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := PrincipalFromContext(c)
		if !ok || !principal.CanWrite || !principal.CanManageCredentials {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "LOCAL_CREDENTIALS_REQUIRED", "message": "sign in with a local administrator credential to manage identities and machine credentials"})
			return
		}
		c.Next()
	}
}
