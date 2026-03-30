package npm

import "strings"

func MetadataCacheKey(packageName string) string {
	return "npm/" + strings.ToLower(packageName) + "/metadata.json"
}

func ScopedMetadataCacheKey(scope, packageName string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/metadata.json"
}

func TarballCacheKey(packageName, filename string) string {
	return "npm/" + strings.ToLower(packageName) + "/-/" + filename
}

func ScopedTarballCacheKey(scope, packageName, filename string) string {
	return "npm/@" + strings.ToLower(scope) + "/" + strings.ToLower(packageName) + "/-/" + filename
}
