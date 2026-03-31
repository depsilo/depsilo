package goproxy

// ListCacheKey for @v/list (short TTL)
func ListCacheKey(module string) string {
	return "go/" + module + "/@v/list"
}

// LatestCacheKey for @latest (short TTL)
func LatestCacheKey(module string) string {
	return "go/" + module + "/@latest"
}

// InfoCacheKey for version .info (long TTL, immutable)
func InfoCacheKey(module, version string) string {
	return "go/" + module + "/@v/" + version + ".info"
}

// ModCacheKey for version .mod (long TTL, immutable)
func ModCacheKey(module, version string) string {
	return "go/" + module + "/@v/" + version + ".mod"
}

// ZipCacheKey for version .zip (long TTL, immutable)
func ZipCacheKey(module, version string) string {
	return "go/" + module + "/@v/" + version + ".zip"
}
