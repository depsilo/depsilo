package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"repocache/internal/db"
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

// JWTAuth returns a middleware that validates JWT tokens or API tokens.
func JWTAuth(secret string, database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := extractToken(c)
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "missing authorization token",
			})
			return
		}

		// Try JWT first
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		})

		if err == nil && token.Valid {
			c.Set(ContextKeyUserID, claims.UserID)
			c.Set(ContextKeyUsername, claims.Username)
			c.Set(ContextKeyRole, claims.Role)
			c.Next()
			return
		}

		// Try API token
		hash := sha256.Sum256([]byte(tokenStr))
		tokenHash := hex.EncodeToString(hash[:])

		var apiToken db.APIToken
		if err := database.Where("token_hash = ?", tokenHash).First(&apiToken).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "invalid or expired token",
			})
			return
		}

		// Check expiry
		if apiToken.ExpiresAt != nil && time.Now().After(*apiToken.ExpiresAt) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "token expired",
			})
			return
		}

		// Update last used
		now := time.Now()
		database.Model(&apiToken).Update("last_used_at", &now)

		// Load user
		var user db.User
		if err := database.First(&user, apiToken.UserID).Error; err != nil || !user.Enabled {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    "UNAUTHORIZED",
				"message": "user disabled or not found",
			})
			return
		}

		c.Set(ContextKeyUserID, user.ID)
		c.Set(ContextKeyUsername, user.Username)
		c.Set(ContextKeyRole, user.Role)
		c.Next()
	}
}

// AdminRequired ensures the user has admin role.
func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get(ContextKeyRole)
		if !exists || role.(string) != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "FORBIDDEN",
				"message": "admin role required",
			})
			return
		}
		c.Next()
	}
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
