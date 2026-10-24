package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const (
	ContextKeyUserID   = "user_id"
	ContextKeyUsername = "username"
	ContextKeyRole     = "role"
)

type Claims struct {
	UserID            uint   `json:"user_id"`
	Username          string `json:"username"`
	Role              string `json:"role"`
	CredentialVersion uint64 `json:"credential_version"`
	SessionID         string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

func Authenticate(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractToken(c)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "missing authorization token"})
			return
		}
		if principal, err := resolveJWTPrincipal(secret, database, tokenString); err == nil {
			setPrincipal(c, principal)
			c.Next()
			return
		}
		principal, err := resolveAPITokenPrincipal(database, tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid or expired token"})
			return
		}
		setPrincipal(c, principal)
		c.Next()
	}
}

func JWTOnly(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractToken(c)
		principal, err := resolveJWTPrincipal(secret, database, tokenString)
		if tokenString == "" || err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "valid JWT required"})
			return
		}
		setPrincipal(c, principal)
		c.Next()
	}
}

// OptionalLogoutPrincipal preserves idempotent logout for missing/expired tokens.
// Only a fully authenticated JWT can identify a server session to revoke.
func OptionalLogoutPrincipal(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := extractToken(c); token != "" {
			if principal, err := resolveJWTPrincipal(secret, database, token); err == nil {
				setPrincipal(c, principal)
			}
		}
		c.Next()
	}
}

func resolveJWTPrincipal(secret string, database *gorm.DB, tokenString string) (Principal, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || token == nil || !token.Valid || claims.UserID == 0 {
		return Principal{}, errors.New("invalid JWT")
	}
	var user db.User
	if err := database.First(&user, claims.UserID).Error; err != nil {
		return Principal{}, err
	}
	if !user.Enabled {
		return Principal{}, errors.New("user disabled")
	}
	if user.Role != "admin" && user.Role != "readonly" {
		return Principal{}, errors.New("unsupported user role")
	}
	if claims.CredentialVersion == 0 || user.CredentialVersion == 0 ||
		claims.CredentialVersion != user.CredentialVersion {
		return Principal{}, errors.New("stale JWT credential state")
	}
	source := permissionSource(user)
	if source != "local" && source != "oidc" && source != "ldap" {
		return Principal{}, errors.New("unsupported permission source")
	}
	principal := Principal{
		ID: user.ID, Username: user.Username, Role: user.Role, Enabled: true,
		AuthMethod: AuthMethodJWT, TokenPermissions: nil, CanWrite: user.Role == "admin",
		CredentialVersion: user.CredentialVersion,
		PermissionSource:  source, LoginMethod: "password", CanManageCredentials: user.Role == "admin" && source == "local",
	}
	if claims.SessionID != "" {
		var session db.IdentitySession
		if err := database.Where("id = ? AND user_id = ?", claims.SessionID, user.ID).First(&session).Error; err != nil {
			return Principal{}, errors.New("identity session unavailable")
		}
		if session.RevokedAt != nil || !time.Now().Before(session.ExpiresAt) || session.ConfigFingerprint == "" {
			return Principal{}, errors.New("identity session expired or revoked")
		}
		var identity db.ExternalIdentity
		if err := database.Where("id = ? AND user_id = ? AND provider <> ? AND issuer <> ? AND subject <> ?", session.IdentityID, user.ID, "", "", "").First(&identity).Error; err != nil {
			return Principal{}, errors.New("identity no longer linked")
		}
		protocol := identity.LoginProtocol()
		if source != "local" && source != protocol {
			return Principal{}, errors.New("identity does not match account permission authority")
		}
		role, err := identitySessionRole(user.Role, session.Role)
		if err != nil {
			return Principal{}, err
		}
		principal.Role, principal.CanWrite = role, role == "admin"
		principal.LoginMethod, principal.CanManageCredentials = protocol, false
		principal.SessionID, principal.SessionExpiresAt = session.ID, session.ExpiresAt
		principal.CurrentIdentityID, principal.CurrentProviderID = identity.ID, identity.Provider
		if claims.ExpiresAt.Time.After(session.ExpiresAt) {
			return Principal{}, errors.New("JWT exceeds identity session lifetime")
		}
	} else if source != "local" || user.PasswordHash == "" {
		return Principal{}, errors.New("account requires an identity session")
	}
	if claims.Role != principal.Role {
		return Principal{}, errors.New("stale JWT role")
	}
	return principal, nil
}

func permissionSource(user db.User) string {
	if user.PermissionSource == "" {
		return "local"
	}
	return user.PermissionSource
}

func identitySessionRole(userRole, sessionRole string) (string, error) {
	if (userRole != "admin" && userRole != "readonly") || (sessionRole != "admin" && sessionRole != "readonly") {
		return "", errors.New("unsupported identity session role")
	}
	if userRole == "readonly" || sessionRole == "readonly" {
		return "readonly", nil
	}
	return "admin", nil
}

func resolveAPITokenPrincipal(database *gorm.DB, tokenString string) (Principal, error) {
	digest := sha256.Sum256([]byte(tokenString))
	tokenHash := hex.EncodeToString(digest[:])
	var apiToken db.APIToken
	if err := database.Where("token_hash = ?", tokenHash).First(&apiToken).Error; err != nil {
		return Principal{}, err
	}
	if apiToken.ExpiresAt != nil && time.Now().After(*apiToken.ExpiresAt) {
		return Principal{}, errors.New("token expired")
	}
	if apiToken.Permissions != "readonly" && apiToken.Permissions != "readwrite" {
		return Principal{}, errors.New("unsupported token permissions")
	}
	var user db.User
	if err := database.First(&user, apiToken.UserID).Error; err != nil {
		return Principal{}, err
	}
	if !user.Enabled {
		return Principal{}, errors.New("user disabled")
	}
	if permissionSource(user) != "local" {
		return Principal{}, errors.New("external user cannot use a local API token")
	}
	if user.Role != "admin" && user.Role != "readonly" {
		return Principal{}, errors.New("unsupported user role")
	}
	now := time.Now()
	if err := database.Model(&apiToken).Update("last_used_at", &now).Error; err != nil {
		zap.L().Warn("failed to update API token last_used_at", zap.Uint("token_id", apiToken.ID), zap.Error(err))
	}
	permissions := apiToken.Permissions
	return Principal{
		ID: user.ID, Username: user.Username, Role: user.Role, Enabled: true,
		AuthMethod: AuthMethodAPIToken, TokenPermissions: &permissions,
		CanWrite:          user.Role == "admin" && permissions == "readwrite",
		CredentialVersion: user.CredentialVersion,
		PermissionSource:  "local", LoginMethod: "api_token",
		CanManageCredentials: user.Role == "admin" && permissions == "readwrite",
	}, nil
}

// GenerateJWT creates a new JWT token for a user.
func GenerateJWT(secret string, user db.User, ttl time.Duration) (string, error) {
	if permissionSource(user) != "local" {
		return "", errors.New("external users require an identity session")
	}
	if user.CredentialVersion == 0 {
		return "", errors.New("user credential version is not initialized")
	}
	claims := Claims{
		UserID:            user.ID,
		Username:          user.Username,
		Role:              user.Role,
		CredentialVersion: user.CredentialVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		zap.L().Error("failed to sign JWT", zap.Error(err))
		return "", err
	}
	return signed, nil
}

// GenerateIdentityJWT binds a bearer JWT to an independently revocable session.
// Refreshing this JWT can never extend the provider authorization's lifetime.
func GenerateIdentityJWT(secret string, user db.User, session db.IdentitySession, ttl time.Duration) (string, error) {
	now := time.Now()
	if user.CredentialVersion == 0 || session.ID == "" || session.UserID != user.ID || session.IdentityID == 0 || session.RevokedAt != nil || !now.Before(session.ExpiresAt) || ttl <= 0 {
		return "", errors.New("invalid identity session")
	}
	role, err := identitySessionRole(user.Role, session.Role)
	if err != nil {
		return "", err
	}
	expiresAt := now.Add(ttl)
	if session.ExpiresAt.Before(expiresAt) {
		expiresAt = session.ExpiresAt
	}
	claims := Claims{
		UserID: user.ID, Username: user.Username, Role: role, CredentialVersion: user.CredentialVersion, SessionID: session.ID,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expiresAt), IssuedAt: jwt.NewNumericDate(now)},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func extractToken(c *gin.Context) string {
	// Authorization: Bearer <token>
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
