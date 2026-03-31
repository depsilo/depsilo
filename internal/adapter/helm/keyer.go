package helm

func CacheKey(path string) string {
	return "helm/" + path
}
