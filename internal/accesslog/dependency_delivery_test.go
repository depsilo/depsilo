package accesslog

import (
	"testing"

	"depsilo/internal/db"
)

func TestDependencyRequestKindUsesPayloadMeaningNotCacheTTL(t *testing.T) {
	for _, test := range []struct{ ecosystem, key, want string }{
		{"maven", "maven/org/pkg/1-SNAPSHOT/pkg-1-SNAPSHOT.jar", db.CacheKindArtifact},
		{"maven", "maven/org/pkg/1/pkg-1.pom", db.CacheKindMetadata},
		{"maven", "maven/org/pkg/1/pkg-1.jar.sha1", db.CacheKindMetadata},
		{"maven", "maven/org/pkg/1/pkg-1.custom", "unknown"},
		{"docker", "docker/hub/blobs/library/alpine/sha256__abcd", db.CacheKindArtifact},
		{"docker", "docker/hub/manifests/library/alpine/sha256__abcd", db.CacheKindMetadata},
		{"go", "go/example.org/pkg/@v/v1.0.0.info", db.CacheKindMetadata},
		{"go", "go/example.org/pkg/@v/v1.0.0.mod", db.CacheKindMetadata},
		{"go", "go/example.org/pkg/@v/v1.0.0.zip", db.CacheKindArtifact},
		// A checksum database lookup names a version, and a pre-release may be
		// spelled like the name of a module archive.
		{"go", "go/sumdb/sum.golang.org/lookup/example.com/mod@v1.0.0-a.zip", db.CacheKindMetadata},
		{"go", "go/sumdb/sum.golang.org/tile/8/0/x001/234", db.CacheKindMetadata},
		// Only the exact prefix is the checksum database.
		{"go", "go/Sumdb/mod/@v/v1.0.0.zip", db.CacheKindArtifact},
		{"go", "go/sumdb.example.org/mod/@v/v1.0.0.zip", db.CacheKindArtifact},
		{"rubygems", "rubygems/quick/Marshal.4.8/pkg-1.gemspec.rz", db.CacheKindMetadata},
		{"rubygems", "rubygems/gems/pkg-1.gem", db.CacheKindArtifact},
		{"apt", "apt/dists/stable/main/binary-amd64/by-hash/SHA256/abcdef", db.CacheKindMetadata},
		{"apt", "apt/pool/main/pkg/pkg_1_amd64.deb", db.CacheKindArtifact},
		{"health", "/ready", db.CacheKindMetadata},
	} {
		t.Run(test.ecosystem+"/"+test.key, func(t *testing.T) {
			if got := DependencyRequestKind(test.ecosystem, test.key); got != test.want {
				t.Fatalf("kind = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRecordedVersionNamesOnlyServedArtifacts(t *testing.T) {
	key := "npm-exact-v1/fixture/-/fixture-1.0.0-beta.1.tgz"
	if got := RecordedVersion("npm", key, 200); got != "1.0.0-beta.1" {
		t.Fatalf("served version = %q", got)
	}
	for _, status := range []int{404, 502} {
		if got := RecordedVersion("npm", key, status); got != "" {
			t.Fatalf("status %d version = %q, want none", status, got)
		}
	}
}
