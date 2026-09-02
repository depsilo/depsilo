package quarantine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

var ErrInvalidCoordinate = errors.New("invalid quarantine package coordinate")

// Coordinate is the ecosystem-dialect identity used by permanent version
// approvals. It is intentionally separate from the raw values accepted by the
// admin API: aliases that an ecosystem considers identical must not create
// distinct approval or revocation decisions.
type Coordinate struct {
	Ecosystem string
	Package   string
	Version   string
}

// NormalizeCoordinate validates a concrete package coordinate and returns the
// stable spelling persisted by the quarantine store. CompareVersions remains
// the authority for equality because some dialects (notably SemVer) retain
// build metadata in their stable spelling while ignoring it for precedence.
func NormalizeCoordinate(ecosystem, pkg, version string) (Coordinate, error) {
	normalized := Coordinate{Ecosystem: strings.ToLower(strings.TrimSpace(ecosystem))}
	dialect, err := packagepolicy.DialectFor(normalized.Ecosystem)
	if err != nil {
		return Coordinate{}, fmt.Errorf("%w: ecosystem: %v", ErrInvalidCoordinate, err)
	}
	normalized.Package, err = dialect.NormalizePackageName(pkg)
	if err != nil {
		return Coordinate{}, fmt.Errorf("%w: package: %v", ErrInvalidCoordinate, err)
	}
	normalized.Version, err = packagepolicy.NormalizeVersion(normalized.Ecosystem, version)
	if err != nil {
		return Coordinate{}, fmt.Errorf("%w: version: %v", ErrInvalidCoordinate, err)
	}
	if len(normalized.Ecosystem) > 32 || len(normalized.Package) > 256 || len(normalized.Version) > 128 {
		return Coordinate{}, fmt.Errorf("%w: normalized coordinate exceeds database limits", ErrInvalidCoordinate)
	}
	return normalized, nil
}

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
	coordinate, err := NormalizeCoordinate(ecosystem, pkg, version)
	if err != nil {
		return false, err
	}
	ids, err := equivalentApprovalIDs(s.db.WithContext(ctx), coordinate)
	if err != nil {
		return false, fmt.Errorf("is approved: %w", err)
	}
	return len(ids) > 0, nil
}

// Approve persists an admin's manual release of a quarantined version
// and writes the matching audit event in the same transaction. Admin
// handler is responsible for validating reason / actor; this layer
// trusts the caller and just records.
func (s *Store) Approve(ctx context.Context, ecosystem, pkg, version, reason string, actorID uint) error {
	if s == nil || s.db == nil {
		return nil
	}
	coordinate, err := NormalizeCoordinate(ecosystem, pkg, version)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := equivalentApprovalIDs(tx, coordinate)
		if err != nil {
			return fmt.Errorf("find equivalent approvals: %w", err)
		}
		if len(ids) > 0 {
			if err := tx.Where("id IN ?", ids).Delete(&db.ApprovedVersion{}).Error; err != nil {
				return fmt.Errorf("replace equivalent approvals: %w", err)
			}
		}
		row := db.ApprovedVersion{
			Ecosystem:  coordinate.Ecosystem,
			Package:    coordinate.Package,
			Version:    coordinate.Version,
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
			Ecosystem: coordinate.Ecosystem,
			Package:   coordinate.Package,
			Version:   coordinate.Version,
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
	coordinate, err := NormalizeCoordinate(ecosystem, pkg, version)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := equivalentApprovalIDs(tx, coordinate)
		if err != nil {
			return fmt.Errorf("find equivalent approvals: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("id IN ?", ids).Delete(&db.ApprovedVersion{}).Error; err != nil {
			return fmt.Errorf("revoke delete: %w", err)
		}
		ev := db.QuarantineEvent{
			Ecosystem: coordinate.Ecosystem,
			Package:   coordinate.Package,
			Version:   coordinate.Version,
			Action:    ActionRevoked,
			Reason:    reason,
			ActorID:   actorID,
			CreatedAt: time.Now().UTC(),
		}
		return tx.Create(&ev).Error
	})
}

// equivalentApprovalIDs locates both current canonical rows and valid rows
// written by older releases. Invalid legacy rows are never treated as an
// approval; schema migration reports them separately instead of guessing.
func equivalentApprovalIDs(database *gorm.DB, target Coordinate) ([]uint, error) {
	var candidates []db.ApprovedVersion
	if err := database.Where("lower(ecosystem) = ?", target.Ecosystem).
		Order("id ASC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	dialect, err := packagepolicy.DialectFor(target.Ecosystem)
	if err != nil {
		return nil, err
	}
	ids := make([]uint, 0, 1)
	for _, candidate := range candidates {
		normalized, err := NormalizeCoordinate(candidate.Ecosystem, candidate.Package, candidate.Version)
		if err != nil || normalized.Package != target.Package {
			continue
		}
		comparison, err := dialect.CompareVersions(normalized.Version, target.Version)
		if err != nil {
			continue
		}
		if comparison == 0 {
			ids = append(ids, candidate.ID)
		}
	}
	return ids, nil
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
