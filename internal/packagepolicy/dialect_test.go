package packagepolicy_test

import (
	"strings"
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestNPMSemVerPrereleasePrecedesRelease(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("npm")
	if err != nil {
		t.Fatalf("DialectFor(npm): %v", err)
	}

	comparison, err := dialect.CompareVersions("1.0.0-alpha", "1.0.0")
	if err != nil {
		t.Fatalf("CompareVersions: %v", err)
	}
	if comparison >= 0 {
		t.Fatalf("CompareVersions(1.0.0-alpha, 1.0.0) = %d, want < 0", comparison)
	}
}

func TestSemVerDialectsUseSemVerPrecedence(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.0-alpha", right: "1.0.0", want: -1},
		{left: "1.0.0-rc.1", right: "1.0.0", want: -1},
		{left: "1.0.0+build.1", right: "1.0.0", want: 0},
	}

	for _, ecosystem := range []string{"npm", "cargo"} {
		t.Run(ecosystem, func(t *testing.T) {
			dialect, err := packagepolicy.DialectFor(ecosystem)
			if err != nil {
				t.Fatalf("DialectFor(%s): %v", ecosystem, err)
			}
			for _, test := range tests {
				got, err := dialect.CompareVersions(test.left, test.right)
				if err != nil {
					t.Fatalf("CompareVersions(%q, %q): %v", test.left, test.right, err)
				}
				if sign(got) != test.want {
					t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", test.left, test.right, got, test.want)
				}
			}
		})
	}
}

func TestSemVerNumericPrereleaseComparisonDoesNotOverflow(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo"} {
		dialect, err := packagepolicy.DialectFor(ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		got, err := dialect.CompareVersions(
			"1.0.0-10000000000000000000",
			"1.0.0-9999999999999999999",
		)
		if err != nil {
			t.Fatalf("%s CompareVersions: %v", ecosystem, err)
		}
		if got <= 0 {
			t.Errorf("%s large numeric prerelease comparison = %d, want > 0", ecosystem, got)
		}
	}
}

func TestSemVerCoreBoundsFollowEcosystemImplementations(t *testing.T) {
	tests := []struct {
		ecosystem string
		valid     string
		invalid   string
	}{
		{ecosystem: "npm", valid: "9007199254740991.0.0", invalid: "9007199254740992.0.0"},
		{ecosystem: "cargo", valid: "18446744073709551615.0.0", invalid: "18446744073709551616.0.0"},
	}
	for _, test := range tests {
		dialect, err := packagepolicy.DialectFor(test.ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		if err := dialect.ValidateVersion(test.valid); err != nil {
			t.Errorf("%s rejected boundary %q: %v", test.ecosystem, test.valid, err)
		}
		if err := dialect.ValidateVersion(test.invalid); err == nil {
			t.Errorf("%s accepted out-of-range core %q", test.ecosystem, test.invalid)
		}
	}
}

func TestPyPIDialectUsesPEP440Precedence(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		t.Fatalf("DialectFor(pypi): %v", err)
	}

	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.dev1", right: "1.0a1", want: -1},
		{left: "1.0rc1", right: "1.0", want: -1},
		{left: "1.0.post1", right: "1.0", want: 1},
		{left: "1!1.0", right: "2026.1", want: 1},
	}

	for _, test := range tests {
		got, err := dialect.CompareVersions(test.left, test.right)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", test.left, test.right, err)
		}
		if sign(got) != test.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", test.left, test.right, got, test.want)
		}
	}
}

func TestMavenDialectUsesComparableVersionPrecedence(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatalf("DialectFor(maven): %v", err)
	}

	for _, prerelease := range []string{"1.2-SNAPSHOT", "1.0-alpha", "1.0-rc1"} {
		release := "1.0"
		if prerelease == "1.2-SNAPSHOT" {
			release = "1.2"
		}
		got, err := dialect.CompareVersions(prerelease, release)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", prerelease, release, err)
		}
		if got >= 0 {
			t.Errorf("CompareVersions(%q, %q) = %d, want < 0", prerelease, release, got)
		}
	}
}

func TestMavenDialectComparableVersionAliasesAndQualifierOrder(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatal(err)
	}
	for _, equivalent := range []string{"1.0", "1.0.0", "1-ga", "1-final", "1-release"} {
		got, err := dialect.CompareVersions("1", equivalent)
		if err != nil {
			t.Fatal(err)
		}
		if got != 0 {
			t.Errorf("ComparableVersion alias %q compared to 1 = %d, want 0", equivalent, got)
		}
	}
	ordered := []string{"1-alpha", "1-beta", "1-milestone", "1-rc", "1-snapshot", "1", "1-sp"}
	for index := 0; index < len(ordered)-1; index++ {
		got, err := dialect.CompareVersions(ordered[index], ordered[index+1])
		if err != nil {
			t.Fatal(err)
		}
		if got >= 0 {
			t.Errorf("ComparableVersion order %q < %q failed: %d", ordered[index], ordered[index+1], got)
		}
	}
	got, err := dialect.CompareVersions("1.999999999999999999999999", "1.1000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got >= 0 {
		t.Errorf("arbitrary precision Maven comparison = %d, want < 0", got)
	}
}

func TestMavenDialectDoesNotAdvertiseUnsafeRanges(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatal(err)
	}
	if dialect.SupportsRanges() {
		t.Fatal("Maven advertised ranges despite ComparableVersion's non-transitive domain")
	}
	if err := dialect.ValidateVersion("1.0-redhat"); err != nil {
		t.Fatalf("exact Maven version was rejected: %v", err)
	}
}

func TestMavenDialectKnownNonTransitiveTripleRequiresExactOnlyPolicy(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := "1-0.A0", "1-0", "1-sp"
	ab, err := dialect.CompareVersions(a, b)
	if err != nil {
		t.Fatal(err)
	}
	bc, err := dialect.CompareVersions(b, c)
	if err != nil {
		t.Fatal(err)
	}
	ac, err := dialect.CompareVersions(a, c)
	if err != nil {
		t.Fatal(err)
	}
	if ab >= 0 || bc >= 0 || ac <= 0 {
		t.Fatalf("known ComparableVersion triple comparisons = (%d, %d, %d), want a < b < c but a > c", ab, bc, ac)
	}
	if dialect.SupportsRanges() {
		t.Fatal("non-transitive Maven comparator cannot support ordered policy ranges")
	}
}

func TestMavenDialectUsesJavaEnglishFullCaseMapping(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := dialect.CompareVersions("1-İ", "1-i")
	if err != nil {
		t.Fatal(err)
	}
	if comparison <= 0 {
		t.Fatalf("ComparableVersion(1-İ, 1-i) = %d, want Java's i+combining-dot qualifier to sort after i", comparison)
	}
}

func TestAPTDialectUsesDebianVersionPrecedence(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatalf("DialectFor(apt): %v", err)
	}

	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0~beta1", right: "1.0", want: -1},
		{left: "1:1.0-1", right: "2.0-1", want: 1},
		{left: "1.0-2", right: "1.0-1", want: 1},
	}

	for _, test := range tests {
		got, err := dialect.CompareVersions(test.left, test.right)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", test.left, test.right, err)
		}
		if sign(got) != test.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", test.left, test.right, got, test.want)
		}
	}
}

func TestFallbackDialectsAreExactOnly(t *testing.T) {
	for _, ecosystem := range []string{"go", "rubygems", "composer", "conda", "alpine", "helm", "docker", "huggingface"} {
		t.Run(ecosystem, func(t *testing.T) {
			dialect, err := packagepolicy.DialectFor(ecosystem)
			if err != nil {
				t.Fatalf("DialectFor(%s): %v", ecosystem, err)
			}
			if dialect.SupportsRanges() {
				t.Fatalf("SupportsRanges() = true, want false")
			}
			comparison, err := dialect.CompareVersions("1.0-RC1", "1.0-rc1")
			if err != nil {
				t.Fatalf("CompareVersions: %v", err)
			}
			if comparison == 0 {
				t.Fatal("exact-only comparison folded distinct version strings")
			}
		})
	}
}

func TestPackageNamesUseEcosystemSpecificNormalization(t *testing.T) {
	tests := []struct {
		ecosystem string
		name      string
		want      string
	}{
		{ecosystem: "pypi", name: "Friendly-._-Bard", want: "friendly-bard"},
		{ecosystem: "npm", name: "@Scope/Package", want: "@Scope/Package"},
		{ecosystem: "cargo", name: "My_Crate", want: "My_Crate"},
		{ecosystem: "nuget", name: "NuGet.Core", want: "nuget.core"},
		{ecosystem: "maven", name: "Org.Example:Artifact", want: "Org.Example:Artifact"},
		{ecosystem: "go", name: "example.com/Module", want: "example.com/Module"},
		{ecosystem: "composer", name: "Acme/HTTP_Client", want: "acme/http_client"},
		{ecosystem: "conda", name: "Conda-Forge/NumPy", want: "Conda-Forge/numpy"},
		{ecosystem: "conda", name: "Pkgs/Main/NumPy", want: "Pkgs/Main/numpy"},
		{ecosystem: "alpine", name: "v3.21/main/x86_64/py3-Requests", want: "v3.21/main/x86_64/py3-Requests"},
		{ecosystem: "huggingface", name: "datasets/Acme/My_Data", want: "datasets/acme/my_data"},
		{ecosystem: "docker", name: "library/alpine", want: "library/alpine"},
	}

	for _, test := range tests {
		t.Run(test.ecosystem, func(t *testing.T) {
			dialect, err := packagepolicy.DialectFor(test.ecosystem)
			if err != nil {
				t.Fatal(err)
			}
			got, err := dialect.NormalizePackageName(test.name)
			if err != nil {
				t.Fatalf("NormalizePackageName(%q): %v", test.name, err)
			}
			if got != test.want {
				t.Fatalf("NormalizePackageName(%q) = %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestGoPackageNamesUseOfficialModulePathIdentity(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("go")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"github.com/Azure/azure-sdk-for-go",
		"example.com/module/v2",
		"gopkg.in/yaml.v3",
	} {
		normalized, err := dialect.NormalizePackageName(name)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", name, err)
		} else if normalized != name {
			t.Errorf("NormalizePackageName(%q) = %q, want case-preserving identity", name, normalized)
		}
	}

	for _, name := range []string{
		"github/Azure/sdk",
		"GitHub.com/Azure/sdk",
		"github.com/!azure/sdk",
		"github.com/Azure//sdk",
		"github.com/Azure/sdk.",
		"example.com/module/v1",
		"example.com/module/v01",
	} {
		if _, err := dialect.NormalizePackageName(name); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted an invalid Go module path", name)
		}
	}
}

func TestGoPackagePrefixAndGlobUseModulePathGrammar(t *testing.T) {
	for _, selector := range []string{
		"github.com/Az*",
		"example.com/module/v2*",
		"example.com/module/v2/*",
	} {
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "go", PackageName: selector, Version: "*",
		})
		if err != nil {
			t.Errorf("PrepareRule(%q): %v", selector, err)
		} else if prepared.NormalizedPackageName != selector {
			t.Errorf("PrepareRule(%q) normalized package = %q, want case-preserving identity", selector, prepared.NormalizedPackageName)
		}
	}
	for _, selector := range []string{
		"github*",
		"GitHub.com/Az*",
		"github.com//Az*",
		"github.com/!azure*",
	} {
		if _, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "go", PackageName: selector, Version: "*",
		}); err == nil {
			t.Errorf("PrepareRule(%q) accepted an invalid Go module prefix", selector)
		}
	}

	dialect, err := packagepolicy.DialectFor("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{
		"github.com/[Aa]zure/*",
		"example.com/module/v[2-9]",
		"example.com/*/v2",
	} {
		normalized, err := packagepolicy.NormalizePackageGlob(dialect, pattern)
		if err != nil {
			t.Errorf("NormalizePackageGlob(%q): %v", pattern, err)
		} else if normalized != pattern {
			t.Errorf("NormalizePackageGlob(%q) = %q, want case-preserving identity", pattern, normalized)
		}
	}
	for _, pattern := range []string{
		"github/*",
		"GitHub.com/*",
		"github.com//**",
		"github.com/!azure/*",
		"github.com/[abc/*",
		"example.com/module/v[01]",
	} {
		if _, err := packagepolicy.NormalizePackageGlob(dialect, pattern); err == nil {
			t.Errorf("NormalizePackageGlob(%q) accepted an invalid Go module glob", pattern)
		}
	}
}

func TestNPMPreservesLegacyCaseSensitivePackageIdentity(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("npm")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"Express",
		"JSONStream",
		"legacy!pkg",
		"legacy~pkg",
		"legacy(pkg)",
		"legacy'pkg",
		"legacy*pkg",
		"@Scope/Package",
		"@scope/_pkg",
		"@scope/-pkg",
		"@scope/node_modules",
		strings.Repeat("a", 214),
	} {
		normalized, err := dialect.NormalizePackageName(name)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", name, err)
		} else if normalized != name {
			t.Errorf("NormalizePackageName(%q) = %q, want case-preserving identity", name, normalized)
		}
	}
	for _, pair := range [][2]string{{"Express", "express"}, {"JSONStream", "jsonstream"}} {
		left, err := dialect.NormalizePackageName(pair[0])
		if err != nil {
			t.Fatalf("NormalizePackageName(%q): %v", pair[0], err)
		}
		right, err := dialect.NormalizePackageName(pair[1])
		if err != nil {
			t.Fatalf("NormalizePackageName(%q): %v", pair[1], err)
		}
		if left == right {
			t.Errorf("legacy npm identities %q and %q collapsed to %q", pair[0], pair[1], left)
		}
	}
	for _, invalid := range []string{
		"@scope",
		"@scope/",
		"@scope/name/extra",
		"@scope/.hidden",
		".hidden",
		"_private",
		"-foo",
		"node_modules",
		"Node_Modules",
		"favicon.ico",
		"FAVICON.ICO",
		"name:tag",
		"näme",
		strings.Repeat("a", 215),
	} {
		if _, err := dialect.NormalizePackageName(invalid); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted a malformed npm package name", invalid)
		}
	}
}

func TestNPMPrefixAndGlobSeamsUseLegacyConsumerGrammar(t *testing.T) {
	for _, selector := range []string{
		"legacy!*",
		"node_modules*",
		"@scope/*",
		"@scope/_*",
		"@scope/-*",
	} {
		prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "npm", PackageName: selector, Version: "*",
		})
		if err != nil {
			t.Errorf("PrepareRule(%q): %v", selector, err)
		} else if prepared.NormalizedPackageName != selector {
			t.Errorf("PrepareRule(%q) normalized package = %q, want identity", selector, prepared.NormalizedPackageName)
		}
	}
	for _, selector := range []string{".hidden*", "_private*", "-foo*", "@scope/.*"} {
		if _, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
			Ecosystem: "npm", PackageName: selector, Version: "*",
		}); err == nil {
			t.Errorf("PrepareRule(%q) accepted a malformed npm prefix", selector)
		}
	}

	dialect, err := packagepolicy.DialectFor("npm")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{
		"legacy!pkg*",
		"node_modules*",
		"@scope/*",
		"@scope/_pkg*",
		"@scope/-pkg?",
	} {
		normalized, err := packagepolicy.NormalizePackageGlob(dialect, pattern)
		if err != nil {
			t.Errorf("NormalizePackageGlob(%q): %v", pattern, err)
		} else if normalized != pattern {
			t.Errorf("NormalizePackageGlob(%q) = %q, want identity", pattern, normalized)
		}
	}
	for _, pattern := range []string{
		"node_modules",
		"FAVICON.ICO",
		".hidden*",
		"_private*",
		"-foo*",
		"@scope/.hidden*",
		"@scope/name/extra*",
	} {
		if _, err := packagepolicy.NormalizePackageGlob(dialect, pattern); err == nil {
			t.Errorf("NormalizePackageGlob(%q) accepted a malformed npm pattern", pattern)
		}
	}
}

func TestStructuredExactOnlyDialectsRejectMalformedConcreteNames(t *testing.T) {
	tests := map[string][]string{
		"composer": {
			"vendor", "/package", "vendor/", "vendor/package/extra",
			"vendör/package", "vendor/pack age", "vendor/...", "CON/package",
			"vendor/Aux.txt", "vendor/package.json",
		},
		"conda": {
			"numpy", "/numpy", "channel/", "channel//numpy",
			"channel/num:py", "chännel/numpy",
		},
		"alpine": {
			"py3-requests", "v3.21/main/x86_64", "v3.21/main/x86_64/pkg/extra",
			"v3.21//x86_64/pkg", "v3.21/main/x86_64/pkg:debug",
		},
		"huggingface": {
			"owner/", "/repo", "owner/repo/extra",
			"spaces/owner/repo", "owner/-repo", "owner/repo..name", "ownér/repo",
			"owner/repo.git", "owner/" + strings.Repeat("a", 91),
		},
		"docker": {
			"Library/alpine", "library/Alpine", "/alpine", "library/",
			"library//alpine", "library/alpine:latest", "library/.alpine", "library/alpine_",
		},
	}

	for ecosystem, names := range tests {
		dialect, err := packagepolicy.DialectFor(ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if _, err := dialect.NormalizePackageName(name); err == nil {
				t.Errorf("%s NormalizePackageName(%q) succeeded, want an error", ecosystem, name)
			}
		}
	}
}

func TestStructuredExactOnlyDialectIdentityCaseRules(t *testing.T) {
	for _, test := range []struct {
		ecosystem string
		left      string
		right     string
		wantEqual bool
	}{
		{ecosystem: "composer", left: "Acme/Package", right: "acme/package", wantEqual: true},
		{ecosystem: "conda", left: "Conda-Forge/NumPy", right: "Conda-Forge/numpy", wantEqual: true},
		{ecosystem: "conda", left: "Conda-Forge/numpy", right: "conda-forge/numpy", wantEqual: false},
		{ecosystem: "alpine", left: "v3.21/main/x86_64/Package", right: "v3.21/main/x86_64/package", wantEqual: false},
		{ecosystem: "huggingface", left: "Org/Model", right: "org/model", wantEqual: true},
	} {
		dialect, err := packagepolicy.DialectFor(test.ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		left, err := dialect.NormalizePackageName(test.left)
		if err != nil {
			t.Fatalf("NormalizePackageName(%q): %v", test.left, err)
		}
		right, err := dialect.NormalizePackageName(test.right)
		if err != nil {
			t.Fatalf("NormalizePackageName(%q): %v", test.right, err)
		}
		if got := left == right; got != test.wantEqual {
			t.Errorf("%s normalized equality for %q and %q = %v, want %v", test.ecosystem, test.left, test.right, got, test.wantEqual)
		}
	}
}

func TestDockerAcceptsDistributionRemoteNameGrammarWithoutCaseFolding(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("docker")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpine", "library/alpine", "team/image_name", "team/image__name", "team/image---name"} {
		got, err := dialect.NormalizePackageName(name)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", name, err)
		} else if got != name {
			t.Errorf("NormalizePackageName(%q) = %q, want identity", name, got)
		}
	}
}

func TestPackageGlobSeamPreservesValidMetacharacters(t *testing.T) {
	for _, test := range []struct {
		ecosystem string
		pattern   string
		want      string
	}{
		{ecosystem: "npm", pattern: "@Scope/[Pp]ackage?", want: "@Scope/[Pp]ackage?"},
		{ecosystem: "pypi", pattern: "Friendly_[A-Z]?*", want: "friendly-[a-z]?*"},
		{ecosystem: "cargo", pattern: "My_[Cc]rate?", want: "My_[Cc]rate?"},
		{ecosystem: "composer", pattern: "Vendor/[Pp]ackage*", want: "vendor/[pp]ackage*"},
		{ecosystem: "conda", pattern: "Conda-Forge/[Nn]um*", want: "Conda-Forge/[nn]um*"},
		{ecosystem: "conda", pattern: "Pkgs/Main/[Nn]um*", want: "Pkgs/Main/[nn]um*"},
		{ecosystem: "alpine", pattern: "v3.21/main/x86_64/py3-[Rr]equests", want: "v3.21/main/x86_64/py3-[Rr]equests"},
		{ecosystem: "huggingface", pattern: "datasets/Org/[Dd]ata?", want: "datasets/org/[dd]ata?"},
		{ecosystem: "docker", pattern: "library/[a-z]*", want: "library/[a-z]*"},
	} {
		dialect, err := packagepolicy.DialectFor(test.ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		got, err := packagepolicy.NormalizePackageGlob(dialect, test.pattern)
		if err != nil {
			t.Errorf("NormalizePackageGlob(%s, %q): %v", test.ecosystem, test.pattern, err)
			continue
		}
		if got != test.want {
			t.Errorf("NormalizePackageGlob(%s, %q) = %q, want %q", test.ecosystem, test.pattern, got, test.want)
		}
	}
}

func TestCargoPackageNamesUseCargoGrammarWithoutFoldingCase(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("cargo")
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{"My_Crate", "crate-name", "版本2", "1crate", "-crate", "_crate"} {
		got, err := dialect.NormalizePackageName(valid)
		if err != nil {
			t.Errorf("NormalizePackageName(%q): %v", valid, err)
		} else if got != valid {
			t.Errorf("NormalizePackageName(%q) = %q, want case-preserving identity", valid, got)
		}
	}
	for _, invalid := range []string{
		"foo/bar", ".", "crate🙂", "a\u0301", "\u0301crate",
	} {
		if _, err := dialect.NormalizePackageName(invalid); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted a non-Cargo package name", invalid)
		}
	}
}

func TestCargoPackageGlobsUseCargoManifestGrammar(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("cargo")
	if err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{"1crate*", "-crate*", "_crate?", "[A-Z]rate", "\\-*"} {
		got, err := packagepolicy.NormalizePackageGlob(dialect, valid)
		if err != nil {
			t.Errorf("NormalizePackageGlob(%q): %v", valid, err)
		} else if got != valid {
			t.Errorf("NormalizePackageGlob(%q) = %q, want identity", valid, got)
		}
	}
	for _, invalid := range []string{"a\u0301*", "\u0301crate*", "crate🙂*", "crate/name*"} {
		if _, err := packagepolicy.NormalizePackageGlob(dialect, invalid); err == nil {
			t.Errorf("NormalizePackageGlob(%q) accepted a non-Cargo package pattern", invalid)
		}
	}
}

func TestPyPIDialectHandlesLocalVersionsAndUnboundedIntegers(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0+ABC_001", right: "1.0+abc.1", want: 0},
		{left: "1.0+1", right: "1.0+abc", want: 1},
		{left: "256!1.0", right: "255!999999999999999999999999", want: 1},
		{left: "1.999999999999999999999999", right: "1.1000000000000000000000000", want: -1},
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

func TestPyPIDialectMatchesPEP440CanonicalOrder(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{
		"1.0.dev456",
		"1.0a1",
		"1.0a2.dev456",
		"1.0a12.dev456",
		"1.0a12",
		"1.0b1.dev456",
		"1.0b2",
		"1.0b2.post345.dev456",
		"1.0b2.post345",
		"1.0rc1.dev456",
		"1.0rc1",
		"1.0",
		"1.0+abc.5",
		"1.0+abc.7",
		"1.0+5",
		"1.0.post456.dev34",
		"1.0.post456",
		"1.0.15",
		"1.1.dev1",
	}
	for index := 0; index < len(ordered)-1; index++ {
		comparison, err := dialect.CompareVersions(ordered[index], ordered[index+1])
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", ordered[index], ordered[index+1], err)
		}
		if comparison >= 0 {
			t.Errorf("PEP 440 order %q < %q failed: %d", ordered[index], ordered[index+1], comparison)
		}
	}
}

func TestSemVerDialectsRejectAbbreviatedVersions(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo"} {
		dialect, err := packagepolicy.DialectFor(ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		if err := dialect.ValidateVersion("1.0"); err == nil {
			t.Errorf("%s accepted abbreviated SemVer 1.0", ecosystem)
		}
		if err := dialect.ValidateVersion("1.0.0-rc.1+build.5"); err != nil {
			t.Errorf("%s rejected valid SemVer: %v", ecosystem, err)
		}
		for _, valid := range []string{"1.2.3--", "1.2.3+-", "1.2.3--+-", "1.2.3+build.01"} {
			if err := dialect.ValidateVersion(valid); err != nil {
				t.Errorf("%s rejected valid SemVer %q: %v", ecosystem, valid, err)
			}
		}
	}
}

func TestRequestPackageNormalizationRejectsSurroundingWhitespace(t *testing.T) {
	for _, ecosystem := range []string{"npm", "cargo", "pypi", "maven", "apt", "go"} {
		dialect, err := packagepolicy.DialectFor(ecosystem)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dialect.NormalizePackageName(" package "); err == nil {
			t.Errorf("%s normalized a package name with surrounding whitespace", ecosystem)
		}
	}
}

func sign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
