package security

import (
	"testing"
)

func TestParseVulnerability_CVSS(t *testing.T) {
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
