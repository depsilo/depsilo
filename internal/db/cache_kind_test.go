package db

import (
	"strings"
	"testing"
)

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

func TestAutoMigrateBackfillsLegacyHuggingFacePackageNames(t *testing.T) {
	database, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}

	rows := []CacheEntry{
		{
			Key:         "huggingface/acme/model/resolve/main/model.bin",
			AdapterType: "huggingface",
		},
		{
			Key:         "huggingface/api/datasets/acme/corpus/tree/main",
			AdapterType: "huggingface",
		},
		{
			Key: "huggingface/__query__/metadata/" +
				strings.Repeat("a", 64) +
				"/api/models/acme/model/tree/main",
			AdapterType: "huggingface",
		},
		{
			Key:         "huggingface/api/models/preserved/name",
			AdapterType: "huggingface",
			PackageName: "operator-value",
		},
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
	want := []string{
		"acme/model",
		"datasets/acme/corpus",
		"acme/model",
		"operator-value",
	}
	for index := range want {
		if got[index].PackageName != want[index] {
			t.Errorf(
				"row %q package_name = %q, want %q",
				got[index].Key,
				got[index].PackageName,
				want[index],
			)
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

func TestClassifyHuggingFaceCacheKind(t *testing.T) {
	const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "model API metadata",
			key:  "huggingface/api/models/google/flan-t5-base",
			want: CacheKindMetadata,
		},
		{
			name: "opaque model API query metadata",
			key:  "huggingface/__query__/metadata/" + strings.Repeat("a", 64) + "/api/models/google/flan-t5-base/tree/main",
			want: CacheKindMetadata,
		},
		{
			name: "opaque artifact query remains artifact",
			key:  "huggingface/__query__/artifact/" + strings.Repeat("b", 64) + "/google/flan-t5-base/resolve/main/model.bin",
			want: CacheKindArtifact,
		},
		{
			name: "real repository named query metadata is an artifact",
			key:  "huggingface/__query__/metadata/resolve/main/model.bin",
			want: CacheKindArtifact,
		},
		{
			name: "dataset API metadata remains mutable at a commit revision",
			key:  "huggingface/api/datasets/org/corpus/tree/" + commit,
			want: CacheKindMetadata,
		},
		{
			name: "model resolve at an immutable commit",
			key:  "huggingface/google/flan-t5-base/resolve/" + commit + "/model.safetensors",
			want: CacheKindArtifact,
		},
		{
			name: "single-token raw file at an immutable commit",
			key:  "huggingface/bert-base-uncased/raw/" + commit + "/config.json",
			want: CacheKindArtifact,
		},
		{
			name: "dataset file at an immutable commit",
			key:  "huggingface/datasets/org/corpus/resolve/" + commit + "/data/train.parquet",
			want: CacheKindArtifact,
		},
		{
			name: "commit file classification ignores package-like subpaths",
			key:  "huggingface/google/flan-t5-base/resolve/" + commit + "/docs/simple/example/index.html",
			want: CacheKindArtifact,
		},
		{
			name: "moving branch is still a downloadable artifact",
			key:  "huggingface/google/flan-t5-base/resolve/main/model.safetensors",
			want: CacheKindArtifact,
		},
		{
			name: "moving tag is still a downloadable artifact",
			key:  "huggingface/bert-base-uncased/raw/v1.0/config.json",
			want: CacheKindArtifact,
		},
		{
			name: "uppercase hex file remains an artifact",
			key:  "huggingface/google/flan-t5-base/resolve/A1B2C3D4E5F60718293A4B5C6D7E8F9012345678/model.safetensors",
			want: CacheKindArtifact,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyCacheKind("huggingface", tt.key); got != tt.want {
				t.Errorf("ClassifyCacheKind(huggingface, %q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
