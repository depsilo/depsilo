package nuget

func CacheKey(path string) string {
	return "nuget/" + path
}
