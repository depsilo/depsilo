package alpine

import "strings"

// CacheKey returns the cache key for an Alpine Linux repository path.
// The path mirrors the upstream layout, e.g.
//   v3.19/main/x86_64/APKINDEX.tar.gz
//   v3.19/main/x86_64/bash-5.2.21-r0.apk
func CacheKey(path string) string {
	return "alpine/" + strings.TrimPrefix(path, "/")
}

// IsMetadata reports whether a path is an index file (short TTL) rather than an
// immutable package archive (long TTL). APKINDEX.tar.gz is the signed package
// index; everything else of interest is a *.apk blob.
func IsMetadata(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "apkindex.tar.gz") ||
		strings.HasSuffix(lower, "/apkindex") ||
		strings.HasSuffix(lower, ".txt")
}
