package packagepolicy_test

import (
	"strings"
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestHuggingFaceConcreteIdentityFollowsHubRepoIDShapes(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("huggingface")
	if err != nil {
		t.Fatal(err)
	}

	valid := []struct {
		name string
		want string
	}{
		{name: "bert-base-uncased", want: "bert-base-uncased"},
		{name: "datasets", want: "datasets"}, // A one-segment model, not the dataset namespace.
		{name: "Org/Model", want: "org/model"},
		{name: "datasets/SQuAD", want: "datasets/squad"},
		{name: "datasets/Org/My_Data", want: "datasets/org/my_data"},
		{name: "_private", want: "_private"},
		{name: strings.Repeat("a", 96), want: strings.Repeat("a", 96)},
		{name: "a/" + strings.Repeat("b", 94), want: "a/" + strings.Repeat("b", 94)},
		{name: "datasets/" + strings.Repeat("a", 96), want: "datasets/" + strings.Repeat("a", 96)},
		{name: "datasets/a/" + strings.Repeat("b", 94), want: "datasets/a/" + strings.Repeat("b", 94)},
	}
	for _, test := range valid {
		t.Run("valid_"+test.name, func(t *testing.T) {
			got, err := dialect.NormalizePackageName(test.name)
			if err != nil {
				t.Fatalf("NormalizePackageName(%q): %v", test.name, err)
			}
			if got != test.want {
				t.Fatalf("NormalizePackageName(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}

	invalid := []string{
		".repo", "repo-", "owner/.repo", "owner/repo--name", "owner/repo..name",
		"owner/repo.git", "spaces/owner/repo", strings.Repeat("a", 97),
		"a/" + strings.Repeat("b", 95), "datasets/" + strings.Repeat("a", 97),
		"datasets/a/" + strings.Repeat("b", 95),
	}
	for _, name := range invalid {
		t.Run("invalid_"+name, func(t *testing.T) {
			if _, err := dialect.NormalizePackageName(name); err == nil {
				t.Fatalf("NormalizePackageName(%q) succeeded, want an error", name)
			}
		})
	}
}

func TestHuggingFacePackageGlobsCoverEveryRequestIdentityShape(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("huggingface")
	if err != nil {
		t.Fatal(err)
	}

	valid := []struct {
		pattern string
		want    string
	}{
		{pattern: "bert-*", want: "bert-*"},
		{pattern: "datasets*", want: "datasets*"},
		{pattern: "Org/*", want: "org/*"},
		{pattern: "Org.git/*", want: "org.git/*"},
		{pattern: "datasets/*", want: "datasets/*"},
		{pattern: "datasets/Org/*", want: "datasets/org/*"},
		{pattern: "datasets/Org/[Dd]ata?", want: "datasets/org/[dd]ata?"},
	}
	for _, test := range valid {
		t.Run(test.pattern, func(t *testing.T) {
			got, err := packagepolicy.NormalizePackageGlob(dialect, test.pattern)
			if err != nil {
				t.Fatalf("NormalizePackageGlob(%q): %v", test.pattern, err)
			}
			if got != test.want {
				t.Fatalf("NormalizePackageGlob(%q) = %q, want %q", test.pattern, got, test.want)
			}
		})
	}

	for _, pattern := range []string{
		".repo*", "owner/.repo*", "owner/repo--*", "spaces/owner/*", "*/owner/repo",
		"owner/repo/extra*", "owner//repo*", "owner/repo.git", strings.Repeat("a", 97) + "*",
		"datasets/" + strings.Repeat("a", 97) + "*",
	} {
		t.Run("invalid_"+pattern, func(t *testing.T) {
			if _, err := packagepolicy.NormalizePackageGlob(dialect, pattern); err == nil {
				t.Fatalf("NormalizePackageGlob(%q) succeeded, want an error", pattern)
			}
		})
	}
}
