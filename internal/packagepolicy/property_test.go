package packagepolicy_test

import (
	"strings"
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestDebianComparisonDoesNotOverflowNumericFields(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := dialect.CompareVersions(
		"1.999999999999999999999999",
		"1.1000000000000000000000000",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got >= 0 {
		t.Fatalf("comparison = %d, want first arbitrary-length numeric field to sort earlier", got)
	}
}

func FuzzVersionComparatorProperties(f *testing.F) {
	seeds := []struct {
		ecosystem string
		a         string
		b         string
		c         string
	}{
		{ecosystem: "npm", a: "1.0.0-alpha", b: "1.0.0-rc.1", c: "1.0.0"},
		{ecosystem: "cargo", a: "0.9.0", b: "1.0.0", c: "2.0.0"},
		{ecosystem: "pypi", a: "1.0.dev1", b: "1.0a1", c: "1.0"},
		{ecosystem: "maven", a: "1.0-alpha", b: "1.0-rc1", c: "1.0"},
		{ecosystem: "apt", a: "1.0~beta1", b: "1.0-1", c: "1:1.0-1"},
		{ecosystem: "go", a: "v1.0.0", b: "v1.1.0", c: "v2.0.0"},
		{ecosystem: "nuget", a: "1.0-alpha", b: "1.0", c: "1.0.1"},
		{ecosystem: "cran", a: "0.01", b: "0.2-0", c: "1.0"},
		{ecosystem: "composer", a: "1.0.0-alpha", b: "1.0.0", c: "2.0.0"},
		{ecosystem: "conda", a: "1.0a1", b: "1.0", c: "2.0"},
		{ecosystem: "alpine", a: "1.0-r0", b: "1.0-r1", c: "2.0-r0"},
	}
	for _, seed := range seeds {
		f.Add(seed.ecosystem, seed.a, seed.b, seed.c)
	}

	f.Fuzz(func(t *testing.T, ecosystem, a, b, c string) {
		dialect, err := packagepolicy.DialectFor(ecosystem)
		if err != nil {
			t.Skip()
		}
		for _, version := range []string{a, b, c} {
			if err := dialect.ValidateVersion(version); err != nil {
				t.Skip()
			}
		}

		ab, err := dialect.CompareVersions(a, b)
		if err != nil {
			t.Fatal(err)
		}
		ba, err := dialect.CompareVersions(b, a)
		if err != nil {
			t.Fatal(err)
		}
		if sign(ab) != -sign(ba) {
			t.Fatalf("antisymmetry failed: compare(%q,%q)=%d compare(%q,%q)=%d", a, b, ab, b, a, ba)
		}

		aa, err := dialect.CompareVersions(a, a)
		if err != nil {
			t.Fatal(err)
		}
		if aa != 0 {
			t.Fatalf("identity failed: compare(%q,%q)=%d", a, a, aa)
		}

		// Every comparator remains subject to transitivity except Maven's
		// pinned ComparableVersion implementation, whose unrestricted grammar
		// has a documented non-transitive triple and is therefore exact-only.
		if !strings.EqualFold(strings.TrimSpace(ecosystem), "maven") {
			bc, err := dialect.CompareVersions(b, c)
			if err != nil {
				t.Fatal(err)
			}
			if ab < 0 && bc < 0 {
				ac, err := dialect.CompareVersions(a, c)
				if err != nil {
					t.Fatal(err)
				}
				if ac >= 0 {
					t.Fatalf("transitivity failed: %q < %q < %q but compare(first,last)=%d", a, b, c, ac)
				}
			}
		}
	})
}
