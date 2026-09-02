package packagepolicy_test

import (
	"errors"
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestPrepareRulePersistsRawAndNormalizedValues(t *testing.T) {
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem:   " PyPI ",
		PackageName: " Friendly-._-Bard ",
		Version:     ">=1.0RC1",
	})
	if err != nil {
		t.Fatalf("PrepareRule: %v", err)
	}

	if prepared.Ecosystem != "pypi" {
		t.Errorf("Ecosystem = %q, want pypi", prepared.Ecosystem)
	}
	if prepared.PackageName != "Friendly-._-Bard" {
		t.Errorf("PackageName raw value = %q", prepared.PackageName)
	}
	if prepared.Version != ">=1.0RC1" {
		t.Errorf("Version raw value = %q", prepared.Version)
	}
	if prepared.NormalizedPackageName != "friendly-bard" {
		t.Errorf("NormalizedPackageName = %q", prepared.NormalizedPackageName)
	}
	if prepared.NormalizedVersion != ">= 1.0rc1" {
		t.Errorf("NormalizedVersion = %q, want %q", prepared.NormalizedVersion, ">= 1.0rc1")
	}
	if prepared.DialectRevision != packagepolicy.CurrentDialectRevision {
		t.Errorf("DialectRevision = %d, want %d", prepared.DialectRevision, packagepolicy.CurrentDialectRevision)
	}
}

func TestPrepareRuleRejectsRangesForExactOnlyEcosystems(t *testing.T) {
	packageNames := map[string]string{
		"go":     "example.com/module",
		"nuget":  "example",
		"conda":  "conda-forge/example",
		"cran":   "example",
		"alpine": "v3.21/main/x86_64/example",
		"maven":  "org.example:example",
	}
	for ecosystem, packageName := range packageNames {
		_, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: ecosystem, PackageName: packageName, Version: ">= 1.0.0",
		})
		if !errors.Is(err, packagepolicy.ErrRangesUnsupported) {
			t.Errorf("%s range error = %v, want ErrRangesUnsupported", ecosystem, err)
		}
	}
}

func TestPrepareRuleRestrictsUnenforceableEcosystemsToPackageWideRules(t *testing.T) {
	tests := []struct {
		ecosystem   string
		packageName string
		versions    []string
	}{
		{ecosystem: "apt", packageName: "libc6", versions: []string{"1:1.0-1", ">= 1:1.0-1"}},
		{ecosystem: "composer", packageName: "vendor/example", versions: []string{"1.16.5", ">= 1.16.5"}},
	}
	for _, test := range tests {
		for _, version := range test.versions {
			_, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
				Ecosystem: test.ecosystem, PackageName: test.packageName, Version: version,
			})
			if !errors.Is(err, packagepolicy.ErrVersionSelectorsUnsupported) {
				t.Errorf("%s selector %q error = %v, want ErrVersionSelectorsUnsupported", test.ecosystem, version, err)
			}
		}
		if _, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: test.ecosystem, PackageName: test.packageName, Version: "*",
		}); err != nil {
			t.Errorf("package-wide %s rule rejected: %v", test.ecosystem, err)
		}
	}
}

func TestPrepareNPMRuleAcceptsStrictExactAndSingleComparator(t *testing.T) {
	for _, version := range []string{"1.16.5", ">= 1.16.5"} {
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "npm", PackageName: "@Scope/example", Version: version,
		})
		if err != nil {
			t.Errorf("npm selector %q rejected: %v", version, err)
			continue
		}
		if prepared.NormalizedPackageName != "@Scope/example" {
			t.Errorf("npm package normalized to %q", prepared.NormalizedPackageName)
		}
	}
}

func TestPrepareNPMRuleRejectsNodeStyleCompoundRanges(t *testing.T) {
	for _, version := range []string{
		"^1.2.3",
		"~1.2.3",
		"1.x",
		">= 1.0.0 < 2.0.0",
		"1.0.0 || 2.0.0",
	} {
		if _, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "npm", PackageName: "example", Version: version,
		}); err == nil {
			t.Errorf("npm compound selector %q was accepted", version)
		}
	}
}

func TestPrepareRuleUsesDialectSpecificTrailingPrefixSeam(t *testing.T) {
	for _, test := range []struct {
		ecosystem string
		selector  string
		want      string
	}{
		{ecosystem: "npm", selector: "@Scope/*", want: "@Scope/*"},
		{ecosystem: "pypi", selector: "Friendly_.*", want: "friendly-*"},
		{ecosystem: "apt", selector: "l*", want: "l*"},
		{ecosystem: "composer", selector: "Vendor/*", want: "vendor/*"},
		{ecosystem: "composer", selector: "Vendor/Pack*", want: "vendor/pack*"},
		{ecosystem: "conda", selector: "Conda-Forge/Num*", want: "Conda-Forge/num*"},
		{ecosystem: "alpine", selector: "v3.21/main/x86_64/py3-*", want: "v3.21/main/x86_64/py3-*"},
	} {
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: test.ecosystem, PackageName: test.selector, Version: "*",
		})
		if err != nil {
			t.Errorf("PrepareRule(%s, %q): %v", test.ecosystem, test.selector, err)
			continue
		}
		if prepared.NormalizedPackageName != test.want {
			t.Errorf("PrepareRule(%s, %q) normalized package = %q, want %q", test.ecosystem, test.selector, prepared.NormalizedPackageName, test.want)
		}
	}
}

func TestPrepareRuleRejectsMalformedStructuredPackageNames(t *testing.T) {
	for _, test := range []packagepolicy.RawRule{
		{Ecosystem: "composer", PackageName: "vendor", Version: "*"},
		{Ecosystem: "conda", PackageName: "channel//name", Version: "*"},
		{Ecosystem: "alpine", PackageName: "v3.21/main/pkg", Version: "*"},
	} {
		if _, err := packagepolicy.PrepareRule(test); !errors.Is(err, packagepolicy.ErrInvalidRule) {
			t.Errorf("PrepareRule(%+v) error = %v, want ErrInvalidRule", test, err)
		}
	}
}

func TestPrepareRuleRestrictsWildcardEcosystemToGlobalRule(t *testing.T) {
	_, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem: "*", PackageName: "requests", Version: "*",
	})
	if !errors.Is(err, packagepolicy.ErrInvalidRule) {
		t.Fatalf("semantic wildcard ecosystem error = %v, want ErrInvalidRule", err)
	}
	if _, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem: "*", PackageName: "*", Version: "*",
	}); err != nil {
		t.Fatalf("global wildcard rule rejected: %v", err)
	}
}

func TestPrepareRuleRejectsDialectWithoutPackageRuleEnforcement(t *testing.T) {
	for _, test := range []packagepolicy.RawRule{
		{Ecosystem: "docker", PackageName: "library/alpine", Version: "latest"},
		{Ecosystem: "rubygems", PackageName: "nokogiri", Version: "*"},
		{Ecosystem: "helm", PackageName: "mychart", Version: "*"},
	} {
		_, err := packagepolicy.PrepareRule(test)
		if !errors.Is(err, packagepolicy.ErrInvalidRule) {
			t.Errorf("%s package rule error = %v, want ErrInvalidRule", test.Ecosystem, err)
		}
	}
}

func TestPrepareRuleNormalizesPrefixWithConcreteDialect(t *testing.T) {
	prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
		Ecosystem: "pypi", PackageName: "Friendly_.*", Version: "*",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.NormalizedPackageName != "friendly-*" {
		t.Fatalf("normalized prefix = %q, want friendly-*", prepared.NormalizedPackageName)
	}
}

func TestPrepareRuleRejectsAmbiguousVersionSyntax(t *testing.T) {
	for _, test := range []packagepolicy.RawRule{
		{Ecosystem: "maven", PackageName: "org.example:demo", Version: ">=1.0 <2.0"},
		{Ecosystem: "go", PackageName: "example.com/module", Version: "1.*"},
		{Ecosystem: "maven", PackageName: "org.example:demo", Version: "1.0 2.0"},
	} {
		if _, err := packagepolicy.PrepareRule(test); !errors.Is(err, packagepolicy.ErrInvalidRule) {
			t.Errorf("PrepareRule(%+v) error = %v, want ErrInvalidRule", test, err)
		}
	}
}
