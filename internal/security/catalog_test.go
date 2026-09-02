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
	catalog, err := NewAdvisoryCatalog(database, ttl)
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

func fixedVersionForTest(t *testing.T, affectedRanges string) string {
	t.Helper()
	var ranges []struct {
		Events []osvEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(affectedRanges), &ranges); err != nil {
		t.Fatalf("decode affected ranges: %v", err)
	}
	for _, affectedRange := range ranges {
		for _, event := range affectedRange.Events {
			if event.Fixed != "" {
				return event.Fixed
			}
		}
	}
	return ""
}

func TestAdvisoryCatalogConstructorValidation(t *testing.T) {
	database := newCatalogTestDB(t)
	if _, err := NewAdvisoryCatalog(nil, time.Hour); err == nil {
		t.Fatal("NewAdvisoryCatalog(nil) succeeded")
	}
	if _, err := NewAdvisoryCatalog(database, 0); err == nil {
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
		fixedVersionForTest(t, stored.AffectedRanges) != "2.3.4" {
		t.Fatalf("stored scan projection = %+v", stored)
	}
}

func TestAdvisoryCatalogRecordScanUsesDialectPackageIdentity(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory(
		"OSV-PYPI-ALIAS", "PyPI", "django-rest-framework", 8.2,
	)

	_, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "Django_rest.framework"},
		[]OSVVulnerability{advisory},
	)
	if err != nil {
		t.Fatalf("RecordScan equivalent PyPI identity: %v", err)
	}

	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", advisory.ID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PackageName != "django-rest-framework" {
		t.Fatalf("stored package name = %q, want PEP 503 identity", stored.PackageName)
	}
	check := loadCatalogCheck(t, database, "pypi", "django-rest-framework")
	if !check.HasVulnerabilities || check.VulnerabilityCount != 1 {
		t.Fatalf("normalized vulnerability check = %+v", check)
	}
}

func TestAdvisoryCatalogRecordScanRejectsPackageNameBeyondStoredIdentityLimit(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	packageName := strings.Repeat("a", 300)
	advisory := catalogTestAdvisory("OSV-PYPI-OVERSIZE", "PyPI", packageName, 8.2)

	receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: packageName},
		[]OSVVulnerability{advisory},
	)
	if !errors.Is(err, ErrInvalidPackageScan) {
		t.Fatalf("RecordScan error = %v, want ErrInvalidPackageScan", err)
	}
	if receipt != (IngestReceipt{}) {
		t.Fatalf("RecordScan receipt = %+v, want zero", receipt)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 0)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 0)
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
		fixedVersionForTest(t, stored.AffectedRanges) != "3.1.4" {
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
		fixedVersionForTest(t, stored[0].AffectedRanges) != "4.5.6" {
		t.Fatalf("npm projection = %+v", stored[0])
	}
	if stored[1].Ecosystem != "pypi" || stored[1].PackageName != "alpha" ||
		fixedVersionForTest(t, stored[1].AffectedRanges) != "1.2.3" {
		t.Fatalf("pypi projection = %+v", stored[1])
	}
}

func TestAdvisoryCatalogImportNormalizesAffectedPackageIdentity(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-PYPI-ALIASES", "PyPI", "Friendly_Bard", 8.4)
	advisory.Affected = append(advisory.Affected, osvAffected{
		Package: &osvPackage{Name: "friendly-bard", Ecosystem: "PyPI"},
	})

	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, advisory))
	if err != nil {
		t.Fatal(err)
	}
	if want := (IngestReceipt{Received: 1, Advisories: 1, Packages: 1}); receipt != want {
		t.Fatalf("Import receipt = %+v, want %+v", receipt, want)
	}

	var stored []db.Vulnerability
	if err := database.Where("osv_id = ?", advisory.ID).Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Ecosystem != "pypi" || stored[0].PackageName != "friendly-bard" {
		t.Fatalf("stored identities = %+v, want one pypi/friendly-bard advisory", stored)
	}
	check := loadCatalogCheck(t, database, "pypi", "friendly-bard")
	if !check.HasVulnerabilities || check.VulnerabilityCount != 1 {
		t.Fatalf("normalized identity check = %+v", check)
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
				fixedVersionForTest(t, stored.AffectedRanges) != "2.0.0" {
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
		fixedVersionForTest(t, stored.AffectedRanges) != "5.0.0" {
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

func TestAdvisoryCatalogStaleImportPreservesEffectiveAdvisoryWithoutAutomaticRule(t *testing.T) {
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
	if receipt.RulesCreated != 0 {
		t.Fatalf("stale Import RulesCreated = %d, want safety-disabled", receipt.RulesCreated)
	}

	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
	var stored db.Vulnerability
	if err := database.Where("ecosystem = ? AND package_name = ?", "pypi", "effective").
		First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CVSSScore != 9.8 || fixedVersionForTest(t, stored.AffectedRanges) != "5.0.0" {
		t.Fatalf("stored advisory = %+v, want effective newer fields", stored)
	}
}

func TestAdvisoryCatalogEnabledAutoBlockPolicyRemainsSafetyDisabled(t *testing.T) {
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
	if first.RulesCreated != 0 {
		t.Fatalf("first RulesCreated = %d, want safety-disabled", first.RulesCreated)
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

	assertCatalogTableCount(t, database, &db.Vulnerability{}, 1)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 1)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogNeverTurnsUnfixedAdvisoryIntoPackageWideDeny(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}
	catalog := newCatalogForTest(t, database, time.Hour)
	advisory := catalogTestAdvisory("OSV-UNFIXED", "PyPI", "bounded", 9.8)
	advisory.Affected[0].Ranges = []osvRange{{
		Type:   "ECOSYSTEM",
		Events: []osvEvent{{Introduced: "0"}},
	}}

	receipt, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "bounded"},
		[]OSVVulnerability{advisory},
	)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 {
		t.Fatalf("RulesCreated = %d, want 0", receipt.RulesCreated)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 1)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 1)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogDoesNotGuessUnenforceableRanges(t *testing.T) {
	for _, test := range []struct {
		ecosystem    string
		osvEcosystem string
		packageName  string
	}{
		{ecosystem: "go", osvEcosystem: "Go", packageName: "example.com/module"},
		{ecosystem: "apt", osvEcosystem: "Debian", packageName: "demo"},
		{ecosystem: "maven", osvEcosystem: "Maven", packageName: "org.example:demo"},
	} {
		t.Run(test.ecosystem, func(t *testing.T) {
			database := newCatalogTestDB(t)
			if err := database.Create(&db.SecurityPolicy{
				Ecosystem:        test.ecosystem,
				AutoBlockEnabled: true,
				MinCVSSScore:     7,
			}).Error; err != nil {
				t.Fatal(err)
			}
			catalog := newCatalogForTest(t, database, time.Hour)
			advisory := catalogTestAdvisory("OSV-UNENFORCEABLE", test.osvEcosystem, test.packageName, 9.8)

			receipt, err := catalog.RecordScan(
				context.Background(),
				PackageRef{Ecosystem: test.ecosystem, Name: test.packageName},
				[]OSVVulnerability{advisory},
			)
			if err != nil {
				t.Fatalf("RecordScan: %v", err)
			}
			if receipt.RulesCreated != 0 {
				t.Fatalf("RulesCreated = %d, want 0", receipt.RulesCreated)
			}
			assertCatalogTableCount(t, database, &db.Vulnerability{}, 1)
			assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 1)
			assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
		})
	}
}

func TestAdvisoryCatalogPreservesRulesCreatedBySecurityScannerUsername(t *testing.T) {
	database := newCatalogTestDB(t)
	rules := []db.PackageRule{
		{
			Ecosystem: "pypi", PackageName: "scan-owned-by-human", Version: "*",
			Action: "deny", Reason: "manual scan rule", CreatedBy: "security-scanner",
		},
		{
			Ecosystem: "pypi", PackageName: "import-owned-by-human", Version: "*",
			Action: "deny", Reason: "manual import rule", CreatedBy: "security-scanner",
		},
	}
	if err := database.Create(&rules).Error; err != nil {
		t.Fatal(err)
	}

	catalog, err := NewAdvisoryCatalog(database, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.RecordScan(
		context.Background(),
		PackageRef{Ecosystem: "pypi", Name: "scan-owned-by-human"},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	imported := catalogTestAdvisory("OSV-HUMAN-OWNER", "PyPI", "import-owned-by-human", 9.8)
	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, imported)); err != nil {
		t.Fatal(err)
	}

	var remaining []db.PackageRule
	if err := database.Order("id ASC").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0].ID != rules[0].ID || remaining[1].ID != rules[1].ID {
		t.Fatalf("rules after scan/import = %+v, want both human-authored rows", remaining)
	}
}

func TestAdvisoryCatalogAuthoritativeScanNeverCreatesAutoBlockRules(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	catalog := newCatalogForTest(t, database, time.Hour)
	pkg := PackageRef{Ecosystem: "pypi", Name: "authoritative-rules"}
	first := catalogTestAdvisory("OSV-AUTHORITATIVE-ONE", "PyPI", pkg.Name, 9.1)
	second := catalogTestAdvisory("OSV-AUTHORITATIVE-TWO", "PyPI", pkg.Name, 8.2)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, second}); err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first}); err != nil {
		t.Fatal(err)
	}
	var rules []db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ?", pkg.Ecosystem, pkg.Name).
		Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules after 2-to-1 scan = %+v, want none", rules)
	}

	if _, err := catalog.RecordScan(context.Background(), pkg, nil); err != nil {
		t.Fatal(err)
	}
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogPartialImportNeverCreatesAutoBlockRules(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	catalog := newCatalogForTest(t, database, time.Hour)
	pkg := PackageRef{Ecosystem: "pypi", Name: "partial-import-rules"}
	first := catalogTestAdvisory("OSV-PARTIAL-ONE", "PyPI", pkg.Name, 9.1)
	second := catalogTestAdvisory("OSV-PARTIAL-TWO", "PyPI", pkg.Name, 8.2)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{first, second}); err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.Import(context.Background(), marshalAdvisories(t, first)); err != nil {
		t.Fatal(err)
	}
	var rules []db.PackageRule
	if err := database.Where("ecosystem = ? AND package_name = ?", pkg.Ecosystem, pkg.Name).
		Order("reason ASC").Find(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules after partial Import = %+v, want none", rules)
	}
}

func TestAdvisoryCatalogCVSSChangesNeverCreateAutoBlockRules(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	catalog := newCatalogForTest(t, database, time.Hour)
	pkg := PackageRef{Ecosystem: "pypi", Name: "score-falls"}
	high := catalogTestAdvisory("OSV-SCORE-FALLS", "PyPI", pkg.Name, 9.1)
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{high}); err != nil {
		t.Fatal(err)
	}

	low := high
	low.Modified = high.Modified.Add(time.Hour)
	low.Severity = []osvSeverity{{Type: "CVSS_V3", Score: "5.0"}}
	receipt, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{low})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 {
		t.Fatalf("low-score RecordScan = %+v, want no rule changes", receipt)
	}
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogImportNeverDeletesPackageRules(t *testing.T) {
	database := newCatalogTestDB(t)
	legacyRules := []db.PackageRule{
		{Ecosystem: "npm", PackageName: "preserved-npm", Version: "*", Action: "deny", CreatedBy: "security-scanner"},
		{Ecosystem: "pypi", PackageName: "preserved-pypi", Version: "*", Action: "deny", CreatedBy: "security-scanner"},
	}
	if err := database.Create(&legacyRules).Error; err != nil {
		t.Fatal(err)
	}

	injected := errors.New("package-rule deletion is forbidden during advisory ingestion")
	deleteCalls := 0
	if err := database.Callback().Delete().Before("gorm:delete").Register("catalog:test-forbid-rule-delete", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "package_rules" {
			deleteCalls++
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}

	catalog := newCatalogForTest(t, database, time.Hour)
	npm := catalogTestAdvisory("OSV-PRESERVED-NPM", "npm", "preserved-npm", 9.1)
	pypi := catalogTestAdvisory("OSV-PRESERVED-PYPI", "PyPI", "preserved-pypi", 9.1)
	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, []OSVVulnerability{npm, pypi}))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if receipt.Advisories != 2 || receipt.Packages != 2 || deleteCalls != 0 {
		t.Fatalf("Import = %+v, package-rule delete calls = %d", receipt, deleteCalls)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 2)
	assertCatalogTableCount(t, database, &db.VulnerabilityCheck{}, 2)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 2)
}

func TestAdvisoryCatalogOlderImportDoesNotCreateOrRegressAutoBlockRule(t *testing.T) {
	database := newCatalogTestDB(t)
	if err := database.Create(&db.SecurityPolicy{
		Ecosystem:        "pypi",
		AutoBlockEnabled: true,
		MinCVSSScore:     7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	catalog := newCatalogForTest(t, database, time.Hour)
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
	receipt, err := catalog.Import(context.Background(), marshalAdvisories(t, older))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.RulesCreated != 0 {
		t.Fatalf("older Import = %+v, want no rule change", receipt)
	}

	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", newer.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.ModifiedAt.Equal(newer.Modified) || stored.CVSSScore != 9.8 {
		t.Fatalf("advisory regressed after older Import: %+v", stored)
	}
}

func TestAdvisoryCatalogSharesSafetyDisabledGateAcrossScanAndImport(t *testing.T) {
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
	if rulesCreated != 0 {
		t.Fatalf("concurrent RulesCreated total = %d, want safety-disabled", rulesCreated)
	}
	assertCatalogTableCount(t, database, &db.Vulnerability{}, 1)
	assertCatalogTableCount(t, database, &db.PackageRule{}, 0)
}

func TestAdvisoryCatalogImportRollsBackEveryWriteStage(t *testing.T) {
	stages := []struct {
		name string
		fail func(*gorm.DB) bool
	}{
		{name: "advisory", fail: func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*db.Vulnerability); return ok }},
		{name: "check", fail: func(tx *gorm.DB) bool { _, ok := tx.Statement.Dest.(*db.VulnerabilityCheck); return ok }},
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
		{name: "control character package name", input: string(mustJSON(t, []OSVVulnerability{valid, catalogTestAdvisory("OSV-CONTROL", "PyPI", "bad\x00name", 5)}))},
		{name: "oversize dialect-valid package name", input: string(mustJSON(t, []OSVVulnerability{valid, catalogTestAdvisory("OSV-OVERSIZE", "PyPI", strings.Repeat("a", 300), 5)}))},
		{name: "ecosystem illegal package name", input: string(mustJSON(t, []OSVVulnerability{valid, catalogTestAdvisory("OSV-ILLEGAL", "PyPI", "bad/name", 5)}))},
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
