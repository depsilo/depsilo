package db

import "testing"

func TestAutoMigrateBackfillsLegacyCacheKinds(t *testing.T) {
	database, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []CacheEntry{
		{Key: "pypi/simple/av/index.html", AdapterType: "pypi"},
		{Key: "pypi/files/av-18.0.0.whl", AdapterType: "pypi"},
		{Key: "npm/react/metadata.json", AdapterType: "npm"},
		{Key: "npm/react/-/react-19.0.0.tgz", AdapterType: "npm"},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	var got []CacheEntry
	if err := database.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{CacheKindMetadata, CacheKindArtifact, CacheKindMetadata, CacheKindArtifact}
	for i := range want {
		if got[i].CacheKind != want[i] {
			t.Errorf("row %q kind = %q, want %q", got[i].Key, got[i].CacheKind, want[i])
		}
	}
}

func TestLegacyCacheKindEcosystems(t *testing.T) {
	tests := []struct{ ecosystem, key, want string }{
		{"go", "go/example.com/mod/@v/list", CacheKindMetadata},
		{"go", "go/example.com/mod/@v/v1.0.0.zip", CacheKindArtifact},
		{"cargo", "cargo/index/av/av", CacheKindMetadata},
		{"composer", "composer/p2/vendor/pkg.json", CacheKindMetadata},
		{"maven", "maven/org/x/maven-metadata.xml", CacheKindMetadata},
		{"nuget", "nuget/v3-flatcontainer/x/1.0/x.1.0.nupkg", CacheKindArtifact},
		{"conda", "conda/linux-64/repodata.json", CacheKindMetadata},
		{"helm", "helm/index.yaml", CacheKindMetadata},
		{"alpine", "alpine/v3.20/main/x86_64/APKINDEX.tar.gz", CacheKindMetadata},
	}
	for _, tt := range tests {
		if got := ClassifyCacheKind(tt.ecosystem, tt.key); got != tt.want {
			t.Errorf("ClassifyCacheKind(%q, %q) = %q, want %q", tt.ecosystem, tt.key, got, tt.want)
		}
	}
}

func TestClassifyDockerMetadata(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"docker/dockerhub/tags/library/alpine/list", CacheKindMetadata},
		{"docker/dockerhub/manifests/library/alpine/latest", CacheKindMetadata},
		{"docker/dockerhub/manifests/library/alpine/sha256__abc", CacheKindArtifact},
		{"docker/dockerhub/blobs/sha256__abc", CacheKindArtifact},
	}
	for _, tt := range tests {
		if got := ClassifyCacheKind("docker", tt.key); got != tt.want {
			t.Errorf("ClassifyCacheKind(docker, %q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
