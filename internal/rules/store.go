package rules

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

// ErrRuleDataIntegrity marks persisted package-rule rows whose SQLite storage
// types cannot be decoded safely. It is distinct from database availability
// failures because the policy middleware must fail closed for corrupt policy.
var ErrRuleDataIntegrity = errors.New("package rule data integrity failure")

// ErrRuleStoreUnavailable marks a rules read that could not reach SQLite.
// Only this class is safe to fail open; malformed rows, scan failures, and a
// missing or incompatible policy schema are integrity failures.
var ErrRuleStoreUnavailable = errors.New("package rule store unavailable")

// Store provides CRUD operations for package rules.
type Store struct {
	db      *gorm.DB
	writeMu sync.Mutex
}

// RulePatch contains the editable raw/display fields of a package rule.
// Store.Update merges the patch before dialect preparation, so a field cannot
// be validated against a stale ecosystem.
type RulePatch struct {
	Ecosystem   *string
	PackageName *string
	Version     *string
	Action      *string
	Reason      *string
}

// NewStore creates a new rules Store.
func NewStore(database *gorm.DB) *Store {
	return &Store{db: database}
}

// List returns all package rules ordered by absolute creation instant, newest
// first. julianday handles legacy SQLite rows whose timestamps contain local
// offsets; ID is the stable insertion-order tie-breaker for equal instants.
func (s *Store) List() ([]db.PackageRule, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: rule database is nil", ErrRuleStoreUnavailable)
	}
	sqlDatabase, err := s.db.DB()
	if err != nil {
		return nil, fmt.Errorf("%w: access database handle: %v", ErrRuleStoreUnavailable, err)
	}
	if err := sqlDatabase.Ping(); err != nil {
		return nil, fmt.Errorf("%w: ping database: %v", ErrRuleStoreUnavailable, err)
	}

	var rules []db.PackageRule
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var invalid int64
		if err := tx.Raw(`
			SELECT COUNT(*)
			FROM package_rules
			WHERE typeof(id) <> 'integer'
			   OR id <= 0
			   OR typeof(ecosystem) <> 'text'
			   OR typeof(package_name) <> 'text'
			   OR typeof(version) <> 'text'
			   OR typeof(action) <> 'text'
			   OR typeof(reason) <> 'text'
			   OR typeof(created_by) <> 'text'
			   OR typeof(normalized_package_name) <> 'text'
			   OR typeof(normalized_version) <> 'text'
			   OR typeof(dialect_revision) <> 'integer'
			   OR dialect_revision < 0
			   OR typeof(created_at) <> 'text'
			   OR julianday(created_at) IS NULL
			   OR typeof(updated_at) <> 'text'
			   OR julianday(updated_at) IS NULL
		`).Scan(&invalid).Error; err != nil {
			return err
		}
		if invalid != 0 {
			return fmt.Errorf("%w: %d row(s) have invalid SQLite storage types", ErrRuleDataIntegrity, invalid)
		}
		return tx.Order("julianday(created_at) DESC").Order("id DESC").Find(&rules).Error
	})
	if err == nil {
		return rules, nil
	}
	if classified := classifyImmediateRuleStoreReadError(err); classified != nil {
		return nil, classified
	}
	// A close or filesystem failure can race the first ping. If the handle is
	// no longer reachable, classify it as an availability failure so Engine can
	// apply its last-known-good or explicitly configured fallback. Otherwise an
	// unknown SQL/scan failure is unsafe policy data or schema and must fail
	// closed.
	if pingErr := sqlDatabase.Ping(); pingErr != nil {
		return nil, fmt.Errorf("%w: read failed (%v); ping failed: %v", ErrRuleStoreUnavailable, err, pingErr)
	}
	return nil, fmt.Errorf("%w: read package rules: %v", ErrRuleDataIntegrity, err)
}

func classifyImmediateRuleStoreReadError(err error) error {
	if errors.Is(err, ErrRuleDataIntegrity) {
		return err
	}
	// SQLITE_AUTH means SQLite explicitly refused this policy read. Treating
	// an authorization decision as a transient outage would let requests pass
	// without policy enforcement, so it must fail closed even if Ping still
	// reports that the underlying connection is alive.
	if isSQLiteRuleStoreAuthorizationError(err) {
		return fmt.Errorf("%w: SQLite authorization denied while reading package rules: %v", ErrRuleDataIntegrity, err)
	}
	if isRuleStoreAvailabilityError(err) {
		return fmt.Errorf("%w: %v", ErrRuleStoreUnavailable, err)
	}
	return nil
}

func isRuleStoreAvailabilityError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, gorm.ErrInvalidDB) {
		return true
	}

	// modernc SQLite exposes extended result codes through Code(). The low
	// byte is the primary SQLite result code. Keep corruption, schema, type,
	// format, and misuse errors out of this list: those are policy integrity
	// failures, not transient storage outages.
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) {
		return false
	}
	switch sqliteError.Code() & 0xff {
	case 5, // SQLITE_BUSY
		6,  // SQLITE_LOCKED
		7,  // SQLITE_NOMEM
		9,  // SQLITE_INTERRUPT
		10, // SQLITE_IOERR
		13, // SQLITE_FULL
		14, // SQLITE_CANTOPEN
		15, // SQLITE_PROTOCOL
		22: // SQLITE_NOLFS
		return true
	default:
		return false
	}
}

func isSQLiteRuleStoreAuthorizationError(err error) bool {
	var sqliteError interface{ Code() int }
	return errors.As(err, &sqliteError) && sqliteError.Code()&0xff == 23
}

// Create inserts a new package rule.
func (s *Store) Create(rule *db.PackageRule) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := prepareRuleModel(rule); err != nil {
		return err
	}
	return s.db.Create(rule).Error
}

// Update applies partial updates to a package rule by ID.
func (s *Store) Update(id uint, patch RulePatch) (*db.PackageRule, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var updated db.PackageRule
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&updated, id).Error; err != nil {
			return err
		}
		applyRulePatch(&updated, patch)
		if err := prepareRuleModel(&updated); err != nil {
			return err
		}
		return tx.Save(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func applyRulePatch(rule *db.PackageRule, patch RulePatch) {
	if patch.Ecosystem != nil {
		rule.Ecosystem = *patch.Ecosystem
	}
	if patch.PackageName != nil {
		rule.PackageName = *patch.PackageName
	}
	if patch.Version != nil {
		rule.Version = *patch.Version
	}
	if patch.Action != nil {
		rule.Action = *patch.Action
	}
	if patch.Reason != nil {
		rule.Reason = *patch.Reason
	}
}

func prepareRuleModel(rule *db.PackageRule) error {
	if rule == nil {
		return errors.New("package rule is nil")
	}
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem:   rule.Ecosystem,
		PackageName: rule.PackageName,
		Version:     rule.Version,
	})
	if err != nil {
		return err
	}
	action, err := packagepolicy.NormalizeRuleAction(rule.Action)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(rule.Reason)
	if len(reason) > 512 || strings.ContainsFunc(reason, unicode.IsControl) {
		return fmt.Errorf("%w: reason must be at most 512 characters and contain no control characters", packagepolicy.ErrInvalidRule)
	}
	createdBy := strings.TrimSpace(rule.CreatedBy)
	if len(createdBy) > 64 || strings.ContainsFunc(createdBy, unicode.IsControl) {
		return fmt.Errorf("%w: creator must be at most 64 characters and contain no control characters", packagepolicy.ErrInvalidRule)
	}

	rule.Ecosystem = prepared.Ecosystem
	rule.PackageName = prepared.PackageName
	rule.Version = prepared.Version
	rule.NormalizedPackageName = prepared.NormalizedPackageName
	rule.NormalizedVersion = prepared.NormalizedVersion
	rule.DialectRevision = prepared.DialectRevision
	rule.Action = action
	rule.Reason = reason
	rule.CreatedBy = createdBy
	return nil
}

// Delete removes a package rule by ID.
func (s *Store) Delete(id uint) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	result := s.db.Delete(&db.PackageRule{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
