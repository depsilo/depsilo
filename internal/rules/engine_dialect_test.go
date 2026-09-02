package rules

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

func TestEngineDoesNotMatchSemVerPrereleaseAgainstReleaseFloor(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "cargo", PackageName: "demo", Version: ">= 1.0.0",
		Action: "deny", CreatedBy: "operator",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(store, nil)

	allowed, matched, err := engine.Check(context.Background(), "cargo", "demo", "1.0.0-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed || matched != nil {
		t.Fatalf("prerelease matched release floor: allowed=%v rule=%+v", allowed, matched)
	}

	allowed, matched, err = engine.Check(context.Background(), "cargo", "demo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || matched == nil || matched.ID != rule.ID {
		t.Fatalf("release did not match floor: allowed=%v rule=%+v", allowed, matched)
	}
}

func TestEngineMatchesNormalizedPyPINameWithoutFoldingMavenCoordinates(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	for _, rule := range []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "Friendly_Kit", Version: "*", Action: "deny"},
		{Ecosystem: "maven", PackageName: "Org.Example:Artifact", Version: "*", Action: "deny"},
		{Ecosystem: "cargo", PackageName: "My_Crate", Version: "*", Action: "deny"},
	} {
		candidate := rule
		if err := store.Create(&candidate); err != nil {
			t.Fatal(err)
		}
	}
	engine := NewEngine(store, nil)

	allowed, _, err := engine.Check(context.Background(), "pypi", "friendly-kit", "1.0")
	if err != nil || allowed {
		t.Fatalf("normalized PyPI name: allowed=%v err=%v", allowed, err)
	}
	allowed, _, err = engine.Check(context.Background(), "maven", "org.example:artifact", "1.0")
	if err != nil || !allowed {
		t.Fatalf("case-distinct Maven coordinate: allowed=%v err=%v", allowed, err)
	}
	allowed, _, err = engine.Check(context.Background(), "cargo", "my_crate", "1.0.0")
	if err != nil || !allowed {
		t.Fatalf("case-distinct Cargo package: allowed=%v err=%v", allowed, err)
	}
}

func TestEngineUsesCRANPackageVersionEqualityForExactRule(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "cran", PackageName: "R6", Version: "0.01", Action: "deny", CreatedBy: "operator",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "cran", "R6", "0.1-0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || matched == nil || matched.ID != rule.ID {
		t.Fatalf("CRAN equivalent version decision = allowed %v matched %+v, want deny rule %d", allowed, matched, rule.ID)
	}
}

func TestEngineRejectsMalformedRequestVersionInsteadOfTrimmingIt(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "cargo", PackageName: "demo", Version: ">= 1.0.0", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "cargo", "demo", " 1.0.0")
	if !errors.Is(err, ErrPolicyEvaluation) {
		t.Fatalf("Check error = %v, want ErrPolicyEvaluation", err)
	}
	if allowed || matched != nil {
		t.Fatalf("malformed request version result = allowed %v, matched %+v", allowed, matched)
	}
	if errors.Is(err, packagepolicy.ErrInvalidRule) {
		t.Fatalf("request evaluation error was mislabeled as rule creation error: %v", err)
	}
}

func TestEngineRejectsRequestSurfaceWithoutPackageRuleEnforcement(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "*", PackageName: "*", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "docker", "library/alpine", "latest")
	if !errors.Is(err, ErrPolicyEvaluation) {
		t.Fatalf("unsupported request surface error = %v, want ErrPolicyEvaluation", err)
	}
	if allowed || matched != nil {
		t.Fatalf("unsupported request surface result = allowed %v matched %+v", allowed, matched)
	}
}

func TestEngineUsesNewestRuleWhenSpecificityAndTimestampTie(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	createdAt := time.Date(2026, 8, 31, 12, 0, 0, 123456789, time.UTC)

	olderAllow := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "allow", CreatedAt: createdAt,
	}
	if err := store.Create(&olderAllow); err != nil {
		t.Fatal(err)
	}
	newerDeny := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny", CreatedAt: createdAt,
	}
	if err := store.Create(&newerDeny); err != nil {
		t.Fatal(err)
	}
	if newerDeny.ID <= olderAllow.ID {
		t.Fatalf("fixture IDs = old %d new %d, want insertion order", olderAllow.ID, newerDeny.ID)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "pypi", "requests", "2.32.3")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || matched == nil || matched.ID != newerDeny.ID {
		t.Fatalf("tie decision = allowed %v matched %+v, want newest deny rule %d", allowed, matched, newerDeny.ID)
	}
}
