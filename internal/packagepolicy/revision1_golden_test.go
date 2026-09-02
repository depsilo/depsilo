package packagepolicy

import "testing"

func TestPrepareRuleRevision1GoldenContract(t *testing.T) {
	tests := []struct {
		raw                      RawRule
		wantEcosystem            string
		wantPackage, wantVersion string
		wantNormalizedPackage    string
		wantNormalizedVersion    string
	}{
		{RawRule{"*", "*", "*"}, "*", "*", "*", "*", "*"},
		{RawRule{" PyPI ", " Friendly-._-Bard ", ">=1.0RC1"}, "pypi", "Friendly-._-Bard", ">=1.0RC1", "friendly-bard", ">= 1.0rc1"},
		{RawRule{"npm", "@Scope/Widget", "*"}, "npm", "@Scope/Widget", "*", "@Scope/Widget", "*"},
		{RawRule{"cargo", "serde", ">=1.0.0-alpha"}, "cargo", "serde", ">=1.0.0-alpha", "serde", ">= 1.0.0-alpha"},
		{RawRule{"maven", "Org.Example:Artifact", "1.0-ALPHA"}, "maven", "Org.Example:Artifact", "1.0-ALPHA", "Org.Example:Artifact", "1.0-alpha"},
		{RawRule{"apt", "libc6", "*"}, "apt", "libc6", "*", "libc6", "*"},
		{RawRule{"go", "example.com/Module", "v1.2.3"}, "go", "example.com/Module", "v1.2.3", "example.com/Module", "v1.2.3"},
		{RawRule{"composer", "Vendor/Package", "*"}, "composer", "Vendor/Package", "*", "vendor/package", "*"},
		{RawRule{"nuget", "Newtonsoft.Json", "1.0.0.0+build"}, "nuget", "Newtonsoft.Json", "1.0.0.0+build", "newtonsoft.json", "1.0.0"},
		{RawRule{"conda", "Conda-Forge/NumPy", "1.0"}, "conda", "Conda-Forge/NumPy", "1.0", "Conda-Forge/numpy", "1.0"},
		{RawRule{"cran", "Data.Table", "0.01.0"}, "cran", "Data.Table", "0.01.0", "Data.Table", "0.1"},
		{RawRule{"alpine", "v3.21/main/x86_64/py3-Requests", "1.0-r0"}, "alpine", "v3.21/main/x86_64/py3-Requests", "1.0-r0", "v3.21/main/x86_64/py3-Requests", "1.0-r0"},
	}

	for _, test := range tests {
		prepared, err := PrepareRuleRevision1(test.raw)
		if err != nil {
			t.Fatalf("PrepareRuleRevision1(%+v): %v", test.raw, err)
		}
		if prepared.DialectRevision != DialectRevision1 ||
			prepared.Ecosystem != test.wantEcosystem ||
			prepared.PackageName != test.wantPackage ||
			prepared.Version != test.wantVersion ||
			prepared.NormalizedPackageName != test.wantNormalizedPackage ||
			prepared.NormalizedVersion != test.wantNormalizedVersion {
			t.Fatalf("PrepareRuleRevision1(%+v) = %+v, want ecosystem=%q raw=(%q,%q) normalized=(%q,%q) revision=%d",
				test.raw, prepared, test.wantEcosystem, test.wantPackage, test.wantVersion,
				test.wantNormalizedPackage, test.wantNormalizedVersion, DialectRevision1)
		}
	}
}

func TestVersionCapabilityRevision1GoldenContract(t *testing.T) {
	wants := map[string]PolicyVersionCapability{
		"apt":      PolicyVersionsPackageOnly,
		"composer": PolicyVersionsPackageOnly, "rubygems": PolicyVersionsPackageOnly,
		"cargo": PolicyVersionsRanges, "npm": PolicyVersionsRanges, "pypi": PolicyVersionsRanges,
		"maven": PolicyVersionsExactOnly, "go": PolicyVersionsExactOnly,
		"nuget": PolicyVersionsExactOnly, "conda": PolicyVersionsExactOnly,
		"cran": PolicyVersionsExactOnly, "alpine": PolicyVersionsExactOnly,
		"huggingface": PolicyVersionsExactOnly, "docker": PolicyVersionsExactOnly,
		"helm": PolicyVersionsExactOnly,
	}
	for ecosystem, want := range wants {
		got, err := versionCapabilityForRevision1(ecosystem)
		if err != nil {
			t.Fatalf("versionCapabilityForRevision1(%q): %v", ecosystem, err)
		}
		if got != want {
			t.Fatalf("versionCapabilityForRevision1(%q) = %v, want %v", ecosystem, got, want)
		}
	}
}

func TestRuleEnforcementRevision1GoldenContract(t *testing.T) {
	for _, ecosystem := range []string{"pypi", "apt", "npm", "go", "cargo", "maven", "composer", "nuget", "conda", "cran", "alpine"} {
		if !ruleEnforcementRevision1(ecosystem) {
			t.Errorf("revision 1 unexpectedly disables %q", ecosystem)
		}
	}
	for _, ecosystem := range []string{"rubygems", "helm", "huggingface", "docker", "unknown"} {
		if ruleEnforcementRevision1(ecosystem) {
			t.Errorf("revision 1 unexpectedly enables %q", ecosystem)
		}
	}
}
