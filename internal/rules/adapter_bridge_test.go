package rules

import (
	"context"
	"testing"

	"depsilo/internal/adapter"
	"depsilo/internal/db"
)

func TestAdapterCheckerEvaluatesAuthenticatedNPMVersionPrecedence(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	for _, rule := range []db.PackageRule{
		{
			Ecosystem: "npm", PackageName: "fixture", Version: "*",
			Action: "deny", Reason: "baseline package deny",
		},
		{
			Ecosystem: "npm", PackageName: "fixture", Version: ">= 1.0.0",
			Action: "allow", Reason: "approved final releases",
		},
	} {
		candidate := rule
		if err := store.Create(&candidate); err != nil {
			t.Fatalf("create rule %#v: %v", rule, err)
		}
	}

	checker := Wrap(NewEngine(store, nil))
	if decision := checker.EvaluatePackageRule(
		context.Background(), "npm", "fixture", "1.0.0-alpha",
	); decision.Outcome != adapter.PackageRuleDeny || decision.Reason != "baseline package deny" {
		t.Fatalf("prerelease decision = %#v", decision)
	}
	if decision := checker.EvaluatePackageRule(
		context.Background(), "npm", "fixture", "1.0.0",
	); decision.Outcome != adapter.PackageRuleAllow {
		t.Fatalf("final decision = %#v", decision)
	}
}

func TestAdapterCheckerFailsClosedForUnsafePolicyData(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: ">= 1.0.0", Action: "deny",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.PackageRule{}).
		Where("id = ?", rule.ID).
		Update("normalized_version", ">= 0.0.0").Error; err != nil {
		t.Fatal(err)
	}

	decision := Wrap(NewEngine(store, nil)).EvaluatePackageRule(
		context.Background(), "npm", "fixture", "1.0.0",
	)
	if decision.Outcome != adapter.PackageRuleUnevaluable {
		t.Fatalf("unsafe policy decision = %#v", decision)
	}
}

func TestAdapterCheckerFailsOpenForUnavailableRuleStore(t *testing.T) {
	database := newRulesTestDB(t)
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}

	decision := Wrap(NewEngine(NewStore(database), nil)).EvaluatePackageRule(
		context.Background(), "npm", "fixture", "1.0.0",
	)
	if decision.Outcome != adapter.PackageRuleAllow {
		t.Fatalf("unavailable store decision = %#v", decision)
	}
}
