package docker

import (
	"fmt"
	"strings"
)

// encodeDigest replaces ":" with "__" for filesystem-safe cache keys.
func encodeDigest(s string) string {
	return strings.ReplaceAll(s, ":", "__")
}

func ManifestCacheKey(registryName, imageName, reference string) string {
	return fmt.Sprintf("docker/%s/manifests/%s/%s", registryName, imageName, encodeDigest(reference))
}

func BlobCacheKey(registryName, digest string) string {
	return fmt.Sprintf("docker/%s/blobs/%s", registryName, encodeDigest(digest))
}

func TagListCacheKey(registryName, imageName string) string {
	return fmt.Sprintf("docker/%s/tags/%s/list", registryName, imageName)
}
