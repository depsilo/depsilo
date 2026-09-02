package npm

import "testing"

func TestCacheKeysDoNotReuseCaseFoldedV090Namespace(t *testing.T) {
	legacyKeys := map[string]struct{}{
		"npm/express/metadata.json":                    {},
		"npm/jsonstream/-/JSONStream-1.3.5.tgz":        {},
		"npm/@legacyscope/name/metadata.json":          {},
		"npm/@scope/legacyname/-/LegacyName-1.0.0.tgz": {},
	}
	currentKeys := []string{
		MetadataCacheKey("express"),
		TarballCacheKey("jsonstream", "JSONStream-1.3.5.tgz"),
		ScopedMetadataCacheKey("legacyscope", "name"),
		ScopedTarballCacheKey("scope", "legacyname", "LegacyName-1.0.0.tgz"),
	}
	for _, key := range currentKeys {
		if _, collision := legacyKeys[key]; collision {
			t.Fatalf("current exact-identity key %q reuses a v0.9 case-folded cache entry", key)
		}
	}
}

func TestCacheKeysPreserveLegacyPackageNameCase(t *testing.T) {
	tests := []struct {
		name  string
		upper string
		lower string
	}{
		{
			name:  "metadata",
			upper: MetadataCacheKey("Express"),
			lower: MetadataCacheKey("express"),
		},
		{
			name:  "tarball",
			upper: TarballCacheKey("JSONStream", "JSONStream-1.3.5.tgz"),
			lower: TarballCacheKey("jsonstream", "JSONStream-1.3.5.tgz"),
		},
		{
			name:  "scoped metadata scope",
			upper: ScopedMetadataCacheKey("LegacyScope", "name"),
			lower: ScopedMetadataCacheKey("legacyscope", "name"),
		},
		{
			name:  "scoped metadata package",
			upper: ScopedMetadataCacheKey("scope", "LegacyName"),
			lower: ScopedMetadataCacheKey("scope", "legacyname"),
		},
		{
			name:  "scoped tarball scope",
			upper: ScopedTarballCacheKey("LegacyScope", "name", "name-1.0.0.tgz"),
			lower: ScopedTarballCacheKey("legacyscope", "name", "name-1.0.0.tgz"),
		},
		{
			name:  "scoped tarball package",
			upper: ScopedTarballCacheKey("scope", "LegacyName", "LegacyName-1.0.0.tgz"),
			lower: ScopedTarballCacheKey("scope", "legacyname", "LegacyName-1.0.0.tgz"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.upper == tt.lower {
				t.Fatalf("case-distinct npm identities share cache key %q", tt.upper)
			}
		})
	}
}
