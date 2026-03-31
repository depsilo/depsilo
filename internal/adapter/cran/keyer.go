package cran

func CacheKey(path string) string {
	return "cran/" + path
}
