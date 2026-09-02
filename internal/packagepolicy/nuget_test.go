package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestNuGetExactMatcherUsesNuGetVersionIdentity(t *testing.T) {
	matcher, err := packagepolicy.CompileVersionMatcher("nuget", "1.0")
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{
		"1",
		"1.0.0",
		"1.00.0",
		"1.0.0.0",
		"1.0.0+build.1",
	} {
		matched, err := matcher.Match(version)
		if err != nil {
			t.Fatalf("Match(%q): %v", version, err)
		}
		if !matched {
			t.Errorf("NuGet exact selector 1.0 did not match equivalent version %q", version)
		}
	}
}

func TestNuGetExactMatcherIgnoresPrereleaseASCIICase(t *testing.T) {
	matcher, err := packagepolicy.CompileVersionMatcher("nuget", "1.0-Alpha.1")
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{
		"1.0.0-alpha.1",
		"1.00.0-ALPHA.1+BUILD.7",
	} {
		matched, err := matcher.Match(version)
		if err != nil {
			t.Fatalf("Match(%q): %v", version, err)
		}
		if !matched {
			t.Errorf("NuGet prerelease selector did not match equivalent version %q", version)
		}
	}
}

func TestNuGetPreparedRuleStoresCanonicalVersion(t *testing.T) {
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem:   "nuget",
		PackageName: "NuGet.Core",
		Version:     "01.00.0.0-Alpha.1+build.7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.NormalizedVersion != "1.0.0-alpha.1" {
		t.Fatalf("NormalizedVersion = %q, want %q", prepared.NormalizedVersion, "1.0.0-alpha.1")
	}
}

func TestNuGetDialectRejectsInvalidVersions(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("nuget")
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{
		"",
		" 1.0",
		"1.0 ",
		"1.0.0.0.0",
		"1..0",
		"1.0.",
		"-1.0",
		"1.0-alpha..1",
		"1.0-01",
		"1.0-alpha_1",
		"1.0+",
		"1.0+build..1",
		"2147483648.0.0",
	} {
		if err := dialect.ValidateVersion(version); err == nil {
			t.Errorf("ValidateVersion(%q) unexpectedly succeeded", version)
		}
	}
}

func TestNuGetDialectUsesNuGetVersionPrecedence(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("nuget")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0-alpha.2", right: "1.0-alpha.10", want: -1},
		{left: "1.0-alpha.1", right: "1.0-alpha.beta", want: -1},
		{left: "1.0-alpha", right: "1.0", want: -1},
		{left: "1.0.0.1", right: "1.0.1", want: -1},
		{left: "1.0-Alpha", right: "1.0-alpha+build", want: 0},
	} {
		got, err := dialect.CompareVersions(test.left, test.right)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", test.left, test.right, err)
		}
		if sign(got) != test.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", test.left, test.right, got, test.want)
		}
	}
}

func TestNuGetDialectRemainsExactOnly(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("nuget")
	if err != nil {
		t.Fatal(err)
	}
	if dialect.SupportsRanges() {
		t.Fatal("NuGet dialect advertised range support")
	}
	if _, err := packagepolicy.CompileVersionMatcher("nuget", ">= 1.0"); err == nil {
		t.Fatal("NuGet dialect accepted an ordered range")
	}
}
