package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

type TokenHandler struct {
	db *gorm.DB
}

func NewTokenHandler(database *gorm.DB) *TokenHandler {
	return &TokenHandler{db: database}
}

func (h *TokenHandler) List(c *gin.Context) {
	var tokens []db.APIToken
	if err := h.db.Select("id, user_id, name, permissions, expires_at, last_used_at, created_at").
		Order("datetime(created_at) DESC").Find(&tokens).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to read API tokens"})
		return
	}
	items := make([]contract.APITokenSummary, len(tokens))
	for i, token := range tokens {
		items[i] = contract.APITokenSummary{
			ID: token.ID, UserID: token.UserID, Name: token.Name, Permissions: contract.APITokenPermissions(token.Permissions),
			ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, CreatedAt: token.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, items)
}

type createTokenRequest struct {
	Name        string `json:"name" binding:"required"`
	Permissions string `json:"permissions"`
	TTLDays     int    `json:"ttl_days"` // 0 = never expires
	TTL         string `json:"ttl"`      // "7d" | "30d" | "90d" | "never" (frontend format)
}

func (h *TokenHandler) Create(c *gin.Context) {
	var req createTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	// Required. Checked here rather than by a binding tag so the message lists
	// the allowed values instead of the validator's "failed on the 'oneof' tag".
	if req.Permissions != "readonly" && req.Permissions != "readwrite" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": `permissions must be "readonly" or "readwrite"`})
		return
	}

	// Convert TTL string to TTLDays if provided
	if req.TTL != "" && req.TTLDays == 0 {
		switch req.TTL {
		case "7d":
			req.TTLDays = 7
		case "30d":
			req.TTLDays = 30
		case "90d":
			req.TTLDays = 90
		case "never":
			req.TTLDays = 0
		}
	}

	userID, _ := c.Get(middleware.ContextKeyUserID)

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}
	plainToken := "rc_" + hex.EncodeToString(tokenBytes)

	// Store hash only
	hash := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(hash[:])

	var expiresAt *time.Time
	if req.TTLDays > 0 {
		t := time.Now().AddDate(0, 0, req.TTLDays)
		expiresAt = &t
	}

	apiToken := db.APIToken{
		UserID:      userID.(uint),
		Name:        req.Name,
		TokenHash:   tokenHash,
		Permissions: req.Permissions,
		ExpiresAt:   expiresAt,
	}

	if err := h.db.Create(&apiToken).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to create token"})
		return
	}

	// Return plain token ONCE — it cannot be retrieved again
	c.JSON(http.StatusCreated, contract.CreateAPITokenResponse{
		ID:          apiToken.ID,
		Name:        apiToken.Name,
		Token:       plainToken,
		Permissions: contract.APITokenPermissions(apiToken.Permissions),
		ExpiresAt:   apiToken.ExpiresAt,
		Warning:     "save this token now — it will not be shown again",
	})
}

func (h *TokenHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	result := h.db.Delete(&db.APIToken{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to revoke API token"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "token not found"})
		return
	}
	c.JSON(http.StatusOK, contract.APITokenRevokeResponse{Message: "token revoked"})
}
