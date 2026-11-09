package security

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

// fakeOSV serves the scanner's two OSV endpoints with OSV's real response
// shapes: /v1/querybatch lists advisory IDs and modification times only,
// while /v1/query returns complete records.
type fakeOSV struct {
	mu        sync.Mutex
	records   map[string][]OSVVulnerability // keyed by "<OSV ecosystem>/<name>"
	truncated map[string]bool
	failQuery bool
	queried   []string
}

func newFakeOSV(t *testing.T) (*fakeOSV, *httptest.Server) {
	t.Helper()
	fake := &fakeOSV{records: map[string][]OSVVulnerability{}, truncated: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/querybatch":
			var request osvBatchRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			response := osvBatchResponse{Results: make([]osvQueryResponse, len(request.Queries))}
			for i, query := range request.Queries {
				key := query.Package.Ecosystem + "/" + query.Package.Name
				for _, record := range fake.records[key] {
					response.Results[i].Vulns = append(response.Results[i].Vulns,
						OSVVulnerability{ID: record.ID, Modified: record.Modified})
				}
				if fake.truncated[key] {
					response.Results[i].NextPageToken = "more"
				}
			}
			_ = json.NewEncoder(w).Encode(response)
		case "/v1/query":
			var request osvQueryRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Package == nil {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			fake.queried = append(fake.queried, request.Package.Name)
			if fake.failQuery {
				http.Error(w, "injected detail failure", http.StatusBadRequest)
				return
			}
			key := request.Package.Ecosystem + "/" + request.Package.Name
			_ = json.NewEncoder(w).Encode(osvQueryResponse{Vulns: fake.records[key]})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeOSV) queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queried...)
}

func (f *fakeOSV) update(fn func(*fakeOSV)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func expireVulnerabilityChecks(t *testing.T, database *gorm.DB) {
	t.Helper()
	if err := database.Model(&db.VulnerabilityCheck{}).Where("1 = 1").
		UpdateColumn("next_fetch_at", time.Unix(0, 0).UTC()).Error; err != nil {
		t.Fatal(err)
	}
}

func storedAdvisoryForTest(t *testing.T, database *gorm.DB, osvID string) db.Vulnerability {
	t.Helper()
	var stored db.Vulnerability
	if err := database.Where("osv_id = ?", osvID).Take(&stored).Error; err != nil {
		t.Fatalf("stored advisory %s: %v", osvID, err)
	}
	return stored
}

// scannedIDNA scans one cached package whose only advisory is a real OSV
// record, returning the scanner state for follow-up scans.
func scannedIDNA(t *testing.T) (*gorm.DB, *fakeOSV, *Scanner, OSVVulnerability) {
	t.Helper()
	database := newScannerTestDB(t)
	addCachedPackage(t, database, "pypi", "idna")
	fake, server := newFakeOSV(t)
	record := decodeOSVRecord(t, realOSVRecord)
	fake.records["PyPI/idna"] = []OSVVulnerability{record}
	scanner := NewScanner(database, newScannerTestFetcher(t, server), newScannerTestCatalog(t, database))
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("first ScanAll: %v", err)
	}
	return database, fake, scanner, record
}

func TestScannerStoresCompleteRecordsForBatchListedAdvisories(t *testing.T) {
	database, fake, _, record := scannedIDNA(t)
	stored := storedAdvisoryForTest(t, database, record.ID)
	if stored.Summary != record.Summary || stored.Severity != "medium" || stored.CVSSScore != 6.2 ||
		stored.DetailRevision != AdvisoryDetailRevision || fixedVersionForTest(t, stored.AffectedRanges) != "3.7" {
		t.Fatalf("batch-listed advisory stored without its details: %+v", stored)
	}
	if got := fake.queries(); len(got) != 1 || got[0] != "idna" {
		t.Fatalf("detail queries = %v, want one query for idna", got)
	}
}

func TestScannerSkipsDetailQueryForUnchangedListing(t *testing.T) {
	database, fake, scanner, record := scannedIDNA(t)
	expireVulnerabilityChecks(t, database)
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("repeat ScanAll: %v", err)
	}
	if got := fake.queries(); len(got) != 1 {
		t.Fatalf("unchanged listing fetched details again: %v", got)
	}
	if stored := storedAdvisoryForTest(t, database, record.ID); stored.Summary != record.Summary || stored.CVSSScore != 6.2 {
		t.Fatalf("unchanged listing replaced stored details: %+v", stored)
	}
}

func TestScannerRefetchesAdvisoriesParsedByOlderRevision(t *testing.T) {
	database, fake, scanner, record := scannedIDNA(t)
	// Simulate a row stored before the vector parser existed.
	if err := database.Model(&db.Vulnerability{}).Where("osv_id = ?", record.ID).
		Updates(map[string]any{"detail_revision": 0, "severity": "unknown", "cvss_score": 0}).Error; err != nil {
		t.Fatal(err)
	}
	expireVulnerabilityChecks(t, database)
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll after parser upgrade: %v", err)
	}
	if got := fake.queries(); len(got) != 2 {
		t.Fatalf("older parser revision was not fetched again: %v", got)
	}
	stored := storedAdvisoryForTest(t, database, record.ID)
	if stored.Severity != "medium" || stored.CVSSScore != 6.2 || stored.DetailRevision != AdvisoryDetailRevision {
		t.Fatalf("older revision was not re-parsed: %+v", stored)
	}
}

func TestScannerKeepsStoredDetailsWhenRecordFetchFails(t *testing.T) {
	database, fake, scanner, record := scannedIDNA(t)
	updated := record
	updated.Summary = "updated upstream"
	updated.Modified = record.Modified.Add(time.Hour)
	fake.update(func(f *fakeOSV) {
		f.records["PyPI/idna"] = []OSVVulnerability{updated}
		f.failQuery = true
	})
	expireVulnerabilityChecks(t, database)

	err := scanner.ScanAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch advisory details for pypi/idna") {
		t.Fatalf("ScanAll error = %v, want detail fetch failure", err)
	}
	// The newer batch listing must not replace stored details with a bare ID.
	stored := storedAdvisoryForTest(t, database, record.ID)
	if stored.Summary != record.Summary || stored.Severity != "medium" || fixedVersionForTest(t, stored.AffectedRanges) != "3.7" {
		t.Fatalf("failed detail fetch degraded the stored advisory: %+v", stored)
	}
	var check db.VulnerabilityCheck
	if err := database.Where("package_name = ?", "idna").Take(&check).Error; err != nil {
		t.Fatal(err)
	}
	if check.NextFetchAt.After(time.Now()) {
		t.Fatalf("failed detail fetch advanced the package check to %v", check.NextFetchAt)
	}

	fake.update(func(f *fakeOSV) { f.failQuery = false })
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("retry ScanAll: %v", err)
	}
	if stored := storedAdvisoryForTest(t, database, record.ID); stored.Summary != "updated upstream" {
		t.Fatalf("retry did not apply the newer record: %+v", stored)
	}
}

func TestScannerQueriesTruncatedBatchListing(t *testing.T) {
	database, fake, scanner, _ := scannedIDNA(t)
	fake.update(func(f *fakeOSV) { f.truncated["PyPI/idna"] = true })
	expireVulnerabilityChecks(t, database)
	if err := scanner.ScanAll(context.Background()); err != nil {
		t.Fatalf("ScanAll: %v", err)
	}
	if got := fake.queries(); len(got) != 2 {
		t.Fatalf("truncated listing was trusted as complete: %v", got)
	}
}

func TestAdvisoryCatalogNeverStoresIDOnlyAdvisory(t *testing.T) {
	database := newCatalogTestDB(t)
	catalog := newCatalogForTest(t, database, time.Hour)
	pkg := PackageRef{Ecosystem: "pypi", Name: "idna"}
	record := decodeOSVRecord(t, realOSVRecord)
	listed := OSVVulnerability{ID: record.ID, Modified: record.Modified, idOnly: true}

	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{listed}); !errors.Is(err, ErrAdvisoryDetailsMissing) {
		t.Fatalf("RecordScan of a bare listing = %v, want ErrAdvisoryDetailsMissing", err)
	}
	var rows int64
	if err := database.Model(&db.VulnerabilityCheck{}).Count(&rows).Error; err != nil || rows != 0 {
		t.Fatalf("rejected listing wrote %d check rows (%v)", rows, err)
	}

	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{record}); err != nil {
		t.Fatal(err)
	}
	listed.Modified = record.Modified.Add(time.Hour)
	if needs, err := catalog.NeedsDetails(context.Background(), pkg, []OSVVulnerability{listed}); err != nil || !needs {
		t.Fatalf("newer listing NeedsDetails = %v, %v; want true", needs, err)
	}
	if _, err := catalog.RecordScan(context.Background(), pkg, []OSVVulnerability{listed}); err != nil {
		t.Fatalf("RecordScan of a listing for a stored advisory: %v", err)
	}
	if stored := storedAdvisoryForTest(t, database, record.ID); stored.Summary != record.Summary || !stored.ModifiedAt.Equal(record.Modified) {
		t.Fatalf("bare listing replaced stored details: %+v", stored)
	}
}

func TestFetcherQueryFollowsResultPages(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request osvQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tokens = append(tokens, request.PageToken)
		response := osvQueryResponse{Vulns: []OSVVulnerability{{ID: "OSV-" + request.PageToken}}}
		if request.PageToken == "" {
			response.NextPageToken = "page-2"
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	advisories, err := newScannerTestFetcher(t, server).Query(context.Background(), "pypi", "idna")
	if err != nil || len(advisories) != 2 || strings.Join(tokens, ",") != ",page-2" {
		t.Fatalf("Query pages = %v (tokens %q), %v", advisories, tokens, err)
	}

	endless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(osvQueryResponse{NextPageToken: "again"})
	}))
	t.Cleanup(endless.Close)
	if _, err := newScannerTestFetcher(t, endless).Query(context.Background(), "pypi", "idna"); err == nil {
		t.Fatal("Query followed result pages without a bound")
	}
}
