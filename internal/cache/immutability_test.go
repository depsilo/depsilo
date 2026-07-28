package cache

import (
	"strings"
	"testing"
	"time"
)

func TestTamperEligibilityRequiresHuggingFaceCommitReference(t *testing.T) {
	manager := &Manager{immutableThreshold: time.Minute}
	const commit = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name        string
		adapterType string
		key         string
		want        bool
	}{
		{
			name:        "commit-addressed model",
			adapterType: "huggingface",
			key:         "huggingface/acme/model/resolve/" + commit + "/model.safetensors",
			want:        true,
		},
		{
			name:        "commit-addressed dataset",
			adapterType: "huggingface",
			key:         "huggingface/datasets/acme/corpus/raw/" + commit + "/README.md",
			want:        true,
		},
		{
			name:        "repository named resolve remains commit addressed",
			adapterType: "huggingface",
			key:         "huggingface/acme/resolve/resolve/" + commit + "/model.bin",
			want:        true,
		},
		{
			name:        "query representation retains commit identity",
			adapterType: "huggingface",
			key:         "huggingface/__query__/artifact/" + strings.Repeat("a", 64) + "/acme/model/resolve/" + commit + "/model.bin",
			want:        true,
		},
		{
			name:        "moving branch despite low operator threshold",
			adapterType: "huggingface",
			key:         "huggingface/acme/model/resolve/main/model.safetensors",
			want:        false,
		},
		{
			name:        "non-canonical uppercase commit",
			adapterType: "huggingface",
			key:         "huggingface/acme/model/resolve/0123456789ABCDEF0123456789ABCDEF01234567/model.safetensors",
			want:        false,
		},
		{
			name:        "other immutable ecosystem",
			adapterType: "pypi",
			key:         "pypi/files/example-1.0.whl",
			want:        true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := manager.tamperEligible(test.adapterType, test.key, 5*time.Minute); got != test.want {
				t.Fatalf("tamperEligible() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVersionFromKeyUsesHuggingFaceRevisionInsteadOfFilename(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	key := "huggingface/acme/model/resolve/" + commit + "/model.safetensors"
	if got := versionFromKey("huggingface", key); got != commit {
		t.Fatalf("versionFromKey() = %q, want commit %q", got, commit)
	}
}
