package docker

import "fmt"

func ManifestCacheKey(registryName, imageName, reference string) string {
	return fmt.Sprintf("docker/%s/manifests/%s/%s", registryName, imageName, reference)
}

func BlobCacheKey(registryName, digest string) string {
	return fmt.Sprintf("docker/%s/blobs/%s", registryName, digest)
}

func TagListCacheKey(registryName, imageName string) string {
	return fmt.Sprintf("docker/%s/tags/%s/list", registryName, imageName)
}
