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

	"repocache/internal/db"
	"repocache/internal/middleware"
)

type TokenHandler struct {
	db *gorm.DB
}

func NewTokenHandler(database *gorm.DB) *TokenHandler {
	return &TokenHandler{db: database}
}

func (h *TokenHandler) List(c *gin.Context) {
	var tokens []db.APIToken
	h.db.Select("id, user_id, name, permissions, expires_at, last_used_at, created_at").
		Order("created_at DESC").Find(&tokens)
	c.JSON(http.StatusOK, tokens)
}

type createTokenRequest struct {
	Name        string `json:"name" binding:"required"`
	Permissions string `json:"permissions" binding:"required,oneof=readonly readwrite"`
	TTLDays     int    `json:"ttl_days"` // 0 = never expires
}

func (h *TokenHandler) Create(c *gin.Context) {
	var req createTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
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
	c.JSON(http.StatusCreated, gin.H{
		"id":          apiToken.ID,
		"name":        apiToken.Name,
		"token":       plainToken,
		"permissions": apiToken.Permissions,
		"expires_at":  apiToken.ExpiresAt,
		"warning":     "save this token now — it will not be shown again",
	})
}

func (h *TokenHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	result := h.db.Delete(&db.APIToken{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "token revoked"})
}
