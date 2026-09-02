package packagepolicy

import (
	"strings"
	"testing"
)

func TestHuggingFacePackagePrefixesCoverEveryRequestIdentityShape(t *testing.T) {
	valid := []struct {
		prefix string
		want   string
	}{
		{prefix: "bert", want: "bert"},
		{prefix: "datasets", want: "datasets"}, // Keep the single-segment model namespace-neutral.
		{prefix: "Org/", want: "org/"},
		{prefix: "Org/Mod", want: "org/mod"},
		{prefix: "datasets/", want: "datasets/"},
		{prefix: "datasets/Org/", want: "datasets/org/"},
		{prefix: "datasets/Org/Data", want: "datasets/org/data"},
	}
	for _, test := range valid {
		t.Run(test.prefix, func(t *testing.T) {
			got, err := normalizeHuggingFacePackagePrefix(test.prefix)
			if err != nil {
				t.Fatalf("normalizeHuggingFacePackagePrefix(%q): %v", test.prefix, err)
			}
			if got != test.want {
				t.Fatalf("normalizeHuggingFacePackagePrefix(%q) = %q, want %q", test.prefix, got, test.want)
			}
		})
	}

	for _, prefix := range []string{
		".repo", "repo--", "owner/.repo", "spaces/owner/", "owner/repo/extra",
		strings.Repeat("a", 97), "datasets/" + strings.Repeat("a", 97),
	} {
		t.Run("invalid_"+prefix, func(t *testing.T) {
			if _, err := normalizeHuggingFacePackagePrefix(prefix); err == nil {
				t.Fatalf("normalizeHuggingFacePackagePrefix(%q) succeeded, want an error", prefix)
			}
		})
	}
}
