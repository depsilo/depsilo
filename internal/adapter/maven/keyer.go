package maven

func CacheKey(path string) string {
	return "maven/" + path
}
