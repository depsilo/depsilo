package packagepolicy_test

import (
	"errors"
	"strings"
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestMavenPackageCoordinateValidation(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{
		"org.apache.maven:maven-core",
		"Com.Example:Artifact_Name",
	} {
		got, err := dialect.NormalizePackageName(valid)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", valid, err)
		} else if got != valid {
			t.Errorf("NormalizePackageName(%q) = %q, want case-preserving identity", valid, got)
		}
	}
	for _, invalid := range []string{
		"org.example",
		":artifact",
		"org.example:",
		"org.example:artifact:extra",
		"org..example:artifact",
		"org/example:artifact",
		"org.example:artifact/name",
	} {
		if _, err := dialect.NormalizePackageName(invalid); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted a non-coordinate", invalid)
		}
	}

	for selector, want := range map[string]string{
		"org.apache.*":       "org.apache.*",
		"org.apache:*":       "org.apache:*",
		"org.apache:maven-*": "org.apache:maven-*",
	} {
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "maven", PackageName: selector, Version: "*",
		})
		if err != nil {
			t.Errorf("PrepareRule(%q): %v", selector, err)
		} else if prepared.NormalizedPackageName != want {
			t.Errorf("PrepareRule(%q) normalized package = %q, want %q", selector, prepared.NormalizedPackageName, want)
		}
	}
}

func TestNuGetPackageIDValidationAndNormalization(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("nuget")
	if err != nil {
		t.Fatal(err)
	}
	for input, want := range map[string]string{
		"Newtonsoft.Json": "newtonsoft.json",
		"Contoso_Core-1":  "contoso_core-1",
	} {
		got, err := dialect.NormalizePackageName(input)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", input, err)
		} else if got != want {
			t.Errorf("NormalizePackageName(%q) = %q, want %q", input, got, want)
		}
	}
	for _, invalid := range []string{
		".foo",
		"foo-",
		"contoso../id",
		"foo/bar",
		"café",
		strings.Repeat("a", 101),
	} {
		if _, err := dialect.NormalizePackageName(invalid); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted an invalid NuGet ID", invalid)
		}
	}
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem: "nuget", PackageName: "Newtonsoft.*", Version: "*",
	})
	if err != nil {
		t.Fatalf("PrepareRule(NuGet prefix): %v", err)
	}
	if prepared.NormalizedPackageName != "newtonsoft.*" {
		t.Fatalf("normalized NuGet prefix = %q", prepared.NormalizedPackageName)
	}
}

func TestCRANPackageAndVersionIdentity(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("cran")
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{"R6", "data.table", "BiocGenerics"} {
		got, err := dialect.NormalizePackageName(valid)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", valid, err)
		} else if got != valid {
			t.Errorf("NormalizePackageName(%q) = %q, want case-preserving identity", valid, got)
		}
	}
	for _, invalid := range []string{"A", "_foo", "foo_", "foo-bar", "foo.", "café"} {
		if _, err := dialect.NormalizePackageName(invalid); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted an invalid CRAN package name", invalid)
		}
	}

	for _, version := range []string{"0.01", "0.01.0", "0.1-0"} {
		if err := dialect.ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q): %v", version, err)
		}
		comparison, err := dialect.CompareVersions("0.01", version)
		if err != nil {
			t.Errorf("CompareVersions(0.01, %q): %v", version, err)
		} else if comparison != 0 {
			t.Errorf("CompareVersions(0.01, %q) = %d, want CRAN equality", version, comparison)
		}
	}
	for _, invalid := range []string{"1", "1..0", "1.a", "v1.0", "1.0+build"} {
		if err := dialect.ValidateVersion(invalid); err == nil {
			t.Errorf("ValidateVersion(%q) accepted an invalid CRAN version", invalid)
		}
	}

	matcher, err := packagepolicy.CompileVersionMatcher("cran", "0.01")
	if err != nil {
		t.Fatal(err)
	}
	matched, err := matcher.Match("0.1-0")
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("CRAN exact rule did not use package_version equality")
	}
	if _, err := packagepolicy.CompileVersionMatcher("cran", ">= 0.1"); !errors.Is(err, packagepolicy.ErrRangesUnsupported) {
		t.Fatalf("CRAN range error = %v, want ErrRangesUnsupported", err)
	}
}
