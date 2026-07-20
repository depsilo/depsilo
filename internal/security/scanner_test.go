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
	catalog, err := NewAdvisoryCatalog(database, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func addCachedPackage(t *testing.T, database *gorm.DB, ecosystem, packageName string) {
	t.Helper()
	entry := &db.CacheEntry{
		Key:         fmt.Sprintf("%s/%s/index", ecosystem, packageName),
		AdapterType: ecosystem,
		PackageName: packageName,
	}
	if err := database.Create(entry).Error; err != nil {
		t.Fatal(err)
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
