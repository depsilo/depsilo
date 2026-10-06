package server

import (
	"testing"
	"time"

	"depsilo/internal/quarantine"
)

func TestSourceBoundMinimumReleaseAgeEcosystems(t *testing.T) {
	for _, ecosystem := range []string{"npm", "pypi", "composer"} {
		if !sourceBoundMinimumReleaseAge(ecosystem) {
			t.Errorf("sourceBoundMinimumReleaseAge(%q) = false, want true", ecosystem)
		}
	}
	for _, ecosystem := range []string{"cargo", "nuget", "rubygems", "go", "apt", "unknown"} {
		if sourceBoundMinimumReleaseAge(ecosystem) {
			t.Errorf("sourceBoundMinimumReleaseAge(%q) = true, want false", ecosystem)
		}
	}
}

func TestSourceBoundPolicyAcceptsBoundThresholdsAndRejectsUnbound(t *testing.T) {
	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge: map[string]string{
			"npm":      "168h",
			"pypi":     "72h",
			"composer": "72h",
		},
	}, sourceBoundMinimumReleaseAge)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance(bound ecosystems): %v", err)
	}
	for ecosystem, want := range map[string]time.Duration{
		"npm":      168 * time.Hour,
		"pypi":     72 * time.Hour,
		"composer": 72 * time.Hour,
	} {
		if got := policy.Threshold(ecosystem); got != want {
			t.Errorf("Threshold(%q) = %v, want %v", ecosystem, got, want)
		}
	}
	if policy.SourceProvenanceBound("cargo") {
		t.Error("cargo reported as source-bound")
	}
	if _, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"cargo": "72h"},
	}, sourceBoundMinimumReleaseAge); err == nil {
		t.Fatal("unbound cargo threshold was accepted")
	}
}
