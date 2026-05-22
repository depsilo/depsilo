package unit

import (
	"context"
	"os"
	"testing"

	"go.uber.org/zap"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/license"
	"depsilo/internal/rules"
)

func init() {
	// Initialize a no-op logger so db.Open and AutoMigrate don't panic.
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
}

// setupRulesEngine creates an in-memory SQLite DB + store + engine for testing.
func setupRulesEngine(t *testing.T, rulesList []db.PackageRule) *rules.Engine {
	t.Helper()
	// Use dev pro mode for testing
	os.Setenv("DEPSILO_DEV_PRO", "1")
	t.Cleanup(func() { os.Unsetenv("DEPSILO_DEV_PRO") })

	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}

	for i := range rulesList {
		database.Create(&rulesList[i])
	}

	licMgr := license.NewManager(config.LicenseConfig{}, nil)
	store := rules.NewStore(database)
	engine := rules.NewEngine(store, licMgr)
	return engine
}

func TestRules_DenyExact(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "log4j", Version: "*", Action: "deny", Reason: "CVE"},
	})
	allowed, rule, err := engine.Check(context.Background(), "pypi", "log4j", "2.14.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("expected denied")
	}
	if rule == nil || rule.Reason != "CVE" {
		t.Error("expected rule with reason CVE")
	}
}

func TestRules_DenyVersionRange(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "log4j", Version: "< 2.17.0", Action: "deny"},
	})
	allowed1, _, err := engine.Check(context.Background(), "pypi", "log4j", "2.14.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed1 {
		t.Error("2.14.0 should be denied")
	}
	allowed2, _, err := engine.Check(context.Background(), "pypi", "log4j", "2.17.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed2 {
		t.Error("2.17.0 should be allowed")
	}
}

func TestRules_WildcardEcosystem(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "*", PackageName: "malicious-pkg", Version: "*", Action: "deny"},
	})
	allowed1, _, err := engine.Check(context.Background(), "pypi", "malicious-pkg", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed1 {
		t.Error("pypi should be denied")
	}
	allowed2, _, err := engine.Check(context.Background(), "npm", "malicious-pkg", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed2 {
		t.Error("npm should be denied")
	}
}

func TestRules_NoMatch_DefaultAllow(t *testing.T) {
	engine := setupRulesEngine(t, nil)
	allowed, rule, err := engine.Check(context.Background(), "pypi", "requests", "2.31.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("should be allowed by default")
	}
	if rule != nil {
		t.Error("no rule should match")
	}
}

func TestRules_AllowExplicit(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "allow"},
	})
	allowed, rule, err := engine.Check(context.Background(), "pypi", "requests", "2.31.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("should be allowed")
	}
	if rule == nil {
		t.Error("rule should match")
	}
}

func TestRules_VersionGreaterThan(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "testpkg", Version: "> 1.0.0", Action: "deny"},
	})
	allowed1, _, _ := engine.Check(context.Background(), "pypi", "testpkg", "1.0.0")
	if !allowed1 {
		t.Error("1.0.0 should be allowed (not > 1.0.0)")
	}
	allowed2, _, _ := engine.Check(context.Background(), "pypi", "testpkg", "1.0.1")
	if allowed2 {
		t.Error("1.0.1 should be denied (> 1.0.0)")
	}
}

func TestRules_ExactVersion(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "testpkg", Version: "1.2.3", Action: "deny"},
	})
	allowed1, _, _ := engine.Check(context.Background(), "pypi", "testpkg", "1.2.3")
	if allowed1 {
		t.Error("exact version 1.2.3 should be denied")
	}
	allowed2, _, _ := engine.Check(context.Background(), "pypi", "testpkg", "1.2.4")
	if !allowed2 {
		t.Error("1.2.4 should be allowed")
	}
}

func TestRules_PrefixMatch(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "evil-*", Version: "*", Action: "deny"},
	})
	allowed1, _, _ := engine.Check(context.Background(), "pypi", "evil-pkg", "1.0.0")
	if allowed1 {
		t.Error("evil-pkg should be denied by prefix rule")
	}
	allowed2, _, _ := engine.Check(context.Background(), "pypi", "good-pkg", "1.0.0")
	if !allowed2 {
		t.Error("good-pkg should be allowed")
	}
}

func TestRules_CommunityBypass(t *testing.T) {
	// Do NOT set DEPSILO_DEV_PRO — community mode
	os.Unsetenv("DEPSILO_DEV_PRO")

	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	database.Create(&db.PackageRule{
		Ecosystem:   "pypi",
		PackageName: "log4j",
		Version:     "*",
		Action:      "deny",
	})

	licMgr := license.NewManager(config.LicenseConfig{}, nil) // no key, no dev mode
	store := rules.NewStore(database)
	engine := rules.NewEngine(store, licMgr)

	allowed, _, err := engine.Check(context.Background(), "pypi", "log4j", "2.14.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Error("community edition should bypass rules")
	}
}

func TestRules_MismatchedEcosystem(t *testing.T) {
	engine := setupRulesEngine(t, []db.PackageRule{
		{Ecosystem: "npm", PackageName: "log4j", Version: "*", Action: "deny"},
	})
	allowed, _, _ := engine.Check(context.Background(), "pypi", "log4j", "2.14.0")
	if !allowed {
		t.Error("pypi request should not match npm rule")
	}
}
