package security

import (
	"strings"

	"depsilo/internal/packagepolicy"
)

// SupportsAutomaticVersionBlocking reports whether scanner-created OSV
// affected sets have a reviewed, lossless projection into Package Rules.
// It is intentionally closed for every ecosystem: OSV intervals include
// prereleases by version ordering and may contain explicit or disjoint affected
// versions, while Operator-authored comparator semantics can exclude
// unmentioned prereleases and cannot represent the complete OSV range model.
func SupportsAutomaticVersionBlocking(ecosystem string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(ecosystem))
	_, err := packagepolicy.VersionCapabilityFor(normalized)
	if err != nil {
		return false, err
	}
	return false, nil
}

// SupportsAutomaticVulnerabilityScanning reports whether an adapter-derived
// cache identity is suitable for an OSV query. APT artifact filenames carry a
// binary package name while Debian OSV records are keyed by source package.
// NuGet flat-container keys are required to be lowercase and therefore discard
// the registry's canonical ID casing. RubyGems platform artifact filenames do
// not expose a reversible name/version/platform boundary. Until registry
// metadata authenticates those identities into cache rows, recording any of
// these derived queries as clean would be a security false negative.
func SupportsAutomaticVulnerabilityScanning(ecosystem string) bool {
	normalized := strings.ToLower(strings.TrimSpace(ecosystem))
	return normalized != "apt" && normalized != "nuget" && normalized != "rubygems" &&
		OSVEcosystem(normalized) != ""
}
