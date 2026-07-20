package security

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func newCatalogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&db.Vulnerability{},
		&db.VulnerabilityCheck{},
		&db.SecurityPolicy{},
		&db.PackageRule{},
	); err != nil {
		t.Fatal(err)
	}
	return database
}

func newCatalogForTest(t *testing.T, database *gorm.DB, ttl time.Duration) *AdvisoryCatalog {
	t.Helper()
	catalog, err := NewAdvisoryCatalog(database, ttl, nil)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func catalogTestAdvisory(id, osvEcosystem, packageName string, score float32) OSVVulnerability {
	return OSVVulnerability{
		ID:      id,
		Summary: "summary " + id,
		Details: "details " + id,
		Aliases: []string{"ALIAS-" + id},
		Severity: []osvSeverity{{
			Type:  "CVSS_V3",
			Score: strings.TrimRight(strings.TrimRight(stringScore(score), "0"), "."),
		}},
		Affected: []osvAffected{{
			Package: &osvPackage{Name: packageName, Ecosystem: osvEcosystem},
			Ranges: []osvRange{{
				Type: "SEMVER",
				Events: []osvEvent{
					{Introduced: "0"},
					{Fixed: "2.0.0"},
				},
			}},
		}},
		References: []osvRef{{Type: "ADVISORY", URL: "https://example.test/" + id}},
		Published:  time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Modified:   time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC),
	}
}

func stringScore(score float32) string {
	data, _ := json.Marshal(score)
	return string(data)
}

func marshalAdvisories(t *testing.T, value any) *strings.Reader {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReader(string(data))
}

func TestAdvisoryCatalogConstructorValidation(t *testing.T) {
	database := newCatalogTestDB(t)
	if _, err := NewAdvisoryCatalog(nil, time.Hour, nil); err == nil {
		t.Fatal("NewAdvisoryCatalog(nil) succeeded")
	}
	if _, err := NewAdvisoryCatalog(database, 0, nil); err == nil {
		t.Fatal("NewAdvisoryCatalog with zero TTL succeeded")
	}
}

func TestAdvisoryCatalogRecordScanCountsTTLAndEmptyScan(t *testing.T) {
	database := newCatalogTestDB(t)
	const ttl = 3 * time.Hour
	catalog := newCatalogForTest(t, database, ttl)
	pkg := PackageRef{Ecosystem: "pypi", Name: "demo"}

	first := catalogTestAdvisory("OSV-ONE", "PyPI", "demo", 7.5)
	duplicate := first
	duplicate.Summary = "latest summary"
	duplicate.Modified = first.Modified.Add(time.Hour)
	second := OSVVulnerability{ID: "OSV-TWO"} // The queried package supplies identity.
	receipt, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, duplicate, second})
	if err != nil {
		t.Fatal(err)
	}
	wantReceipt := IngestReceipt{Received: 3, Advisories: 2, Packages: 1, Duplicates: 1}
	if receipt != wantReceipt {
		t.Fatalf("RecordScan receipt = %+v, want %+v", receipt, wantReceipt)
	}

	var vulnerabilities []db.Vulnerability
	if err := database.Order("osv_id").Find(&vulnerabilities).Error; err != nil {
		t.Fatal(err)
	}
	if len(vulnerabilities) != 2 {
		t.Fatalf("stored vulnerabilities = %d, want 2", len(vulnerabilities))
	}
	for _, vulnerability := range vulnerabilities {
		if vulnerability.Ecosystem != "pypi" || vulnerability.PackageName != "demo" {
			t.Fatalf("stored identity = %s/%s", vulnerability.Ecosystem, vulnerability.PackageName)
		}
	}
	if vulnerabilities[0].Summary != "latest summary" {
		t.Fatalf("deduplicated summary = %q, want latest summary", vulnerabilities[0].Summary)
	}

	check := loadCatalogCheck(t, database, "pypi", "demo")
	if !check.HasVulnerabilities || check.VulnerabilityCount != 2 {
		t.Fatalf("scan check = %+v, want 2 vulnerabilities", check)
	}
	if got := check.NextFetchAt.Sub(check.LastFetchedAt); got != ttl {
		t.Fatalf("scan TTL = %v, want %v", got, ttl)
	}

	emptyReceipt, err := catalog.RecordScan(context.Background(), pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := (IngestReceipt{Packages: 1}); emptyReceipt != want {
		t.Fatalf("empty RecordScan receipt = %+v, want %+v", emptyReceipt, want)
	}
	check = loadCatalogCheck(t, database, "pypi", "demo")
	if check.HasVulnerabilities || check.VulnerabilityCount != 0 {
		t.Fatalf("empty scan check = %+v, want no current vulnerabilities", check)
	}
	var historical int64
	if err := database.Model(&db.Vulnerability{}).Count(&historical).Error; err != nil {
		t.Fatal(err)
	}
	if historical != 2 {
		t.Fatalf("empty scan deleted historical vulnerabilities: count = %d", historical)
	}
}

func TestAdvisoryCatalogRecordScanProjectsOnlyQueriedPackage(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-SCAN-MULTI", "npm", "other", 8.2)
	advisory.Affected[0].Ranges[0].Events[1].Fixed = "9.9.9"
	advisory.Affected = append(advisory.Affected, osvAffected{
		Package: &osvPackage{Name: "target", Ecosystem: "PyPI"},
		Ranges: []osvRange{{
			Type: "ECOSYSTEM",
			Events: []osvEvent{
				{Introduced: "0"},
				{Fixed: "2.3.4"},
			},
		}},
	})

	receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "target"},
		[]OSVVulnerability{advisory},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := (IngestReceipt{Received: 1, Advisories: 1, Packages: 1}); receipt != want {
		t.Fatalf("RecordScan receipt = %+v, want %+v", receipt, want)
	}

	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", advisory.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Ecosystem != "pypi" || stored.PackageName != "target" ||
		ExtractFixedVersion(stored.AffectedRanges) != "2.3.4" {
		t.Fatalf("stored scan projection = %+v", stored)
	}
}

func TestAdvisoryCatalogRecordScanRejectsUnsupportedMismatchedPackage(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-UNSUPPORTED-MISMATCH", "UnsupportedEco", "other-package", 8.0)

	receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "queried-package"},
		[]OSVVulnerability{advisory},
	)
	if !errors.Is(err, ErrInvalidPackageScan) {
		t.Fatalf("RecordScan error = %v, want ErrInvalidPackageScan", err)
	}
	if receipt != (IngestReceipt{}) {
		t.Fatalf("failed RecordScan receipt = %+v, want zero", receipt)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 0)
}

func TestAdvisoryCatalogRecordScanDeduplicatesByNewestModifiedAt(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	newer := catalogTestAdvisory("OSV-SCAN-ORDER", "PyPI", "ordered", 9.2)
	newer.Summary = "newer"
	newer.Modified = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	older := catalogTestAdvisory("OSV-SCAN-ORDER", "PyPI", "ordered", 4.2)
	older.Summary = "older"
	older.Modified = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "ordered"},
		[]OSVVulnerability{newer, older},
	); err != nil {
		t.Fatal(err)
	}
	var stored db.Vulnerability
	if err := database.Where(
		"osv_id = ? AND ecosystem = ? AND package_name = ?",
		newer.ID, "pypi", "ordered",
	).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Summary != "newer" || !stored.ModifiedAt.Equal(newer.Modified) {
		t.Fatalf("stored scan duplicate winner = %+v, want newer", stored)
	}
}

func TestAdvisoryCatalogImportSingleArrayCountsTTLAndCompleteUpdate(t *testing.T) {
	database := newCatalogTestDB(t)
	const ttl = 90 * time.Minute
	catalog := newCatalogForTest(t, database, ttl)

	original := catalogTestAdvisory("OSV-PYPI-ONE", "PyPI", "demo", 5.0)
	singleReceipt, err := catalog.Import(context.Background(), marshalAdvisories(t, original))
	if err != nil {
		t.Fatal(err)
	}
	if want := (IngestReceipt{Received: 1, Advisories: 1, Packages: 1}); singleReceipt != want {
		t.Fatalf("single-object receipt = %+v, want %+v", singleReceipt, want)
	}

	updated := original
	updated.Summary = "updated summary"
	updated.Details = "updated details"
	updated.Aliases = []string{"CVE-UPDATED", "GHSA-UPDATED"}
	updated.References = []osvRef{{Type: "WEB", URL: "https://updated.example.test"}}
	updated.Severity = []osvSeverity{{Type: "CVSS_V3", Score: "9.4"}}
	updated.Affected[0].Ranges[0].Events[1].Fixed = "3.1.4"
	updated.Published = time.Date(2023, 3, 4, 5, 6, 7, 0, time.UTC)
	updated.Modified = time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	second := catalogTestAdvisory("OSV-PYPI-TWO", "PyPI", "demo", 4.2)
	npm := catalogTestAdvisory("OSV-NPM", "npm", "left-pad", 8.1)
	unsupported := catalogTestAdvisory("OSV-LINUX", "Linux", "kernel", 9.9)

	arrayReceipt, err := catalog.Import(context.Background(), marshalAdvisories(t, []OSVVulnerability{
		updated,
		second,
		second,
		npm,
		unsupported,
	}))
	if err != nil {
		t.Fatal(err)
	}
	wantReceipt := IngestReceipt{Received: 5, Advisories: 3, Packages: 2, Duplicates: 1, Skipped: 1}
	if arrayReceipt != wantReceipt {
		t.Fatalf("array receipt = %+v, want %+v", arrayReceipt, wantReceipt)
	}

	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", updated.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Summary != updated.Summary || stored.Details != updated.Details ||
		stored.Severity != "critical" || stored.CVSSScore != 9.4 ||
		stored.Aliases != "CVE-UPDATED,GHSA-UPDATED" ||
		stored.References != `["https://updated.example.test"]` ||
		!stored.PublishedAt.Equal(updated.Published) || !stored.ModifiedAt.Equal(updated.Modified) ||
		ExtractFixedVersion(stored.AffectedRanges) != "3.1.4" {
		t.Fatalf("upsert did not replace all advisory fields: %+v", stored)
	}

	pypiCheck := loadCatalogCheck(t, database, "pypi", "demo")
	if pypiCheck.VulnerabilityCount != 2 {
		t.Fatalf("pypi check count = %d, want actual DB total 2", pypiCheck.VulnerabilityCount)
	}
	if got := pypiCheck.NextFetchAt.Sub(pypiCheck.LastFetchedAt); got != ttl {
		t.Fatalf("pypi import TTL = %v, want %v", got, ttl)
	}
	npmCheck := loadCatalogCheck(t, database, "npm", "left-pad")
	if npmCheck.VulnerabilityCount != 1 {
		t.Fatalf("npm check count = %d, want 1", npmCheck.VulnerabilityCount)
	}
	var unsupportedRows int64
	if err := database.Model(&db.Vulnerability{}).Where("osv_id = ?", unsupported.ID).Count(&unsupportedRows).Error; err != nil {
		t.Fatal(err)
	}
	if unsupportedRows != 0 {
		t.Fatalf("unsupported ecosystem stored %d rows", unsupportedRows)
	}
}

func TestAdvisoryCatalogImportProjectsOneAdvisoryAcrossPackages(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-MULTI-PACKAGE", "PyPI", "alpha", 8.4)
	advisory.Affected[0].Ranges[0].Events[1].Fixed = "1.2.3"
	advisory.Affected = append(advisory.Affected, osvAffected{
		Package: &osvPackage{Name: "bravo", Ecosystem: "npm"},
		Ranges: []osvRange{{
			Type: "SEMVER",
			Events: []osvEvent{
				{Introduced: "0"},
				{Fixed: "4.5.6"},
			},
		}},
	})

	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, advisory))
	if err != nil {
		t.Fatal(err)
	}
	if want := (IngestReceipt{Received: 1, Advisories: 2, Packages: 2}); receipt != want {
		t.Fatalf("Import receipt = %+v, want %+v", receipt, want)
	}

	var stored []db.Vulnerability
	if err := database.Where("osv_id = ?", advisory.ID).
		Order("ecosystem, package_name").
		Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored projections = %d, want 2", len(stored))
	}
	if stored[0].Ecosystem != "npm" || stored[0].PackageName != "bravo" ||
		ExtractFixedVersion(stored[0].AffectedRanges) != "4.5.6" {
		t.Fatalf("npm projection = %+v", stored[0])
	}
	if stored[1].Ecosystem != "pypi" || stored[1].PackageName != "alpha" ||
		ExtractFixedVersion(stored[1].AffectedRanges) != "1.2.3" {
		t.Fatalf("pypi projection = %+v", stored[1])
	}
}

func TestAdvisoryCatalogImportDeduplicatesByNewestModifiedAt(t *testing.T) {
	for _, order := range []struct {
		name       string
		newerFirst bool
	}{
		{name: "older then newer"},
		{name: "newer then older", newerFirst: true},
	} {
		t.Run(order.name, func(t *testing.T) {
			database := newCatalogTestDB(t)
			catalog := newCatalogForTest(t, database, time.Hour)
			older := catalogTestAdvisory("OSV-PAYLOAD-ORDER", "PyPI", "ordered", 4.1)
			older.Summary = "older"
			older.Modified = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			older.Affected[0].Ranges[0].Events[1].Fixed = "1.0.0"
			newer := catalogTestAdvisory("OSV-PAYLOAD-ORDER", "PyPI", "ordered", 9.1)
			newer.Summary = "newer"
			newer.Modified = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			newer.Affected[0].Ranges[0].Events[1].Fixed = "2.0.0"
			input := []OSVVulnerability{older, newer}
			if order.newerFirst {
				input[0], input[1] = input[1], input[0]
			}

			receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, input))
			if err != nil {
				t.Fatal(err)
			}
			if want := (IngestReceipt{Received: 2, Advisories: 1, Packages: 1, Duplicates: 1}); receipt != want {
				t.Fatalf("Import receipt = %+v, want %+v", receipt, want)
			}
			var stored db.Vulnerability
			if err := database.Where(
				"osv_id = ? AND ecosystem = ? AND package_name = ?",
				newer.ID, "pypi", "ordered",
			).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Summary != "newer" || stored.CVSSScore != 9.1 ||
				ExtractFixedVersion(stored.AffectedRanges) != "2.0.0" {
				t.Fatalf("stored duplicate winner = %+v, want newer projection", stored)
			}
		})
	}
}

func TestAdvisoryCatalogImportDoesNotRegressStoredAdvisory(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	newer := catalogTestAdvisory("OSV-MONOTONIC", "PyPI", "monotonic", 9.3)
	newer.Summary = "newer stored advisory"
	newer.Modified = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer.Affected[0].Ranges[0].Events[1].Fixed = "5.0.0"
	older := catalogTestAdvisory("OSV-MONOTONIC", "PyPI", "monotonic", 4.3)
	older.Summary = "stale imported advisory"
	older.Modified = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	older.Affected[0].Ranges[0].Events[1].Fixed = "1.0.0"

	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, newer)); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, older)); err != nil {
		t.Fatal(err)
	}

	var stored db.Vulnerability
	if err := database.Where(
		"osv_id = ? AND ecosystem = ? AND package_name = ?",
		newer.ID, "pypi", "monotonic",
	).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Summary != "newer stored advisory" || stored.CVSSScore != 9.3 ||
		!stored.ModifiedAt.Equal(newer.Modified) ||
		ExtractFixedVersion(stored.AffectedRanges) != "5.0.0" {
		t.Fatalf("stale import regressed stored advisory: %+v", stored)
	}
}

func TestAdvisoryCatalogReconcileTimestampRules(t *testing.T) {
	nonZero := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		existingModified time.Time
		incomingModified time.Time
		wantSummary      string
	}{
		{
			name:             "timestamped existing beats missing incoming timestamp",
			existingModified: nonZero,
			wantSummary:      "existing",
		},
		{
			name:             "timestamped existing wins an equal timestamp tie",
			existingModified: nonZero,
			incomingModified: nonZero,
			wantSummary:      "existing",
		},
		{
			name:        "incoming wins when both timestamps are missing",
			wantSummary: "incoming",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newCatalogTestDB(t)
			catalog := newCatalogForTest(t, database, time.Hour)
			existing := catalogTestAdvisory("OSV-TIMESTAMP-RULE", "PyPI", "timestamp", 8)
			existing.Summary = "existing"
			existing.Modified = test.existingModified
			incoming := catalogTestAdvisory("OSV-TIMESTAMP-RULE", "PyPI", "timestamp", 8)
			incoming.Summary = "incoming"
			incoming.Modified = test.incomingModified

			if _, err := catalog.Import(context.Background(), marshalAdvisories(t, existing)); err != nil {
				t.Fatal(err)
			}
			if _, err := catalog.Import(context.Background(), marshalAdvisories(t, incoming)); err != nil {
				t.Fatal(err)
			}
			var stored db.Vulnerability
			if err := database.Where(
				"osv_id = ? AND ecosystem = ? AND package_name = ?",
				existing.ID, "pypi", "timestamp",
			).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Summary != test.wantSummary {
				t.Fatalf("stored summary = %q, want %q", stored.Summary, test.wantSummary)
			}
		})
	}
}

func TestAdvisoryCatalogStaleImportAutoBlocksFromEffectiveAdvisory(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	newer := catalogTestAdvisory("OSV-EFFECTIVE-RULE", "PyPI", "effective", 9.8)
	newer.Modified = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	newer.Affected[0].Ranges[0].Events[1].Fixed = "5.0.0"
	older := catalogTestAdvisory("OSV-EFFECTIVE-RULE", "PyPI", "effective", 4.0)
	older.Modified = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	older.Affected[0].Ranges[0].Events[1].Fixed = "1.0.0"

	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, newer)); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     9,
	}).Error; err != nil {
		t.Fatal(err)
	}
	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, older))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 1 {
		t.Fatalf("stale Import RulesCreated = %d, want 1 from effective advisory", receipt.RulesCreated)
	}

	var rule db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ?", "pypi", "effective").
		First(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if rule.Version != "<5.0.0" || !strings.Contains(rule.Reason, "CVSS 9.8") {
		t.Fatalf("auto-block rule = %+v, want effective advisory fields", rule)
	}
}

func TestAdvisoryCatalogAutoBlockIsTransactionalAndIdempotent(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-BLOCK", "PyPI", "dangerous", 9.8)

	first, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "dangerous"},
		[]OSVVulnerability{advisory},
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.RulesCreated != 1 {
		t.Fatalf("first RulesCreated = %d, want 1", first.RulesCreated)
	}
	second, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "dangerous"},
		[]OSVVulnerability{advisory},
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.RulesCreated != 0 {
		t.Fatalf("second RulesCreated = %d, want 0", second.RulesCreated)
	}

	var rules []db.PackageRule
	if err := database.Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("auto-block rules = %d, want 1", len(rules))
	}
	if rules[0].Action != "deny" || rules[0].Version != "<2.0.0" ||
		rules[0].CreatedBy != autoBlockCreatedBy || !strings.Contains(rules[0].Reason, advisory.ID) {
		t.Fatalf("auto-block rule = %+v", rules[0])
	}
}

func TestAdvisoryCatalogInvalidatesRuleCacheAfterCommittedCreate(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "invalidate-create"}
	advisory := catalogTestAdvisory("OSV-INVALIDATE-CREATE", "PyPI", pkg.Name, 9.1)

	first, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{advisory})
	if err != nil {
		t.Fatal(err)
	}
	if first.RulesCreated != 1 || invalidations != 1 {
		t.Fatalf("first RecordScan = (%+v, invalidations %d), want one created rule and one invalidation", first, invalidations)
	}

	second, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{advisory})
	if err != nil {
		t.Fatal(err)
	}
	if second.RulesCreated != 0 || invalidations != 1 {
		t.Fatalf("second RecordScan = (%+v, invalidations %d), want no rule change or invalidation", second, invalidations)
	}
}

func TestAdvisoryCatalogReconcilesExistingAutoBlockRuleAndDuplicates(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "reconcile-update"}
	original := catalogTestAdvisory("OSV-RECONCILE-UPDATE", "PyPI", pkg.Name, 9.1)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{original}); err != nil {
		t.Fatal(err)
	}

	var existing db.PackageRule
	if err := database.Where("created_by = ?", autoBlockCreatedBy).First(&existing).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := existing
	duplicate.ID = 0
	duplicate.Version = "<1.0.0"
	if err := database.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}

	updated := original
	updated.Modified = original.Modified.Add(time.Hour)
	updated.Severity = []osvSeverity{{Type: "CVSS_V3", Score: "9.8"}}
	updated.Affected[0].Ranges[0].Events[1].Fixed = "3.0.0"
	invalidations = 0
	receipt, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{updated})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 || invalidations != 1 {
		t.Fatalf("updated RecordScan = (%+v, invalidations %d), want update-only and one invalidation", receipt, invalidations)
	}

	var rules []db.PackageRule
	if err := database.Where("created_by = ?", autoBlockCreatedBy).Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Version != "<3.0.0" || rules[0].Action != "deny" ||
		rules[0].Reason != "Auto-blocked: OSV-RECONCILE-UPDATE (CVSS 9.8)" {
		t.Fatalf("reconciled rules = %+v", rules)
	}
}

func TestAdvisoryCatalogDisabledPolicyRemovesRecognizableRulesOnEmptyScan(t *testing.T) {
	database := newCatalogTestDB(t)
	policy := db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}
	if err := database.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "disabled-empty"}
	first := catalogTestAdvisory("OSV-DISABLED-ONE", "PyPI", pkg.Name, 9.1)
	second := catalogTestAdvisory("OSV-DISABLED-TWO", "PyPI", pkg.Name, 8.2)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, second}); err != nil {
		t.Fatal(err)
	}
	unknown := db.PackageRule{
		Ecosystem:   pkg.Ecosystem,
		PackageName: pkg.Name,
		Version:     "*",
		Action:      "deny",
		Reason:      "legacy scanner rule without an OSV identity",
		CreatedBy:   autoBlockCreatedBy,
	}
	if err := database.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&policy).Update("auto_block_enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	invalidations = 0
	receipt, err := catalog.RecordScan(context.Background(), pkg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 || invalidations != 1 {
		t.Fatalf("empty disabled RecordScan = (%+v, invalidations %d), want deletion and one invalidation", receipt, invalidations)
	}

	var rules []db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ? AND created_by = ?", pkg.Ecosystem, pkg.Name, autoBlockCreatedBy).
		Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != unknown.ID {
		t.Fatalf("rules after disabled empty scan = %+v, want only unrecognized legacy rule", rules)
	}
}

func TestAdvisoryCatalogAuthoritativeScanRemovesAbsentAutoBlockRules(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "authoritative-rules"}
	first := catalogTestAdvisory("OSV-AUTHORITATIVE-ONE", "PyPI", pkg.Name, 9.1)
	second := catalogTestAdvisory("OSV-AUTHORITATIVE-TWO", "PyPI", pkg.Name, 8.2)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, second}); err != nil {
		t.Fatal(err)
	}

	invalidations = 0
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first}); err != nil {
		t.Fatal(err)
	}
	if invalidations != 1 {
		t.Fatalf("2-to-1 scan invalidations = %d, want 1", invalidations)
	}
	var rules []db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ? AND created_by = ?", pkg.Ecosystem, pkg.Name, autoBlockCreatedBy).
		Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Reason != "Auto-blocked: OSV-AUTHORITATIVE-ONE (CVSS 9.1)" {
		t.Fatalf("rules after 2-to-1 scan = %+v", rules)
	}

	invalidations = 0
	if _, err := catalog.RecordScan(context.Background(), pkg, nil); err != nil {
		t.Fatal(err)
	}
	if invalidations != 1 {
		t.Fatalf("1-to-0 scan invalidations = %d, want 1", invalidations)
	}
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogPartialImportPreservesOtherAutoBlockRules(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "partial-import-rules"}
	first := catalogTestAdvisory("OSV-PARTIAL-ONE", "PyPI", pkg.Name, 9.1)
	second := catalogTestAdvisory("OSV-PARTIAL-TWO", "PyPI", pkg.Name, 8.2)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, second}); err != nil {
		t.Fatal(err)
	}

	invalidations = 0
	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, first)); err != nil {
		t.Fatal(err)
	}
	if invalidations != 0 {
		t.Fatalf("unchanged partial Import invalidations = %d, want 0", invalidations)
	}
	var rules []db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ? AND created_by = ?", pkg.Ecosystem, pkg.Name, autoBlockCreatedBy).
		Order("reason ASC").Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].Reason != "Auto-blocked: OSV-PARTIAL-ONE (CVSS 9.1)" ||
		rules[1].Reason != "Auto-blocked: OSV-PARTIAL-TWO (CVSS 8.2)" {
		t.Fatalf("rules after partial Import = %+v", rules)
	}
}

func TestAdvisoryCatalogRemovesAutoBlockRuleWhenScoreFallsBelowThreshold(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	pkg := PackageRef{Ecosystem: "pypi", Name: "score-falls"}
	high := catalogTestAdvisory("OSV-SCORE-FALLS", "PyPI", pkg.Name, 9.1)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{high}); err != nil {
		t.Fatal(err)
	}

	low := high
	low.Modified = high.Modified.Add(time.Hour)
	low.Severity = []osvSeverity{{Type: "CVSS_V3", Score: "5.0"}}
	invalidations = 0
	receipt, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{low})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 || invalidations != 1 {
		t.Fatalf("low-score RecordScan = (%+v, invalidations %d), want deletion and one invalidation", receipt, invalidations)
	}
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogDoesNotInvalidateRulesWhenLaterWriteRollsBack(t *testing.T) {
	database := newCatalogTestDB(t)
	for _, ecosystem := range []string{"npm", "pypi"} {
		if err := database.Create(&db.SecurityPolicy{
			Ecosystem:        ecosystem,
			AutoBlockEnabled: true,
			MinCVSSScore:     7,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	injected := errors.New("injected later rule write failure")
	if err := database.Callback().Create().Before("gorm:create").Register("catalog:test-late-rule-rollback", func(tx *gorm.DB) {
		rule, ok := tx.Statement.Dest.(*db.PackageRule)
		if ok && rule.Ecosystem == "pypi" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	npm := catalogTestAdvisory("OSV-ROLLBACK-NPM", "npm", "rollback-npm", 9.1)
	pypi := catalogTestAdvisory("OSV-ROLLBACK-PYPI", "PyPI", "rollback-pypi", 9.1)
	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, []OSVVulnerability{npm, pypi}))
	if !errors.Is(err, injected) {
		t.Fatalf("Import error = %v, want %v", err, injected)
	}
	if receipt != (IngestReceipt{}) || invalidations != 0 {
		t.Fatalf("rolled-back Import = (%+v, invalidations %d), want zero receipt and no invalidation", receipt, invalidations)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 0)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogOlderImportDoesNotRegressAutoBlockRule(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	invalidations := 0
	catalog, err := NewAdvisoryCatalog(database, time.Hour, func() { invalidations++ })
	if err != nil {
		t.Fatal(err)
	}
	newer := catalogTestAdvisory("OSV-OLDER-IMPORT", "PyPI", "older-import", 9.8)
	newer.Modified = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer.Affected[0].Ranges[0].Events[1].Fixed = "3.0.0"
	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, newer)); err != nil {
		t.Fatal(err)
	}

	older := newer
	older.Modified = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	older.Severity = []osvSeverity{{Type: "CVSS_V3", Score: "7.1"}}
	older.Affected[0].Ranges[0].Events[1].Fixed = "1.0.0"
	invalidations = 0
	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, older))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 || invalidations != 0 {
		t.Fatalf("older Import = (%+v, invalidations %d), want no rule change", receipt, invalidations)
	}

	var rule db.PackageRule
	if err := database.Where("created_by = ?", autoBlockCreatedBy).First(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if rule.Version != "<3.0.0" || rule.Reason != "Auto-blocked: OSV-OLDER-IMPORT (CVSS 9.8)" {
		t.Fatalf("rule regressed after older Import: %+v", rule)
	}
	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", newer.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.ModifiedAt.Equal(newer.Modified) || stored.CVSSScore != 9.8 {
		t.Fatalf("advisory regressed after older Import: %+v", stored)
	}
}

func TestAdvisoryCatalogSharesAutoBlockGateAcrossScanAndImport(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-SHARED-GATE", "PyPI", "shared", 9.1)
	importJSON := mustJSON(t, advisory)

	type result struct {
		receipt IngestReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	go func() {
		<-start
		receipt, err := catalog.RecordScan(
			context.Background(),
			PackageRef{Ecosystem: "pypi", Name: "shared"},
			[]OSVVulnerability{advisory},
		)
		results <- result{receipt: receipt, err: err}
	}()
	go func() {
		<-start
		receipt, err := catalog.Import(context.Background(), strings.NewReader(string(importJSON)))
		results <- result{receipt: receipt, err: err}
	}()
	close(start)

	rulesCreated := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		rulesCreated += result.receipt.RulesCreated
	}
	if rulesCreated != 1 {
		t.Fatalf("concurrent RulesCreated total = %d, want 1", rulesCreated)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 1)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 1)
}

func TestAdvisoryCatalogImportRollsBackEveryWriteStage(t *testing.T) {
	stages := []struct {
		name string
		fail func(*gorm.DB) bool
	}{
		{name: "advisory", fail: func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*db.Vulnerability); return ok }},
		{name: "check", fail: func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*db.VulnerabilityCheck); return ok }},
		{name: "rule", fail: func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*db.PackageRule); return ok }},
	}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			database := newCatalogTestDB(t)
			if err := database.Create(&db.SecurityPolicy{
				Ecosystem:        "pypi",
				AutoBlockEnabled: true,
				MinCVSSScore:     7,
			}).Error; err != nil {
				t.Fatal(err)
			}
			catalog := newCatalogForTest(t, database, time.Hour)
			injected := errors.New("injected " + stage.name + " write failure")
			callbackName := "catalog:test-fail-" + stage.name
			if err := database.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
				if stage.fail(tx) {
					tx.AddError(injected)
				}
			}); err != nil {
				t.Fatal(err)
			}

			receipt, err := catalog.Import(context.Background(), marshalAdvisories(t,
				catalogTestAdvisory("OSV-ROLLBACK", "PyPI", "rollback", 9.9),
			))
			if !errors.Is(err, injected) {
				t.Fatalf("Import error = %v, want %v", err, injected)
			}
			if receipt != (IngestReceipt{}) {
				t.Fatalf("failed Import receipt = %+v, want zero", receipt)
			}
			assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
			assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 0)
			assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
		})
	}
}

func TestAdvisoryCatalogWriteGateHonorsContext(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	if err := database.Callback().Create().Before("gorm:create").Register("catalog:test-hold-writer", func(tx *gorm.DB) {
		vulnerability, ok := tx.Statement.Dest.(*db.Vulnerability)
		if !ok || vulnerability.PackageName != "held" {
			return
		}
		enteredOnce.Do(func() { close(entered) })
		<-release
	}); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := catalog.RecordScan(
			context.Background(),
			PackageRef{Ecosystem: "pypi", Name: "held"},
			[]OSVVulnerability{{ID: "OSV-HELD"}},
		)
		firstDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("first writer did not enter its transaction")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	receipt, err := catalog.RecordScan(
		ctx,
		PackageRef{Ecosystem: "pypi", Name: "waiting"},
		[]OSVVulnerability{{ID: "OSV-WAITING"}},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("waiting RecordScan error = %v, want deadline exceeded", err)
	}
	if receipt != (IngestReceipt{}) {
		close(release)
		t.Fatalf("canceled RecordScan receipt = %+v, want zero", receipt)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RecordScan: %v", err)
	}
}

func TestAdvisoryCatalogCanceledContextDoesNotReadOrWrite(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if receipt, err := catalog.RecordScan(
		ctx,
		PackageRef{Ecosystem: "pypi", Name: "canceled"},
		[]OSVVulnerability{{ID: "OSV-CANCELED"}},
	); !errors.Is(err, context.Canceled) || receipt != (IngestReceipt{}) {
		t.Fatalf("canceled RecordScan = (%+v, %v)", receipt, err)
	}
	if receipt, err := catalog.Import(ctx, readerThatPanics{}); !errors.Is(err, context.Canceled) || receipt != (IngestReceipt{}) {
		t.Fatalf("canceled Import = (%+v, %v)", receipt, err)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
}

func TestAdvisoryCatalogImportPreservesReaderFailureAsServerError(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	injected := errors.New("injected reader failure")

	receipt, err := catalog.Import(context.Background(), failingReader{err: injected})
	if !errors.Is(err, injected) {
		t.Fatalf("Import error = %v, want wrapped reader failure", err)
	}
	if errors.Is(err, ErrInvalidAdvisoryImport) {
		t.Fatalf("Import error = %v, reader failure must not be classified as invalid input", err)
	}
	if receipt != (IngestReceipt{}) {
		t.Fatalf("reader failure receipt = %+v, want zero", receipt)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
}

func TestAdvisoryCatalogImportRejectsOversizeInput(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	receipt, err := catalog.Import(context.Background(), &repeatedByteReader{
		remaining: maxAdvisoryImportBytes + 1,
		value:     ' ',
	})
	if !errors.Is(err, ErrAdvisoryImportTooLarge) {
		t.Fatalf("oversize Import error = %v", err)
	}
	if receipt != (IngestReceipt{}) {
		t.Fatalf("oversize Import receipt = %+v, want zero", receipt)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
}

func TestAdvisoryCatalogImportPrevalidatesEntireFile(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	valid := catalogTestAdvisory("OSV-VALID", "PyPI", "demo", 5)

	tests := []struct {
		name  string
		input string
	}{
		{name: "malformed", input: `[{`},
		{name: "empty", input: ``},
		{name: "null", input: `null`},
		{name: "missing ID", input: string(mustJSON(t, []OSVVulnerability{valid, catalogTestAdvisory("", "PyPI", "demo", 5)}))},
		{name: "missing package", input: string(mustJSON(t, OSVVulnerability{ID: "OSV-NO-PACKAGE"}))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt, err := catalog.Import(context.Background(), strings.NewReader(test.input))
			if !errors.Is(err, ErrInvalidAdvisoryImport) {
				t.Fatalf("Import error = %v, want ErrInvalidAdvisoryImport", err)
			}
			if receipt != (IngestReceipt{}) {
				t.Fatalf("invalid Import receipt = %+v, want zero", receipt)
			}
			assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
			assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 0)
		})
	}
}

func TestAdvisoryCatalogAllowsOSVIDAcrossPackagesAndRejectsMismatchedScan(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	first := catalogTestAdvisory("OSV-GLOBAL", "PyPI", "one", 5)
	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, first)); err != nil {
		t.Fatal(err)
	}

	conflictingImport := catalogTestAdvisory("OSV-GLOBAL", "npm", "two", 5)
	if receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, conflictingImport)); err != nil || receipt.Advisories != 1 {
		t.Fatalf("second-package Import = (%+v, %v)", receipt, err)
	}
	if receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "npm", Name: "two"},
		[]OSVVulnerability{{ID: "OSV-GLOBAL"}},
	); err != nil || receipt.Advisories != 1 {
		t.Fatalf("second-package RecordScan = (%+v, %v)", receipt, err)
	}

	conflictingScan := catalogTestAdvisory("OSV-SCAN-CONFLICT", "npm", "other", 5)
	if receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "one"},
		[]OSVVulnerability{conflictingScan},
	); !errors.Is(err, ErrInvalidPackageScan) || receipt != (IngestReceipt{}) {
		t.Fatalf("conflicting RecordScan = (%+v, %v)", receipt, err)
	}

	var stored []db.Vulnerability
	if err := database.Where("osv_id = ?", "OSV-GLOBAL").Order("ecosystem").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].Ecosystem != "npm" || stored[0].PackageName != "two" ||
		stored[1].Ecosystem != "pypi" || stored[1].PackageName != "one" {
		t.Fatalf("stored multi-package identities = %+v", stored)
	}
	var conflictingRows int64
	if err := database.Model(&db.Vulnerability{}).
		Where("osv_id = ?", "OSV-SCAN-CONFLICT").
		Count(&conflictingRows).Error; err != nil {
		t.Fatal(err)
	}
	if conflictingRows != 0 {
		t.Fatalf("conflicting scan stored %d rows", conflictingRows)
	}
}

func loadCatalogCheck(t *testing.T, database *gorm.DB, ecosystem, packageName string) db.VulnerabilityCheck {
	t.Helper()
	var check db.VulnerabilityCheck
	if err := database.Where("ecosystem = ? AND package_name = ?", ecosystem, packageName).
		First(&check).Error; err != nil {
		t.Fatal(err)
	}
	return check
}

func assertCatalogTableCount(t *testing.T, database *gorm.DB, model any, want int64) {
	t.Helper()
	var count int64
	if err := database.Model(model).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%T count = %d, want %d", model, count, want)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type repeatedByteReader struct {
	remaining int
	value     byte
}

func (r *repeatedByteReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := range p[:n] {
		p[i] = r.value
	}
	r.remaining -= n
	return n, nil
}

type readerThatPanics struct{}

func (readerThatPanics) Read([]byte) (int, error) {
	panic("canceled Import read its source")
}

type failingReader struct {
	err error
}

func (r failingReader) Read([]byte) (int, error) {
	return 0, r.err
}
