package composer

func PackagesCacheKey() string {
	return "composer/packages.json"
}

func MetadataCacheKey(vendor, pkg string) string {
	return "composer/p2/" + vendor + "/" + pkg + ".json"
}

func DevMetadataCacheKey(vendor, pkg string) string {
	return "composer/p2/" + vendor + "/" + pkg + "~dev.json"
}

func DistCacheKey(vendor, pkg, reference string) string {
	return "composer/dist/" + vendor + "/" + pkg + "/" + reference + ".zip"
}
