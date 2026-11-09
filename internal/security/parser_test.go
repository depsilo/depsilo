package security

import (
	"encoding/json"
	"testing"
)

// realOSVRecord is shaped like the full records OSV returns from /v1/query:
// CVSS scores are vectors, and GitHub advisories add a qualitative rating.
const realOSVRecord = `{
  "id": "GHSA-jjg7-2v4v-x38h",
  "summary": "Internationalized Domain Names in Applications (IDNA) vulnerable to denial of service",
  "modified": "2026-09-10T03:50:12.774088Z",
  "published": "2024-04-11T16:54:35Z",
  "aliases": ["CVE-2024-3651", "PYSEC-2024-60"],
  "severity": [
    {"type": "CVSS_V3", "score": "CVSS:3.1/AV:L/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"},
    {"type": "CVSS_V4", "score": "CVSS:4.0/AV:L/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:H/SC:N/SI:N/SA:N"}
  ],
  "affected": [{
    "package": {"name": "idna", "ecosystem": "PyPI"},
    "ranges": [{"type": "ECOSYSTEM", "events": [{"introduced": "0"}, {"fixed": "3.7"}]}],
    "database_specific": {"source": "https://github.com/github/advisory-database"}
  }],
  "database_specific": {"severity": "MODERATE", "cwe_ids": ["CWE-400"], "github_reviewed": true},
  "schema_version": "1.7.3"
}`

func decodeOSVRecord(t *testing.T, raw string) OSVVulnerability {
	t.Helper()
	var record OSVVulnerability
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestParseVulnerabilityScoresRealOSVVector(t *testing.T) {
	parsed := ParseVulnerability(decodeOSVRecord(t, realOSVRecord), "pypi")
	if parsed.Severity != "medium" || parsed.CVSSScore != 6.2 || parsed.DetailRevision != AdvisoryDetailRevision {
		t.Fatalf("parsed severity = %q, score = %v, revision = %d; want medium 6.2 at revision %d",
			parsed.Severity, parsed.CVSSScore, parsed.DetailRevision, AdvisoryDetailRevision)
	}
	if parsed.PackageName != "idna" || parsed.Summary == "" || fixedVersionForTest(t, parsed.AffectedRanges) != "3.7" {
		t.Fatalf("parsed record lost details: %+v", parsed)
	}
}

func TestParseVulnerabilityFallsBackToQualitativeRatingWithoutV3Vector(t *testing.T) {
	record := decodeOSVRecord(t, realOSVRecord)
	record.Severity = record.Severity[1:] // CVSS v4 only
	for label, want := range map[string]string{
		"CRITICAL": "critical", "HIGH": "high", "MODERATE": "medium", "medium": "medium", "LOW": "low",
	} {
		record.DatabaseSpecific = json.RawMessage(`{"severity":"` + label + `"}`)
		parsed := ParseVulnerability(record, "pypi")
		if parsed.Severity != want || parsed.CVSSScore != 0 {
			t.Errorf("label %s: severity = %q, score = %v; want %s with no invented score", label, parsed.Severity, parsed.CVSSScore, want)
		}
	}
}

func TestParseVulnerabilityLeavesUnratedRecordsUnknown(t *testing.T) {
	for name, raw := range map[string]string{
		"no severity":               `{"id":"GO-2024-0001","affected":[{"package":{"name":"golang.org/x/text","ecosystem":"Go"}}]}`,
		"malformed vector":          `{"id":"X-1","severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N"}]}`,
		"unrecognized rating":       `{"id":"X-2","database_specific":{"severity":"IMPORTANT"}}`,
		"non-string rating":         `{"id":"X-3","database_specific":{"severity":{"level":"HIGH"}}}`,
		"non-object specific field": `{"id":"X-4","database_specific":["HIGH"]}`,
	} {
		parsed := ParseVulnerability(decodeOSVRecord(t, raw), "go")
		if parsed.Severity != "unknown" || parsed.CVSSScore != 0 {
			t.Errorf("%s: severity = %q, score = %v; want unknown", name, parsed.Severity, parsed.CVSSScore)
		}
	}
}

func TestParseVulnerability_CVSS(t *testing.T) {
	// Operator imports from other tools may carry a bare numeric score.
	vuln := OSVVulnerability{
		ID:      "GHSA-test-1",
		Summary: "Test vulnerability",
		Severity: []osvSeverity{
			{Type: "CVSS_V3", Score: "9.8"},
		},
	}
	parsed := ParseVulnerability(vuln, "pypi")
	if parsed.Severity != "critical" {
		t.Errorf("severity = %q, want critical", parsed.Severity)
	}
	if parsed.CVSSScore < 9.0 {
		t.Errorf("cvss = %f, want >= 9.0", parsed.CVSSScore)
	}
	if parsed.Ecosystem != "pypi" {
		t.Errorf("ecosystem = %q, want pypi", parsed.Ecosystem)
	}
}

func TestClassifyCVSS_Levels(t *testing.T) {
	tests := []struct {
		score    float32
		expected string
	}{
		{9.8, "critical"},
		{9.0, "critical"},
		{8.5, "high"},
		{7.0, "high"},
		{6.5, "medium"},
		{4.0, "medium"},
		{3.9, "low"},
		{0.1, "low"},
		{0, "unknown"},
	}
	for _, tt := range tests {
		got := classifyCVSS(tt.score)
		if got != tt.expected {
			t.Errorf("classifyCVSS(%f) = %q, want %q", tt.score, got, tt.expected)
		}
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
		{"nuget", "NuGet"},
		{"composer", "Packagist"},
		{"rubygems", "RubyGems"},
		{"cran", "CRAN"},
		{"apt", "Debian"},
	}
	for _, tt := range tests {
		got := OSVEcosystem(tt.depsilo)
		if got != tt.osv {
			t.Errorf("OSVEcosystem(%q) = %q, want %q", tt.depsilo, got, tt.osv)
		}
	}
}

func TestUnsupportedEcosystem(t *testing.T) {
	for _, eco := range []string{"conda", "helm", "docker"} {
		if got := OSVEcosystem(eco); got != "" {
			t.Errorf("OSVEcosystem(%q) = %q, want empty", eco, got)
		}
	}
}

func TestReverseEcosystem(t *testing.T) {
	if got := reverseEcosystem("PyPI"); got != "pypi" {
		t.Errorf("reverseEcosystem(PyPI) = %q, want pypi", got)
	}
	if got := reverseEcosystem("Unknown"); got != "" {
		t.Errorf("reverseEcosystem(Unknown) = %q, want empty", got)
	}
}

func TestParseVulnerability_ExtractsPackageFromAffected(t *testing.T) {
	vuln := OSVVulnerability{
		ID: "GHSA-test-2",
		Affected: []osvAffected{
			{
				Package: &osvPackage{Name: "lodash", Ecosystem: "npm"},
				Ranges:  []osvRange{{Type: "SEMVER", Events: []osvEvent{{Introduced: "0"}, {Fixed: "4.17.21"}}}},
			},
		},
	}
	parsed := ParseVulnerability(vuln, "npm")
	if parsed.PackageName != "lodash" {
		t.Errorf("package_name = %q, want lodash", parsed.PackageName)
	}
	if parsed.Ecosystem != "npm" {
		t.Errorf("ecosystem = %q, want npm", parsed.Ecosystem)
	}
}
