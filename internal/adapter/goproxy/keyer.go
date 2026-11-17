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

// Checksum database keys share the "go/sumdb/" prefix (db.GoSumDBKeyPrefix),
// which the cache classifies as metadata. The handler never reads a request
// whose first path element is "sumdb" as a module request, so no module key
// can start with it.

// SumDBSupportedCacheKey for sumdb/<name>/supported (short TTL)
func SumDBSupportedCacheKey(name string) string {
	return "go/sumdb/" + name + "/supported"
}

// SumDBLatestCacheKey for sumdb/<name>/latest, the newest signed tree head
// (short TTL)
func SumDBLatestCacheKey(name string) string {
	return "go/sumdb/" + name + "/latest"
}

// SumDBLookupCacheKey for sumdb/<name>/lookup/<module>@<version> (long TTL:
// the record is immutable and a copy with an older tree head stays verifiable)
func SumDBLookupCacheKey(name, moduleVersion string) string {
	return "go/sumdb/" + name + "/lookup/" + moduleVersion
}

// SumDBTileCacheKey for sumdb/<name>/tile/... (long TTL, immutable)
func SumDBTileCacheKey(name, tile string) string {
	return "go/sumdb/" + name + "/tile/" + tile
}
