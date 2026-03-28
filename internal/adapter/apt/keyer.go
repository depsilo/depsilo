package apt

import "strings"

// CacheKey returns the cache key for an APT repository path.
func CacheKey(repo, filepath string) string {
	return "apt/" + repo + "/" + strings.TrimPrefix(filepath, "/")
}

// TTLClass determines whether a path is metadata (short TTL) or blob (long TTL).
// Returns true for metadata files.
func IsMetadata(filepath string) bool {
	lower := strings.ToLower(filepath)
	for _, suffix := range []string{
		"inrelease",
		"release",
		"release.gpg",
		"packages",
		"packages.gz",
		"packages.xz",
		"packages.bz2",
		"sources",
		"sources.gz",
		"sources.xz",
	} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
