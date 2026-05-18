package packagekey

import "strings"

// IsPackageFile reports whether a cache key represents an actual package
// file download (as opposed to a metadata / index request). Only package
// files should be recorded for SBOM and security scanning.
func IsPackageFile(adapterType, key string) bool {
	switch adapterType {
	case "pypi":
		return strings.HasPrefix(key, "pypi/files/")
	case "npm":
		return strings.HasSuffix(key, ".tgz")
	case "go":
		return strings.HasSuffix(key, ".zip")
	case "cargo":
		return strings.HasSuffix(key, ".crate")
	case "apt":
		return strings.HasSuffix(key, ".deb")
	case "maven":
		return strings.HasSuffix(key, ".jar") || strings.HasSuffix(key, ".aar") || strings.HasSuffix(key, ".pom")
	case "rubygems":
		return strings.HasSuffix(key, ".gem")
	case "composer":
		return strings.HasSuffix(key, ".zip")
	case "nuget":
		return strings.HasSuffix(key, ".nupkg")
	case "conda":
		return strings.HasSuffix(key, ".tar.bz2") || strings.HasSuffix(key, ".conda")
	case "cran":
		return strings.HasSuffix(key, ".tar.gz") || strings.HasSuffix(key, ".zip") || strings.HasSuffix(key, ".tgz")
	case "helm":
		return strings.HasSuffix(key, ".tgz")
	case "docker":
		return strings.Contains(key, "/blobs/") || strings.Contains(key, "/manifests/")
	}
	return false
}
