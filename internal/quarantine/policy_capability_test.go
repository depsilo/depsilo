package quarantine

import (
	"strings"
	"testing"
)

func TestMinimumReleaseAgeCapabilityProfile(t *testing.T) {
	enabled := true
	policy, err := NewPolicy(Config{MinReleaseAgeEnabled: &enabled})
	if err != nil {
		t.Fatalf("NewPolicy(enabled): %v", err)
	}

	for _, ecosystem := range []string{
		"pypi", "npm", "go", "cargo", "maven", "rubygems", "composer",
		"nuget", "conda", "cran", "helm", "alpine", "docker",
		"huggingface", "apt", "unknown-future",
	} {
		if got := policy.Threshold(ecosystem); got != 0 {
			t.Errorf("Threshold(%q) = %v, want safe zero", ecosystem, got)
		}
	}
	if policy.HasActiveThresholds() {
		t.Fatal("production policy reported an active source-unbound threshold")
	}
}

func TestMinimumReleaseAgeRejectsUnsupportedPositiveThresholdWhenEnabled(t *testing.T) {
	enabled := true
	for _, ecosystem := range []string{"npm", "cargo", "composer", "nuget", "pypi", "maven", "huggingface", "unknown-future"} {
		t.Run(ecosystem, func(t *testing.T) {
			_, err := NewPolicy(Config{
				MinReleaseAgeEnabled: &enabled,
				MinReleaseAge:        map[string]string{ecosystem: "1d"},
			})
			if err == nil {
				t.Fatal("NewPolicy accepted a positive threshold without a trustworthy artifact-to-release identity seam")
			}
			if !strings.Contains(err.Error(), ecosystem) || !strings.Contains(err.Error(), "not supported") {
				t.Fatalf("NewPolicy error = %q, want ecosystem-specific unsupported error", err)
			}
		})
	}
}

func TestMinimumReleaseAgeExplicitDisablePreservesLegacyUnsupportedTable(t *testing.T) {
	enabled := false
	policy, err := NewPolicy(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"pypi": "3d", "maven": "3d"},
	})
	if err != nil {
		t.Fatalf("NewPolicy(disabled legacy table): %v", err)
	}
	if got := policy.Threshold("pypi"); got != 0 {
		t.Fatalf("disabled policy threshold = %v, want zero", got)
	}
}

func TestMinimumReleaseAgeDefaultNeverEnablesUnknownEcosystem(t *testing.T) {
	_, err := NewPolicy(Config{MinReleaseAge: map[string]string{"default": "1d"}})
	if err == nil || !strings.Contains(err.Error(), "source provenance") {
		t.Fatalf("NewPolicy(default threshold) error = %v, want source-provenance rejection", err)
	}
}
