package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/credential"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

var (
	ErrInitialAdminExists              = errors.New("an administrator already exists")
	ErrExistingAdminCredentialMismatch = errors.New("existing administrator credentials do not match")
)

type AuthHandler struct {
	db           *gorm.DB
	cfg          config.AuthConfig
	loginLimiter *loginAttemptLimiter
}

func NewAuthHandler(database *gorm.DB, cfg config.AuthConfig) *AuthHandler {
	return &AuthHandler{db: database, cfg: cfg, loginLimiter: newLoginAttemptLimiter()}
}

const (
	loginAttemptLimit       = 5
	loginAttemptWindow      = time.Minute
	maxTrackedLoginAttempts = 4096
	maxLoginRequestBody     = 8 << 10
)

type loginAttemptKey [sha256.Size]byte

type loginAttemptWindowState struct {
	attempts int
	resetAt  time.Time
}

// loginAttemptLimiter bounds both work per credential tuple and its own
// memory. It has no cleanup goroutine: the first request at an expiry boundary
// reclaims stale entries, while a full table rejects new tuples rather than
// evicting an active block and letting an attacker bypass it by key churn.
type loginAttemptLimiter struct {
	mu         sync.Mutex
	attempts   map[loginAttemptKey]loginAttemptWindowState
	nextExpiry time.Time
}

func newLoginAttemptLimiter() *loginAttemptLimiter {
	return &loginAttemptLimiter{attempts: make(map[loginAttemptKey]loginAttemptWindowState)}
}

func makeLoginAttemptKey(clientIP, username string) loginAttemptKey {
	identity := strings.TrimSpace(clientIP) + "\x00" + strings.ToLower(strings.TrimSpace(username))
	return sha256.Sum256([]byte(identity))
}

func (l *loginAttemptLimiter) reserve(clientIP, username string) (bool, time.Duration) {
	now := time.Now()
	key := makeLoginAttemptKey(clientIP, username)

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.nextExpiry.IsZero() && !now.Before(l.nextExpiry) {
		l.pruneExpiredLocked(now)
	}
	if state, ok := l.attempts[key]; ok {
		if state.attempts >= loginAttemptLimit {
			return false, positiveRetryDelay(state.resetAt.Sub(now))
		}
		state.attempts++
		l.attempts[key] = state
		return true, 0
	}
	if len(l.attempts) >= maxTrackedLoginAttempts {
		retry := loginAttemptWindow
		if !l.nextExpiry.IsZero() {
			retry = positiveRetryDelay(l.nextExpiry.Sub(now))
		}
		return false, retry
	}

	resetAt := now.Add(loginAttemptWindow)
	l.attempts[key] = loginAttemptWindowState{attempts: 1, resetAt: resetAt}
	if l.nextExpiry.IsZero() || resetAt.Before(l.nextExpiry) {
		l.nextExpiry = resetAt
	}
	return true, 0
}

func (l *loginAttemptLimiter) clear(clientIP, username string) {
	key := makeLoginAttemptKey(clientIP, username)
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (l *loginAttemptLimiter) pruneExpiredLocked(now time.Time) {
	nextExpiry := time.Time{}
	for key, state := range l.attempts {
		if !now.Before(state.resetAt) {
			delete(l.attempts, key)
			continue
		}
		if nextExpiry.IsZero() || state.resetAt.Before(nextExpiry) {
			nextExpiry = state.resetAt
		}
	}
	l.nextExpiry = nextExpiry
}

func positiveRetryDelay(delay time.Duration) time.Duration {
	if delay < time.Second {
		return time.Second
	}
	return delay
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLoginRequestBody)
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "REQUEST_TOO_LARGE", "message": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid request body"})
		return
	}
	clientIP := c.ClientIP()
	allowed, retryAfter := h.loginLimiter.reserve(clientIP, req.Username)
	if !allowed {
		retrySeconds := int64((retryAfter + time.Second - 1) / time.Second)
		c.Header("Retry-After", strconv.FormatInt(retrySeconds, 10))
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "RATE_LIMITED", "message": "too many login attempts; try again later"})
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
	h.loginLimiter.clear(clientIP, req.Username)

	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, user, h.cfg.TokenTTL)
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
	token, err := middleware.GenerateJWT(h.cfg.JWTSecret, db.User{
		ID: principal.ID, Username: principal.Username, Role: principal.Role,
		CredentialVersion: principal.CredentialVersion,
	}, h.cfg.TokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "failed to generate token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": time.Now().Add(h.cfg.TokenTTL).Unix()})
}

// ValidateInitialAdminCredentials combines the stricter first-run username
// rules with the shared interactive credential policy.
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
	return credential.CredentialPolicy.ValidatePassword(username, password)
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
		Username:          username,
		PasswordHash:      string(hash),
		Role:              "admin",
		Enabled:           true,
		CredentialVersion: db.InitialCredentialVersion,
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
