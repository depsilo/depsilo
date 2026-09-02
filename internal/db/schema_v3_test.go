package db

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPackageRuleDialectMigrationBackfillsNormalizedValues(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	createdAt := time.Unix(100, 0).UTC()
	updatedAt := time.Unix(200, 0).UTC()
	if err := database.Exec(`
		INSERT INTO package_rules
			(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
		VALUES
			(17, 'pypi', '*', '*', 'deny', 'legacy ecosystem rule', 'operator', ?, ?)
	`, createdAt, updatedAt).Error; err != nil {
		t.Fatalf("seed legacy rule: %v", err)
	}
	if len(schemaMigrations) < 3 {
		t.Fatal("package rule dialect migration is missing")
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package rule migration: %v", err)
	}
	var migrated struct {
		ID                    uint
		NormalizedPackageName string
		NormalizedVersion     string
		DialectRevision       uint
		CreatedAt             time.Time
		UpdatedAt             time.Time
	}
	if err := database.Table("package_rules").Where("id = ?", 17).Take(&migrated).Error; err != nil {
		t.Fatalf("read migrated rule: %v", err)
	}
	if migrated.NormalizedPackageName != "*" {
		t.Errorf("normalized package = %q", migrated.NormalizedPackageName)
	}
	if migrated.NormalizedVersion != "*" {
		t.Errorf("normalized version = %q", migrated.NormalizedVersion)
	}
	if migrated.DialectRevision != 1 {
		t.Errorf("dialect revision = %d", migrated.DialectRevision)
	}
	if !migrated.CreatedAt.Equal(createdAt) || !migrated.UpdatedAt.Equal(updatedAt) {
		t.Errorf("migration changed timestamps: created=%s updated=%s", migrated.CreatedAt, migrated.UpdatedAt)
	}
}

func TestPackageRuleDialectMigrationRejectsScannerNamedOperatorRuleAtomically(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	now := time.Unix(300, 0).UTC()
	if err := database.Exec(`
		INSERT INTO package_rules
			(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
		VALUES
			(18, 'pypi', 'requests', '<2.0', 'deny', 'Auto-blocked: PYSEC-2026-1 (CVSS 9.8)', 'security-scanner', ?, ?),
			(19, 'pypi', '*', '*', 'deny', 'reviewed global rule', 'operator', ?, ?)
	`, now, now, now, now).Error; err != nil {
		t.Fatalf("seed v0.9.1 package rules: %v", err)
	}
	if err := database.Create(&SecurityPolicy{
		Ecosystem: "pypi", AutoBlockEnabled: true, MinCVSSScore: 9, CreatedBy: "admin",
	}).Error; err != nil {
		t.Fatalf("seed v0.9.1 security policy: %v", err)
	}

	err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "rule 18") || !strings.Contains(err.Error(), "recreate") {
		t.Fatalf("migration error = %v, want ambiguous scanner-named rule 18", err)
	}
	if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
		t.Fatal("failed migration left normalized package-rule columns behind")
	}
	var rules []PackageRule
	if err := database.Order("id ASC").Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].ID != 18 || rules[1].ID != 19 {
		t.Fatalf("rules after failed migration = %+v, want both legacy rows", rules)
	}
	var policy SecurityPolicy
	if err := database.Where("ecosystem = ?", "pypi").Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if !policy.AutoBlockEnabled {
		t.Fatal("failed migration changed the legacy policy despite rollback")
	}
}

func TestPackageRuleDialectMigrationKeepsAllAmbiguousRuleFailuresAtomic(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	now := time.Unix(400, 0).UTC()
	if err := database.Exec(`
		INSERT INTO package_rules
			(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
		VALUES
			(28, 'cargo', 'serde', '<1.0.200', 'deny', 'Auto-blocked: RUSTSEC-2026-1 (CVSS 9.1)', 'security-scanner', ?, ?),
			(29, 'cargo', 'serde', '<1.0.100', 'deny', 'reviewed legacy selector', 'operator', ?, ?)
	`, now, now, now, now).Error; err != nil {
		t.Fatalf("seed mixed legacy package rules: %v", err)
	}
	if err := database.Create(&SecurityPolicy{
		Ecosystem: "cargo", AutoBlockEnabled: true, MinCVSSScore: 8, CreatedBy: "admin",
	}).Error; err != nil {
		t.Fatal(err)
	}

	err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "rule 28") || !strings.Contains(err.Error(), "rule 29") {
		t.Fatalf("migration error = %v, want both ambiguous rule IDs", err)
	}
	if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
		t.Fatal("failed migration left normalized package-rule columns behind")
	}
	var rules int64
	if err := database.Model(&PackageRule{}).Count(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if rules != 2 {
		t.Fatalf("failed migration retained %d rules, want both rows after atomic rollback", rules)
	}
	var policy SecurityPolicy
	if err := database.Where("ecosystem = ?", "cargo").Take(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if !policy.AutoBlockEnabled {
		t.Fatal("failed migration changed the legacy policy despite rollback")
	}
}

func TestPackageRuleDialectMigrationRejectsLegacySelectorSemanticChanges(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	if err := database.Exec(`
		INSERT INTO package_rules
			(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
		VALUES
			(41, 'npm', 'express', '*', 'deny', 'legacy case-folded package', 'operator', ?, ?),
			(42, 'pypi', '*', '>= 1.0', 'allow', 'legacy generic comparator', 'operator', ?, ?)
	`, time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC(), time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()).Error; err != nil {
		t.Fatalf("seed legacy semantic rules: %v", err)
	}

	err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
	if err == nil {
		t.Fatal("migration silently changed legacy package/version selector semantics")
	}
	if !strings.Contains(err.Error(), "rule 41") || !strings.Contains(err.Error(), "rule 42") ||
		!strings.Contains(err.Error(), "recreate") {
		t.Fatalf("migration error = %q, want every ambiguous rule ID and recreation guidance", err)
	}
	if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
		t.Fatal("failed migration left normalized_package_name behind")
	}
}

func TestPackageRuleDialectMigrationRepairsLegacyBlocklistIdentities(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	now := time.Unix(300, 0).UTC()
	rows := []MaliciousPackage{
		{SourceID: "MAL-NPM", Ecosystem: "npm", Package: "express", ImportedAt: now},
		{SourceID: "MAL-PYPI", Ecosystem: "pypi", Package: "requests", ImportedAt: now},
		{SourceID: "MAL-RUBY", Ecosystem: "rubygems", Package: "rack", ImportedAt: now},
		{SourceID: "MAL-UNKNOWN", Ecosystem: "apt", Package: "curl", ImportedAt: now},
		{
			SourceID: "MAL-NUGET", Ecosystem: "NuGet", Package: "Example.Client",
			Versions: `["1.0","2.0.0-Alpha+build.1"]`, Aliases: "GHSA-OLD", Summary: "old",
			Modified: now, ImportedAt: now,
		},
		{
			SourceID: "MAL-NUGET", Ecosystem: "nuget", Package: "example.client",
			Versions: `["1.0.0+metadata","2.0.0-alpha"]`, Aliases: "GHSA-NEW", Summary: "new",
			Modified: now.Add(time.Minute), ImportedAt: now.Add(time.Minute),
		},
		{
			SourceID: "MAL-COMPOSER", Ecosystem: "Composer", Package: "Vendor/Package",
			Versions: `["1.2.3"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-COMPOSER", Ecosystem: "composer", Package: "vendor/package",
			Versions: `[]`, ImportedAt: now.Add(time.Minute),
		},
		{
			SourceID: "MAL-CARGO", Ecosystem: "CARGO", Package: "Serde-Core",
			Versions: `["1.0.0+BUILD.1"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-GO", Ecosystem: "Go", Package: "github.com/evil/module",
			Versions: `["v1.2.3"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-MAVEN", Ecosystem: "MAVEN", Package: "org.example:Evil-Core",
			Versions: `["1.0-RC1"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-BAD-PACKAGE", Ecosystem: "nuget", Package: "bad..client",
			Versions: `["1.0"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-BAD-VERSION", Ecosystem: "nuget", Package: "bad.version",
			Versions: `["1..0"]`, ImportedAt: now,
		},
		{
			SourceID: "MAL-BAD-JSON", Ecosystem: "composer", Package: "bad/json",
			Versions: `["1.0"`, ImportedAt: now,
		},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed blocklist rows: %v", err)
	}
	overrides := []MalwareOverride{
		{Ecosystem: "npm", Package: "express", Version: "1.0.0", Reason: "legacy", ExpiresAt: now.Add(time.Hour)},
		{Ecosystem: "pypi", Package: "requests", Version: "2.0", Reason: "dropped", ExpiresAt: now.Add(time.Hour)},
		{Ecosystem: "rubygems", Package: "rack", Version: "3.0", Reason: "dropped", ExpiresAt: now.Add(time.Hour)},
		{Ecosystem: "apt", Package: "curl", Version: "8.0", Reason: "dropped", ExpiresAt: now.Add(time.Hour)},
		{
			Ecosystem: "NuGet", Package: "Example.Client", Version: "1.0",
			Reason: "older equivalent override", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "nuget", Package: "example.client", Version: "1.0.0+metadata",
			Reason: "latest equivalent override", CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(2 * time.Hour),
		},
		{
			Ecosystem: "composer", Package: "Vendor/Package", Version: "",
			Reason: "package-wide", ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "cargo", Package: "Serde-Core", Version: "1.0.0+BUILD.1",
			Reason: "cargo exact", ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "go", Package: "github.com/evil/module", Version: "v1.2.3",
			Reason: "go transport spelling", ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "maven", Package: "org.example:Evil-Core", Version: "1.0-RC1",
			Reason: "maven exact", ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "nuget", Package: "bad.version", Version: "1..0",
			Reason: "invalid version", ExpiresAt: now.Add(time.Hour),
		},
		{
			Ecosystem: "composer", Package: "not-a-coordinate", Version: "1.0",
			Reason: "invalid package", ExpiresAt: now.Add(time.Hour),
		},
	}
	if err := database.Create(&overrides).Error; err != nil {
		t.Fatalf("seed overrides: %v", err)
	}
	if err := database.Create(&BlocklistSyncState{
		ID: 1, LastSyncAt: &now, LastSuccessAt: &now, EntryCount: int64(len(rows)),
	}).Error; err != nil {
		t.Fatalf("seed sync state: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}

	var migratedRows []MaliciousPackage
	if err := database.Order("ecosystem ASC, source_id ASC").Find(&migratedRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedRows) != 7 {
		t.Fatalf("migrated blocklist rows = %+v, want seven recoverable non-npm rows", migratedRows)
	}
	rowsBySource := make(map[string]MaliciousPackage, len(migratedRows))
	for _, row := range migratedRows {
		rowsBySource[row.SourceID] = row
		switch row.Ecosystem {
		case "cargo", "composer", "nuget", "go", "maven":
		default:
			t.Fatalf("migration retained unsupported ecosystem row %+v", row)
		}
	}
	if row := rowsBySource["MAL-NUGET"]; row.Package != "example.client" ||
		row.Versions != `["1.0.0","2.0.0-alpha"]` || row.Summary != "new" ||
		row.Aliases != "GHSA-NEW,GHSA-OLD" {
		t.Fatalf("canonical NuGet row = %+v", row)
	}
	if row := rowsBySource["MAL-COMPOSER"]; row.Ecosystem != "composer" ||
		row.Package != "vendor/package" || row.Versions != "" {
		t.Fatalf("canonical Composer row = %+v", row)
	}
	if row := rowsBySource["MAL-CARGO"]; row.Package != "Serde-Core" || row.Versions != `["1.0.0+BUILD.1"]` {
		t.Fatalf("canonical Cargo row = %+v", row)
	}
	if row := rowsBySource["MAL-GO"]; row.Package != "github.com/evil/module" || row.Versions != `["1.2.3"]` {
		t.Fatalf("canonical Go row = %+v", row)
	}
	if row := rowsBySource["MAL-MAVEN"]; row.Package != "org.example:Evil-Core" || row.Versions != `["1.0-rc1"]` {
		t.Fatalf("canonical Maven row = %+v", row)
	}
	for _, failClosedSource := range []string{"MAL-BAD-VERSION", "MAL-BAD-JSON"} {
		if row := rowsBySource[failClosedSource]; row.ID == 0 || row.Versions != "" {
			t.Errorf("invalid-version row %s = %+v, want a retained all-version block", failClosedSource, row)
		}
	}
	for _, removedSource := range []string{
		"MAL-NPM", "MAL-PYPI", "MAL-RUBY", "MAL-UNKNOWN",
		"MAL-BAD-PACKAGE",
	} {
		if _, ok := rowsBySource[removedSource]; ok {
			t.Errorf("migration retained unrecoverable row %s", removedSource)
		}
	}

	var migratedOverrides []MalwareOverride
	if err := database.Order("ecosystem ASC, package ASC, version ASC").Find(&migratedOverrides).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedOverrides) != 5 {
		t.Fatalf("migrated overrides = %+v, want five recoverable coordinates", migratedOverrides)
	}
	overridesByEcosystem := make(map[string]MalwareOverride, len(migratedOverrides))
	for _, override := range migratedOverrides {
		overridesByEcosystem[override.Ecosystem] = override
		switch override.Ecosystem {
		case "cargo", "composer", "nuget", "go", "maven":
		default:
			t.Fatalf("migration retained unsupported override %+v", override)
		}
	}
	if override := overridesByEcosystem["nuget"]; override.Package != "example.client" ||
		override.Version != "1.0.0" || override.Reason != "latest equivalent override" ||
		!override.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("canonical NuGet override = %+v", override)
	}
	if override := overridesByEcosystem["composer"]; override.Package != "vendor/package" || override.Version != "" {
		t.Fatalf("canonical Composer override = %+v", override)
	}
	if override := overridesByEcosystem["go"]; override.Version != "1.2.3" {
		t.Fatalf("canonical Go override = %+v", override)
	}
	if override := overridesByEcosystem["maven"]; override.Version != "1.0-rc1" {
		t.Fatalf("canonical Maven override = %+v", override)
	}
	var state BlocklistSyncState
	if err := database.First(&state, 1).Error; err != nil {
		t.Fatal(err)
	}
	if state.LastSuccessAt != nil || state.EntryCount != int64(len(migratedRows)) ||
		!strings.Contains(state.LastError, "full blocklist resync required") ||
		!strings.Contains(state.LastError, "treated 2 invalid-version rows as all-version") {
		t.Fatalf("sync state = %+v, want a required full resync and an accurate retained count", state)
	}
	if state.LastSyncAt == nil || !state.LastSyncAt.Equal(now) {
		t.Fatalf("last sync attempt = %v, want preserved %v", state.LastSyncAt, now)
	}
}

func TestPackageRuleDialectMigrationInvalidatesLegacyNPMScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(300, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "npm/legacy/metadata.json", AdapterType: "npm",
			PackageName: "legacy", StoragePath: "/tmp/legacy", CreatedAt: now,
		},
		{
			Key: "npm-exact-v1/ExactName/metadata.json", AdapterType: "npm",
			PackageName: "ExactName", StoragePath: "/tmp/exact", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed npm cache entries: %v", err)
	}
	npmVulnerability := Vulnerability{
		OSVID: "GHSA-NPM", Ecosystem: "npm", PackageName: "legacy", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-NPM-SENTINEL", Ecosystem: "alpine", PackageName: "preserved", CreatedAt: now,
	}
	if err := database.Create(&npmVulnerability).Error; err != nil {
		t.Fatalf("seed npm vulnerability: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability: %v", err)
	}
	for _, vulnerabilityID := range []uint{npmVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "npm", PackageName: "legacy", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed npm vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "npm", PackageName: "legacy", Version: "1.0.0",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed npm project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply npm scanner identity migration: %v", err)
	}

	for modelName, model := range map[string]any{
		"vulnerabilities":      &Vulnerability{},
		"vulnerability checks": &VulnerabilityCheck{},
		"project packages":     &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "npm").Count(&count).Error; err != nil {
			t.Fatalf("count npm %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy npm %s", count, modelName)
		}
	}
	var npmDismissals, alpineDismissals, cacheRows int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", npmVulnerability.ID).Count(&npmDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&CacheEntry{}).Where("id IN ?", []uint{entries[0].ID, entries[1].ID}).Count(&cacheRows).Error; err != nil {
		t.Fatal(err)
	}
	if npmDismissals != 0 || alpineDismissals != 1 || cacheRows != 2 {
		t.Fatalf(
			"post-migration npm dismissals=%d Alpine dismissals=%d cache rows=%d, want 0/1/2",
			npmDismissals, alpineDismissals, cacheRows,
		)
	}
}

func TestPackageRuleDialectMigrationClearsLegacyAPTScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(315, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "apt/ubuntu/pool/main/c/curl/curl_8.0-1_amd64.deb", AdapterType: "apt",
			PackageName: "curl", StoragePath: "/tmp/apt-artifact", CreatedAt: now,
		},
		{
			Key: "apt/ubuntu/dists/jammy/InRelease", AdapterType: "APT",
			PackageName: "ubuntu", StoragePath: "/tmp/apt-metadata", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/apt-2.0-r0.apk", AdapterType: "alpine",
			PackageName: "apt", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	aptVulnerability := Vulnerability{
		OSVID: "DSA-APT-GUESSED", Ecosystem: "APT", PackageName: "curl", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-APT-SENTINEL", Ecosystem: "alpine", PackageName: "apt", CreatedAt: now,
	}
	if err := database.Create(&aptVulnerability).Error; err != nil {
		t.Fatalf("seed APT vulnerability advisory: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability advisory: %v", err)
	}
	for _, vulnerabilityID := range []uint{aptVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "APT", PackageName: "curl", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed APT vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "APT", PackageName: "curl", Version: "8.0-1",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed APT project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply APT scanner identity migration: %v", err)
	}

	var migratedEntries []CacheEntry
	if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedEntries) != len(entries) {
		t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
	}
	if migratedEntries[0].PackageName != "" || migratedEntries[1].PackageName != "" {
		t.Fatalf(
			"APT cache package identities = %q/%q, want both empty without source-package provenance",
			migratedEntries[0].PackageName,
			migratedEntries[1].PackageName,
		)
	}
	if migratedEntries[2].PackageName != "apt" {
		t.Fatalf("non-APT cache package identity changed to %q", migratedEntries[2].PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerability advisories": &Vulnerability{},
		"vulnerability checks":     &VulnerabilityCheck{},
		"project packages":         &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "apt").Count(&count).Error; err != nil {
			t.Fatalf("count APT %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy APT %s", count, modelName)
		}
	}
	var aptDismissals, alpineDismissals, alpineAdvisories int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", aptVulnerability.ID).Count(&aptDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&Vulnerability{}).
		Where("id = ?", alpineVulnerability.ID).Count(&alpineAdvisories).Error; err != nil {
		t.Fatal(err)
	}
	if aptDismissals != 0 || alpineDismissals != 1 || alpineAdvisories != 1 {
		t.Fatalf(
			"post-migration APT dismissals=%d Alpine dismissals=%d advisories=%d, want 0/1/1",
			aptDismissals,
			alpineDismissals,
			alpineAdvisories,
		)
	}
}

func TestPackageRuleDialectMigrationRepairsLegacyGoScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(320, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "go/github.com/!azure/azure-sdk-for-go/@v/v1.2.3.zip", AdapterType: "go",
			PackageName: "github.com/!azure/azure-sdk-for-go", StoragePath: "/tmp/go-artifact", CreatedAt: now,
		},
		{
			Key: "go/github.com/!azure/azure-sdk-for-go/@v/list", AdapterType: "GO",
			PackageName: "untrusted-legacy-name", StoragePath: "/tmp/go-metadata", CreatedAt: now,
		},
		{
			Key: "go/github.com/Azure/azure-sdk-for-go/@v/v1.2.3.zip", AdapterType: "go",
			PackageName: "github.com/Azure/azure-sdk-for-go", StoragePath: "/tmp/go-invalid", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/py3-requests-2.31.0-r0.apk", AdapterType: "alpine",
			PackageName: "py3-requests", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	goVulnerability := Vulnerability{
		OSVID: "GO-LEGACY-IDENTITY", Ecosystem: "Go",
		PackageName: "github.com/!azure/azure-sdk-for-go", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-PRESERVED", Ecosystem: "alpine", PackageName: "py3-requests", CreatedAt: now,
	}
	if err := database.Create(&goVulnerability).Error; err != nil {
		t.Fatalf("seed Go vulnerability advisory: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability advisory: %v", err)
	}
	for _, vulnerabilityID := range []uint{goVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "Go", PackageName: goVulnerability.PackageName,
		LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed Go vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "Go", PackageName: goVulnerability.PackageName, Version: "v1.2.3",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Go project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply Go scanner identity migration: %v", err)
	}

	var migratedEntries []CacheEntry
	if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedEntries) != len(entries) {
		t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
	}
	for _, index := range []int{0, 1} {
		if migratedEntries[index].PackageName != "github.com/Azure/azure-sdk-for-go" {
			t.Fatalf(
				"Go cache identity %d = %q, want canonical decoded module path",
				index,
				migratedEntries[index].PackageName,
			)
		}
	}
	if migratedEntries[2].PackageName != "" {
		t.Fatalf("non-canonical Go cache identity = %q, want empty", migratedEntries[2].PackageName)
	}
	if migratedEntries[3].PackageName != "py3-requests" {
		t.Fatalf("non-Go cache identity changed to %q", migratedEntries[3].PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerability advisories": &Vulnerability{},
		"vulnerability checks":     &VulnerabilityCheck{},
		"project packages":         &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "go").Count(&count).Error; err != nil {
			t.Fatalf("count Go %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy Go %s", count, modelName)
		}
	}
	var goDismissals, alpineDismissals, alpineAdvisories int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", goVulnerability.ID).Count(&goDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&Vulnerability{}).
		Where("id = ?", alpineVulnerability.ID).Count(&alpineAdvisories).Error; err != nil {
		t.Fatal(err)
	}
	if goDismissals != 0 || alpineDismissals != 1 || alpineAdvisories != 1 {
		t.Fatalf(
			"post-migration Go dismissals=%d Alpine dismissals=%d advisories=%d, want 0/1/1",
			goDismissals,
			alpineDismissals,
			alpineAdvisories,
		)
	}
}

func TestPackageRuleDialectMigrationRepairsLegacyCRANScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(323, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "cran/src/contrib/Data.Table_1.15.4.tar.gz", AdapterType: "cran",
			PackageName: "Data", StoragePath: "/tmp/cran-artifact", CreatedAt: now,
		},
		{
			Key: "cran/src/contrib/PACKAGES.gz", AdapterType: "CRAN",
			PackageName: "PACKAGES.gz", StoragePath: "/tmp/cran-metadata", CreatedAt: now,
		},
		{
			Key: "cran/not-a-repository/Data.Table_1.15.4.tar.gz", AdapterType: "cran",
			PackageName: "Data.Table", StoragePath: "/tmp/cran-ambiguous-path", CreatedAt: now,
		},
		{
			Key: "cran/src/contrib/not_a_package_1.0.tar.gz", AdapterType: "cran",
			PackageName: "not", StoragePath: "/tmp/cran-ambiguous-name", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/R-1.0-r0.apk", AdapterType: "alpine",
			PackageName: "R", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	cranVulnerability := Vulnerability{
		OSVID: "OSV-CRAN-GUESSED", Ecosystem: "CRAN", PackageName: "PACKAGES.gz", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-CRAN-SENTINEL", Ecosystem: "alpine", PackageName: "R", CreatedAt: now,
	}
	if err := database.Create(&cranVulnerability).Error; err != nil {
		t.Fatalf("seed CRAN vulnerability advisory: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability advisory: %v", err)
	}
	for _, vulnerabilityID := range []uint{cranVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "CRAN", PackageName: "PACKAGES.gz",
		LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed CRAN vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "CRAN", PackageName: "Data", Version: "1.15.4",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed CRAN project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply CRAN scanner identity migration: %v", err)
	}

	var migratedEntries []CacheEntry
	if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedEntries) != len(entries) {
		t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
	}
	if migratedEntries[0].PackageName != "Data.Table" {
		t.Fatalf("strict CRAN artifact identity = %q, want Data.Table", migratedEntries[0].PackageName)
	}
	for _, index := range []int{1, 2, 3} {
		if migratedEntries[index].PackageName != "" {
			t.Fatalf("ambiguous CRAN cache identity %d = %q, want empty", index, migratedEntries[index].PackageName)
		}
	}
	if migratedEntries[4].PackageName != "R" {
		t.Fatalf("non-CRAN cache identity changed to %q", migratedEntries[4].PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerability advisories": &Vulnerability{},
		"vulnerability checks":     &VulnerabilityCheck{},
		"project packages":         &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "cran").Count(&count).Error; err != nil {
			t.Fatalf("count CRAN %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy CRAN %s", count, modelName)
		}
	}
	var cranDismissals, alpineDismissals, alpineAdvisories int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", cranVulnerability.ID).Count(&cranDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&Vulnerability{}).
		Where("id = ?", alpineVulnerability.ID).Count(&alpineAdvisories).Error; err != nil {
		t.Fatal(err)
	}
	if cranDismissals != 0 || alpineDismissals != 1 || alpineAdvisories != 1 {
		t.Fatalf(
			"post-migration CRAN dismissals=%d Alpine dismissals=%d advisories=%d, want 0/1/1",
			cranDismissals,
			alpineDismissals,
			alpineAdvisories,
		)
	}
}

func TestPackageRuleDialectMigrationRepairsLegacyCargoScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(324, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "cargo/crates/Serde/1.0.228.crate", AdapterType: "cargo",
			PackageName: "serde", StoragePath: "/tmp/cargo-artifact", CreatedAt: now,
		},
		{
			Key: "cargo/index/se/rd/serde", AdapterType: "CARGO",
			PackageName: "serde", StoragePath: "/tmp/cargo-index", CreatedAt: now,
		},
		{
			Key: "cargo/config.json", AdapterType: "cargo",
			PackageName: "config.json", StoragePath: "/tmp/cargo-config", CreatedAt: now,
		},
		{
			Key: "cargo/crates/serde/not-an-artifact", AdapterType: "cargo",
			PackageName: "serde", StoragePath: "/tmp/cargo-ambiguous", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/cargo-1.0-r0.apk", AdapterType: "alpine",
			PackageName: "cargo", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	cargoVulnerability := Vulnerability{
		OSVID: "RUSTSEC-LEGACY-INDEX", Ecosystem: "Cargo", PackageName: "serde", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-CARGO-SENTINEL", Ecosystem: "alpine", PackageName: "cargo", CreatedAt: now,
	}
	if err := database.Create(&cargoVulnerability).Error; err != nil {
		t.Fatalf("seed Cargo vulnerability advisory: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability advisory: %v", err)
	}
	for _, vulnerabilityID := range []uint{cargoVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "Cargo", PackageName: "serde",
		LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed Cargo vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "Cargo", PackageName: "serde", Version: "1.0.228",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Cargo project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply Cargo scanner identity migration: %v", err)
	}

	var migratedEntries []CacheEntry
	if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedEntries) != len(entries) {
		t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
	}
	if migratedEntries[0].PackageName != "Serde" {
		t.Fatalf("Cargo artifact identity = %q, want case-exact Serde", migratedEntries[0].PackageName)
	}
	for _, index := range []int{1, 2, 3} {
		if migratedEntries[index].PackageName != "" {
			t.Fatalf("non-artifact Cargo cache identity %d = %q, want empty", index, migratedEntries[index].PackageName)
		}
	}
	if migratedEntries[4].PackageName != "cargo" {
		t.Fatalf("non-Cargo cache identity changed to %q", migratedEntries[4].PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerability advisories": &Vulnerability{},
		"vulnerability checks":     &VulnerabilityCheck{},
		"project packages":         &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "cargo").Count(&count).Error; err != nil {
			t.Fatalf("count Cargo %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy Cargo %s", count, modelName)
		}
	}
	var cargoDismissals, alpineDismissals, alpineAdvisories int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", cargoVulnerability.ID).Count(&cargoDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&Vulnerability{}).
		Where("id = ?", alpineVulnerability.ID).Count(&alpineAdvisories).Error; err != nil {
		t.Fatal(err)
	}
	if cargoDismissals != 0 || alpineDismissals != 1 || alpineAdvisories != 1 {
		t.Fatalf(
			"post-migration Cargo dismissals=%d Alpine dismissals=%d advisories=%d, want 0/1/1",
			cargoDismissals,
			alpineDismissals,
			alpineAdvisories,
		)
	}
}

func TestPackageRuleDialectMigrationClearsLegacyNuGetScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(325, 0).UTC()
	entries := []CacheEntry{
		{
			Key:         "nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg",
			AdapterType: "nuget", PackageName: "newtonsoft.json",
			StoragePath: "/tmp/nuget-artifact", CreatedAt: now,
		},
		{
			Key: "nuget/v3/registration/newtonsoft.json/index.json", AdapterType: "NUGET",
			PackageName: "newtonsoft.json", StoragePath: "/tmp/nuget-metadata", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/nuget-6.0-r0.apk", AdapterType: "alpine",
			PackageName: "nuget", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	nugetVulnerability := Vulnerability{
		OSVID: "GHSA-NUGET-LOWERCASE", Ecosystem: "NuGet", PackageName: "newtonsoft.json", CreatedAt: now,
	}
	alpineVulnerability := Vulnerability{
		OSVID: "ALPINE-NUGET-SENTINEL", Ecosystem: "alpine", PackageName: "nuget", CreatedAt: now,
	}
	if err := database.Create(&nugetVulnerability).Error; err != nil {
		t.Fatalf("seed NuGet vulnerability advisory: %v", err)
	}
	if err := database.Create(&alpineVulnerability).Error; err != nil {
		t.Fatalf("seed Alpine vulnerability advisory: %v", err)
	}
	for _, vulnerabilityID := range []uint{nugetVulnerability.ID, alpineVulnerability.ID} {
		if err := database.Create(&DismissedVuln{
			VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed vulnerability dismissal: %v", err)
		}
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "NuGet", PackageName: "newtonsoft.json",
		LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed NuGet vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "NuGet", PackageName: "newtonsoft.json", Version: "13.0.3",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed NuGet project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply NuGet scanner identity migration: %v", err)
	}

	var migratedEntries []CacheEntry
	if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedEntries) != len(entries) {
		t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
	}
	if migratedEntries[0].PackageName != "" || migratedEntries[1].PackageName != "" {
		t.Fatalf(
			"NuGet cache package identities = %q/%q, want both empty without canonical registry casing",
			migratedEntries[0].PackageName,
			migratedEntries[1].PackageName,
		)
	}
	if migratedEntries[2].PackageName != "nuget" {
		t.Fatalf("non-NuGet cache identity changed to %q", migratedEntries[2].PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerability advisories": &Vulnerability{},
		"vulnerability checks":     &VulnerabilityCheck{},
		"project packages":         &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "nuget").Count(&count).Error; err != nil {
			t.Fatalf("count NuGet %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy NuGet %s", count, modelName)
		}
	}
	var nugetDismissals, alpineDismissals, alpineAdvisories int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", nugetVulnerability.ID).Count(&nugetDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", alpineVulnerability.ID).Count(&alpineDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&Vulnerability{}).
		Where("id = ?", alpineVulnerability.ID).Count(&alpineAdvisories).Error; err != nil {
		t.Fatal(err)
	}
	if nugetDismissals != 0 || alpineDismissals != 1 || alpineAdvisories != 1 {
		t.Fatalf(
			"post-migration NuGet dismissals=%d Alpine dismissals=%d advisories=%d, want 0/1/1",
			nugetDismissals,
			alpineDismissals,
			alpineAdvisories,
		)
	}
}

func TestPackageRuleDialectMigrationInvalidatesLegacyRubyGemsScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(325, 0).UTC()
	cacheEntry := CacheEntry{
		Key:         "rubygems/gems/nokogiri-1.16.5-x86_64-linux.gem",
		AdapterType: "rubygems",
		PackageName: "nokogiri-1.16.5-x86_64",
		StoragePath: "/tmp/nokogiri-platform-gem",
		CreatedAt:   now,
	}
	if err := database.Create(&cacheEntry).Error; err != nil {
		t.Fatalf("seed RubyGems cache entry: %v", err)
	}
	vulnerability := Vulnerability{
		OSVID: "GHSA-RUBYGEMS-GUESSED", Ecosystem: "rubygems",
		PackageName: cacheEntry.PackageName, CreatedAt: now,
	}
	if err := database.Create(&vulnerability).Error; err != nil {
		t.Fatalf("seed RubyGems vulnerability: %v", err)
	}
	if err := database.Create(&DismissedVuln{
		VulnerabilityID: vulnerability.ID, DismissedBy: "operator", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed RubyGems dismissal: %v", err)
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "rubygems", PackageName: cacheEntry.PackageName,
		LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed RubyGems vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "rubygems", PackageName: cacheEntry.PackageName,
		Version: "linux", FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed RubyGems project package: %v", err)
	}
	alpineCheck := VulnerabilityCheck{
		Ecosystem: "alpine", PackageName: "ruby", LastFetchedAt: now,
		NextFetchAt: now.Add(time.Hour),
	}
	if err := database.Create(&alpineCheck).Error; err != nil {
		t.Fatalf("seed non-RubyGems vulnerability check: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply RubyGems scanner identity migration: %v", err)
	}

	for modelName, model := range map[string]any{
		"vulnerabilities":      &Vulnerability{},
		"vulnerability checks": &VulnerabilityCheck{},
		"project packages":     &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Where("lower(ecosystem) = ?", "rubygems").Count(&count).Error; err != nil {
			t.Fatalf("count RubyGems %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d legacy RubyGems %s", count, modelName)
		}
	}
	var dismissals, cacheRows, alpineChecks int64
	if err := database.Model(&DismissedVuln{}).
		Where("vulnerability_id = ?", vulnerability.ID).Count(&dismissals).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&CacheEntry{}).Where("id = ?", cacheEntry.ID).Count(&cacheRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&VulnerabilityCheck{}).Where("id = ?", alpineCheck.ID).Count(&alpineChecks).Error; err != nil {
		t.Fatal(err)
	}
	if dismissals != 0 || cacheRows != 1 || alpineChecks != 1 {
		t.Fatalf(
			"post-migration dismissals=%d cache rows=%d Alpine checks=%d, want 0/1/1",
			dismissals,
			cacheRows,
			alpineChecks,
		)
	}
}

func TestLegacyScannerIdentityInvalidationExceedsSQLiteBindLimit(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(
		&Vulnerability{},
		&DismissedVuln{},
		&VulnerabilityCheck{},
		&ProjectPackage{},
	); err != nil {
		t.Fatalf("create scanner tables: %v", err)
	}

	// modernc SQLite accepts at most 32,766 bind parameters. The old migration
	// plucked every ID and expanded one IN list, so this realistic catalog size
	// failed before it could invalidate any scanner decisions.
	const legacyRows = 32770
	if err := database.Exec(`
		WITH RECURSIVE sequence(value) AS (
			SELECT 1
			UNION ALL
			SELECT value + 1 FROM sequence WHERE value < ?
		)
		INSERT INTO vulnerabilities (osv_id, ecosystem, package_name)
		SELECT printf('GHSA-NPM-LARGE-%05d', value), 'npm', printf('legacy-%05d', value)
		FROM sequence
	`, legacyRows).Error; err != nil {
		t.Fatalf("seed large npm vulnerability catalog: %v", err)
	}
	if err := database.Exec(`
		INSERT INTO dismissed_vulns (vulnerability_id, dismissed_by)
		SELECT id, 'operator' FROM vulnerabilities WHERE ecosystem = 'npm'
	`).Error; err != nil {
		t.Fatalf("seed large npm dismissal set: %v", err)
	}
	cargo := Vulnerability{OSVID: "GHSA-CARGO-SENTINEL", Ecosystem: "cargo", PackageName: "preserved"}
	if err := database.Create(&cargo).Error; err != nil {
		t.Fatalf("seed non-npm vulnerability: %v", err)
	}
	if err := database.Create(&DismissedVuln{VulnerabilityID: cargo.ID, DismissedBy: "operator"}).Error; err != nil {
		t.Fatalf("seed non-npm dismissal: %v", err)
	}

	var seeded int64
	if err := database.Model(&Vulnerability{}).Where("ecosystem = ?", "npm").Count(&seeded).Error; err != nil {
		t.Fatal(err)
	}
	if seeded != legacyRows {
		t.Fatalf("seeded npm vulnerabilities = %d, want %d", seeded, legacyRows)
	}
	if err := repairLegacyNPMScannerIdentity(database); err != nil {
		t.Fatalf("invalidate oversized npm scanner identity set: %v", err)
	}

	var npmVulnerabilities, remainingDismissals int64
	if err := database.Model(&Vulnerability{}).Where("ecosystem = ?", "npm").Count(&npmVulnerabilities).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).Count(&remainingDismissals).Error; err != nil {
		t.Fatal(err)
	}
	if npmVulnerabilities != 0 || remainingDismissals != 1 {
		t.Fatalf(
			"post-invalidation npm vulnerabilities=%d dismissals=%d, want 0/1 non-npm sentinel",
			npmVulnerabilities,
			remainingDismissals,
		)
	}
}

func TestPackageRuleDialectMigrationRepairsMavenScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(300, 0).UTC()
	entries := []CacheEntry{
		{
			Key:         "maven/org/apache/logging/log4j/log4j-core/2.20.0/log4j-core-2.20.0.jar",
			AdapterType: "maven", PackageName: "log4j-core", StoragePath: "/tmp/artifact", CreatedAt: now,
		},
		{
			Key:         "maven/org/apache/logging/log4j/log4j-core/maven-metadata.xml",
			AdapterType: "maven", PackageName: "logging", StoragePath: "/tmp/metadata", CreatedAt: now,
		},
		{
			Key:         "pypi/files/requests-2.32.0-py3-none-any.whl",
			AdapterType: "pypi", PackageName: "requests", StoragePath: "/tmp/pypi", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	mavenVulnerability := Vulnerability{
		OSVID: "GHSA-MAVEN", Ecosystem: "maven", PackageName: "log4j-core", CreatedAt: now,
	}
	if err := database.Create(&mavenVulnerability).Error; err != nil {
		t.Fatalf("seed Maven vulnerability: %v", err)
	}
	if err := database.Create(&DismissedVuln{
		VulnerabilityID: mavenVulnerability.ID, DismissedBy: "operator", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Maven vulnerability dismissal: %v", err)
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "maven", PackageName: "log4j-core", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed Maven vulnerability check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "maven", PackageName: "log4j-core", Version: "2.20.0",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Maven project package: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}

	var artifact, metadata, pypi CacheEntry
	if err := database.Where("id = ?", entries[0].ID).Take(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Where("id = ?", entries[1].ID).Take(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Where("id = ?", entries[2].ID).Take(&pypi).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.PackageName != "org.apache.logging.log4j:log4j-core" {
		t.Fatalf("Maven artifact package name = %q, want full OSV coordinate", artifact.PackageName)
	}
	if metadata.PackageName != "" {
		t.Fatalf("ambiguous Maven metadata package name = %q, want empty", metadata.PackageName)
	}
	if pypi.PackageName != "requests" {
		t.Fatalf("non-Maven package name changed to %q", pypi.PackageName)
	}

	for modelName, model := range map[string]any{
		"vulnerabilities":      &Vulnerability{},
		"vulnerability checks": &VulnerabilityCheck{},
		"project packages":     &ProjectPackage{},
		"dismissals":           &DismissedVuln{},
	} {
		var count int64
		query := database.Model(model)
		if modelName != "dismissals" {
			query = query.Where("lower(ecosystem) = ?", "maven")
		}
		if err := query.Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", modelName, err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d stale Maven %s", count, modelName)
		}
	}
}

func TestPackageRuleDialectMigrationRepairsComposerScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(325, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "composer/p2/Symfony/Console.json", AdapterType: "composer",
			PackageName: "wrong/legacy", StoragePath: "/tmp/composer-p2", CreatedAt: now,
		},
		{
			Key: "composer/dist/monolog/monolog/abc123.tar.gz", AdapterType: "composer",
			PackageName: "wrong/dist", StoragePath: "/tmp/composer-dist", CreatedAt: now,
		},
		{
			Key: "composer/p2/not-a-package.json", AdapterType: "composer",
			PackageName: "guessed", StoragePath: "/tmp/composer-bad", CreatedAt: now,
		},
		{
			Key: "composer/packages.json", AdapterType: "composer",
			PackageName: "metadata-guess", StoragePath: "/tmp/composer-index", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/curl-1.0-r0.apk", AdapterType: "alpine",
			PackageName: "curl", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("seed cache entries: %v", err)
	}
	composerVulnerability := Vulnerability{
		OSVID: "GHSA-COMPOSER-LEGACY", Ecosystem: "composer", PackageName: "wrong/legacy", CreatedAt: now,
	}
	if err := database.Create(&composerVulnerability).Error; err != nil {
		t.Fatalf("seed Composer vulnerability: %v", err)
	}
	if err := database.Create(&DismissedVuln{
		VulnerabilityID: composerVulnerability.ID, DismissedBy: "operator", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Composer dismissal: %v", err)
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "composer", PackageName: "wrong/legacy", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed Composer check: %v", err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "composer", PackageName: "wrong/legacy", Version: "1.0",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Composer project package: %v", err)
	}
	otherVulnerability := Vulnerability{
		OSVID: "GHSA-ALPINE-SENTINEL", Ecosystem: "alpine", PackageName: "curl", CreatedAt: now,
	}
	if err := database.Create(&otherVulnerability).Error; err != nil {
		t.Fatalf("seed non-Composer vulnerability: %v", err)
	}
	if err := database.Create(&DismissedVuln{
		VulnerabilityID: otherVulnerability.ID, DismissedBy: "operator", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed non-Composer dismissal: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}

	var p2, dist, bad, metadata, alpine CacheEntry
	for _, item := range []struct {
		id   uint
		into *CacheEntry
	}{
		{entries[0].ID, &p2}, {entries[1].ID, &dist}, {entries[2].ID, &bad},
		{entries[3].ID, &metadata}, {entries[4].ID, &alpine},
	} {
		if err := database.Where("id = ?", item.id).Take(item.into).Error; err != nil {
			t.Fatal(err)
		}
	}
	if p2.PackageName != "symfony/console" {
		t.Fatalf("Composer p2 identity = %q, want normalized coordinate", p2.PackageName)
	}
	if dist.PackageName != "monolog/monolog" {
		t.Fatalf("Composer dist identity = %q, want coordinate", dist.PackageName)
	}
	if bad.PackageName != "" || metadata.PackageName != "" {
		t.Fatalf("ambiguous Composer identities = %q/%q, want empty", bad.PackageName, metadata.PackageName)
	}
	if alpine.PackageName != "curl" {
		t.Fatalf("non-Composer identity changed to %q", alpine.PackageName)
	}

	var composerVulnerabilities, composerChecks, composerProjects, dismissals int64
	if err := database.Model(&Vulnerability{}).Where("lower(ecosystem) = ?", "composer").Count(&composerVulnerabilities).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&VulnerabilityCheck{}).Where("lower(ecosystem) = ?", "composer").Count(&composerChecks).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&ProjectPackage{}).Where("lower(ecosystem) = ?", "composer").Count(&composerProjects).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DismissedVuln{}).Count(&dismissals).Error; err != nil {
		t.Fatal(err)
	}
	if composerVulnerabilities != 0 || composerChecks != 0 || composerProjects != 0 || dismissals != 1 {
		t.Fatalf("Composer invalidation counts = vuln:%d checks:%d projects:%d dismissals:%d, want 0/0/0/1 sentinel", composerVulnerabilities, composerChecks, composerProjects, dismissals)
	}
}

func TestPackageRuleDialectMigrationRepairsPyPIScannerIdentity(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	now := time.Unix(350, 0).UTC()
	entries := []CacheEntry{
		{
			Key: "pypi/simple/Django_REST.Framework/index.html", AdapterType: "pypi",
			PackageName: "Django_REST.Framework", StoragePath: "/tmp/pypi", CreatedAt: now,
		},
		{
			Key: "pypi/files/packages/aa/foo-bar-1.0.zip", AdapterType: "pypi",
			PackageName: "foo", StoragePath: "/tmp/pypi-ambiguous", CreatedAt: now,
		},
		{
			Key: "alpine/v3.19/main/x86_64/py3-requests-2.31.0-r0.apk", AdapterType: "alpine",
			PackageName: "py3-requests", StoragePath: "/tmp/alpine", CreatedAt: now,
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	pypiVulnerability := Vulnerability{
		OSVID: "GHSA-PYPI-ALIAS", Ecosystem: "pypi", PackageName: "Django_REST.Framework", CreatedAt: now,
	}
	if err := database.Create(&pypiVulnerability).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&DismissedVuln{
		VulnerabilityID: pypiVulnerability.ID, DismissedBy: "operator", CreatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "pypi", PackageName: "Django_REST.Framework", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&VulnerabilityCheck{
		Ecosystem: "pypi", PackageName: "foo", LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "pypi", PackageName: "Django_REST.Framework", Version: "1.0",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}
	var pypiEntry, ambiguousEntry, alpineEntry CacheEntry
	if err := database.Where("id = ?", entries[0].ID).Take(&pypiEntry).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Where("id = ?", entries[1].ID).Take(&ambiguousEntry).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Where("id = ?", entries[2].ID).Take(&alpineEntry).Error; err != nil {
		t.Fatal(err)
	}
	if pypiEntry.PackageName != "django-rest-framework" {
		t.Fatalf("PyPI cache package identity = %q", pypiEntry.PackageName)
	}
	if ambiguousEntry.PackageName != "" {
		t.Fatalf("ambiguous PyPI artifact identity = %q, want empty", ambiguousEntry.PackageName)
	}
	if alpineEntry.PackageName != "py3-requests" {
		t.Fatalf("non-PyPI package identity changed to %q", alpineEntry.PackageName)
	}
	for modelName, model := range map[string]any{
		"vulnerabilities":      &Vulnerability{},
		"vulnerability checks": &VulnerabilityCheck{},
		"project packages":     &ProjectPackage{},
		"dismissals":           &DismissedVuln{},
	} {
		var count int64
		query := database.Model(model)
		if modelName != "dismissals" {
			query = query.Where("lower(ecosystem) = ?", "pypi")
		}
		if err := query.Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d stale PyPI %s", count, modelName)
		}
	}
}

func TestPackageRuleDialectMigrationCollapsesHuggingFaceCaseAliases(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	now := time.Unix(400, 0).UTC()
	legacyCache := CacheEntry{
		Key:         "huggingface/OpenAI/Whisper-Tiny/resolve/main/config.json",
		AdapterType: "huggingface", PackageName: "OpenAI/Whisper-Tiny", StoragePath: "/tmp/hf", Size: 7,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := database.Create(&legacyCache).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&HuggingFaceRefPin{
		Key: "huggingface/OpenAI/Whisper-Tiny/ref/main", Commit: strings.Repeat("a", 40),
		ExpiresAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&ProjectPackage{
		ProjectID: 1, Ecosystem: "huggingface", PackageName: "OpenAI/Whisper-Tiny", Version: "main",
		FirstSeenAt: now, LastSeenAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	revocations := []HuggingFaceRepositoryRevocation{
		{
			Repository: "OpenAI/Whisper-Tiny", EscapedRepo: "OpenAI/Whisper-Tiny",
			Token: "11111111111111111111111111111111", CleanupSafe: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			Repository: "openai/whisper-tiny", EscapedRepo: "openai/whisper-tiny",
			Token: "22222222222222222222222222222222", CleanupSafe: true,
			CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
		},
	}
	if err := database.Create(&revocations).Error; err != nil {
		t.Fatal(err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}
	for modelName, model := range map[string]any{
		"mixed ref pins":         &HuggingFaceRefPin{},
		"mixed project packages": &ProjectPackage{},
	} {
		var count int64
		if err := database.Model(model).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("migration retained %d %s", count, modelName)
		}
	}
	var retired CacheEntry
	if err := database.First(&retired, legacyCache.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retired.Key == legacyCache.Key || !strings.HasPrefix(retired.Key, schemaV3RetiredHuggingFaceKeyPrefix) ||
		retired.AdapterType != schemaV3RetiredHuggingFaceAdapterType || retired.PackageName != "" ||
		!retired.ExpiresAt.Before(now) || retired.StoragePath != legacyCache.StoragePath || retired.Size != legacyCache.Size {
		t.Fatalf("legacy cache row was not safely retired: %+v", retired)
	}
	var marker HuggingFaceRepositoryRevocation
	if err := database.Take(&marker).Error; err != nil {
		t.Fatal(err)
	}
	var markerCount int64
	if err := database.Model(&HuggingFaceRepositoryRevocation{}).Count(&markerCount).Error; err != nil {
		t.Fatal(err)
	}
	if markerCount != 1 || marker.Repository != "openai/whisper-tiny" ||
		marker.EscapedRepo != "openai/whisper-tiny" || marker.CleanupSafe {
		t.Fatalf("canonical revocation marker = %+v (count %d)", marker, markerCount)
	}
}

func TestPackageRuleDialectMigrationRetiresHuggingFaceRowsAroundExistingReservedKeys(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	reservedKey := schemaV3RetiredHuggingFaceKeyPrefix + "2"
	blocker := CacheEntry{
		Key: reservedKey, AdapterType: "pypi", StoragePath: "pypi/files/blocker", Size: 3,
	}
	if err := database.Create(&blocker).Error; err != nil {
		t.Fatalf("seed reserved-key collision: %v", err)
	}
	legacy := CacheEntry{
		Key:          "huggingface/OpenAI/Whisper-Tiny/resolve/main/config.json",
		AdapterType:  "huggingface",
		PackageName:  "OpenAI/Whisper-Tiny",
		StoragePath:  "huggingface/OpenAI/Whisper-Tiny/resolve/main/config.json",
		Size:         7,
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		LastAccessed: time.Now().UTC(),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy Hugging Face row: %v", err)
	}
	if legacy.ID != 2 {
		t.Fatalf("legacy cache entry ID = %d, want 2 for the collision fixture", legacy.ID)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}
	var retired CacheEntry
	if err := database.First(&retired, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if want := reservedKey + "-1"; retired.Key != want {
		t.Fatalf("retired key = %q, want collision-free %q", retired.Key, want)
	}
	if len(retired.Key) > 512 || retired.AdapterType != schemaV3RetiredHuggingFaceAdapterType ||
		retired.PackageName != "" || retired.StoragePath != legacy.StoragePath || retired.Size != legacy.Size {
		t.Fatalf("retired cache row = %+v", retired)
	}
	var persistedBlocker CacheEntry
	if err := database.First(&persistedBlocker, blocker.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedBlocker.Key != reservedKey {
		t.Fatalf("collision blocker key changed to %q", persistedBlocker.Key)
	}
}

func TestPackageRuleDialectMigrationCanonicalizesQuarantineApprovals(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	first := time.Unix(500, 0).UTC()
	second := first.Add(time.Second)
	approvals := []ApprovedVersion{
		{
			Ecosystem: "NPM", Package: "left-pad", Version: "1.0.0+first",
			Reason: "older approval", ApprovedBy: 3, CreatedAt: first,
		},
		{
			Ecosystem: "npm", Package: "left-pad", Version: "1.0.0+second",
			Reason: "newer approval", ApprovedBy: 7, CreatedAt: second,
		},
		{
			Ecosystem: "PyPI", Package: "My_Pkg", Version: "v1.0-1",
			Reason: "python approval", ApprovedBy: 11, CreatedAt: first,
		},
	}
	if err := database.Create(&approvals).Error; err != nil {
		t.Fatalf("seed legacy approvals: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration: %v", err)
	}
	var migrated []ApprovedVersion
	if err := database.Order("ecosystem ASC, package ASC").Find(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 2 {
		t.Fatalf("approval count = %d, want 2 canonical semantic identities", len(migrated))
	}
	if migrated[0].Ecosystem != "npm" || migrated[0].Package != "left-pad" ||
		migrated[0].Version != "1.0.0+second" || migrated[0].Reason != "newer approval" ||
		migrated[0].ApprovedBy != 7 {
		t.Fatalf("canonical npm approval = %+v", migrated[0])
	}
	if migrated[1].Ecosystem != "pypi" || migrated[1].Package != "my-pkg" ||
		migrated[1].Version != "1.0.post1" {
		t.Fatalf("canonical PyPI approval = %+v", migrated[1])
	}
}

func TestPackageRuleDialectMigrationBatchesLargeQuarantineApprovalRewrite(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}

	const approvalCount = 4681 // 7 inserted columns exceed SQLite's 32,766-variable limit in one statement.
	approvals := make([]ApprovedVersion, approvalCount)
	for index := range approvals {
		approvals[index] = ApprovedVersion{
			Ecosystem: "npm",
			Package:   fmt.Sprintf("migration-scale-%04d", index),
			Version:   "1.0.0",
			Reason:    "scale qualification",
			CreatedAt: time.Unix(int64(index+1), 0).UTC(),
		}
	}
	if err := database.CreateInBatches(&approvals, 200).Error; err != nil {
		t.Fatalf("seed legacy approvals: %v", err)
	}

	if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
		t.Fatalf("apply package identity migration at SQLite scale: %v", err)
	}
	var count int64
	if err := database.Model(&ApprovedVersion{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != approvalCount {
		t.Fatalf("approval count = %d, want %d", count, approvalCount)
	}
}

func TestPackageRuleDialectMigrationRejectsInvalidQuarantineApproval(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	if err := database.Create(&ApprovedVersion{
		Ecosystem: "npm", Package: "left-pad", Version: "not-semver",
		Reason: "legacy invalid approval", CreatedAt: time.Unix(600, 0).UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "approval") || !strings.Contains(err.Error(), "not-semver") {
		t.Fatalf("migration error = %v, want actionable invalid approval", err)
	}
	var row ApprovedVersion
	if err := database.Take(&row).Error; err != nil {
		t.Fatalf("failed migration did not preserve legacy row: %v", err)
	}
	if row.Version != "not-semver" {
		t.Fatalf("failed migration mutated invalid approval to %+v", row)
	}
}

func TestPackageRuleDialectMigrationRejectsAmbiguousLegacyRulesAtomically(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
			t.Fatalf("apply migration %d: %v", index+1, err)
		}
	}
	if err := database.Exec(`
		INSERT INTO package_rules
			(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
		VALUES
			(23, 'go', 'example.com/module', '< 1.2.3', 'deny', 'legacy range', 'operator', ?, ?)
	`, time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()).Error; err != nil {
		t.Fatalf("seed legacy rule: %v", err)
	}

	err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
	if err == nil {
		t.Fatal("migration accepted an exact-only ecosystem range")
	}
	if !strings.Contains(err.Error(), "rule 23") || !strings.Contains(err.Error(), "ranges are not supported") {
		t.Fatalf("migration error = %q, want actionable rule ID and reason", err)
	}
	if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
		t.Fatal("failed migration left normalized_package_name behind")
	}
	var records int64
	if err := database.Model(&schemaMigrationRecord{}).Where("version = ?", 3).Count(&records).Error; err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("failed migration recorded %d version-3 ledger rows", records)
	}
}

func TestPackageRuleDialectMigrationRejectsNonCanonicalLegacyActionAtomically(t *testing.T) {
	for _, action := range []string{"audit", "ALLOW", " allow "} {
		t.Run(action, func(t *testing.T) {
			database := openCompileCacheMigrationTestDB(t)
			if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 2; index++ {
				if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
					t.Fatalf("apply migration %d: %v", index+1, err)
				}
			}
			if err := database.Exec(`
				INSERT INTO package_rules
					(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
				VALUES
					(29, 'pypi', '*', '*', ?, 'invalid decision', 'operator', ?, ?)
			`, action, time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()).Error; err != nil {
				t.Fatalf("seed non-canonical legacy rule: %v", err)
			}

			err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
			if err == nil {
				t.Fatalf("migration accepted non-canonical package-rule action %q", action)
			}
			if !strings.Contains(err.Error(), "rule 29") || !strings.Contains(err.Error(), "must already be exactly allow or deny") {
				t.Fatalf("migration error = %q, want actionable rule ID and strict action reason", err)
			}
			if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
				t.Fatal("failed migration left normalized_package_name behind")
			}
			var records int64
			if err := database.Model(&schemaMigrationRecord{}).Where("version = ?", 3).Count(&records).Error; err != nil {
				t.Fatal(err)
			}
			if records != 0 {
				t.Fatalf("failed migration recorded %d version-3 ledger rows", records)
			}
		})
	}
}

func TestPackageRuleDialectMigrationRejectsUnenforceableVersionRules(t *testing.T) {
	tests := []struct {
		name        string
		ecosystem   string
		packageName string
		version     string
		wantReason  string
	}{
		{
			name: "APT filename omits epoch", ecosystem: "apt", packageName: "libc6",
			version: "1:2.36-9+deb12u10", wantReason: "cannot be enforced",
		},
		{
			name: "RubyGems artifact identity is not enforceable", ecosystem: "rubygems", packageName: "nokogiri",
			version: "1.16.5", wantReason: "unsupported package rule ecosystem",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openCompileCacheMigrationTestDB(t)
			if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 2; index++ {
				if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
					t.Fatalf("apply migration %d: %v", index+1, err)
				}
			}
			if err := database.Exec(`
				INSERT INTO package_rules
					(id, ecosystem, package_name, version, action, reason, created_by, created_at, updated_at)
				VALUES
					(31, ?, ?, ?, 'deny', 'legacy exact selector', 'operator', ?, ?)
			`, test.ecosystem, test.packageName, test.version, time.Unix(100, 0).UTC(), time.Unix(200, 0).UTC()).Error; err != nil {
				t.Fatalf("seed %s version rule: %v", test.ecosystem, err)
			}

			err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC())
			if err == nil {
				t.Fatalf("migration accepted a %s version rule that proxy requests cannot enforce", test.ecosystem)
			}
			if !strings.Contains(err.Error(), "rule 31") || !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("migration error = %q, want actionable %s rule ID and reason", err, test.ecosystem)
			}
			if database.Migrator().HasColumn(&PackageRule{}, "NormalizedPackageName") {
				t.Fatal("failed migration left normalized_package_name behind")
			}
			var records int64
			if err := database.Model(&schemaMigrationRecord{}).Where("version = ?", 3).Count(&records).Error; err != nil {
				t.Fatal(err)
			}
			if records != 0 {
				t.Fatalf("failed migration recorded %d version-3 ledger rows", records)
			}
		})
	}
}
