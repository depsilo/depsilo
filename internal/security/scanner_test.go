package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

func newScannerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "scanner.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&db.CacheEntry{},
		&db.Vulnerability{},
		&db.VulnerabilityCheck{},
		&db.SecurityPolicy{},
		&db.PackageRule{},
	); err != nil {
		t.Fatal(err)
	}
	return database
}

func newScannerTestFetcher(t *testing.T, server *httptest.Server) *Fetcher {
	t.Helper()
	fetcher := &Fetcher{
		client:  server.Client(),
		baseURL: server.URL,
		limiter: time.NewTicker(time.Nanosecond),
		closed:  make(chan struct{}),
	}
	t.Cleanup(fetcher.Close)
	return fetcher
}

func newScannerTestCatalog(t *testing.T, database *gorm.DB) *AdvisoryCatalog {
	t.Helper()
	catalog, err := NewAdvisoryCatalog(database, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func addCachedPackage(t *testing.T, database *gorm.DB, ecosystem, packageName string) {
	t.Helper()
	key := fmt.Sprintf("%s/%s/index", ecosystem, packageName)
	if ecosystem == "npm" {
		key = packagekey.NPMExactIdentityCachePrefix + packageName + "/metadata.json"
	}
	entry := &db.CacheEntry{
		Key:         key,
		AdapterType: ecosystem,
		PackageName: packageName,
	}
	if err := database.Create(entry).Error; err != nil {
		t.Fatal(err)
	}
}

func TestScannerIgnoresLegacyCaseFoldedNPMCacheNamespace(t *testing.T) {
	database := newScannerTestDB(t)
	entries := []db.CacheEntry{
		{
			Key: "npm/legacy-name/metadata.json", AdapterType: "npm",
			PackageName: "legacy-name",
		},
		{
			Key:         packagekey.NPMExactIdentityCachePrefix + "ExactName/metadata.json",
			AdapterType: "npm", PackageName: "ExactName",
		},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Queries) != 1 || request.Queries[0].Package == nil ||
			request.Queries[0].Package.Name != "ExactName" {
			t.Fatalf("OSV queries = %+v, want only the exact-identity npm cache row", request.Queries)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	var legacyChecks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("ecosystem = ? AND package_name = ?", "npm", "legacy-name").
		Count(&legacyChecks).Error; err != nil {
		t.Fatal(err)
	}
	if legacyChecks != 0 {
		t.Fatalf("legacy npm cache namespace produced %d vulnerability checks", legacyChecks)
	}
}

func TestScannerDoesNotRecordFalseCleanForAmbiguousRubyGemsArtifact(t *testing.T) {
	database := newScannerTestDB(t)
	key := "rubygems/gems/nokogiri-1.16.5-x86_64-linux.gem"
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "rubygems",
		PackageName: packagekey.ExtractName("rubygems", key),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	// Cache Manager enqueues this exact adapter-derived identity after the
	// artifact commit. It must be rejected before a clean OSV receipt can be
	// persisted; the periodic cache scan below must reject the row as well.
	if err := scanner.ScanPackage(context.Background(), entry.AdapterType, entry.PackageName); err != nil {
		t.Fatalf("ScanPackage: %v", err)
	}
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	var checks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("ecosystem = ?", "rubygems").
		Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("ambiguous RubyGems artifact produced %d vulnerability checks, want none", checks)
	}
}

func TestScannerDoesNotRecordFalseCleanForAmbiguousPyPIArtifact(t *testing.T) {
	database := newScannerTestDB(t)
	key := "pypi/files/packages/aa/foo-bar-1.0.zip"
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "pypi",
		PackageName: packagekey.ExtractName("pypi", key),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	// Cache Manager enqueues this exact filename-derived value after commit.
	// Both that immediate path and the later cache walk must reject ambiguity.
	if err := scanner.ScanPackage(context.Background(), entry.AdapterType, entry.PackageName); err != nil {
		t.Fatalf("ScanPackage: %v", err)
	}
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	var checks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("ecosystem = ?", "pypi").
		Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("ambiguous PyPI artifact produced %d vulnerability checks, want none", checks)
	}
}

func TestScannerScansTrustedPyPISimpleIndexIdentity(t *testing.T) {
	database := newScannerTestDB(t)
	key := "pypi/simple/Friendly_Bard/index.html"
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "pypi",
		PackageName: packagekey.ExtractName("pypi", key),
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Queries) != 1 || request.Queries[0].Package == nil {
			t.Fatalf("OSV queries = %+v, want one PyPI package", request.Queries)
		}
		pkg := request.Queries[0].Package
		if pkg.Ecosystem != "PyPI" || pkg.Name != "friendly-bard" {
			t.Fatalf("OSV package = %+v, want PyPI/friendly-bard", pkg)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	var check db.VulnerabilityCheck
	if err := database.Where("ecosystem = ? AND package_name = ?", "pypi", "friendly-bard").Take(&check).Error; err != nil {
		t.Fatalf("trusted PyPI vulnerability check: %v", err)
	}
}

func TestScannerSkipsInvalidCachedIdentitiesAndContinues(t *testing.T) {
	database := newScannerTestDB(t)
	entries := []db.CacheEntry{
		{Key: "cargo/crates/foo/bar/1.0.0.crate", AdapterType: "cargo", PackageName: "foo/bar"},
		{Key: "cran/src/contrib/bad-name_1.0.tar.gz", AdapterType: "cran", PackageName: "bad-name"},
		{Key: "cargo/crates/serde/1.0.0.crate", AdapterType: "cargo", PackageName: "serde"},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Queries) != 1 || request.Queries[0].Package == nil {
			t.Fatalf("OSV queries = %+v, want one trusted package", request.Queries)
		}
		pkg := request.Queries[0].Package
		if pkg.Ecosystem != "crates.io" || pkg.Name != "serde" {
			t.Fatalf("OSV package = %+v, want crates.io/serde", pkg)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}

	var trustedCheck db.VulnerabilityCheck
	if err := database.Where("ecosystem = ? AND package_name = ?", "cargo", "serde").Take(&trustedCheck).Error; err != nil {
		t.Fatalf("trusted Cargo vulnerability check: %v", err)
	}
	var invalidChecks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("package_name IN ?", []string{"foo/bar", "bad-name"}).
		Count(&invalidChecks).Error; err != nil {
		t.Fatal(err)
	}
	if invalidChecks != 0 {
		t.Fatalf("invalid cached identities produced %d vulnerability checks, want none", invalidChecks)
	}
}

func TestScannerScanPackageRejectsInvalidIdentityWithoutRecordingClean(t *testing.T) {
	database := newScannerTestDB(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "invalid identity reached OSV", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	for _, pkg := range []PackageRef{
		{Ecosystem: "cargo", Name: "foo/bar"},
		{Ecosystem: "cran", Name: "bad-name"},
	} {
		if err := scanner.ScanPackage(context.Background(), pkg.Ecosystem, pkg.Name); err == nil {
			t.Errorf("ScanPackage(%s/%s) succeeded, want invalid-identity error", pkg.Ecosystem, pkg.Name)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid identities made %d OSV requests, want none", requests)
	}
	var checks int64
	if err := database.Model(&db.VulnerabilityCheck{}).Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("invalid immediate scans produced %d vulnerability checks, want none", checks)
	}
}

func TestScannerLastScanTimeConcurrentAccess(t *testing.T) {
	database := newScannerTestDB(t)
	scanner := NewScanner(database, nil, newScannerTestCatalog(t, database))

	const scans = 64
	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = scanner.LastScanTime()
				}
			}
		}()
	}

	for range scans {
		if err := scanner.ScanAll(context.Background()); err != nil {
			t.Fatalf("ScanAll: %v", err)
		}
	}
	close(done)
	readers.Wait()

	if scanner.LastScanTime().IsZero() {
		t.Fatal("LastScanTime remained zero after successful scans")
	}
}

func TestScannerCanceledDatabaseQueryDoesNotCompleteScan(t *testing.T) {
	database := newScannerTestDB(t)
	scanner := NewScanner(database, nil, newScannerTestCatalog(t, database))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := scanner.ScanAll(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ScanAll error = %v, want context.Canceled", err)
	}
	if scanner.IsScanning() {
		t.Fatal("scanner remained in the scanning state")
	}
	if !scanner.LastScanTime().IsZero() {
		t.Fatalf("canceled scan changed LastScanTime to %v", scanner.LastScanTime())
	}
}

func TestScannerScanPackageRequiresCatalogWhenPersisting(t *testing.T) {
	database := newScannerTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[]}`))
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner(database, newScannerTestFetcher(t, server), nil)

	err := scanner.ScanPackage(context.Background(), "pypi", "missing-catalog")
	if err == nil || !strings.Contains(err.Error(), "no advisory catalog") {
		t.Fatalf("ScanPackage error = %v, want missing advisory catalog", err)
	}
}

func TestScannerCancellationReleasesScanCAS(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "blocked-request")

	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-release
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- scanner.ScanAll(ctx) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("OSV request did not start")
	}

	if !scanner.IsScanning() {
		t.Fatal("scanner did not report an active scan")
	}
	if err := scanner.ScanAll(context.Background()); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("concurrent ScanAll error = %v, want already in progress", err)
	}

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled ScanAll error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled scan did not return")
	}
	if scanner.IsScanning() {
		t.Fatal("scanner CAS remained set after cancellation")
	}
	if !scanner.LastScanTime().IsZero() {
		t.Fatalf("canceled scan changed LastScanTime to %v", scanner.LastScanTime())
	}

	if err := database.Where("package_name = ?", "blocked-request").Delete(&db.CacheEntry{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("scan after cancellation: %v", err)
	}
	if scanner.LastScanTime().IsZero() {
		t.Fatal("successful scan after cancellation did not update LastScanTime")
	}
}

func TestScannerStartScanReservesSynchronouslyAndCloseWaits(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "manual-scan")

	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))

	if err := scanner.StartScan(context.Background(), time.Hour); err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	if !scanner.IsScanning() {
		t.Fatal("StartScan returned before reserving the scanner")
	}
	if err := scanner.StartScan(context.Background(), time.Hour); !errors.Is(err, ErrScanInProgress) {
		t.Fatalf("second StartScan error = %v, want ErrScanInProgress", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("manual scan did not reach the OSV server")
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := scanner.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if scanner.IsScanning() {
		t.Fatal("Close returned while scanner was still active")
	}
	if !scanner.LastScanTime().IsZero() {
		t.Fatalf("canceled manual scan changed LastScanTime to %v", scanner.LastScanTime())
	}
	if err := scanner.StartScan(context.Background(), time.Hour); !errors.Is(err, ErrScannerClosed) {
		t.Fatalf("StartScan after Close error = %v, want ErrScannerClosed", err)
	}
}

func TestScannerAggregatesBatchFailureWithoutMarkingComplete(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "successful-package")
	addCachedPackage(t, database, "npm", "failed-package")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, query := range request.Queries {
			if query.Package != nil && query.Package.Name == "failed-package" {
				http.Error(w, "injected query failure", http.StatusBadRequest)
				return
			}
		}
		response := osvBatchResponse{Results: make([]osvQueryResponse, len(request.Queries))}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))

	err := scanner.ScanAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "query OSV batch for npm") {
		t.Fatalf("ScanAll error = %v, want npm batch failure", err)
	}
	if !scanner.LastScanTime().IsZero() {
		t.Fatalf("partial scan changed LastScanTime to %v", scanner.LastScanTime())
	}
	if scanner.IsScanning() {
		t.Fatal("scanner remained in the scanning state after partial failure")
	}

	var goodCheck db.VulnerabilityCheck
	if err := database.Where("ecosystem = ? AND package_name = ?", "pypi", "successful-package").First(&goodCheck).Error; err != nil {
		t.Fatalf("successful batch was not persisted: %v", err)
	}
	var failedChecks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("ecosystem = ? AND package_name = ?", "npm", "failed-package").
		Count(&failedChecks).Error; err != nil {
		t.Fatal(err)
	}
	if failedChecks != 0 {
		t.Fatalf("failed batch created %d check records", failedChecks)
	}
}

func TestScannerQueriesMavenWithCompleteCoordinateFromCacheKey(t *testing.T) {
	database := newScannerTestDB(t)
	key := "maven/org/apache/logging/log4j/log4j-core/2.20.0/log4j-core-2.20.0.jar"
	if err := database.Create(&db.CacheEntry{
		Key: key, AdapterType: "maven", PackageName: packagekey.ExtractName("maven", key),
	}).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Queries) != 1 || request.Queries[0].Package == nil {
			t.Fatalf("OSV batch queries = %+v, want one Maven package", request.Queries)
		}
		pkg := request.Queries[0].Package
		if pkg.Ecosystem != "Maven" || pkg.Name != "org.apache.logging.log4j:log4j-core" {
			t.Fatalf("OSV package = %+v, want complete Maven coordinate", pkg)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
}

func TestScannerCanonicalizesPyPIIdentityBeforeQueryAndPersistence(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "Django_rest.framework")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Queries) != 1 || request.Queries[0].Package == nil ||
			request.Queries[0].Package.Name != "django-rest-framework" {
			t.Fatalf("OSV query = %+v, want canonical PyPI name", request.Queries)
		}
		advisory := catalogTestAdvisory(
			"OSV-PYPI-ALIAS", "PyPI", "django-rest-framework", 8.2,
		)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{
			Results: []osvQueryResponse{{Vulns: []OSVVulnerability{advisory}}},
		})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	var check db.VulnerabilityCheck
	if err := database.Where("ecosystem = ? AND package_name = ?", "pypi", "django-rest-framework").Take(&check).Error; err != nil {
		t.Fatalf("canonical vulnerability check: %v", err)
	}
	if !check.HasVulnerabilities || check.VulnerabilityCount != 1 {
		t.Fatalf("canonical vulnerability check = %+v", check)
	}
}

func TestScannerDisablesNuGetUntilCanonicalRegistryIDIsProven(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "nuget", "newtonsoft.json")
	scanner := NewScanner(database, nil, newScannerTestCatalog(t, database))

	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll with only unproven NuGet identity: %v", err)
	}
	if err := scanner.ScanPackage(context.Background(), "nuget", "newtonsoft.json"); err != nil {
		t.Fatalf("ScanPackage with unproven NuGet identity: %v", err)
	}
	var checks int64
	if err := database.Model(&db.VulnerabilityCheck{}).Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("NuGet false-clean scan persisted %d checks", checks)
	}
}

func TestScannerAggregatesProcessFailureAndContinues(t *testing.T) {
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "failed-write")
	addCachedPackage(t, database, "pypi", "successful-write")

	injected := errors.New("injected vulnerability write failure")
	if err := database.Callback().Create().Before("gorm:create").Register("test:fail-vulnerability-write", func(tx *gorm.DB) {
		vulnerability, ok := tx.Statement.Dest.(*db.Vulnerability)
		if ok && vulnerability.PackageName == "failed-write" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response := osvBatchResponse{Results: make([]osvQueryResponse, len(request.Queries))}
		for i, query := range request.Queries {
			name := query.Package.Name
			response.Results[i].Vulns = []OSVVulnerability{{ID: "OSV-" + name}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))

	err := scanner.ScanAll(context.Background())
	if !errors.Is(err, injected) {
		t.Fatalf("ScanAll error = %v, want injected write failure", err)
	}
	if !scanner.LastScanTime().IsZero() {
		t.Fatalf("partial scan changed LastScanTime to %v", scanner.LastScanTime())
	}

	var goodCheck db.VulnerabilityCheck
	if err := database.Where("package_name = ?", "successful-write").First(&goodCheck).Error; err != nil {
		t.Fatalf("successful package was not processed after another failed: %v", err)
	}
	var failedChecks int64
	if err := database.Model(&db.VulnerabilityCheck{}).Where("package_name = ?", "failed-write").Count(&failedChecks).Error; err != nil {
		t.Fatal(err)
	}
	if failedChecks != 0 {
		t.Fatalf("failed package transaction created %d check records", failedChecks)
	}
}
