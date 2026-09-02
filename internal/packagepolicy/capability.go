package packagepolicy

import "strings"

// PolicyVersionCapability describes which version selectors can be enforced
// on real proxy requests for an ecosystem. It is deliberately separate from
// a dialect's ability to compare two complete versions: APT has a correct
// Debian comparator, but .deb request paths omit the epoch. npm's authenticated
// packument route carries the exact versions-map key into its Adapter, while
// Composer dist URLs carry version_normalized rather than the resolved pretty
// version.
type PolicyVersionCapability uint8

const (
	PolicyVersionsPackageOnly PolicyVersionCapability = iota
	PolicyVersionsExactOnly
	PolicyVersionsRanges
)

// VersionCapabilityFor returns the request-path enforcement capability for a
// concrete ecosystem.
func VersionCapabilityFor(ecosystem string) (PolicyVersionCapability, error) {
	return versionCapabilityForRevision1(ecosystem)
}

// versionCapabilityForRevision1 is the request-provenance capability snapshot
// persisted by schema v3. It is intentionally explicit instead of deriving
// from a future dialect's SupportsRanges result.
func versionCapabilityForRevision1(ecosystem string) (PolicyVersionCapability, error) {
	normalized := strings.ToLower(strings.TrimSpace(ecosystem))
	switch normalized {
	case "apt", "composer", "rubygems":
		return PolicyVersionsPackageOnly, nil
	case "cargo", "npm", "pypi":
		return PolicyVersionsRanges, nil
	case "maven", "go", "nuget", "conda", "cran", "alpine", "huggingface", "docker", "helm":
		return PolicyVersionsExactOnly, nil
	default:
		_, err := dialectForRevision1(normalized)
		return PolicyVersionsPackageOnly, err
	}
}
