package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestPyPIDialectRejectsUnicodeRegexFoldAliases(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0+K", "1.0+ſ", "1.0poſt1"} {
		if err := dialect.ValidateVersion(version); err == nil {
			t.Errorf("ValidateVersion(%q) accepted a non-ASCII PEP 440 alias", version)
		}
	}
}

func TestPyPIDialectValidatesProjectNameBeforeNormalization(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".foo", "foo-", "café"} {
		if _, err := dialect.NormalizePackageName(name); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted an invalid PyPI project name", name)
		}
	}
	normalized, err := dialect.NormalizePackageName("Friendly-._-Bard")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "friendly-bard" {
		t.Fatalf("normalized PyPI name = %q, want friendly-bard", normalized)
	}
}
