package packagekey

import (
	"strings"
	"testing"
)

func TestExtractHuggingFacePackageIdentity(t *testing.T) {
	const commit = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	tests := []struct {
		name        string
		key         string
		wantPackage string
		wantVersion string
	}{
		{
			name:        "owner and model name",
			key:         "huggingface/google/flan-t5-base/resolve/main/model.safetensors",
			wantPackage: "google/flan-t5-base",
			wantVersion: "main",
		},
		{
			name:        "single-token model",
			key:         "huggingface/bert-base-uncased/raw/" + commit + "/config.json",
			wantPackage: "bert-base-uncased",
			wantVersion: commit,
		},
		{
			name:        "owner and dataset name keep their namespace",
			key:         "huggingface/datasets/org/corpus/resolve/v2/data/train.parquet",
			wantPackage: "datasets/org/corpus",
			wantVersion: "v2",
		},
		{
			name:        "single-token dataset keeps its namespace",
			key:         "huggingface/datasets/squad/raw/main/README.md",
			wantPackage: "datasets/squad",
			wantVersion: "main",
		},
		{
			name:        "encoded slash in revision",
			key:         "huggingface/acme/model/resolve/refs%2Fpr%2F1/README.md",
			wantPackage: "acme/model",
			wantVersion: "refs/pr/1",
		},
		{
			name:        "repository may be named resolve",
			key:         "huggingface/acme/resolve/resolve/" + commit + "/model.bin",
			wantPackage: "acme/resolve",
			wantVersion: commit,
		},
		{
			name:        "opaque query artifact retains package identity",
			key:         "huggingface/__query__/artifact/" + strings.Repeat("a", 64) + "/acme/model/resolve/main/model.bin",
			wantPackage: "acme/model",
			wantVersion: "main",
		},
		{
			name:        "real repository named query artifact is not a wrapper",
			key:         "huggingface/__query__/artifact/resolve/main/model.bin",
			wantPackage: "__query__/artifact",
			wantVersion: "main",
		},
		{
			name:        "single-token query repository with commit ref",
			key:         "huggingface/__query__/resolve/" + commit + "/model.bin",
			wantPackage: "__query__",
			wantVersion: commit,
		},
		{
			name:        "model API retains package identity",
			key:         "huggingface/api/models/google/flan-t5-base",
			wantPackage: "google/flan-t5-base",
		},
		{
			name:        "dataset API retains namespaced package identity",
			key:         "huggingface/api/datasets/org/corpus/tree/main",
			wantPackage: "datasets/org/corpus",
		},
		{
			name:        "opaque query metadata retains package identity",
			key:         "huggingface/__query__/metadata/" + strings.Repeat("b", 64) + "/api/models/acme/model/tree/main/folder",
			wantPackage: "acme/model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractName("huggingface", tt.key); got != tt.wantPackage {
				t.Errorf("ExtractName(huggingface, %q) = %q, want %q", tt.key, got, tt.wantPackage)
			}
			if got := ExtractVersion("huggingface", tt.key); got != tt.wantVersion {
				t.Errorf("ExtractVersion(huggingface, %q) = %q, want %q", tt.key, got, tt.wantVersion)
			}
		})
	}
}

func TestHuggingFacePackageFilesExcludeAPIMetadata(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "model resolve file",
			key:  "huggingface/google/flan-t5-base/resolve/main/model.safetensors",
			want: true,
		},
		{
			name: "model raw file",
			key:  "huggingface/bert-base-uncased/raw/main/config.json",
			want: true,
		},
		{
			name: "dataset resolve file",
			key:  "huggingface/datasets/org/corpus/resolve/main/data/train.parquet",
			want: true,
		},
		{
			name: "model API metadata",
			key:  "huggingface/api/models/google/flan-t5-base",
		},
		{
			name: "dataset API metadata",
			key:  "huggingface/api/datasets/org/corpus/tree/main",
		},
		{
			name: "opaque query API metadata",
			key:  "huggingface/__query__/metadata/" + strings.Repeat("b", 64) + "/api/models/acme/model/tree/main",
		},
		{
			name: "incomplete file route",
			key:  "huggingface/google/flan-t5-base/resolve/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPackageFile("huggingface", tt.key); got != tt.want {
				t.Errorf("IsPackageFile(huggingface, %q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
