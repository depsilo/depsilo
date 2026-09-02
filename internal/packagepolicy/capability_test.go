package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestVersionCapabilitySeparatesComparatorFromRequestPath(t *testing.T) {
	tests := []struct {
		ecosystem string
		want      packagepolicy.PolicyVersionCapability
	}{
		{ecosystem: "npm", want: packagepolicy.PolicyVersionsRanges},
		{ecosystem: "cargo", want: packagepolicy.PolicyVersionsRanges},
		{ecosystem: "pypi", want: packagepolicy.PolicyVersionsRanges},
		{ecosystem: "apt", want: packagepolicy.PolicyVersionsPackageOnly},
		{ecosystem: "rubygems", want: packagepolicy.PolicyVersionsPackageOnly},
		{ecosystem: "composer", want: packagepolicy.PolicyVersionsPackageOnly},
		{ecosystem: "maven", want: packagepolicy.PolicyVersionsExactOnly},
		{ecosystem: "go", want: packagepolicy.PolicyVersionsExactOnly},
	}
	for _, test := range tests {
		got, err := packagepolicy.VersionCapabilityFor(test.ecosystem)
		if err != nil {
			t.Fatalf("VersionCapabilityFor(%q): %v", test.ecosystem, err)
		}
		if got != test.want {
			t.Errorf("VersionCapabilityFor(%q) = %v, want %v", test.ecosystem, got, test.want)
		}
	}

	apt, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatal(err)
	}
	if !apt.SupportsRanges() {
		t.Fatal("APT lost its Debian comparison capability while request-path selectors were disabled")
	}
}
