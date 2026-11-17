package packagekey

import "strings"

// goSumDBKeyPrefix starts every cache key of the Go adapter's checksum
// database relay. It repeats db.GoSumDBKeyPrefix, which this package cannot
// import because internal/db imports this package; a test in internal/adapter
// holds the two together.
const goSumDBKeyPrefix = "go/sumdb/"

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
		// A checksum database lookup names a version, which may end in ".zip"
		// like a module archive; it is verification data, never a package file.
		// The prefix is matched exactly, as db.ClassifyCacheKind matches it.
		return strings.HasSuffix(key, ".zip") && !strings.HasPrefix(key, goSumDBKeyPrefix)
	case "cargo":
		return strings.HasSuffix(key, ".crate")
	case "apt":
		_, ok := trimDebianPackageExtension(key)
		return ok
	case "maven":
		lower := strings.ToLower(key)
		for _, sidecar := range []string{".asc", ".md5", ".sha1", ".sha256", ".sha512"} {
			if strings.HasSuffix(lower, sidecar) {
				return false
			}
		}
		coordinate, version := ParseMavenPath(strings.TrimPrefix(key, "maven/"))
		return coordinate != "" && version != ""
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
	case "alpine":
		return strings.HasSuffix(key, ".apk")
	case "helm":
		return strings.HasSuffix(key, ".tgz")
	case "docker":
		return strings.Contains(key, "/blobs/") || strings.Contains(key, "/manifests/")
	case "huggingface":
		_, _, ok := parseHuggingFaceFileKey(key)
		return ok
	}
	return false
}
