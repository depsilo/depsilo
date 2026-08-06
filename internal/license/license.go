package license

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/audit"
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

// licenseStorageSingletonID is the fixed ID used for the singleton LicenseStorage
// row. The DB schema does not enforce singleton uniqueness — the manager layer
// guarantees only this ID is ever read/written.
const licenseStorageSingletonID uint = 1

// Manager handles license validation and status tracking.
type Manager struct {
	key          string
	database     *gorm.DB
	tasks        asyncruntime.Submitter
	mu           sync.RWMutex
	status       LicenseStatus
	revalidating bool
}

var (
	ErrNoLicenseKey        = errors.New("no license key configured")
	ErrRevalidationRunning = errors.New("license revalidation already running")
	ErrRevalidationClosed  = errors.New("license revalidation is unavailable")
)

// NewManager creates a new license Manager from config + DB.
// Key precedence: DB-stored key (set via UI) > config.toml key > none.
// DEPSILO_DEV_PRO=1 bypasses everything per dev mode.
func NewManager(cfg config.LicenseConfig, database *gorm.DB) *Manager {
	return newManager(nil, cfg, database)
}

// NewManagerWithSubmitter creates a manager whose manual re-validations are
// cancelled and joined with the owning async runtime.
func NewManagerWithSubmitter(tasks asyncruntime.Submitter, cfg config.LicenseConfig, database *gorm.DB) *Manager {
	return newManager(tasks, cfg, database)
}

func newManager(tasks asyncruntime.Submitter, cfg config.LicenseConfig, database *gorm.DB) *Manager {
	m := &Manager{
		database: database,
		tasks:    tasks,
	}

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

// doValidate marks the current key as Pro. The Lemon Squeezy self-serve
// validation channel was removed when Depsilo's monetization shifted from
// $9 self-serve to Enterprise-contract-only: keys are now issued directly
// from a contract conversation rather than purchased, so there is no
// upstream license server to phone. Any non-empty key activates Pro.
// Empty keys remain OSS-only.
//
// Future Enterprise contract tooling can layer real validation on top of
// this — e.g. signed JWT keys with embedded expiry, or a depsilo-owned
// license endpoint — without rewiring the public API. For today the
// trust model is "if the operator entered a key, the operator is on a
// contract."
func (m *Manager) doValidate() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.LastChecked = time.Now().UTC()
	m.status.Error = ""

	if m.key == "" {
		m.status.IsPro = false
		return
	}

	m.status.IsPro = true
	if m.status.ActivatedAt == nil {
		now := time.Now().UTC()
		m.status.ActivatedAt = &now
	}
	// No upstream ExpiresAt to honour — contract expiry is tracked
	// out-of-band today. Leaving m.status.ExpiresAt nil signals "no
	// known expiry" to the frontend, which suppresses the "expires
	// in N days" UI.
	zap.L().Info("license accepted (Enterprise contract)",
		zap.String("key", MaskKey(m.key)),
	)
}

// Revalidate schedules a manual re-validation without blocking the caller.
// Its reservation is rolled back synchronously if the owning runtime rejects
// the task, so callers never report a validation that cannot start.
func (m *Manager) Revalidate() error {
	m.mu.Lock()
	if m.key == "" {
		m.mu.Unlock()
		return ErrNoLicenseKey
	}
	if m.revalidating {
		m.mu.Unlock()
		return ErrRevalidationRunning
	}
	m.revalidating = true
	tasks := m.tasks
	m.mu.Unlock()

	release := func() {
		m.mu.Lock()
		m.revalidating = false
		m.mu.Unlock()
	}
	if tasks == nil {
		release()
		return ErrRevalidationClosed
	}
	if err := tasks.Submit(func(ctx context.Context) {
		defer release()
		select {
		case <-ctx.Done():
			return
		default:
			m.doValidate()
		}
	}); err != nil {
		release()
		return errors.Join(ErrRevalidationClosed, err)
	}
	return nil
}

// SetKey persists a new license key (DB), updates manager state,
// and triggers a synchronous validation against the upstream license server.
// Returns any validation error, but the key is persisted unconditionally so
// the user can retry validation later via Revalidate.
func (m *Manager) SetKey(ctx context.Context, newKey string, userID uint) error {
	newKey = strings.TrimSpace(newKey)

	m.mu.Lock()
	if m.database != nil {
		if err := m.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(&db.LicenseStorage{
				ID:        licenseStorageSingletonID,
				Key:       newKey,
				UpdatedBy: userID,
				UpdatedAt: time.Now().UTC(),
			}).Error; err != nil {
				return err
			}
			return audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{
				ActorID: userID,
				Action:  "license.set",
				Target:  "license",
				Success: true,
			})
		}); err != nil {
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
		if err := m.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("id = ?", licenseStorageSingletonID).Delete(&db.LicenseStorage{}).Error; err != nil {
				return err
			}
			return audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{
				ActorID: userID,
				Action:  "license.clear",
				Target:  "license",
				Success: true,
			})
		}); err != nil {
			return fmt.Errorf("delete license key: %w", err)
		}
	}
	m.key = ""
	m.status = LicenseStatus{
		IsPro:       false,
		KeyMasked:   "",
		LastChecked: time.Now().UTC(),
	}

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
