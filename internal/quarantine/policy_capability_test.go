package quarantine

import (
	"strings"
	"testing"
	"time"
)

func TestMinimumReleaseAgeBoundEcosystemAcceptsPositiveThreshold(t *testing.T) {
	enabled := true
	bound := func(ecosystem string) bool { return ecosystem == "npm" }
	policy, err := NewPolicyWithProvenance(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"npm": "168h"},
	}, bound)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance(bound npm): %v", err)
	}
	if got := policy.Threshold("npm"); got != 168*time.Hour {
		t.Fatalf("Threshold(npm) = %v, want 168h", got)
	}
	if !policy.SourceProvenanceBound("npm") || policy.SourceProvenanceBound("pypi") {
		t.Fatalf("bound predicate not scoped to npm: npm=%v pypi=%v",
			policy.SourceProvenanceBound("npm"), policy.SourceProvenanceBound("pypi"))
	}
	if !policy.HasActiveThresholds() {
		t.Fatal("bound policy reported no active thresholds")
	}

	_, err = NewPolicyWithProvenance(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"pypi": "72h"},
	}, bound)
	if err == nil || !strings.Contains(err.Error(), "pypi") {
		t.Fatalf("unbound pypi threshold error = %v, want ecosystem-specific rejection", err)
	}
}

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
			if !strings.Contains(err.Error(), ecosystem) {
				t.Fatalf("NewPolicy error = %q, want ecosystem-specific error", err)
			}
			want := "not supported"
			if approximateCapable(ecosystem) {
				want = "approximate"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("NewPolicy error = %q, want %q guidance", err, want)
			}
		})
	}
}

func TestMinimumReleaseAgeApproximateSourceRequiresAcknowledgement(t *testing.T) {
	enabled := true
	for _, ecosystem := range []string{"conda", "cran", "maven", "alpine"} {
		t.Run(ecosystem, func(t *testing.T) {
			_, err := NewPolicyWithProvenance(Config{
				MinReleaseAgeEnabled: &enabled,
				MinReleaseAge:        map[string]string{ecosystem: "72h"},
			}, nil)
			if err == nil || !strings.Contains(err.Error(), "approximate") {
				t.Fatalf("unacknowledged %s threshold error = %v, want approximate guidance", ecosystem, err)
			}

			policy, err := NewPolicyWithProvenance(Config{
				MinReleaseAgeEnabled: &enabled,
				MinReleaseAge:        map[string]string{ecosystem: "72h"},
				ApproximateSources:   []string{ecosystem},
			}, nil)
			if err != nil {
				t.Fatalf("NewPolicyWithProvenance(acknowledged %s): %v", ecosystem, err)
			}
			if got := policy.Threshold(ecosystem); got != 72*time.Hour {
				t.Fatalf("Threshold(%s) = %v, want 72h", ecosystem, got)
			}
			if !policy.SourceProvenanceBound(ecosystem) || !policy.ApproximateProvenance(ecosystem) {
				t.Fatalf("%s bound=%v approximate=%v, want both true",
					ecosystem, policy.SourceProvenanceBound(ecosystem), policy.ApproximateProvenance(ecosystem))
			}
			if policy.ApproximateProvenance("npm") {
				t.Fatal("npm reported as approximate provenance")
			}
		})
	}
}

func TestMinimumReleaseAgeAcknowledgedUnsupportedEcosystemIsRejected(t *testing.T) {
	enabled := true
	// Helm and Docker are not approximate-capable yet: the operator listing
	// them in approximate_sources must not make a positive threshold safe.
	for _, ecosystem := range []string{"helm", "docker"} {
		_, err := NewPolicyWithProvenance(Config{
			MinReleaseAgeEnabled: &enabled,
			MinReleaseAge:        map[string]string{ecosystem: "72h"},
			ApproximateSources:   []string{ecosystem},
		}, nil)
		if err == nil {
			t.Fatalf("acknowledged %s threshold was accepted", ecosystem)
		}
		if !strings.Contains(err.Error(), "approximate_sources does not enable it") {
			t.Fatalf("acknowledged %s error = %v, want actionable guidance", ecosystem, err)
		}
	}

	policy, err := NewPolicyWithProvenance(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"helm": "0"},
		ApproximateSources:   []string{"helm"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance(zero helm threshold): %v", err)
	}
	if policy.SourceProvenanceBound("helm") || policy.ApproximateProvenance("helm") {
		t.Fatalf("helm bound=%v approximate=%v, want both false",
			policy.SourceProvenanceBound("helm"), policy.ApproximateProvenance("helm"))
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
