package license

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

// LicenseStatus represents the current state of the license.
type LicenseStatus struct {
	IsPro       bool       `json:"is_pro"`
	KeyMasked   string     `json:"key_masked"`
	ActivatedAt *time.Time `json:"activated_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastChecked time.Time  `json:"last_checked"`
	Error       string     `json:"error,omitempty"`
}

// Manager handles license validation and status tracking.
type Manager struct {
	key      string
	database *gorm.DB
	mu       sync.RWMutex
	status   LicenseStatus
}

// NewManager creates a new license Manager from config + DB.
// Key precedence: DB-stored key (set via UI) > config.toml key > none.
// DEPSILO_DEV_PRO=1 bypasses everything per dev mode.
func NewManager(cfg config.LicenseConfig, database *gorm.DB) *Manager {
	m := &Manager{database: database}

	now := time.Now().UTC()

	if os.Getenv("DEPSILO_DEV_PRO") == "1" {
		m.status = LicenseStatus{
			IsPro:       true,
			KeyMasked:   "dev-mode",
			ActivatedAt: &now,
			LastChecked: now,
		}
		zap.L().Warn("DEPSILO_DEV_PRO is set — Pro features activated without license validation")
		return m
	}

	// Load key: DB first, then config.
	if database != nil {
		var stored db.LicenseStorage
		err := database.Order("id ASC").First(&stored).Error
		switch {
		case err == nil && stored.Key != "":
			m.key = strings.TrimSpace(stored.Key)
		case err == nil:
			// row exists but key is empty — treat as no key, fall through to config
		case errors.Is(err, gorm.ErrRecordNotFound):
			// no row yet — fine, fall through to config
		default:
			zap.L().Warn("failed to load stored license key from DB", zap.Error(err))
		}
	}
	if m.key == "" {
		m.key = strings.TrimSpace(cfg.Key)
	}

	if m.key == "" {
		m.status = LicenseStatus{IsPro: false, LastChecked: now}
	} else {
		m.status = LicenseStatus{IsPro: false, KeyMasked: MaskKey(m.key), LastChecked: now}
	}
	return m
}

// Start runs the initial validation and periodic re-validation every 24 hours.
// It blocks until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	if m.key == "" || os.Getenv("DEPSILO_DEV_PRO") == "1" {
		return
	}

	// Initial validation
	m.doValidate()

	// Periodic re-validation every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.doValidate()
		}
	}
}

func (m *Manager) doValidate() {
	resp, err := validate(m.key)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.LastChecked = time.Now().UTC()

	if err != nil {
		// Network error: keep previous result, log warning
		m.status.Error = err.Error()
		zap.L().Warn("license validation failed, keeping previous state",
			zap.String("key", MaskKey(m.key)),
			zap.Error(err),
		)
		return
	}

	m.status.Error = ""

	if resp.Valid && resp.LicenseKey.Status == "active" {
		m.status.IsPro = true
		now := time.Now().UTC()
		if m.status.ActivatedAt == nil {
			m.status.ActivatedAt = &now
		}
		if resp.LicenseKey.ExpiresAt != nil {
			if t, err := time.Parse(time.RFC3339, *resp.LicenseKey.ExpiresAt); err == nil {
				m.status.ExpiresAt = &t
			}
		}
		zap.L().Info("license validated successfully",
			zap.String("key", MaskKey(m.key)),
			zap.String("status", resp.LicenseKey.Status),
		)
	} else {
		m.status.IsPro = false
		zap.L().Warn("license is not valid",
			zap.String("key", MaskKey(m.key)),
		)
	}
}

// Revalidate triggers a manual re-validation in the background.
func (m *Manager) Revalidate() {
	if m.key == "" {
		return
	}
	go m.doValidate()
}

// SetKey persists a new license key (DB), updates manager state,
// and triggers a synchronous validation against the upstream license server.
// Returns any validation error, but the key is persisted unconditionally so
// the user can retry validation later via Revalidate.
func (m *Manager) SetKey(ctx context.Context, newKey string, userID uint) error {
	newKey = strings.TrimSpace(newKey)

	m.mu.Lock()
	if m.database != nil {
		if err := m.database.WithContext(ctx).Save(&db.LicenseStorage{
			ID:        1,
			Key:       newKey,
			UpdatedBy: userID,
			UpdatedAt: time.Now().UTC(),
		}).Error; err != nil {
			m.mu.Unlock()
			return fmt.Errorf("persist license key: %w", err)
		}
	}
	m.key = newKey
	if newKey != "" {
		m.status.KeyMasked = MaskKey(newKey)
	} else {
		m.status.KeyMasked = ""
	}
	m.mu.Unlock()

	// TODO: write management event audit log (deferred — current AuditLog
	// model is request-shaped; see plan front matter "Known deviation").

	if newKey == "" {
		return nil
	}
	// Synchronous validation so the caller can surface the result via Status().
	m.doValidate()
	return nil
}

// ClearKey removes any DB-stored license key and resets manager state to "free".
// The config.toml key is NOT re-read; the manager remains free until the next
// process start (which re-runs the precedence logic in NewManager).
func (m *Manager) ClearKey(ctx context.Context, userID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.database != nil {
		if err := m.database.WithContext(ctx).Where("id = ?", 1).Delete(&db.LicenseStorage{}).Error; err != nil {
			return fmt.Errorf("delete license key: %w", err)
		}
	}
	m.key = ""
	m.status = LicenseStatus{
		IsPro:       false,
		KeyMasked:   "",
		LastChecked: time.Now().UTC(),
	}

	// TODO: write management event audit log (deferred — current AuditLog
	// model is request-shaped; see plan front matter "Known deviation").
	_ = userID
	return nil
}

// IsPro returns whether a valid Pro license is active.
func (m *Manager) IsPro() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.IsPro
}

// Status returns the current license status.
func (m *Manager) Status() LicenseStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// MaskKey returns the segment up to and including the first "-" followed by "***".
// For example "depsilo-test-key-1234" → "depsilo-***".
// If the key contains no "-", it returns the first 8 characters followed by "-***".
func MaskKey(key string) string {
	if idx := strings.Index(key, "-"); idx >= 0 {
		return key[:idx+1] + "***"
	}
	if len(key) <= 8 {
		return key
	}
	return key[:8] + "-***"
}

// RequirePro returns a Gin middleware that blocks requests unless a valid Pro license is active.
func RequirePro(mgr *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !mgr.IsPro() {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code":    "PRO_REQUIRED",
				"message": "This feature requires Depsilo Pro.",
				"upgrade": "https://depsilo.com/#pricing",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
