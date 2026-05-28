package huggingface_test

import (
	"testing"
	"time"

	"depsilo/internal/adapter/huggingface"
)

func TestIsCommitSHA(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", true},  // 40 lowercase hex
		{"A1B2C3D4E5F60718293A4B5C6D7E8F9012345678", false}, // uppercase — not standard SHA
		{"main", false},
		{"v1.0", false},
		{"refs/heads/feature", false},
		{"", false},
		{"a1b2c3", false},                                    // too short
		{"a1b2c3d4e5f60718293a4b5c6d7e8f90123456789", false}, // 41 chars
	}
	for _, tc := range cases {
		got := huggingface.IsCommitSHA(tc.in)
		if got != tc.want {
			t.Errorf("IsCommitSHA(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTTLFor(t *testing.T) {
	sha := "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"
	if got := huggingface.TTLForRef(sha); got != 72*time.Hour {
		t.Errorf("TTLForRef(SHA) = %v, want 72h", got)
	}
	if got := huggingface.TTLForRef("main"); got != 5*time.Minute {
		t.Errorf("TTLForRef(branch) = %v, want 5m", got)
	}
	if got := huggingface.TTLForRef(""); got != 5*time.Minute {
		t.Errorf("TTLForRef(empty) = %v, want 5m", got)
	}
}

func TestParseRequestPath(t *testing.T) {
	cases := []struct {
		path     string
		wantKind huggingface.PathKind
		wantRepo string
		wantRef  string
		wantSub  string
	}{
		{
			path:     "/google/flan-t5-base/resolve/main/config.json",
			wantKind: huggingface.PathResolve,
			wantRepo: "google/flan-t5-base",
			wantRef:  "main",
			wantSub:  "config.json",
		},
		{
			path:     "/bert-base-uncased/resolve/main/pytorch_model.bin",
			wantKind: huggingface.PathResolve,
			wantRepo: "bert-base-uncased",
			wantRef:  "main",
			wantSub:  "pytorch_model.bin",
		},
		{
			path:     "/google/flan-t5/raw/v1.0/README.md",
			wantKind: huggingface.PathRaw,
			wantRepo: "google/flan-t5",
			wantRef:  "v1.0",
			wantSub:  "README.md",
		},
		{
			path:     "/api/models/bert-base-uncased",
			wantKind: huggingface.PathAPIModelInfo,
			wantRepo: "bert-base-uncased",
		},
		{
			path:     "/api/models/google/flan-t5-base/tree/main",
			wantKind: huggingface.PathAPIModelTree,
			wantRepo: "google/flan-t5-base",
			wantRef:  "main",
		},
		{
			path:     "/api/datasets/squad",
			wantKind: huggingface.PathAPIDatasetInfo,
			wantRepo: "squad",
		},
		{
			path:     "/api/datasets/wikitext/revision/abc1234567890123456789012345678901234567",
			wantKind: huggingface.PathAPIDatasetRevision,
			wantRepo: "wikitext",
			wantRef:  "abc1234567890123456789012345678901234567",
		},
		{
			path:     "/unknown/path",
			wantKind: huggingface.PathUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := huggingface.ParseRequestPath(tc.path)
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", got.Kind, tc.wantKind)
			}
			if got.Repo != tc.wantRepo {
				t.Errorf("Repo = %q, want %q", got.Repo, tc.wantRepo)
			}
			if got.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", got.Ref, tc.wantRef)
			}
			if got.Subpath != tc.wantSub {
				t.Errorf("Subpath = %q, want %q", got.Subpath, tc.wantSub)
			}
		})
	}
}

func TestCacheKey(t *testing.T) {
	parsed := huggingface.ParseRequestPath("/google/flan-t5-base/resolve/main/config.json")
	got := huggingface.CacheKey(parsed)
	want := "huggingface/google/flan-t5-base/resolve/main/config.json"
	if got != want {
		t.Errorf("CacheKey = %q, want %q", got, want)
	}
}
