package pypi

import "strings"

// IndexCacheKey returns the cache key for a package's simple index page.
func IndexCacheKey(packageName string) string {
	return "pypi/simple/" + strings.ToLower(packageName) + "/index.html"
}

// FileCacheKey returns the cache key for a package file.
func FileCacheKey(filepath string) string {
	return "pypi/files/" + strings.TrimPrefix(filepath, "/")
}
