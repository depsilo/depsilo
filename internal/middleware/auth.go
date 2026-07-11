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
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
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
	return Principal{
		ID: user.ID, Username: user.Username, Role: user.Role, Enabled: true,
		AuthMethod: AuthMethodJWT, TokenPermissions: nil, CanWrite: user.Role == "admin",
	}, nil
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
		CanWrite: user.Role == "admin" && permissions == "readwrite",
	}, nil
}

// GenerateJWT creates a new JWT token for a user.
func GenerateJWT(secret string, userID uint, username, role string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
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

func extractToken(c *gin.Context) string {
	// Authorization: Bearer <token>
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Query param fallback
	if t := c.Query("token"); t != "" {
		return t
	}
	return ""
}
