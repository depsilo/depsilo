package security

import "testing"

func TestAutomaticVulnerabilityScanningCapabilities(t *testing.T) {
	tests := []struct {
		ecosystem string
		want      bool
	}{
		{ecosystem: "pypi", want: true},
		{ecosystem: "apt", want: false},
		{ecosystem: "npm", want: true},
		{ecosystem: "go", want: true},
		{ecosystem: "cargo", want: true},
		{ecosystem: "maven", want: true},
		{ecosystem: "composer", want: true},
		{ecosystem: "cran", want: true},
		{ecosystem: "nuget", want: false},
		{ecosystem: "rubygems", want: false},
		{ecosystem: "conda", want: false},
		{ecosystem: "alpine", want: false},
		{ecosystem: "helm", want: false},
		{ecosystem: "docker", want: false},
		{ecosystem: "huggingface", want: false},
		{ecosystem: "unknown", want: false},
	}
	for _, test := range tests {
		t.Run(test.ecosystem, func(t *testing.T) {
			if got := SupportsAutomaticVulnerabilityScanning(test.ecosystem); got != test.want {
				t.Fatalf("SupportsAutomaticVulnerabilityScanning(%q) = %v, want %v", test.ecosystem, got, test.want)
			}
		})
	}
}
