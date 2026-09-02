package security

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

func TestScannerDoesNotRecordFalseCleanForAmbiguousComposerMetadata(t *testing.T) {
	database := newScannerTestDB(t)
	key := "composer/p2/not-a-package"
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "composer",
		PackageName: packagekey.ExtractName("composer", key),
	}
	if entry.PackageName != "" {
		t.Fatalf("ambiguous Composer identity = %q, want empty", entry.PackageName)
	}
	if err := database.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(osvBatchResponse{Results: []osvQueryResponse{{}}})
	}))
	t.Cleanup(server.Close)

	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	// The cache-manager fast path passes the extracted value directly to
	// ScanPackage. An empty identity must be a no-op, and the periodic cache
	// walk must skip the row as well; neither path may persist a clean receipt.
	if err := scanner.ScanPackage(context.Background(), entry.AdapterType, entry.PackageName); err != nil {
		t.Fatalf("ScanPackage: %v", err)
	}
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	if requests != 0 {
		t.Fatalf("ambiguous Composer metadata made %d OSV requests, want none", requests)
	}

	var checks int64
	if err := database.Model(&db.VulnerabilityCheck{}).
		Where("ecosystem = ?", "composer").Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("ambiguous Composer metadata produced %d vulnerability checks, want none", checks)
	}
}

func TestScannerScansTrustedComposerPackagistMetadataIdentity(t *testing.T) {
	database := newScannerTestDB(t)
	key := "composer/p2/symfony/console.json"
	entry := db.CacheEntry{
		Key:         key,
		AdapterType: "composer",
		PackageName: packagekey.ExtractName("composer", key),
	}
	if entry.PackageName != "symfony/console" {
		t.Fatalf("trusted Composer identity = %q, want symfony/console", entry.PackageName)
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
			t.Fatalf("OSV queries = %+v, want one Composer package", request.Queries)
		}
		pkg := request.Queries[0].Package
		if pkg.Ecosystem != "Packagist" || pkg.Name != "symfony/console" {
			t.Fatalf("OSV package = %+v, want Packagist/symfony/console", pkg)
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
	if err := database.Where("ecosystem = ? AND package_name = ?", "composer", "symfony/console").Take(&check).Error; err != nil {
		t.Fatalf("trusted Composer vulnerability check: %v", err)
	}
}
