package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// IndexCacheKey returns the cache key for a package's simple index page.
func IndexCacheKey(prefix, packageName string) string {
	return prefix + "/simple/" + strings.ToLower(packageName) + "/index.html"
}

// signedIndexCacheKey versions cached index HTML by both token format and
// signing key. Cached pages contain signed artifact references, so neither an
// old token format nor references signed by a rotated key may be reused.
func signedIndexCacheKey(prefix, packageName string, signingKey []byte) string {
	digest := sha256.Sum256(signingKey)
	return prefix + "/simple/" + strings.ToLower(packageName) +
		"/_signed/" + externalArtifactTokenVersion + "/" + hex.EncodeToString(digest[:]) + "/index.html"
}

// IndexPackageFromCacheKey extracts the project name from either the legacy
// unsigned index-key shape or the versioned signed shape used by extra indexes.
func IndexPackageFromCacheKey(prefix, key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, prefix+"/simple/")
	if !ok {
		return "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && validIndexPackage(parts[0]) && parts[1] == "index.html" {
		return parts[0], true
	}
	if len(parts) == 5 && validIndexPackage(parts[0]) && parts[1] == "_signed" &&
		validTokenCacheVersion(parts[2]) && validLowerHexSHA256(parts[3]) && parts[4] == "index.html" {
		return parts[0], true
	}
	return "", false
}

func validIndexPackage(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "\\?#\x00\r\n")
}

func validTokenCacheVersion(value string) bool {
	if len(value) < 2 || value[0] != 'v' {
		return false
	}
	for _, character := range value[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validLowerHexSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// FileCacheKey returns the cache key for a package file.
func FileCacheKey(prefix, filepath string) string {
	return prefix + "/files/" + strings.TrimPrefix(filepath, "/")
}

// ExternalFileCacheKey gives a fixed external target one compact cache
// identity without storing its full URL (or a reversible token) in cache
// metadata and storage paths.
func ExternalFileCacheKey(prefix, targetURL string) string {
	digest := sha256.Sum256([]byte(targetURL))
	return prefix + "/files/_external/sha256/" + hex.EncodeToString(digest[:])
}
