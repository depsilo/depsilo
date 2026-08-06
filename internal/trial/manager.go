package trial

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

// TrialDuration is the length of a Pro trial, baked in at compile time per spec §4.2.
const TrialDuration = 14 * 24 * time.Hour

// Manager owns the local 14-day Pro trial state machine.
// Single-instance, singleton record (ID = 1). Thread-safe.
type Manager struct {
	database *gorm.DB
	mu       sync.RWMutex
	record   *db.TrialRecord // nil if trial has never been activated
}

// NewManager loads the existing TrialRecord (if any) from the database.
// At most one TrialRecord row is expected; if more are present, the lowest-ID
// one wins (a no-op for a single-instance deploy that respects the contract).
func NewManager(database *gorm.DB) (*Manager, error) {
	m := &Manager{database: database}
	var rec db.TrialRecord
	err := database.Order("id ASC").First(&rec).Error
	switch {
	case err == nil:
		m.record = &rec
	case errors.Is(err, gorm.ErrRecordNotFound):
		// no trial record yet — fine
	default:
		return nil, fmt.Errorf("trial: load record: %w", err)
	}
	return m, nil
}

// IsActive reports whether a trial currently grants Pro access.
// Hot path — called from RequirePro middleware on every Pro request.
func (m *Manager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.record != nil && time.Now().UTC().Before(m.record.ExpiresAt)
}

// IsUsed reports whether a TrialRecord exists at all (active or expired).
func (m *Manager) IsUsed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.record != nil
}

// Available reports whether the user can still start a trial.
func (m *Manager) Available() bool {
	return !m.IsUsed()
}

// Record returns a copy of the current trial record (or nil).
func (m *Manager) Record() *db.TrialRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.record == nil {
		return nil
	}
	cp := *m.record
	return &cp
}

// Activate creates the singleton TrialRecord. Returns ErrTrialAlreadyUsed
// if a record already exists.
func (m *Manager) Activate(ctx context.Context, userID uint, fromIP string) (*db.TrialRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.record != nil {
		return nil, ErrTrialAlreadyUsed
	}

	now := time.Now().UTC()
	rec := &db.TrialRecord{
		ActivatedAt:   now,
		ExpiresAt:     now.Add(TrialDuration),
		ActivatedBy:   userID,
		ActivatedFrom: fromIP,
	}
	var existing db.TrialRecord
	err := m.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Defensive re-check against durable state in case another manager raced us.
		var count int64
		if err := tx.Model(&db.TrialRecord{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			if err := tx.Order("id ASC").First(&existing).Error; err != nil {
				return err
			}
			return ErrTrialAlreadyUsed
		}
		if err := tx.Create(rec).Error; err != nil {
			return err
		}
		return audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{
			ActorID:  userID,
			Action:   "trial.activate",
			Target:   "trial",
			Success:  true,
			ClientIP: fromIP,
			Detail:   rec.ExpiresAt.Format(time.RFC3339),
		})
	})
	if errors.Is(err, ErrTrialAlreadyUsed) {
		m.record = &existing
		return nil, ErrTrialAlreadyUsed
	}
	if err != nil {
		return nil, err
	}
	m.record = rec

	return rec, nil
}
