package blocklist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

// Store owns the three blocklist tables. Safe for concurrent use —
// every method is a straight GORM call.
type Store struct {
	db  *gorm.DB
	now func() time.Time // injectable for tests
}

func NewStore(database *gorm.DB) *Store {
	return &Store{db: database, now: func() time.Time { return time.Now().UTC() }}
}

// Match describes why a version was flagged. Returned to the checker
// so the 451 body and the audit event can cite the advisory.
type Match struct {
	SourceID string
	Summary  string
}

// Check reports whether (ecosystem, pkg, version) is known-malicious
// and whether an unexpired operator override exempts it. The package
// name is normalized internally — callers pass the raw request name.
//
// Errors are returned (not swallowed) so the checker can decide the
// degrade posture: a DB hiccup must not silently disable malware
// blocking without at least a warning log.
func (s *Store) Check(ctx context.Context, ecosystem, pkg, version string) (*Match, bool, error) {
	ecosystem = CanonicalEcosystem(ecosystem)
	name, err := normalizeNameStrict(ecosystem, pkg)
	if err != nil {
		return nil, false, fmt.Errorf("normalize blocklist package identity: %w", err)
	}

	var rows []db.MaliciousPackage
	if err := s.db.WithContext(ctx).
		Where("ecosystem = ? AND package = ?", ecosystem, name).
		Find(&rows).Error; err != nil {
		return nil, false, fmt.Errorf("blocklist query: %w", err)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	match, err := matchVersion(rows, ecosystem, version)
	if err != nil {
		return nil, false, err
	}
	if match == nil {
		return nil, false, nil
	}

	overridden, err := s.hasOverride(ctx, ecosystem, name, version)
	if err != nil {
		// The match stands; the override state is what failed. Report
		// the error alongside the match so the checker can log it and
		// fail toward blocking (the safe direction for malware).
		return match, false, fmt.Errorf("override query: %w", err)
	}
	return match, overridden, nil
}

// matchVersion finds the first advisory row covering the exact
// version. An empty Versions list means the advisory covers every
// version of the package.
func matchVersion(rows []db.MaliciousPackage, ecosystem, version string) (*Match, error) {
	// All-version advisories do not need a parseable request version. Preserve
	// the existing fail-safe treatment of corrupt JSON as all-version data.
	for i := range rows {
		r := &rows[i]
		if r.Versions == "" || r.Versions == "[]" {
			return &Match{SourceID: r.SourceID, Summary: r.Summary}, nil
		}
		var versions []string
		if err := json.Unmarshal([]byte(r.Versions), &versions); err != nil {
			return &Match{SourceID: r.SourceID, Summary: r.Summary}, nil
		}
	}

	dialect, err := packagepolicy.DialectFor(ecosystem)
	if err != nil {
		return nil, fmt.Errorf("blocklist version dialect: %w", err)
	}
	requestVersion, err := normalizeVersionStrict(ecosystem, version)
	if err != nil {
		return nil, fmt.Errorf("normalize requested %s version %q: %w", ecosystem, version, err)
	}
	var firstIntegrityError error
	for i := range rows {
		r := &rows[i]
		var versions []string
		if err := json.Unmarshal([]byte(r.Versions), &versions); err != nil {
			continue // handled as all-version in the first pass
		}
		for _, advisoryVersion := range versions {
			canonical, err := normalizeVersionStrict(ecosystem, advisoryVersion)
			if err != nil {
				if firstIntegrityError == nil {
					firstIntegrityError = fmt.Errorf("blocklist row %d has invalid %s version %q: %w", r.ID, ecosystem, advisoryVersion, err)
				}
				continue
			}
			equal, err := dialect.CompareVersions(canonical, requestVersion)
			if err != nil {
				if firstIntegrityError == nil {
					firstIntegrityError = fmt.Errorf("compare blocklist row %d version: %w", r.ID, err)
				}
				continue
			}
			if equal == 0 {
				return &Match{SourceID: r.SourceID, Summary: r.Summary}, nil
			}
		}
	}
	return nil, firstIntegrityError
}

// hasOverride reports an unexpired override for the exact version or
// a package-wide (Version == "") one.
func (s *Store) hasOverride(ctx context.Context, ecosystem, name, version string) (bool, error) {
	var overrides []db.MalwareOverride
	if err := s.db.WithContext(ctx).
		Where("ecosystem = ? AND package = ? AND expires_at > ?", ecosystem, name, s.now()).
		Find(&overrides).Error; err != nil {
		return false, err
	}
	requestVersion, err := normalizeVersionStrict(ecosystem, version)
	if err != nil {
		return false, err
	}
	dialect, err := packagepolicy.DialectFor(ecosystem)
	if err != nil {
		return false, err
	}
	for index := range overrides {
		if overrides[index].Version == "" {
			return true, nil
		}
		overrideVersion, err := normalizeVersionStrict(ecosystem, overrides[index].Version)
		if err != nil {
			return false, fmt.Errorf("invalid override %d version: %w", overrides[index].ID, err)
		}
		comparison, err := dialect.CompareVersions(overrideVersion, requestVersion)
		if err != nil {
			return false, err
		}
		if comparison == 0 {
			return true, nil
		}
	}
	return false, nil
}

// CreateOverride records an audited exemption expiring OverrideTTL
// from now. Reason is validated at the handler layer; the store only
// guards against the empty string so programmatic misuse is loud.
func (s *Store) CreateOverride(ctx context.Context, ecosystem, pkg, version, reason string, actorID uint) (*db.MalwareOverride, error) {
	if reason == "" {
		return nil, errors.New("blocklist: override reason is mandatory")
	}
	ecosystem = CanonicalEcosystem(ecosystem)
	name, err := normalizeNameStrict(ecosystem, pkg)
	if err != nil {
		return nil, fmt.Errorf("normalize override package identity: %w", err)
	}
	normalizedVersion, err := normalizeVersionStrict(ecosystem, version)
	if err != nil {
		return nil, fmt.Errorf("normalize override version identity: %w", err)
	}
	ov := &db.MalwareOverride{
		Ecosystem: ecosystem,
		Package:   name,
		Version:   normalizedVersion,
		Reason:    reason,
		ActorID:   actorID,
		ExpiresAt: s.now().Add(OverrideTTL),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ov).Error; err != nil {
			return err
		}
		return tx.Create(&db.QuarantineEvent{
			Ecosystem: ecosystem,
			Package:   ov.Package,
			Version:   ov.Version,
			Action:    ActionOverrideCreated,
			Reason:    reason,
			ActorID:   actorID,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return ov, nil
}

// RevokeOverride deletes an override and records the audit event in
// the same transaction.
func (s *Store) RevokeOverride(ctx context.Context, id uint, reason string, actorID uint) error {
	if reason == "" {
		return errors.New("blocklist: revoke reason is mandatory")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ov db.MalwareOverride
		if err := tx.First(&ov, id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&ov).Error; err != nil {
			return err
		}
		return tx.Create(&db.QuarantineEvent{
			Ecosystem: ov.Ecosystem,
			Package:   ov.Package,
			Version:   ov.Version,
			Action:    ActionOverrideRevoked,
			Reason:    reason,
			ActorID:   actorID,
		}).Error
	})
}

// ListOverrides returns every override, including recently expired
// ones (the UI shows them greyed-out so operators see what just
// lapsed); rows older than 7 days past expiry are pruned inline.
func (s *Store) ListOverrides(ctx context.Context) ([]db.MalwareOverride, error) {
	cutoff := s.now().Add(-7 * 24 * time.Hour)
	if err := s.db.WithContext(ctx).
		Where("expires_at < ?", cutoff).
		Delete(&db.MalwareOverride{}).Error; err != nil {
		return nil, err
	}
	var out []db.MalwareOverride
	err := s.db.WithContext(ctx).Order("expires_at DESC").Find(&out).Error
	return out, err
}

// ReplaceEcosystem swaps the full imported row set for one ecosystem
// inside a transaction — the dataset is authoritative, so removals
// upstream (retracted advisories) must disappear locally too.
func (s *Store) ReplaceEcosystem(ctx context.Context, ecosystem string, rows []db.MaliciousPackage) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("ecosystem = ?", ecosystem).
			Delete(&db.MaliciousPackage{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, 200).Error
	})
}

// HasEntries reports whether any advisory rows exist for the package —
// the admin API uses it to warn when an override can never match.
func (s *Store) HasEntries(ctx context.Context, ecosystem, pkg string) (bool, error) {
	ecosystem = CanonicalEcosystem(ecosystem)
	name, err := normalizeNameStrict(ecosystem, pkg)
	if err != nil {
		return false, err
	}
	var n int64
	err = s.db.WithContext(ctx).Model(&db.MaliciousPackage{}).
		Where("ecosystem = ? AND package = ?", ecosystem, name).
		Count(&n).Error
	return n > 0, err
}

// EntryCounts returns the row count per ecosystem for the status API.
func (s *Store) EntryCounts(ctx context.Context) (map[string]int64, int64, error) {
	type row struct {
		Ecosystem string
		N         int64
	}
	var rows []row
	if err := s.db.WithContext(ctx).Model(&db.MaliciousPackage{}).
		Select("ecosystem, COUNT(*) AS n").Group("ecosystem").Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make(map[string]int64, len(rows))
	var total int64
	for _, r := range rows {
		out[r.Ecosystem] = r.N
		total += r.N
	}
	return out, total, nil
}

// SyncState returns the singleton status row (zero value when the
// first sync hasn't happened yet).
func (s *Store) SyncState(ctx context.Context) (db.BlocklistSyncState, error) {
	var st db.BlocklistSyncState
	err := s.db.WithContext(ctx).First(&st, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.BlocklistSyncState{ID: 1}, nil
	}
	return st, err
}

// RecordSync persists the outcome of a sync attempt. Failures keep
// LastSuccessAt/EntryCount from the previous good run.
func (s *Store) RecordSync(ctx context.Context, syncErr error, entryCount int64, took time.Duration) error {
	st, err := s.SyncState(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	st.ID = 1
	st.LastSyncAt = &now
	st.DurationMs = took.Milliseconds()
	if syncErr != nil {
		st.LastError = syncErr.Error()
	} else {
		st.LastError = ""
		st.LastSuccessAt = &now
		st.EntryCount = entryCount
	}
	return s.db.WithContext(ctx).Save(&st).Error
}
