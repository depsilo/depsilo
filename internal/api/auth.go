package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

var (
	ErrInitialAdminExists              = errors.New("an administrator already exists")
	ErrExistingAdminCredentialMismatch = errors.New("existing administrator credentials do not match")
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

func (h *AuthHandler) Me(c *gin.Context) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "principal unavailable"})
		return
	}
	c.JSON(http.StatusOK, principal)
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.AuthMethod != middleware.AuthMethodJWT {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "valid JWT required"})
		return
	}
	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, principal.ID, principal.Username, principal.Role, h.cfg.TokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": time.Now().Add(h.cfg.TokenTTL).Unix()})
}

// ValidateInitialAdminCredentials applies the first-run credential policy. A
// long passphrase is accepted without arbitrary composition rules; shorter
// passwords must use at least three character classes.
func ValidateInitialAdminCredentials(username, password string) error {
	if username != strings.TrimSpace(username) {
		return errors.New("administrator username must not start or end with whitespace")
	}
	if len(username) < 3 || len(username) > 64 {
		return errors.New("administrator username must be between 3 and 64 characters")
	}
	for index, character := range username {
		valid := unicode.IsLetter(character) || unicode.IsDigit(character) || character == '.' || character == '_' || character == '-'
		if !valid || (index == 0 && (character == '.' || character == '_' || character == '-')) {
			return errors.New("administrator username may contain letters, numbers, dots, underscores, and hyphens, and must start with a letter or number")
		}
	}
	if !utf8.ValidString(password) {
		return errors.New("administrator password must be valid UTF-8")
	}
	passwordRunes := utf8.RuneCountInString(password)
	if passwordRunes < 12 {
		return errors.New("administrator password must be at least 12 characters")
	}
	if len(password) > 72 {
		return errors.New("administrator password must be at most 72 bytes")
	}
	classes := 0
	hasLower, hasUpper, hasDigit, hasSymbol := false, false, false, false
	for _, character := range password {
		switch {
		case unicode.IsControl(character):
			return errors.New("administrator password must not contain control characters")
		case unicode.IsLower(character):
			hasLower = true
		case unicode.IsUpper(character):
			hasUpper = true
		case unicode.IsDigit(character):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	for _, present := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if present {
			classes++
		}
	}
	if passwordRunes < 20 && classes < 3 {
		return errors.New("administrator password must use at least three character classes or be a passphrase of at least 20 characters")
	}
	lowerPassword := strings.ToLower(password)
	if strings.Contains(lowerPassword, strings.ToLower(username)) {
		return errors.New("administrator password must not contain the username")
	}
	for _, common := range []string{"adminadminadmin", "password123!", "change-me-in-production", "qwerty123456"} {
		if lowerPassword == common {
			return errors.New("administrator password is too common")
		}
	}
	return nil
}

// CreateInitialAdmin creates the first administrator with operator-provided
// credentials. It never overwrites or resets an existing administrator.
func CreateInitialAdmin(database *gorm.DB, username, password string) error {
	if err := ValidateInitialAdminCredentials(username, password); err != nil {
		return err
	}
	var count int64
	if err := database.Model(&db.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if count > 0 {
		return ErrInitialAdminExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash administrator password: %w", err)
	}
	user := db.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         "admin",
		Enabled:      true,
	}
	if err := database.Create(&user).Error; err != nil {
		return fmt.Errorf("create administrator: %w", err)
	}
	return nil
}

// VerifyExistingAdminCredentials authorizes config recovery without silently
// resetting an administrator from a surviving database.
func VerifyExistingAdminCredentials(database *gorm.DB, username, password string) error {
	var user db.User
	if err := database.Where("username = ? AND role = ? AND enabled = ?", username, "admin", true).First(&user).Error; err != nil {
		return ErrExistingAdminCredentialMismatch
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrExistingAdminCredentialMismatch
	}
	return nil
}

// EnsureInitialAdmin bootstraps configured/headless deployments while keeping
// the interactive setup database empty until the wizard supplies credentials.
// Headless installs can provide DEPSILO_ADMIN_USERNAME and
// DEPSILO_ADMIN_PASSWORD; otherwise a random password is printed once.
func EnsureInitialAdmin(database *gorm.DB, setupPending bool) error {
	if setupPending {
		return nil
	}
	var count int64
	if err := database.Model(&db.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if count > 0 {
		return nil
	}

	username := os.Getenv("DEPSILO_ADMIN_USERNAME")
	if username == "" {
		username = "admin"
	}
	password := os.Getenv("DEPSILO_ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		var err error
		password, err = config.NewSecureToken()
		if err != nil {
			return fmt.Errorf("generate initial administrator password: %w", err)
		}
	}
	if err := CreateInitialAdmin(database, username, password); err != nil {
		return err
	}
	if generated {
		zap.L().Warn("created initial administrator with a one-time random password; change it after login",
			zap.String("username", username),
			zap.String("initial_admin_password", password))
	} else {
		zap.L().Info("created initial administrator from environment",
			zap.String("username", username))
	}
	return nil
}

// EnsureDefaultAdmin is retained for source compatibility. New code should
// call EnsureInitialAdmin and explicitly say whether interactive setup is
// pending. It no longer creates the predictable admin/admin credential.
func EnsureDefaultAdmin(database *gorm.DB) {
	if err := EnsureInitialAdmin(database, false); err != nil {
		zap.L().Error("failed to create initial administrator", zap.Error(err))
	}
}
