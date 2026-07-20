package db

import (
	"strings"

	"gorm.io/gorm"
)

// backfillCacheKinds classifies rows created before cache_kind was introduced.
// New writes are tagged by cache.Manager, so this runs only for legacy rows.
func backfillCacheKinds(database *gorm.DB) error {
	var rows []CacheEntry
	return database.Where("cache_kind = '' OR cache_kind IS NULL").
		FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
			metadataIDs := make([]uint, 0, len(rows))
			artifactIDs := make([]uint, 0, len(rows))
			for _, row := range rows {
				if ClassifyCacheKind(row.AdapterType, row.Key) == CacheKindMetadata {
					metadataIDs = append(metadataIDs, row.ID)
				} else {
					artifactIDs = append(artifactIDs, row.ID)
				}
			}
			if len(metadataIDs) > 0 {
				if err := database.Model(&CacheEntry{}).Where("id IN ?", metadataIDs).Update("cache_kind", CacheKindMetadata).Error; err != nil {
					return err
				}
			}
			if len(artifactIDs) > 0 {
				if err := database.Model(&CacheEntry{}).Where("id IN ?", artifactIDs).Update("cache_kind", CacheKindArtifact).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

// ClassifyCacheKind derives whether a generated adapter cache key is mutable
// metadata or an immutable artifact. It is used both for legacy backfills and
// new writes so classification remains correct even when operators configure
// ttl_index greater than the tamper-detection threshold.
func ClassifyCacheKind(adapterType, key string) string {
	adapterType = strings.ToLower(adapterType)
	key = strings.ToLower(key)
	metadata := false
	switch {
	case strings.Contains(key, "/simple/") && strings.HasSuffix(key, "/index.html"):
		metadata = true // PyPI and configured extra indexes
	case adapterType == "npm":
		metadata = strings.HasSuffix(key, "/metadata.json")
	case adapterType == "apt":
		metadata = hasAnySuffix(key, "inrelease", "release", "release.gpg", "packages", "packages.gz", "packages.xz", "packages.bz2", "sources", "sources.gz", "sources.xz")
	case adapterType == "go":
		metadata = strings.HasSuffix(key, "/@v/list") || strings.HasSuffix(key, "/@latest")
	case adapterType == "cargo":
		metadata = key == "cargo/config.json" || strings.HasPrefix(key, "cargo/index/")
	case adapterType == "maven":
		metadata = strings.HasSuffix(key, "maven-metadata.xml") || strings.Contains(key, "-snapshot")
	case adapterType == "rubygems":
		metadata = !(strings.Contains(key, "/gems/") && strings.HasSuffix(key, ".gem")) &&
			!(strings.Contains(key, "/quick/") && strings.HasSuffix(key, ".gemspec.rz"))
	case adapterType == "composer":
		metadata = key == "composer/packages.json" || strings.HasPrefix(key, "composer/p2/")
	case adapterType == "nuget":
		metadata = !strings.HasSuffix(key, ".nupkg")
	case adapterType == "conda":
		metadata = !strings.HasSuffix(key, ".tar.bz2") && !strings.HasSuffix(key, ".conda")
	case adapterType == "cran":
		metadata = !hasAnySuffix(key, ".tar.gz", ".zip", ".tgz")
	case adapterType == "alpine":
		metadata = hasAnySuffix(key, "apkindex.tar.gz", "/apkindex", ".txt")
	case adapterType == "helm":
		metadata = !strings.HasSuffix(key, ".tgz")
	case adapterType == "docker":
		metadata = strings.Contains(key, "/tags/") || dockerTaggedManifestKey(key)
	}
	if metadata {
		return CacheKindMetadata
	}
	return CacheKindArtifact
}

func dockerTaggedManifestKey(key string) bool {
	const marker = "/manifests/"
	idx := strings.Index(key, marker)
	if idx < 0 {
		return false
	}
	rest := strings.TrimPrefix(key[idx+len(marker):], "/")
	lastSlash := strings.LastIndex(rest, "/")
	if lastSlash < 0 || lastSlash == len(rest)-1 {
		return false
	}
	reference := rest[lastSlash+1:]
	return !strings.HasPrefix(reference, "sha256__")
}

func hasAnySuffix(value string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}
