package pypi

import "strings"

// IndexCacheKey returns the cache key for a package's simple index page.
func IndexCacheKey(prefix, packageName string) string {
	return prefix + "/simple/" + strings.ToLower(packageName) + "/index.html"
}

// FileCacheKey returns the cache key for a package file.
func FileCacheKey(prefix, filepath string) string {
	return prefix + "/files/" + strings.TrimPrefix(filepath, "/")
}
