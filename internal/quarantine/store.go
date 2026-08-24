package quarantine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Event action constants — exported strings so admin API JSON
// responses can filter on them without an indirection layer.
// Add new values rather than reusing old ones; the audit table
// (db.QuarantineEvent) is keyed off these for reporting.
const (
	ActionBlocked   = "blocked"          // request denied: too young, no bypass
	ActionDowngrade = "served_eligible"  // serve_last_eligible mode resolved to older version
	ActionBypassed  = "bypassed"         // matched an Allow rule
	ActionApproved  = "approved"         // admin approved an individual version
	ActionRevoked   = "approval_revoked" // admin revoked a prior approval
	ActionWarned    = "warned"           // would have blocked, but observe mode served it

	// Known-malicious blocklist actions (DIRECTION Task 2), written by
	// Checker step 0. The override CRUD actions (override_created /
	// override_revoked) live in internal/blocklist next to the code
	// that records them.
	ActionMalwareBlocked  = "malware_blocked"  // request denied: known-malicious version
	ActionMalwareBypassed = "malware_bypassed" // served under an unexpired operator override
	ActionMalwareWarned   = "malware_warned"   // would have blocked, but observe mode served it

	// Tamper detection (DIRECTION T1): an immutable artifact's
	// re-fetched bytes did not match the first-seen SHA-256. Written
	// by internal/tamper; shares the quarantine event stream.
	ActionTamperDetected = "tamper_detected"
)

// Store wraps the GORM handle with the small set of helpers the
// quarantine package needs. Tests pass an in-memory SQLite via
// db.Open; production passes the shared application *gorm.DB. The
// thin abstraction is intentional — the rest of the package never
// imports gorm directly, only db model types and this Store.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store backed by the given GORM handle. AutoMigrate
// is the caller's job (centralised in internal/db/repository.go); this
// constructor merely binds.
func NewStore(database *gorm.DB) *Store {
	return &Store{db: database}
}

// LookupTimestamp returns the cached publish time for a version,
// or (zero, false, nil) when no row exists yet. The boolean is the
// "found" indicator — distinct from the time.Time zero value because
// callers must NOT treat "not cached" as "published at epoch."
func (s *Store) LookupTimestamp(ctx context.Context, ecosystem, pkg, version string) (time.Time, bool, error) {
	if s == nil || s.db == nil {
		return time.Time{}, false, nil
	}
	var row db.PackageTimestamp
	err := s.db.WithContext(ctx).
		Where("ecosystem = ? AND package = ? AND version = ?", ecosystem, pkg, version).
		First(&row).Error
	switch {
	case err == nil:
		return row.PublishAt, true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return time.Time{}, false, nil
	default:
		return time.Time{}, false, fmt.Errorf("lookup timestamp: %w", err)
	}
}

// SaveTimestamp upserts the cached publish time. Conflict on the
// composite primary key updates publish_at — keeps the row count
// stable if a resolver returns a more authoritative timestamp on a
// re-fetch (rare but possible if the upstream API was returning a
// stale response earlier).
func (s *Store) SaveTimestamp(ctx context.Context, ecosystem, pkg, version string, publishAt time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	row := db.PackageTimestamp{
		Ecosystem: ecosystem,
		Package:   pkg,
		Version:   version,
		PublishAt: publishAt,
		CreatedAt: time.Now().UTC(),
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package"}, {Name: "version"}},
			DoUpdates: clause.AssignmentColumns([]string{"publish_at"}),
		}).
		Create(&row).Error
}

// IsApproved reports whether the operator has manually approved this
// specific (ecosystem, package, version). Permanent — no expiry.
func (s *Store) IsApproved(ctx context.Context, ecosystem, pkg, version string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&db.ApprovedVersion{}).
		Where("ecosystem = ? AND package = ? AND version = ?", ecosystem, pkg, version).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("is approved: %w", err)
	}
	return count > 0, nil
}

// Approve persists an admin's manual release of a quarantined version
// and writes the matching audit event in the same transaction. Admin
// handler is responsible for validating reason / actor; this layer
// trusts the caller and just records.
func (s *Store) Approve(ctx context.Context, ecosystem, pkg, version, reason string, actorID uint) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := db.ApprovedVersion{
			Ecosystem:  ecosystem,
			Package:    pkg,
			Version:    version,
			Reason:     reason,
			ApprovedBy: actorID,
			CreatedAt:  time.Now().UTC(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package"}, {Name: "version"}},
			DoUpdates: clause.AssignmentColumns([]string{"reason", "approved_by", "created_at"}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("approve insert: %w", err)
		}
		ev := db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   pkg,
			Version:   version,
			Action:    ActionApproved,
			Reason:    reason,
			ActorID:   actorID,
			CreatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&ev).Error; err != nil {
			return fmt.Errorf("approve event: %w", err)
		}
		return nil
	})
}

// Revoke removes an approval and writes a matching audit event.
// Idempotent — deleting a non-existent approval is a no-op (no event
// recorded in that case, to avoid log noise from repeated DELETEs).
func (s *Store) Revoke(ctx context.Context, ecosystem, pkg, version, reason string, actorID uint) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("ecosystem = ? AND package = ? AND version = ?", ecosystem, pkg, version).
			Delete(&db.ApprovedVersion{})
		if res.Error != nil {
			return fmt.Errorf("revoke delete: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
		ev := db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   pkg,
			Version:   version,
			Action:    ActionRevoked,
			Reason:    reason,
			ActorID:   actorID,
			CreatedAt: time.Now().UTC(),
		}
		return tx.Create(&ev).Error
	})
}

// RecordEvent persists a request-time decision (block, serve_eligible,
// bypass). Fire-and-forget from the checker — failures log a warning
// but never propagate, because failing a request because audit failed
// to write would be the wrong tradeoff.
func (s *Store) RecordEvent(ctx context.Context, ev db.QuarantineEvent) error {
	if s == nil || s.db == nil {
		return nil
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Create(&ev).Error
}
