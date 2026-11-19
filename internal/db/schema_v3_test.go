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

func TestPackageRuleDialectMigrationRepairsLegacyScannerIdentity(t *testing.T) {
	type check struct{ ecosystem, packageName string }
	type project struct{ ecosystem, packageName, version string }
	entry := func(key, adapterType, packageName, storagePath string) CacheEntry {
		return CacheEntry{Key: key, AdapterType: adapterType, PackageName: packageName, StoragePath: storagePath}
	}
	const goModule = "github.com/!azure/azure-sdk-for-go"
	const rubyGemsGuess = "nokogiri-1.16.5-x86_64"

	for _, tc := range []struct {
		name string
		// ecosystem is the lowercase scanner ecosystem whose advisories,
		// checks, and project packages the migration must invalidate.
		ecosystem string
		now       int64
		entries   []CacheEntry
		// wantNames is the post-migration PackageName per entry; nil checks
		// only that every cache row is retained.
		wantNames     []string
		vulnerability Vulnerability
		sentinel      *Vulnerability
		checks        []check
		project       project
		// sentinelCheck is a non-ecosystem check that must survive.
		sentinelCheck *check
	}{
		{
			name: "npm", ecosystem: "npm", now: 300,
			entries: []CacheEntry{
				entry("npm/legacy/metadata.json", "npm", "legacy", "/tmp/legacy"),
				entry("npm-exact-v1/ExactName/metadata.json", "npm", "ExactName", "/tmp/exact"),
			},
			vulnerability: Vulnerability{OSVID: "GHSA-NPM", Ecosystem: "npm", PackageName: "legacy"},
			sentinel:      &Vulnerability{OSVID: "ALPINE-NPM-SENTINEL", Ecosystem: "alpine", PackageName: "preserved"},
			checks:        []check{{"npm", "legacy"}},
			project:       project{"npm", "legacy", "1.0.0"},
		},
		{
			// APT identities are cleared without source-package provenance.
			name: "APT", ecosystem: "apt", now: 315,
			entries: []CacheEntry{
				entry("apt/ubuntu/pool/main/c/curl/curl_8.0-1_amd64.deb", "apt", "curl", "/tmp/apt-artifact"),
				entry("apt/ubuntu/dists/jammy/InRelease", "APT", "ubuntu", "/tmp/apt-metadata"),
				entry("alpine/v3.19/main/x86_64/apt-2.0-r0.apk", "alpine", "apt", "/tmp/alpine"),
			},
			wantNames:     []string{"", "", "apt"},
			vulnerability: Vulnerability{OSVID: "DSA-APT-GUESSED", Ecosystem: "APT", PackageName: "curl"},
			sentinel:      &Vulnerability{OSVID: "ALPINE-APT-SENTINEL", Ecosystem: "alpine", PackageName: "apt"},
			checks:        []check{{"APT", "curl"}},
			project:       project{"APT", "curl", "8.0-1"},
		},
		{
			// Go identities become the canonical decoded module path; a
			// non-canonical (unescaped) key is cleared.
			name: "Go", ecosystem: "go", now: 320,
			entries: []CacheEntry{
				entry("go/github.com/!azure/azure-sdk-for-go/@v/v1.2.3.zip", "go", goModule, "/tmp/go-artifact"),
				entry("go/github.com/!azure/azure-sdk-for-go/@v/list", "GO", "untrusted-legacy-name", "/tmp/go-metadata"),
				entry("go/github.com/Azure/azure-sdk-for-go/@v/v1.2.3.zip", "go", "github.com/Azure/azure-sdk-for-go", "/tmp/go-invalid"),
				entry("alpine/v3.19/main/x86_64/py3-requests-2.31.0-r0.apk", "alpine", "py3-requests", "/tmp/alpine"),
			},
			wantNames: []string{
				"github.com/Azure/azure-sdk-for-go", "github.com/Azure/azure-sdk-for-go", "", "py3-requests",
			},
			vulnerability: Vulnerability{OSVID: "GO-LEGACY-IDENTITY", Ecosystem: "Go", PackageName: goModule},
			sentinel:      &Vulnerability{OSVID: "ALPINE-PRESERVED", Ecosystem: "alpine", PackageName: "py3-requests"},
			checks:        []check{{"Go", goModule}},
			project:       project{"Go", goModule, "v1.2.3"},
		},
		{
			// Only a strict src/contrib artifact yields a CRAN identity.
			name: "CRAN", ecosystem: "cran", now: 323,
			entries: []CacheEntry{
				entry("cran/src/contrib/Data.Table_1.15.4.tar.gz", "cran", "Data", "/tmp/cran-artifact"),
				entry("cran/src/contrib/PACKAGES.gz", "CRAN", "PACKAGES.gz", "/tmp/cran-metadata"),
				entry("cran/not-a-repository/Data.Table_1.15.4.tar.gz", "cran", "Data.Table", "/tmp/cran-ambiguous-path"),
				entry("cran/src/contrib/not_a_package_1.0.tar.gz", "cran", "not", "/tmp/cran-ambiguous-name"),
				entry("alpine/v3.19/main/x86_64/R-1.0-r0.apk", "alpine", "R", "/tmp/alpine"),
			},
			wantNames:     []string{"Data.Table", "", "", "", "R"},
			vulnerability: Vulnerability{OSVID: "OSV-CRAN-GUESSED", Ecosystem: "CRAN", PackageName: "PACKAGES.gz"},
			sentinel:      &Vulnerability{OSVID: "ALPINE-CRAN-SENTINEL", Ecosystem: "alpine", PackageName: "R"},
			checks:        []check{{"CRAN", "PACKAGES.gz"}},
			project:       project{"CRAN", "Data", "1.15.4"},
		},
		{
			// Cargo artifacts keep the case-exact crate name; index, config,
			// and non-artifact keys are cleared.
			name: "Cargo", ecosystem: "cargo", now: 324,
			entries: []CacheEntry{
				entry("cargo/crates/Serde/1.0.228.crate", "cargo", "serde", "/tmp/cargo-artifact"),
				entry("cargo/index/se/rd/serde", "CARGO", "serde", "/tmp/cargo-index"),
				entry("cargo/config.json", "cargo", "config.json", "/tmp/cargo-config"),
				entry("cargo/crates/serde/not-an-artifact", "cargo", "serde", "/tmp/cargo-ambiguous"),
				entry("alpine/v3.19/main/x86_64/cargo-1.0-r0.apk", "alpine", "cargo", "/tmp/alpine"),
			},
			wantNames:     []string{"Serde", "", "", "", "cargo"},
			vulnerability: Vulnerability{OSVID: "RUSTSEC-LEGACY-INDEX", Ecosystem: "Cargo", PackageName: "serde"},
			sentinel:      &Vulnerability{OSVID: "ALPINE-CARGO-SENTINEL", Ecosystem: "alpine", PackageName: "cargo"},
			checks:        []check{{"Cargo", "serde"}},
			project:       project{"Cargo", "serde", "1.0.228"},
		},
		{
			// NuGet identities are cleared without canonical registry casing.
			name: "NuGet", ecosystem: "nuget", now: 325,
			entries: []CacheEntry{
				entry("nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg", "nuget", "newtonsoft.json", "/tmp/nuget-artifact"),
				entry("nuget/v3/registration/newtonsoft.json/index.json", "NUGET", "newtonsoft.json", "/tmp/nuget-metadata"),
				entry("alpine/v3.19/main/x86_64/nuget-6.0-r0.apk", "alpine", "nuget", "/tmp/alpine"),
			},
			wantNames:     []string{"", "", "nuget"},
			vulnerability: Vulnerability{OSVID: "GHSA-NUGET-LOWERCASE", Ecosystem: "NuGet", PackageName: "newtonsoft.json"},
			sentinel:      &Vulnerability{OSVID: "ALPINE-NUGET-SENTINEL", Ecosystem: "alpine", PackageName: "nuget"},
			checks:        []check{{"NuGet", "newtonsoft.json"}},
			project:       project{"NuGet", "newtonsoft.json", "13.0.3"},
		},
		{
			name: "RubyGems", ecosystem: "rubygems", now: 325,
			entries: []CacheEntry{
				entry("rubygems/gems/nokogiri-1.16.5-x86_64-linux.gem", "rubygems", rubyGemsGuess, "/tmp/nokogiri-platform-gem"),
			},
			vulnerability: Vulnerability{OSVID: "GHSA-RUBYGEMS-GUESSED", Ecosystem: "rubygems", PackageName: rubyGemsGuess},
			checks:        []check{{"rubygems", rubyGemsGuess}},
			project:       project{"rubygems", rubyGemsGuess, "linux"},
			sentinelCheck: &check{"alpine", "ruby"},
		},
		{
			// Maven artifacts gain the full OSV coordinate; metadata is
			// ambiguous and cleared.
			name: "Maven", ecosystem: "maven", now: 300,
			entries: []CacheEntry{
				entry("maven/org/apache/logging/log4j/log4j-core/2.20.0/log4j-core-2.20.0.jar", "maven", "log4j-core", "/tmp/artifact"),
				entry("maven/org/apache/logging/log4j/log4j-core/maven-metadata.xml", "maven", "logging", "/tmp/metadata"),
				entry("pypi/files/requests-2.32.0-py3-none-any.whl", "pypi", "requests", "/tmp/pypi"),
			},
			wantNames:     []string{"org.apache.logging.log4j:log4j-core", "", "requests"},
			vulnerability: Vulnerability{OSVID: "GHSA-MAVEN", Ecosystem: "maven", PackageName: "log4j-core"},
			checks:        []check{{"maven", "log4j-core"}},
			project:       project{"maven", "log4j-core", "2.20.0"},
		},
		{
			// Composer p2 and dist keys yield normalized coordinates.
			name: "Composer", ecosystem: "composer", now: 325,
			entries: []CacheEntry{
				entry("composer/p2/Symfony/Console.json", "composer", "wrong/legacy", "/tmp/composer-p2"),
				entry("composer/dist/monolog/monolog/abc123.tar.gz", "composer", "wrong/dist", "/tmp/composer-dist"),
				entry("composer/p2/not-a-package.json", "composer", "guessed", "/tmp/composer-bad"),
				entry("composer/packages.json", "composer", "metadata-guess", "/tmp/composer-index"),
				entry("alpine/v3.19/main/x86_64/curl-1.0-r0.apk", "alpine", "curl", "/tmp/alpine"),
			},
			wantNames:     []string{"symfony/console", "monolog/monolog", "", "", "curl"},
			vulnerability: Vulnerability{OSVID: "GHSA-COMPOSER-LEGACY", Ecosystem: "composer", PackageName: "wrong/legacy"},
			sentinel:      &Vulnerability{OSVID: "GHSA-ALPINE-SENTINEL", Ecosystem: "alpine", PackageName: "curl"},
			checks:        []check{{"composer", "wrong/legacy"}},
			project:       project{"composer", "wrong/legacy", "1.0"},
		},
		{
			// PyPI simple-index identities are PEP 503 normalized; an
			// ambiguous file name is cleared.
			name: "PyPI", ecosystem: "pypi", now: 350,
			entries: []CacheEntry{
				entry("pypi/simple/Django_REST.Framework/index.html", "pypi", "Django_REST.Framework", "/tmp/pypi"),
				entry("pypi/files/packages/aa/foo-bar-1.0.zip", "pypi", "foo", "/tmp/pypi-ambiguous"),
				entry("alpine/v3.19/main/x86_64/py3-requests-2.31.0-r0.apk", "alpine", "py3-requests", "/tmp/alpine"),
			},
			wantNames:     []string{"django-rest-framework", "", "py3-requests"},
			vulnerability: Vulnerability{OSVID: "GHSA-PYPI-ALIAS", Ecosystem: "pypi", PackageName: "Django_REST.Framework"},
			checks:        []check{{"pypi", "Django_REST.Framework"}, {"pypi", "foo"}},
			project:       project{"pypi", "Django_REST.Framework", "1.0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openCompileCacheMigrationTestDB(t)
			if err := database.AutoMigrate(&schemaMigrationRecord{}); err != nil {
				t.Fatal(err)
			}
			for index := 0; index < 2; index++ {
				if err := applySchemaMigration(database, schemaMigrations[index], time.Now().UTC()); err != nil {
					t.Fatalf("apply migration %d: %v", index+1, err)
				}
			}

			now := time.Unix(tc.now, 0).UTC()
			entries := tc.entries
			for index := range entries {
				entries[index].CreatedAt = now
			}
			if err := database.Create(&entries).Error; err != nil {
				t.Fatalf("seed cache entries: %v", err)
			}
			vulnerability := tc.vulnerability
			vulnerability.CreatedAt = now
			if err := database.Omit("DetailRevision").Create(&vulnerability).Error; err != nil {
				t.Fatalf("seed %s vulnerability: %v", tc.name, err)
			}
			dismissed := []uint{vulnerability.ID}
			var sentinel Vulnerability
			if tc.sentinel != nil {
				sentinel = *tc.sentinel
				sentinel.CreatedAt = now
				if err := database.Omit("DetailRevision").Create(&sentinel).Error; err != nil {
					t.Fatalf("seed sentinel vulnerability: %v", err)
				}
				dismissed = append(dismissed, sentinel.ID)
			}
			for _, vulnerabilityID := range dismissed {
				if err := database.Create(&DismissedVuln{
					VulnerabilityID: vulnerabilityID, DismissedBy: "operator", CreatedAt: now,
				}).Error; err != nil {
					t.Fatalf("seed vulnerability dismissal: %v", err)
				}
			}
			for _, seed := range tc.checks {
				if err := database.Create(&VulnerabilityCheck{
					Ecosystem: seed.ecosystem, PackageName: seed.packageName,
					LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
				}).Error; err != nil {
					t.Fatalf("seed %s vulnerability check: %v", tc.name, err)
				}
			}
			if err := database.Create(&ProjectPackage{
				ProjectID: 1, Ecosystem: tc.project.ecosystem, PackageName: tc.project.packageName,
				Version: tc.project.version, FirstSeenAt: now, LastSeenAt: now,
			}).Error; err != nil {
				t.Fatalf("seed %s project package: %v", tc.name, err)
			}
			var sentinelCheck VulnerabilityCheck
			if tc.sentinelCheck != nil {
				sentinelCheck = VulnerabilityCheck{
					Ecosystem: tc.sentinelCheck.ecosystem, PackageName: tc.sentinelCheck.packageName,
					LastFetchedAt: now, NextFetchAt: now.Add(time.Hour),
				}
				if err := database.Create(&sentinelCheck).Error; err != nil {
					t.Fatalf("seed sentinel vulnerability check: %v", err)
				}
			}

			if err := applySchemaMigration(database, schemaMigrations[2], time.Now().UTC()); err != nil {
				t.Fatalf("apply %s scanner identity migration: %v", tc.name, err)
			}

			var migratedEntries []CacheEntry
			if err := database.Order("id ASC").Find(&migratedEntries).Error; err != nil {
				t.Fatal(err)
			}
			if len(migratedEntries) != len(entries) {
				t.Fatalf("cache rows after migration = %d, want %d retained objects", len(migratedEntries), len(entries))
			}
			for index, migrated := range migratedEntries {
				if migrated.ID != entries[index].ID {
					t.Fatalf("cache row %d id = %d, want retained %d", index, migrated.ID, entries[index].ID)
				}
				if tc.wantNames != nil && migrated.PackageName != tc.wantNames[index] {
					t.Fatalf("cache identity %d (%s) = %q, want %q",
						index, migrated.Key, migrated.PackageName, tc.wantNames[index])
				}
			}

			for modelName, model := range map[string]any{
				"vulnerabilities":      &Vulnerability{},
				"vulnerability checks": &VulnerabilityCheck{},
				"project packages":     &ProjectPackage{},
			} {
				var count int64
				if err := database.Model(model).Where("lower(ecosystem) = ?", tc.ecosystem).Count(&count).Error; err != nil {
					t.Fatalf("count %s %s: %v", tc.name, modelName, err)
				}
				if count != 0 {
					t.Fatalf("migration retained %d legacy %s %s", count, tc.name, modelName)
				}
			}
			var ecosystemDismissals, allDismissals int64
			if err := database.Model(&DismissedVuln{}).
				Where("vulnerability_id = ?", vulnerability.ID).Count(&ecosystemDismissals).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Model(&DismissedVuln{}).Count(&allDismissals).Error; err != nil {
				t.Fatal(err)
			}
			wantDismissals := int64(len(dismissed) - 1)
			if ecosystemDismissals != 0 || allDismissals != wantDismissals {
				t.Fatalf("post-migration %s dismissals=%d all dismissals=%d, want 0/%d sentinel",
					tc.name, ecosystemDismissals, allDismissals, wantDismissals)
			}
			if tc.sentinel != nil {
				var sentinelDismissals, sentinelAdvisories int64
				if err := database.Model(&DismissedVuln{}).
					Where("vulnerability_id = ?", sentinel.ID).Count(&sentinelDismissals).Error; err != nil {
					t.Fatal(err)
				}
				if err := database.Model(&Vulnerability{}).
					Where("id = ?", sentinel.ID).Count(&sentinelAdvisories).Error; err != nil {
					t.Fatal(err)
				}
				if sentinelDismissals != 1 || sentinelAdvisories != 1 {
					t.Fatalf("sentinel dismissals=%d advisories=%d, want 1/1", sentinelDismissals, sentinelAdvisories)
				}
			}
			if tc.sentinelCheck != nil {
				var sentinelChecks int64
				if err := database.Model(&VulnerabilityCheck{}).
					Where("id = ?", sentinelCheck.ID).Count(&sentinelChecks).Error; err != nil {
					t.Fatal(err)
				}
				if sentinelChecks != 1 {
					t.Fatalf("sentinel vulnerability checks = %d, want 1", sentinelChecks)
				}
			}
		})
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
