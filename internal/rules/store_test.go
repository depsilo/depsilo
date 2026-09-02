package rules

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

type ruleStoreSQLiteError struct {
	code int
}

func (e ruleStoreSQLiteError) Error() string { return fmt.Sprintf("SQLite result code %d", e.code) }
func (e ruleStoreSQLiteError) Code() int     { return e.code }

func TestSQLiteAuthorizationFailureIsPolicyIntegrityNotAvailability(t *testing.T) {
	authorizationError := fmt.Errorf("read package_rules: %w", ruleStoreSQLiteError{code: 23})
	if !isSQLiteRuleStoreAuthorizationError(authorizationError) {
		t.Fatal("SQLITE_AUTH was not recognized as an authorization failure")
	}
	if isRuleStoreAvailabilityError(authorizationError) {
		t.Fatal("SQLITE_AUTH was classified as a transient store outage and could fail open")
	}
	classified := classifyImmediateRuleStoreReadError(authorizationError)
	if !errors.Is(classified, ErrRuleDataIntegrity) || errors.Is(classified, ErrRuleStoreUnavailable) {
		t.Fatalf("SQLITE_AUTH classification = %v, want policy integrity only", classified)
	}

	// Preserve the deliberate availability behavior for a genuinely transient
	// lock while guarding the authorization code's boundary.
	if !isRuleStoreAvailabilityError(ruleStoreSQLiteError{code: 5}) {
		t.Fatal("SQLITE_BUSY is no longer classified as a transient store outage")
	}
}

func TestStoreListClassifiesNilDatabaseAsUnavailable(t *testing.T) {
	var store *Store
	if _, err := store.List(); !errors.Is(err, ErrRuleStoreUnavailable) {
		t.Fatalf("nil Store.List error = %v, want ErrRuleStoreUnavailable", err)
	}
	if _, err := (&Store{}).List(); !errors.Is(err, ErrRuleStoreUnavailable) {
		t.Fatalf("empty Store.List error = %v, want ErrRuleStoreUnavailable", err)
	}
}

func TestStoreListOrdersMixedOffsetTimestampsByInstant(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)

	olderAllow := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "allow",
		CreatedAt: time.Date(2026, 1, 1, 0, 30, 0, 0, time.FixedZone("legacy", 60*60)),
	}
	if err := store.Create(&olderAllow); err != nil {
		t.Fatal(err)
	}
	newerDeny := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
		CreatedAt: time.Date(2025, 12, 31, 23, 45, 0, 0, time.UTC),
	}
	if err := store.Create(&newerDeny); err != nil {
		t.Fatal(err)
	}

	rules, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].ID != newerDeny.ID {
		t.Fatalf("rule order = %+v, want newer absolute instant rule %d first", rules, newerDeny.ID)
	}
}

func TestStoreCreatePersistsPreparedRuleAtomically(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem:   "PyPI",
		PackageName: "Friendly_Kit",
		Version:     ">=1.0RC1",
		Action:      "deny",
		CreatedBy:   "operator",
	}

	if err := store.Create(&rule); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rule.Ecosystem != "pypi" || rule.PackageName != "Friendly_Kit" || rule.Version != ">=1.0RC1" {
		t.Errorf("raw fields changed unexpectedly: %+v", rule)
	}
	if rule.NormalizedPackageName != "friendly-kit" || rule.NormalizedVersion != ">= 1.0rc1" || rule.DialectRevision != 1 {
		t.Errorf("prepared fields = package %q version %q revision %d", rule.NormalizedPackageName, rule.NormalizedVersion, rule.DialectRevision)
	}
}

func TestStoreUpdateRejectsInvalidMergedRuleWithoutPartialWrite(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "pypi", PackageName: "demo", Version: ">=1.0",
		Action: "deny", CreatedBy: "operator",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	originalUpdatedAt := rule.UpdatedAt
	goEcosystem := "go"

	_, err = store.Update(rule.ID, RulePatch{Ecosystem: &goEcosystem})
	if !errors.Is(err, packagepolicy.ErrInvalidRule) {
		t.Fatalf("Update error = %v, want invalid package rule", err)
	}
	var persisted db.PackageRule
	if err := database.First(&persisted, rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Ecosystem != "pypi" || persisted.NormalizedVersion != ">= 1.0" {
		t.Errorf("failed update changed persisted rule: %+v", persisted)
	}
	if !persisted.UpdatedAt.Equal(originalUpdatedAt) {
		t.Errorf("failed update changed updated_at from %s to %s", originalUpdatedAt, persisted.UpdatedAt)
	}
}
