package rubygems

func CacheKey(path string) string {
	return "rubygems/" + path
}
