package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestSemVerRangesExcludeUnmentionedPrereleases(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo"} {
		matcher, err := packagepolicy.CompileVersionMatcher(ecosystem, ">= 1.0.0")
		if err != nil {
			t.Fatalf("CompileVersionMatcher(%s): %v", ecosystem, err)
		}
		matched, err := matcher.Match("1.1.0-alpha")
		if err != nil {
			t.Fatalf("Match(%s): %v", ecosystem, err)
		}
		if matched {
			t.Errorf("%s >= 1.0.0 matched unmentioned prerelease 1.1.0-alpha", ecosystem)
		}
	}
}

func TestSemVerSimpleComparatorHandlesZeroCorePrerelease(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo"} {
		matcher, err := packagepolicy.CompileVersionMatcher(ecosystem, "< 0.0.0-alpha")
		if err != nil {
			t.Fatalf("CompileVersionMatcher(%s): %v", ecosystem, err)
		}
		matched, err := matcher.Match("0.0.0-0")
		if err != nil {
			t.Fatalf("Match(%s): %v", ecosystem, err)
		}
		if !matched {
			t.Errorf("%s < 0.0.0-alpha did not match 0.0.0-0", ecosystem)
		}
	}
}

func TestSemVerSimpleComparatorHandlesArbitraryPrecisionPrerelease(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo"} {
		matcher, err := packagepolicy.CompileVersionMatcher(ecosystem, ">= 1.0.0-9999999999999999999")
		if err != nil {
			t.Fatalf("CompileVersionMatcher(%s): %v", ecosystem, err)
		}
		matched, err := matcher.Match("1.0.0-10000000000000000000")
		if err != nil {
			t.Fatalf("Match(%s): %v", ecosystem, err)
		}
		if !matched {
			t.Errorf("%s large numeric prerelease was ordered below the range target", ecosystem)
		}
	}
}

func TestPEP440OrderedComparisonAppliesSpecifierExclusions(t *testing.T) {
	matcher, err := packagepolicy.CompileVersionMatcher("pypi", "> 1.0.0")
	if err != nil {
		t.Fatalf("CompileVersionMatcher: %v", err)
	}
	matched, err := matcher.Match("1.0.post1")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if matched {
		t.Fatal("> 1.0 matched 1.0.post1 contrary to PEP 440 ordered comparison rules")
	}
}

func TestPEP440OrderedComparisonHandlesLocalVersions(t *testing.T) {
	for _, test := range []struct {
		selector string
		version  string
		want     bool
	}{
		{selector: ">= 1.0", version: "1.0+local", want: true},
		{selector: "<= 1.0", version: "1.0+local", want: true},
		{selector: "> 1.0", version: "1.0+local", want: false},
		{selector: ">= 1.0", version: "1.1a1", want: true},
		{selector: "> 1.0a1", version: "1.0.post1", want: true},
		{selector: "> 1.0a1", version: "1.0a2+local", want: true},
		{selector: "> 1.0a1", version: "1.0a1+local", want: false},
		{selector: "> 1.0.post1", version: "1.0.post2+local", want: true},
		{selector: "< 1.0.post1", version: "1.0.dev0", want: true},
		{selector: "< 1.0.post1", version: "1.0.post1.dev0", want: false},
	} {
		matcher, err := packagepolicy.CompileVersionMatcher("pypi", test.selector)
		if err != nil {
			t.Fatalf("CompileVersionMatcher(%q): %v", test.selector, err)
		}
		got, err := matcher.Match(test.version)
		if err != nil {
			t.Fatalf("Match(%q): %v", test.version, err)
		}
		if got != test.want {
			t.Errorf("%q Match(%q) = %v, want %v", test.selector, test.version, got, test.want)
		}
	}
}

func TestPEP440OrderedComparisonRejectsLocalTarget(t *testing.T) {
	if _, err := packagepolicy.CompileVersionMatcher("pypi", ">= 1.0+local"); err == nil {
		t.Fatal("ordered PEP 440 comparison accepted a local version target")
	}
}

func TestPEP440BareExactUsesVersionPrecedenceEquality(t *testing.T) {
	matcher, err := packagepolicy.CompileVersionMatcher("pypi", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		version string
		want    bool
	}{
		{version: "1.0.0", want: true},
		{version: "1.0+local", want: false},
	} {
		got, err := matcher.Match(test.version)
		if err != nil {
			t.Fatalf("Match(%q): %v", test.version, err)
		}
		if got != test.want {
			t.Errorf("bare exact 1.0 Match(%q) = %v, want %v", test.version, got, test.want)
		}
	}
}

func TestMavenPreparedSelectorPreservesSelfComparison(t *testing.T) {
	for _, selector := range []string{"0alpha1", "1.0-RC1", "1-final"} {
		matcher, err := packagepolicy.CompileVersionMatcher("maven", selector)
		if err != nil {
			t.Fatalf("CompileVersionMatcher(%q): %v", selector, err)
		}
		matched, err := matcher.Match(selector)
		if err != nil {
			t.Fatalf("Match(%q): %v", selector, err)
		}
		if !matched {
			t.Errorf("exact Maven selector %q did not match itself", selector)
		}
	}
}
