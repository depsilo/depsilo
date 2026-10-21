package accesslog

import (
	"net/http"
	"strings"

	"depsilo/internal/db"
)

// SuccessfulArtifactDownload is positive evidence that this GET response body
// was delivered. It does not prove a package-manager install succeeded or that
// a partial-content response represented an entire artifact.
func SuccessfulArtifactDownload(e Event) bool {
	return potentialArtifactDelivery(e) && e.DependencyKind == db.CacheKindArtifact && e.DeliveryResult == "completed"
}

// potentialArtifactDelivery also accepts unknown payload classification, so
// the lifetime observer can preserve its existing observation-gap semantics.
func potentialArtifactDelivery(e Event) bool {
	return (e.DependencyKind == db.CacheKindArtifact || e.DependencyKind == "unknown") &&
		e.Method == http.MethodGet && e.CacheKey != "" && e.AdapterType != "" &&
		e.BytesSent > 0 && e.StatusCode >= 200 && e.StatusCode < 300 &&
		(e.CacheResult == "hit" || e.CacheResult == "miss")
}

// DependencyRequestKind describes the downloaded payload, not its cache TTL.
// Immutable metadata can be cached as an artifact; mutable SNAPSHOT binaries
// can be cached as metadata. Neither distinction changes what a user downloaded.
func DependencyRequestKind(ecosystem, key string) string {
	key = strings.ToLower(key)
	suffix := func(endings ...string) bool {
		for _, ending := range endings {
			if strings.HasSuffix(key, ending) {
				return true
			}
		}
		return false
	}
	switch strings.ToLower(ecosystem) {
	case "", "health", "system":
		return db.CacheKindMetadata
	case "go":
		if suffix(".zip") {
			return db.CacheKindArtifact
		}
		return db.CacheKindMetadata
	case "rubygems":
		if strings.Contains(key, "/gems/") && suffix(".gem") {
			return db.CacheKindArtifact
		}
		return db.CacheKindMetadata
	case "docker":
		if strings.Contains(key, "/blobs/") {
			return db.CacheKindArtifact
		}
		return db.CacheKindMetadata
	case "apt":
		if suffix(".deb", ".udeb", ".ddeb", ".tar.gz", ".tar.xz", ".tar.bz2", ".diff.gz", ".diff.xz") {
			return db.CacheKindArtifact
		}
		return db.CacheKindMetadata
	case "maven":
		if suffix("maven-metadata.xml", ".pom", ".module", ".sha1", ".sha256", ".sha512", ".md5", ".asc") {
			return db.CacheKindMetadata
		}
		if suffix(".jar", ".war", ".ear", ".aar", ".zip", ".tar.gz", ".tar.xz", ".tar.bz2", ".tgz", ".so", ".dll", ".exe", ".klib", ".wasm") {
			return db.CacheKindArtifact
		}
		return "unknown"
	default:
		return db.ClassifyCacheKind(ecosystem, key)
	}
}
