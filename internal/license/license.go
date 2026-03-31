package license

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/config"
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
	key    string
	mu     sync.RWMutex
	status LicenseStatus
}

// NewManager creates a new license Manager from the given config.
// Special key "dev-pro-test" activates Pro without API validation (for testing only).
func NewManager(cfg config.LicenseConfig) *Manager {
	m := &Manager{key: strings.TrimSpace(cfg.Key)}

	now := time.Now()

	if m.key == "dev-pro-test" {
		m.status = LicenseStatus{
			IsPro:       true,
			KeyMasked:   "dev-pro-***",
			ActivatedAt: &now,
			LastChecked: now,
		}
		zap.L().Warn("using dev-pro-test license key — for testing only, not for production")
		return m
	}

	if m.key == "" {
		m.status = LicenseStatus{
			IsPro:       false,
			KeyMasked:   "",
			LastChecked: now,
		}
	} else {
		m.status = LicenseStatus{
			IsPro:       false,
			KeyMasked:   MaskKey(m.key),
			LastChecked: now,
		}
	}

	return m
}

// Start runs the initial validation and periodic re-validation every 24 hours.
// It blocks until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	if m.key == "" || m.key == "dev-pro-test" {
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

	m.status.LastChecked = time.Now()

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
		now := time.Now()
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

// MaskKey returns the first 8 characters of the key followed by "-***".
func MaskKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:8] + "-***"
}
