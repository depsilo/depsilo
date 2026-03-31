package cargo

func ConfigCacheKey() string {
	return "cargo/config.json"
}

func IndexCacheKey(prefix, crateName string) string {
	return "cargo/index/" + prefix + "/" + crateName
}

func CrateCacheKey(crateName, version string) string {
	return "cargo/crates/" + crateName + "/" + version + ".crate"
}
