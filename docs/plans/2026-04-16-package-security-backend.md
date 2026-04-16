# Package Security Intelligence — Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add backend infrastructure for querying OSV.dev, storing vulnerability data, auto-blocking vulnerable packages, and exposing admin API endpoints (Pro feature).

**Architecture:** New `internal/security/` package with 4 files (fetcher, scanner, policy, importer). 4 new DB models. 10 API endpoints under `/api/v1/admin/security/*` gated by Pro license. Background goroutine for periodic scanning. Cache manager integration for new-package triggers.

**Tech Stack:** Go, GORM, OSV.dev REST API, net/http, zap, singleflight-style caching

**Spec:** `docs/specs/2026-04-15-package-security-intelligence.md`

**Frontend plan:** Separate document (`docs/plans/2026-04-16-package-security-frontend.md`) — implement after this plan is complete.

---

### Task 1: DB Models & Migration

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/repository.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add SecurityConfig to config.go**

In `internal/config/config.go`, add the `Security` field to the `Config` struct (after `License`):

```go
Security SecurityConfig `mapstructure:"security"`
```

Add the `SecurityConfig` type at the end of the file:

```go
type SecurityConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	OSVURL       string        `mapstructure:"osv_api_url"`
	ScanInterval time.Duration `mapstructure:"scan_interval"`
	CheckTTL     time.Duration `mapstructure:"check_ttl"`
	Proxy        string        `mapstructure:"proxy"`
}
```

- [ ] **Step 2: Add 4 new models to models.go**

Append to `internal/db/models.go`:

```go
type Vulnerability struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	OSVID          string    `gorm:"size:64;uniqueIndex" json:"osv_id"`
	Ecosystem      string    `gorm:"size:16;index" json:"ecosystem"`
	PackageName    string    `gorm:"size:256;index" json:"package_name"`
	AffectedRanges string    `gorm:"type:text" json:"affected_ranges"`
	Severity       string    `gorm:"size:16;index" json:"severity"`
	CVSSScore      float32   `gorm:"default:0" json:"cvss_score"`
	Summary        string    `gorm:"type:text" json:"summary"`
	Details        string    `gorm:"type:text" json:"details"`
	Aliases        string    `gorm:"size:512" json:"aliases"`
	References     string    `gorm:"type:text" json:"references"`
	PublishedAt    time.Time `gorm:"index" json:"published_at"`
	ModifiedAt     time.Time `json:"modified_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type VulnerabilityCheck struct {
	ID                 uint      `gorm:"primarykey" json:"id"`
	Ecosystem          string    `gorm:"size:16;uniqueIndex:idx_vuln_check_eco_pkg" json:"ecosystem"`
	PackageName        string    `gorm:"size:256;uniqueIndex:idx_vuln_check_eco_pkg" json:"package_name"`
	HasVulnerabilities bool      `gorm:"default:false" json:"has_vulnerabilities"`
	VulnerabilityCount int       `gorm:"default:0" json:"vulnerability_count"`
	LastFetchedAt      time.Time `gorm:"index" json:"last_fetched_at"`
	NextFetchAt        time.Time `gorm:"index" json:"next_fetch_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SecurityPolicy struct {
	ID               uint      `gorm:"primarykey" json:"id"`
	Ecosystem        string    `gorm:"size:16;uniqueIndex" json:"ecosystem"`
	AutoBlockEnabled bool      `gorm:"default:false" json:"auto_block_enabled"`
	MinCVSSScore     float32   `gorm:"default:9.0" json:"min_cvss_score"`
	CreatedBy        string    `gorm:"size:64" json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DismissedVuln struct {
	ID              uint      `gorm:"primarykey" json:"id"`
	VulnerabilityID uint      `gorm:"uniqueIndex:idx_dismissed" json:"vulnerability_id"`
	DismissedBy     string    `gorm:"size:64" json:"dismissed_by"`
	CreatedAt       time.Time `json:"created_at"`
}
```

- [ ] **Step 3: Add new models to AutoMigrate**

In `internal/db/repository.go`, add the 4 new models to the `AutoMigrate` call:

```go
func AutoMigrate(db *gorm.DB) error {
	zap.L().Info("running database auto-migration")
	return db.AutoMigrate(
		&CacheEntry{},
		&AccessLog{},
		&UpstreamRecord{},
		&User{},
		&APIToken{},
		&UpstreamLatencyLog{},
		&AuditLog{},
		&PackageRule{},
		&Vulnerability{},
		&VulnerabilityCheck{},
		&SecurityPolicy{},
		&DismissedVuln{},
	)
}
```

- [ ] **Step 4: Add security config to config.example.toml**

Append to `config.example.toml` before EOF:

```toml

[security]
enabled = true
osv_api_url = "https://api.osv.dev"
scan_interval = "24h"
check_ttl = "24h"
# proxy = "http://127.0.0.1:7890"
```

- [ ] **Step 5: Run build and verify**

Run: `go build ./...`
Expected: success

- [ ] **Step 6: Commit**

```bash
git add internal/db/models.go internal/db/repository.go internal/config/config.go config.example.toml
git commit -m "feat(security): add vulnerability DB models and security config"
```

---

### Task 2: OSV.dev API Client (fetcher.go)

**Files:**
- Create: `internal/security/fetcher.go`

- [ ] **Step 1: Create fetcher.go**

Create `internal/security/fetcher.go`:

```go
package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// ecosystemMap converts Depsilo adapter types to OSV ecosystem names.
var ecosystemMap = map[string]string{
	"pypi":     "PyPI",
	"npm":      "npm",
	"go":       "Go",
	"cargo":    "crates.io",
	"maven":    "Maven",
	"nuget":    "NuGet",
	"composer": "Packagist",
	"rubygems": "RubyGems",
	"cran":     "CRAN",
	"apt":      "Debian",
}

// OSVEcosystem returns the OSV ecosystem name for a Depsilo adapter type.
// Returns empty string if the ecosystem is not supported by OSV.
func OSVEcosystem(depsiloType string) string {
	return ecosystemMap[depsiloType]
}

// Fetcher queries the OSV.dev API for vulnerability data.
type Fetcher struct {
	client  *http.Client
	baseURL string
	limiter *time.Ticker
}

// NewFetcher creates a new OSV.dev API client.
func NewFetcher(baseURL, proxy string) *Fetcher {
	transport := &http.Transport{}
	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &Fetcher{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		baseURL: baseURL,
		limiter: time.NewTicker(time.Second), // max 1 req/s
	}
}

// Close releases resources.
func (f *Fetcher) Close() {
	f.limiter.Stop()
}

// osvQueryRequest is the request body for /v1/query.
type osvQueryRequest struct {
	Package *osvPackage `json:"package"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// osvBatchRequest is the request body for /v1/querybatch.
type osvBatchRequest struct {
	Queries []osvQueryRequest `json:"queries"`
}

// OSVVulnerability represents a single vulnerability from OSV.dev.
type OSVVulnerability struct {
	ID        string        `json:"id"`
	Summary   string        `json:"summary"`
	Details   string        `json:"details"`
	Aliases   []string      `json:"aliases"`
	Severity  []osvSeverity `json:"severity"`
	Affected  []osvAffected `json:"affected"`
	References []osvRef     `json:"references"`
	Published time.Time     `json:"published"`
	Modified  time.Time     `json:"modified"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package *osvPackage  `json:"package"`
	Ranges  []osvRange   `json:"ranges"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
}

type osvRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// osvQueryResponse is the response from /v1/query.
type osvQueryResponse struct {
	Vulns []OSVVulnerability `json:"vulns"`
}

// osvBatchResponse is the response from /v1/querybatch.
type osvBatchResponse struct {
	Results []osvQueryResponse `json:"results"`
}

// Query fetches vulnerabilities for a single package.
func (f *Fetcher) Query(ctx context.Context, ecosystem, packageName string) ([]OSVVulnerability, error) {
	osvEco := OSVEcosystem(ecosystem)
	if osvEco == "" {
		return nil, nil // unsupported ecosystem
	}

	<-f.limiter.C // rate limit

	body := osvQueryRequest{
		Package: &osvPackage{Name: packageName, Ecosystem: osvEco},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	var result osvQueryResponse
	if err := f.doPost(ctx, "/v1/query", data, &result); err != nil {
		return nil, err
	}
	return result.Vulns, nil
}

// QueryBatch fetches vulnerabilities for multiple packages in one request.
// packages is a list of {ecosystem, packageName} pairs.
// Returns results in the same order as input.
func (f *Fetcher) QueryBatch(ctx context.Context, packages []PackageRef) ([][]OSVVulnerability, error) {
	if len(packages) == 0 {
		return nil, nil
	}

	<-f.limiter.C // rate limit

	queries := make([]osvQueryRequest, len(packages))
	for i, pkg := range packages {
		osvEco := OSVEcosystem(pkg.Ecosystem)
		if osvEco == "" {
			continue
		}
		queries[i] = osvQueryRequest{
			Package: &osvPackage{Name: pkg.Name, Ecosystem: osvEco},
		}
	}

	body := osvBatchRequest{Queries: queries}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal batch query: %w", err)
	}

	var result osvBatchResponse
	if err := f.doPost(ctx, "/v1/querybatch", data, &result); err != nil {
		return nil, err
	}

	out := make([][]OSVVulnerability, len(result.Results))
	for i, r := range result.Results {
		out[i] = r.Vulns
	}
	return out, nil
}

// PackageRef identifies a package by ecosystem and name.
type PackageRef struct {
	Ecosystem string
	Name      string
}

// doPost sends a POST request with JSON body and decodes the response.
// Retries up to 3 times on 5xx errors with exponential backoff.
func (f *Fetcher) doPost(ctx context.Context, path string, body []byte, result interface{}) error {
	url := f.baseURL + path
	backoff := time.Second

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := f.client.Do(req)
		if err != nil {
			return fmt.Errorf("OSV request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("decode OSV response: %w", err)
			}
			return nil
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			zap.L().Warn("OSV request failed, retrying",
				zap.Int("status", resp.StatusCode),
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				continue
			}
		}

		return fmt.Errorf("OSV returned %d: %s", resp.StatusCode, string(respBody))
	}

	return fmt.Errorf("OSV request failed after 3 attempts")
}
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/security/fetcher.go
git commit -m "feat(security): add OSV.dev API client"
```

---

### Task 3: OSV Response Parser (parser.go)

Converts OSV API responses into DB models. Separated from fetcher for testability.

**Files:**
- Create: `internal/security/parser.go`

- [ ] **Step 1: Create parser.go**

Create `internal/security/parser.go`:

```go
package security

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"depsilo/internal/db"
)

// ParseVulnerability converts an OSV vulnerability into a DB model.
// ecosystemHint is the Depsilo ecosystem (e.g., "pypi") used when OSV response
// doesn't specify one clearly.
func ParseVulnerability(v OSVVulnerability, ecosystemHint string) *db.Vulnerability {
	severity, cvss := extractSeverity(v.Severity)
	ranges := extractAffectedRanges(v.Affected)
	refs := extractReferences(v.References)
	aliases := strings.Join(v.Aliases, ",")
	ecosystem := ecosystemHint
	packageName := ""

	// Try to extract ecosystem and package from affected entries
	if len(v.Affected) > 0 && v.Affected[0].Package != nil {
		packageName = v.Affected[0].Package.Name
		if eco := reverseEcosystem(v.Affected[0].Package.Ecosystem); eco != "" {
			ecosystem = eco
		}
	}

	return &db.Vulnerability{
		OSVID:          v.ID,
		Ecosystem:      ecosystem,
		PackageName:    packageName,
		AffectedRanges: ranges,
		Severity:       severity,
		CVSSScore:      cvss,
		Summary:        v.Summary,
		Details:        v.Details,
		Aliases:        aliases,
		References:     refs,
		PublishedAt:    v.Published,
		ModifiedAt:     v.Modified,
	}
}

// extractSeverity returns severity level and CVSS score from OSV severity array.
func extractSeverity(severities []osvSeverity) (string, float32) {
	for _, s := range severities {
		if s.Type == "CVSS_V3" {
			score := parseCVSSScore(s.Score)
			return classifyCVSS(score), score
		}
	}
	return "unknown", 0
}

// parseCVSSScore extracts the numeric score from a CVSS vector or plain number.
func parseCVSSScore(scoreStr string) float32 {
	// Try plain number first
	if f, err := strconv.ParseFloat(scoreStr, 32); err == nil {
		return float32(f)
	}
	// Try CVSS vector format: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" — score not in vector
	// OSV sometimes puts just the score as a string
	return 0
}

// classifyCVSS maps CVSS score to severity label.
func classifyCVSS(score float32) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "unknown"
	}
}

// extractAffectedRanges serializes affected ranges to JSON for storage.
func extractAffectedRanges(affected []osvAffected) string {
	type rangeEntry struct {
		Type   string     `json:"type"`
		Events []osvEvent `json:"events"`
	}

	var allRanges []rangeEntry
	for _, a := range affected {
		for _, r := range a.Ranges {
			allRanges = append(allRanges, rangeEntry{
				Type:   r.Type,
				Events: r.Events,
			})
		}
	}

	data, _ := json.Marshal(allRanges)
	return string(data)
}

// extractReferences serializes reference URLs to JSON.
func extractReferences(refs []osvRef) string {
	urls := make([]string, 0, len(refs))
	for _, r := range refs {
		urls = append(urls, r.URL)
	}
	data, _ := json.Marshal(urls)
	return string(data)
}

// reverseEcosystem converts an OSV ecosystem name back to Depsilo adapter type.
func reverseEcosystem(osvEco string) string {
	for k, v := range ecosystemMap {
		if v == osvEco {
			return k
		}
	}
	return ""
}

// ExtractFixedVersion extracts the "fixed" version from affected ranges JSON.
// Returns the first fixed version found, or empty string.
func ExtractFixedVersion(affectedRangesJSON string) string {
	type rangeEntry struct {
		Type   string     `json:"type"`
		Events []osvEvent `json:"events"`
	}

	var ranges []rangeEntry
	if err := json.Unmarshal([]byte(affectedRangesJSON), &ranges); err != nil {
		return ""
	}

	for _, r := range ranges {
		for _, e := range r.Events {
			if e.Fixed != "" {
				return e.Fixed
			}
		}
	}
	return ""
}

// FormatVersionConstraint creates a version constraint string for a PackageRule.
func FormatVersionConstraint(v *db.Vulnerability) string {
	fixed := ExtractFixedVersion(v.AffectedRanges)
	if fixed != "" {
		return fmt.Sprintf("<%s", fixed)
	}
	return "*" // block all versions if no fixed version known
}
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/security/parser.go
git commit -m "feat(security): add OSV response parser"
```

---

### Task 4: Unit Tests — Parser

**Files:**
- Create: `tests/unit/security_parser_test.go`

- [ ] **Step 1: Create parser test file**

Create `tests/unit/security_parser_test.go`:

```go
package unit

import (
	"testing"

	"depsilo/internal/security"
)

func TestClassifyCVSS(t *testing.T) {
	vuln := security.OSVVulnerability{
		ID:      "GHSA-test-1",
		Summary: "Test vulnerability",
		Severity: []security.OsvSeverityEntry{
			{Type: "CVSS_V3", Score: "9.8"},
		},
	}
	parsed := security.ParseVulnerability(vuln, "pypi")
	if parsed.Severity != "critical" {
		t.Errorf("severity = %q, want critical", parsed.Severity)
	}
	if parsed.CVSSScore < 9.0 {
		t.Errorf("cvss = %f, want >= 9.0", parsed.CVSSScore)
	}
}

func TestExtractFixedVersion(t *testing.T) {
	rangesJSON := `[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"2.28.0"}]}]`
	fixed := security.ExtractFixedVersion(rangesJSON)
	if fixed != "2.28.0" {
		t.Errorf("fixed = %q, want 2.28.0", fixed)
	}
}

func TestExtractFixedVersion_NoFixed(t *testing.T) {
	rangesJSON := `[{"type":"SEMVER","events":[{"introduced":"0"}]}]`
	fixed := security.ExtractFixedVersion(rangesJSON)
	if fixed != "" {
		t.Errorf("fixed = %q, want empty", fixed)
	}
}

func TestFormatVersionConstraint(t *testing.T) {
	vuln := security.ParseVulnerability(security.OSVVulnerability{
		ID: "CVE-test",
		Affected: []security.OsvAffectedEntry{
			{
				Package: &security.OsvPackageEntry{Name: "requests", Ecosystem: "PyPI"},
				Ranges: []security.OsvRangeEntry{
					{Type: "SEMVER", Events: []security.OsvEventEntry{{Introduced: "0"}, {Fixed: "2.28.0"}}},
				},
			},
		},
	}, "pypi")
	constraint := security.FormatVersionConstraint(vuln)
	if constraint != "<2.28.0" {
		t.Errorf("constraint = %q, want <2.28.0", constraint)
	}
}

func TestEcosystemMapping(t *testing.T) {
	tests := []struct {
		depsilo string
		osv     string
	}{
		{"pypi", "PyPI"},
		{"npm", "npm"},
		{"go", "Go"},
		{"cargo", "crates.io"},
		{"maven", "Maven"},
	}
	for _, tt := range tests {
		got := security.OSVEcosystem(tt.depsilo)
		if got != tt.osv {
			t.Errorf("OSVEcosystem(%q) = %q, want %q", tt.depsilo, got, tt.osv)
		}
	}
}

func TestUnsupportedEcosystem(t *testing.T) {
	for _, eco := range []string{"conda", "helm", "docker"} {
		if got := security.OSVEcosystem(eco); got != "" {
			t.Errorf("OSVEcosystem(%q) = %q, want empty", eco, got)
		}
	}
}
```

**Note:** The test references exported types from the security package. If `osvSeverity`, `osvAffected`, etc. are unexported (lowercase), we need to either export them or use a different test strategy. The parser tests should use the public `ParseVulnerability` function with `OSVVulnerability` structs. Since `OSVVulnerability` and its nested types are used in the test, they need to be exported. Review the actual exports in fetcher.go and adjust type names in the test accordingly. If the types are unexported, move the test to `internal/security/parser_test.go` (same package).

- [ ] **Step 2: Ensure OSV types are exported in fetcher.go**

The `OSVVulnerability` type is already exported. Check that nested types used in tests (`osvSeverity`, `osvAffected`, `osvRange`, `osvEvent`, `osvPackage`) are also exported. If they're not, export them by renaming:
- `osvSeverity` → keep unexported, use `OsvSeverityEntry` as an exported alias or restructure tests to only use `OSVVulnerability`

**Simplest approach:** Move test to `internal/security/parser_test.go` (package `security`) so it can access unexported types directly.

If this is needed, create the file at `internal/security/parser_test.go` instead and use `package security` as the package name.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/security/ -v` or `go test ./tests/unit/ -v`
Expected: all tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/security/parser_test.go  # or tests/unit/security_parser_test.go
git commit -m "test(security): add parser unit tests"
```

---

### Task 5: Scanner (scanner.go)

**Files:**
- Create: `internal/security/scanner.go`

- [ ] **Step 1: Create scanner.go**

Create `internal/security/scanner.go`:

```go
package security

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Scanner coordinates vulnerability scanning.
type Scanner struct {
	db       *gorm.DB
	fetcher  *Fetcher
	checkTTL time.Duration
	scanning atomic.Bool
	lastScan time.Time
}

// NewScanner creates a new vulnerability scanner.
func NewScanner(database *gorm.DB, fetcher *Fetcher, checkTTL time.Duration) *Scanner {
	return &Scanner{
		db:       database,
		fetcher:  fetcher,
		checkTTL: checkTTL,
	}
}

// IsScanning returns true if a scan is currently in progress.
func (s *Scanner) IsScanning() bool {
	return s.scanning.Load()
}

// LastScanTime returns when the last full scan completed.
func (s *Scanner) LastScanTime() time.Time {
	return s.lastScan
}

// ScanAll scans all cached packages that need a vulnerability check refresh.
func (s *Scanner) ScanAll(ctx context.Context) error {
	if !s.scanning.CompareAndSwap(false, true) {
		return fmt.Errorf("scan already in progress")
	}
	defer s.scanning.Store(false)

	zap.L().Info("security scan started")
	start := time.Now()

	// Get distinct packages from cache
	type pkgRow struct {
		AdapterType string
		PackageName string
	}
	var packages []pkgRow
	if err := s.db.Model(&db.CacheEntry{}).
		Select("DISTINCT adapter_type, package_name").
		Where("package_name != ''").
		Find(&packages).Error; err != nil {
		return fmt.Errorf("query cached packages: %w", err)
	}

	// Filter: skip unsupported ecosystems and packages with fresh checks
	now := time.Now()
	var toScan []PackageRef
	for _, pkg := range packages {
		if OSVEcosystem(pkg.AdapterType) == "" {
			continue // unsupported ecosystem
		}

		var check db.VulnerabilityCheck
		err := s.db.Where("ecosystem = ? AND package_name = ?", pkg.AdapterType, pkg.PackageName).First(&check).Error
		if err == nil && check.NextFetchAt.After(now) {
			continue // still fresh
		}

		toScan = append(toScan, PackageRef{Ecosystem: pkg.AdapterType, Name: pkg.PackageName})
	}

	if len(toScan) == 0 {
		zap.L().Info("security scan complete, all packages up to date")
		s.lastScan = time.Now()
		return nil
	}

	zap.L().Info("scanning packages", zap.Int("count", len(toScan)))

	// Group by ecosystem for batch queries
	byEcosystem := make(map[string][]PackageRef)
	for _, pkg := range toScan {
		byEcosystem[pkg.Ecosystem] = append(byEcosystem[pkg.Ecosystem], pkg)
	}

	totalVulns := 0
	for eco, pkgs := range byEcosystem {
		// Batch in groups of 1000
		for i := 0; i < len(pkgs); i += 1000 {
			end := i + 1000
			if end > len(pkgs) {
				end = len(pkgs)
			}
			batch := pkgs[i:end]

			results, err := s.fetcher.QueryBatch(ctx, batch)
			if err != nil {
				zap.L().Error("batch query failed", zap.String("ecosystem", eco), zap.Error(err))
				continue
			}

			for j, vulns := range results {
				pkg := batch[j]
				count := s.processResults(pkg.Ecosystem, pkg.Name, vulns)
				totalVulns += count
			}
		}
	}

	s.lastScan = time.Now()
	zap.L().Info("security scan complete",
		zap.Int("packages", len(toScan)),
		zap.Int("vulnerabilities", totalVulns),
		zap.Duration("duration", time.Since(start)),
	)
	return nil
}

// ScanPackage scans a single package for vulnerabilities.
// Skips if a fresh check exists (within checkTTL).
func (s *Scanner) ScanPackage(ctx context.Context, ecosystem, packageName string) error {
	if OSVEcosystem(ecosystem) == "" {
		return nil // unsupported
	}
	if packageName == "" {
		return nil
	}

	// Check if we already have a fresh result
	var check db.VulnerabilityCheck
	err := s.db.Where("ecosystem = ? AND package_name = ?", ecosystem, packageName).First(&check).Error
	if err == nil && check.NextFetchAt.After(time.Now()) {
		return nil // still fresh, skip
	}

	vulns, err := s.fetcher.Query(ctx, ecosystem, packageName)
	if err != nil {
		return fmt.Errorf("query OSV for %s/%s: %w", ecosystem, packageName, err)
	}

	s.processResults(ecosystem, packageName, vulns)
	return nil
}

// processResults stores vulnerabilities and updates the check record.
// Returns the number of vulnerabilities found.
func (s *Scanner) processResults(ecosystem, packageName string, vulns []OSVVulnerability) int {
	now := time.Now()

	// Upsert vulnerabilities
	for _, v := range vulns {
		parsed := ParseVulnerability(v, ecosystem)
		if parsed.PackageName == "" {
			parsed.PackageName = packageName
		}
		if parsed.Ecosystem == "" {
			parsed.Ecosystem = ecosystem
		}

		s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "osv_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"affected_ranges", "severity", "cvss_score", "summary", "details", "aliases", "references", "modified_at", "updated_at"}),
		}).Create(parsed)
	}

	// Update check record
	s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"has_vulnerabilities", "vulnerability_count", "last_fetched_at", "next_fetch_at", "updated_at"}),
	}).Create(&db.VulnerabilityCheck{
		Ecosystem:          ecosystem,
		PackageName:        packageName,
		HasVulnerabilities: len(vulns) > 0,
		VulnerabilityCount: len(vulns),
		LastFetchedAt:      now,
		NextFetchAt:        now.Add(s.checkTTL),
	})

	// Check auto-block policies
	if len(vulns) > 0 {
		s.checkAutoBlock(ecosystem, packageName, vulns)
	}

	return len(vulns)
}

// checkAutoBlock creates deny rules for vulnerabilities exceeding the ecosystem's CVSS threshold.
func (s *Scanner) checkAutoBlock(ecosystem, packageName string, vulns []OSVVulnerability) {
	var policy db.SecurityPolicy
	if err := s.db.Where("ecosystem = ? AND auto_block_enabled = ?", ecosystem, true).First(&policy).Error; err != nil {
		return // no policy or not enabled
	}

	for _, v := range vulns {
		parsed := ParseVulnerability(v, ecosystem)
		if parsed.CVSSScore < policy.MinCVSSScore {
			continue
		}

		// Check if rule already exists
		var count int64
		s.db.Model(&db.PackageRule{}).Where(
			"ecosystem = ? AND package_name = ? AND created_by = 'security-scanner' AND reason LIKE ?",
			ecosystem, packageName, "%"+v.ID+"%",
		).Count(&count)
		if count > 0 {
			continue // already blocked
		}

		constraint := FormatVersionConstraint(parsed)
		rule := db.PackageRule{
			Ecosystem:   ecosystem,
			PackageName: packageName,
			Version:     constraint,
			Action:      "deny",
			Reason:      fmt.Sprintf("Auto-blocked: %s (CVSS %.1f)", v.ID, parsed.CVSSScore),
			CreatedBy:   "security-scanner",
		}
		if err := s.db.Create(&rule).Error; err != nil {
			zap.L().Warn("failed to create auto-block rule",
				zap.String("package", packageName),
				zap.String("vuln", v.ID),
				zap.Error(err),
			)
		} else {
			zap.L().Info("auto-blocked vulnerable package",
				zap.String("ecosystem", ecosystem),
				zap.String("package", packageName),
				zap.String("vuln", v.ID),
				zap.Float32("cvss", parsed.CVSSScore),
			)
		}
	}
}

// StartBackgroundScan runs periodic full scans.
func StartBackgroundScan(ctx context.Context, scanner *Scanner, interval time.Duration) {
	// Initial scan after 30-second startup delay
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	if err := scanner.ScanAll(ctx); err != nil {
		zap.L().Error("initial security scan failed", zap.Error(err))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := scanner.ScanAll(ctx); err != nil {
				zap.L().Error("periodic security scan failed", zap.Error(err))
			}
		}
	}
}
```

- [ ] **Step 2: Add missing fmt import**

The `ScanAll` method uses `fmt.Errorf` — ensure `"fmt"` is in the import block.

- [ ] **Step 3: Run build**

Run: `go build ./...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/security/scanner.go
git commit -m "feat(security): add vulnerability scanner with batch queries and auto-block"
```

---

### Task 6: Offline Importer (importer.go)

**Files:**
- Create: `internal/security/importer.go`

- [ ] **Step 1: Create importer.go**

Create `internal/security/importer.go`:

```go
package security

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Importer handles offline vulnerability data import.
type Importer struct {
	db *gorm.DB
}

// NewImporter creates a new vulnerability importer.
func NewImporter(database *gorm.DB) *Importer {
	return &Importer{db: database}
}

// Import parses OSV JSON data and upserts vulnerabilities into the database.
// Accepts either a single OSV object or an array of OSV objects.
// Returns the number of vulnerabilities imported/updated.
func (imp *Importer) Import(data []byte) (int, error) {
	var vulns []OSVVulnerability

	// Try array first
	if err := json.Unmarshal(data, &vulns); err != nil {
		// Try single object
		var single OSVVulnerability
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return 0, fmt.Errorf("invalid JSON: not an array or single OSV object: %w", err2)
		}
		vulns = []OSVVulnerability{single}
	}

	count := 0
	for _, v := range vulns {
		if v.ID == "" {
			continue // skip entries without ID
		}

		parsed := ParseVulnerability(v, "")
		if parsed.Ecosystem == "" || parsed.PackageName == "" {
			zap.L().Warn("skipping vulnerability with missing ecosystem/package",
				zap.String("osv_id", v.ID),
			)
			continue
		}

		err := imp.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "osv_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"affected_ranges", "severity", "cvss_score", "summary", "details", "aliases", "references", "modified_at", "updated_at"}),
		}).Create(parsed).Error
		if err != nil {
			zap.L().Warn("failed to import vulnerability", zap.String("osv_id", v.ID), zap.Error(err))
			continue
		}

		// Update check record
		now := time.Now()
		imp.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"has_vulnerabilities", "vulnerability_count", "last_fetched_at", "next_fetch_at", "updated_at"}),
		}).Create(&db.VulnerabilityCheck{
			Ecosystem:          parsed.Ecosystem,
			PackageName:        parsed.PackageName,
			HasVulnerabilities: true,
			VulnerabilityCount: 1, // approximate; exact count updated on next full scan
			LastFetchedAt:      now,
			NextFetchAt:        now.Add(24 * time.Hour),
		})

		count++
	}

	return count, nil
}
```

- [ ] **Step 2: Run build**

Run: `go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/security/importer.go
git commit -m "feat(security): add offline vulnerability importer"
```

---

### Task 7: Admin API Handler (security.go)

**Files:**
- Create: `internal/api/admin/security.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Create security handler**

Create `internal/api/admin/security.go`:

```go
package admin

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/security"
)

type SecurityHandler struct {
	db       *gorm.DB
	scanner  *security.Scanner
	importer *security.Importer
}

func NewSecurityHandler(database *gorm.DB, scanner *security.Scanner, importer *security.Importer) *SecurityHandler {
	return &SecurityHandler{
		db:       database,
		scanner:  scanner,
		importer: importer,
	}
}

// Dashboard returns security overview statistics.
func (h *SecurityHandler) Dashboard(c *gin.Context) {
	var totalVulns int64
	h.db.Model(&db.Vulnerability{}).Count(&totalVulns)

	var affectedPkgs int64
	h.db.Model(&db.VulnerabilityCheck{}).Where("has_vulnerabilities = ?", true).Count(&affectedPkgs)

	type severityCount struct {
		Severity string
		Count    int64
	}
	var bySeverity []severityCount
	h.db.Model(&db.Vulnerability{}).Select("severity, count(*) as count").Group("severity").Find(&bySeverity)

	severityMap := map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, s := range bySeverity {
		severityMap[s.Severity] = s.Count
	}

	var autoBlocked int64
	h.db.Model(&db.PackageRule{}).Where("created_by = 'security-scanner'").Count(&autoBlocked)

	lastScan := h.scanner.LastScanTime()
	var lastScanStr *string
	if !lastScan.IsZero() {
		s := lastScan.Format(time.RFC3339)
		lastScanStr = &s
	}

	c.JSON(http.StatusOK, gin.H{
		"total_vulnerabilities": totalVulns,
		"affected_packages":    affectedPkgs,
		"by_severity":          severityMap,
		"auto_blocked_count":   autoBlocked,
		"last_scan_at":         lastScanStr,
		"scan_in_progress":     h.scanner.IsScanning(),
	})
}

// ListVulnerabilities returns paginated vulnerability list with filters.
func (h *SecurityHandler) ListVulnerabilities(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	query := h.db.Model(&db.Vulnerability{})

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}
	if sev := c.Query("severity"); sev != "" {
		query = query.Where("severity = ?", sev)
	}
	if pkg := c.Query("package"); pkg != "" {
		query = query.Where("package_name LIKE ?", "%"+pkg+"%")
	}

	var total int64
	query.Count(&total)

	var vulns []db.Vulnerability
	query.Order("cvss_score DESC, published_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&vulns)

	c.JSON(http.StatusOK, gin.H{
		"items": vulns,
		"total": total,
		"page":  page,
	})
}

// ListPackages returns cached packages that have known vulnerabilities.
func (h *SecurityHandler) ListPackages(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	query := h.db.Model(&db.VulnerabilityCheck{}).Where("has_vulnerabilities = ?", true)

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var total int64
	query.Count(&total)

	var checks []db.VulnerabilityCheck
	query.Order("vulnerability_count DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&checks)

	c.JSON(http.StatusOK, gin.H{
		"items": checks,
		"total": total,
		"page":  page,
	})
}

// ListSuggestions returns vulnerabilities that don't have a corresponding deny rule yet.
func (h *SecurityHandler) ListSuggestions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}

	query := h.db.Model(&db.Vulnerability{}).
		Where("id NOT IN (?)",
			h.db.Model(&db.DismissedVuln{}).Select("vulnerability_id"),
		).
		Where("cvss_score > 0").
		Order("cvss_score DESC")

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var total int64
	query.Count(&total)

	var vulns []db.Vulnerability
	query.Offset((page - 1) * perPage).Limit(perPage).Find(&vulns)

	c.JSON(http.StatusOK, gin.H{
		"items": vulns,
		"total": total,
		"page":  page,
	})
}

// ApproveSuggestion creates a deny rule from a vulnerability.
func (h *SecurityHandler) ApproveSuggestion(c *gin.Context) {
	vulnID, _ := strconv.Atoi(c.Param("vuln_id"))

	var vuln db.Vulnerability
	if err := h.db.First(&vuln, vulnID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "vulnerability not found"})
		return
	}

	var body struct {
		Version string `json:"version"`
		Reason  string `json:"reason"`
	}
	c.ShouldBindJSON(&body)

	version := body.Version
	if version == "" {
		version = security.FormatVersionConstraint(&vuln)
	}
	reason := body.Reason
	if reason == "" {
		reason = vuln.OSVID
		if vuln.CVSSScore > 0 {
			reason += " (CVSS " + strconv.FormatFloat(float64(vuln.CVSSScore), 'f', 1, 32) + ")"
		}
	}

	rule := db.PackageRule{
		Ecosystem:   vuln.Ecosystem,
		PackageName: vuln.PackageName,
		Version:     version,
		Action:      "deny",
		Reason:      reason,
		CreatedBy:   "admin",
	}
	if err := h.db.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"rule_id": rule.ID})
}

// DismissSuggestion marks a vulnerability as dismissed.
func (h *SecurityHandler) DismissSuggestion(c *gin.Context) {
	vulnID, _ := strconv.Atoi(c.Param("vuln_id"))

	dismissed := db.DismissedVuln{
		VulnerabilityID: uint(vulnID),
		DismissedBy:     "admin",
	}
	if err := h.db.Create(&dismissed).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "ALREADY_DISMISSED", "message": "already dismissed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "dismissed"})
}

// TriggerScan starts a full vulnerability scan.
func (h *SecurityHandler) TriggerScan(c *gin.Context) {
	if h.scanner.IsScanning() {
		c.JSON(http.StatusConflict, gin.H{"code": "SCAN_IN_PROGRESS", "message": "scan already in progress"})
		return
	}

	go func() {
		if err := h.scanner.ScanAll(c.Request.Context()); err != nil {
			// Error already logged by scanner
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"status": "scan_started"})
}

// ImportData handles offline vulnerability JSON upload.
func (h *SecurityHandler) ImportData(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NO_FILE", "message": "no file uploaded"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "READ_ERROR", "message": err.Error()})
		return
	}

	count, err := h.importer.Import(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "IMPORT_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"imported": count})
}

// ListPolicies returns security policies for all ecosystems.
func (h *SecurityHandler) ListPolicies(c *gin.Context) {
	var policies []db.SecurityPolicy
	h.db.Order("ecosystem").Find(&policies)
	c.JSON(http.StatusOK, policies)
}

// UpdatePolicy creates or updates a security policy for an ecosystem.
func (h *SecurityHandler) UpdatePolicy(c *gin.Context) {
	ecosystem := c.Param("ecosystem")

	var body struct {
		AutoBlockEnabled bool    `json:"auto_block_enabled"`
		MinCVSSScore     float32 `json:"min_cvss_score"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	policy := db.SecurityPolicy{
		Ecosystem:        ecosystem,
		AutoBlockEnabled: body.AutoBlockEnabled,
		MinCVSSScore:     body.MinCVSSScore,
		CreatedBy:        "admin",
	}

	result := h.db.Where("ecosystem = ?", ecosystem).Assign(policy).FirstOrCreate(&policy)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, policy)
}
```

- [ ] **Step 2: Register routes in router.go**

In `internal/api/router.go`, add after the rules routes (after line 162):

```go
	// Security intelligence (Pro)
	securityHandler := admin.NewSecurityHandler(deps.DB, deps.SecurityScanner, deps.SecurityImporter)
	proGroup.GET("/security/dashboard", securityHandler.Dashboard)
	proGroup.GET("/security/vulnerabilities", securityHandler.ListVulnerabilities)
	proGroup.GET("/security/packages", securityHandler.ListPackages)
	proGroup.GET("/security/suggestions", securityHandler.ListSuggestions)
	proGroup.POST("/security/suggestions/:vuln_id/approve", securityHandler.ApproveSuggestion)
	proGroup.POST("/security/suggestions/:vuln_id/dismiss", securityHandler.DismissSuggestion)
	proGroup.POST("/security/scan", securityHandler.TriggerScan)
	proGroup.POST("/security/import", securityHandler.ImportData)
	proGroup.GET("/security/policies", securityHandler.ListPolicies)
	proGroup.PUT("/security/policies/:ecosystem", securityHandler.UpdatePolicy)
```

Add `SecurityScanner` and `SecurityImporter` to the `Deps` struct:

```go
SecurityScanner  *security.Scanner
SecurityImporter *security.Importer
```

Add the import `"depsilo/internal/security"` to the import block.

- [ ] **Step 3: Run build**

Run: `go build ./...`
Expected: compilation may fail because main.go doesn't populate `SecurityScanner`/`SecurityImporter` yet. That's OK — next task wires it up.

- [ ] **Step 4: Commit**

```bash
git add internal/api/admin/security.go internal/api/router.go
git commit -m "feat(security): add admin API endpoints for security intelligence"
```

---

### Task 8: Wire Up in main.go & Cache Manager Integration

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/cache/manager.go`

- [ ] **Step 1: Add security scanner initialization to main.go**

In `cmd/server/main.go`, after the audit logger setup (around line 182), add:

```go
	// Security scanner
	var securityScanner *security.Scanner
	var securityImporter *security.Importer
	secCfg := cfg.Security
	if secCfg.OSVURL == "" {
		secCfg.OSVURL = "https://api.osv.dev"
	}
	if secCfg.ScanInterval == 0 {
		secCfg.ScanInterval = 24 * time.Hour
	}
	if secCfg.CheckTTL == 0 {
		secCfg.CheckTTL = 24 * time.Hour
	}

	fetcher := security.NewFetcher(secCfg.OSVURL, secCfg.Proxy)
	securityScanner = security.NewScanner(database, fetcher, secCfg.CheckTTL)
	securityImporter = security.NewImporter(database)

	if secCfg.Enabled {
		go security.StartBackgroundScan(ctx, securityScanner, secCfg.ScanInterval)
		zap.L().Info("security vulnerability scanner enabled",
			zap.Duration("scan_interval", secCfg.ScanInterval),
			zap.Duration("check_ttl", secCfg.CheckTTL),
		)
	}
```

Add `SecurityScanner` and `SecurityImporter` to the `api.Deps` struct initialization:

```go
SecurityScanner:  securityScanner,
SecurityImporter: securityImporter,
```

Add `"depsilo/internal/security"` to the import block.

- [ ] **Step 2: Add SetSecurityScanner to cache manager**

In `internal/cache/manager.go`, add after the `SetAuditLogger` pattern (or similar setter):

```go
// securityScanner is the optional security scanner, set via SetSecurityScanner.
var securityScanner interface {
	ScanPackage(ctx context.Context, ecosystem, packageName string) error
}

// SetSecurityScanner sets the security scanner used by the cache manager.
func SetSecurityScanner(s interface{ ScanPackage(ctx context.Context, ecosystem, packageName string) error }) {
	securityScanner = s
}
```

Then in the `Get` method, after a successful cache write (where access is logged), add:

```go
// Trigger async security scan for new packages
if securityScanner != nil {
	go func() {
		if err := securityScanner.ScanPackage(context.Background(), adapterType, ExtractPackageName(adapterType, key)); err != nil {
			zap.L().Debug("security scan for new package failed", zap.Error(err))
		}
	}()
}
```

- [ ] **Step 3: Wire SetSecurityScanner in main.go**

In main.go, after creating the scanner:

```go
cache.SetSecurityScanner(securityScanner)
```

- [ ] **Step 4: Run build**

Run: `go build ./...`
Expected: success

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go internal/cache/manager.go
git commit -m "feat(security): wire scanner into main and cache manager"
```

---

### Task 9: Final Build & Unit Test Verification

- [ ] **Step 1: Run full build**

Run: `go build ./...`
Expected: success

- [ ] **Step 2: Run all unit tests**

Run: `go test ./tests/unit/ -v`
Expected: all existing tests pass (no regressions)

- [ ] **Step 3: Run go vet**

Run: `go vet ./...`
Expected: no issues

- [ ] **Step 4: Commit any fixes if needed**

---

## File Structure Summary

| File | Purpose |
|------|---------|
| `internal/db/models.go` | +4 models: Vulnerability, VulnerabilityCheck, SecurityPolicy, DismissedVuln |
| `internal/db/repository.go` | +4 models in AutoMigrate |
| `internal/config/config.go` | +SecurityConfig struct |
| `config.example.toml` | +[security] section |
| `internal/security/fetcher.go` | OSV.dev API client (query, querybatch, retry, rate limit) |
| `internal/security/parser.go` | OSV response → DB model conversion |
| `internal/security/scanner.go` | Scan coordinator (ScanAll, ScanPackage, auto-block, background goroutine) |
| `internal/security/importer.go` | Offline JSON import |
| `internal/api/admin/security.go` | 10 API endpoint handlers |
| `internal/api/router.go` | Route registration + Deps struct update |
| `cmd/server/main.go` | Scanner initialization + background goroutine startup |
| `internal/cache/manager.go` | SetSecurityScanner + new-package trigger |
| `internal/security/parser_test.go` | Parser unit tests |
