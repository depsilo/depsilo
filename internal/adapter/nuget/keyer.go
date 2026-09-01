package nuget

import (
	"crypto/sha256"
	"encoding/hex"
	pathpkg "path"
	"strings"
)

func CacheKey(path string) string {
	return "nuget/" + path
}

func queryCacheKey(resourcePath, rawQuery string) string {
	key := CacheKey(resourcePath)
	if rawQuery == "" {
		return key
	}
	digest := sha256.Sum256([]byte(rawQuery))
	marker := ".__query__." + hex.EncodeToString(digest[:])
	extension := pathpkg.Ext(key)
	// Preserve the terminal extension so cache-kind and package identity
	// classification continue to recognize .nupkg artifacts.
	if extension != "" {
		return strings.TrimSuffix(key, extension) + marker + extension
	}
	return key + marker
}
