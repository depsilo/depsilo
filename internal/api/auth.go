package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depslio/internal/config"
	"depslio/internal/db"
	"depslio/internal/middleware"
)

type AuthHandler struct {
	db  *gorm.DB
	cfg config.AuthConfig
}

func NewAuthHandler(database *gorm.DB, cfg config.AuthConfig) *AuthHandler {
	return &AuthHandler{db: database, cfg: cfg}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	User      struct {
		ID       uint   `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	} `json:"user"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid request body"})
		return
	}

	var user db.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid credentials"})
		return
	}

	if !user.Enabled {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "user disabled"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid credentials"})
		return
	}

	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, user.ID, user.Username, user.Role, h.cfg.TokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}

	// Update last login
	now := time.Now()
	h.db.Model(&user).Update("last_login_at", &now)

	resp := loginResponse{
		Token:     token,
		ExpiresAt: time.Now().Add(h.cfg.TokenTTL).Unix(),
	}
	resp.User.ID = user.ID
	resp.User.Username = user.Username
	resp.User.Role = user.Role

	zap.L().Info("user logged in", zap.String("username", user.Username))
	c.JSON(http.StatusOK, resp)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	// JWT is stateless; client should discard the token
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	userID, _ := c.Get(middleware.ContextKeyUserID)
	username, _ := c.Get(middleware.ContextKeyUsername)
	role, _ := c.Get(middleware.ContextKeyRole)

	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, userID.(uint), username.(string), role.(string), h.cfg.TokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":      token,
		"expires_at": time.Now().Add(h.cfg.TokenTTL).Unix(),
	})
}

// EnsureDefaultAdmin creates a default admin user if no admin exists.
func EnsureDefaultAdmin(database *gorm.DB) {
	var count int64
	database.Model(&db.User{}).Where("role = ?", "admin").Count(&count)
	if count > 0 {
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		zap.L().Fatal("failed to hash default password", zap.Error(err))
	}

	user := db.User{
		Username:     "admin",
		PasswordHash: string(hash),
		Role:         "admin",
		Enabled:      true,
	}
	if err := database.Create(&user).Error; err != nil {
		zap.L().Fatal("failed to create default admin", zap.Error(err))
	}
	zap.L().Warn("created default admin user (username: admin, password: admin) — CHANGE THE PASSWORD IMMEDIATELY via the admin dashboard")
}
