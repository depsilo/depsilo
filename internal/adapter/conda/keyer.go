package conda

func CacheKey(path string) string {
	return "conda/" + path
}
