package huggingface_test

import (
	"net/url"
	"strings"
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
		{"a1b2c3", false}, // too short
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
		{
			path:     "/datasets/bigcode/the-stack/resolve/main/data/train.parquet",
			wantKind: huggingface.PathResolve,
			wantRepo: "datasets/bigcode/the-stack",
			wantRef:  "main",
			wantSub:  "data/train.parquet",
		},
		{
			path:     "/acme/model/resolve/refs%2Fpr%2F1/a%3Fb",
			wantKind: huggingface.PathResolve,
			wantRepo: "acme/model",
			wantRef:  "refs/pr/1",
			wantSub:  "a?b",
		},
		{
			path:     "/api/models/acme/model/tree/main/folder%2Fsub",
			wantKind: huggingface.PathAPIModelTree,
			wantRepo: "acme/model",
			wantRef:  "main",
			wantSub:  "folder/sub",
		},
		{
			path:     "/api/models/acme/model/revision/main/not-valid",
			wantKind: huggingface.PathUnknown,
		},
		{
			path:     "/api/models/acme/tree",
			wantKind: huggingface.PathAPIModelInfo,
			wantRepo: "acme/tree",
		},
		{
			path:     "/api/models/acme/tree/tree/main",
			wantKind: huggingface.PathAPIModelTree,
			wantRepo: "acme/tree",
			wantRef:  "main",
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

func TestCacheKeyWithQueryIsCanonicalAndOpaque(t *testing.T) {
	parsed := huggingface.ParseRequestPath("/api/models/google/flan-t5-base/tree/main")
	first := huggingface.CacheKeyWithQuery(parsed, url.Values{
		"cursor": {"secret-cursor"},
		"limit":  {"10"},
	})
	second := huggingface.CacheKeyWithQuery(parsed, url.Values{
		"limit":  {"10"},
		"cursor": {"secret-cursor"},
	})
	if first != second {
		t.Fatalf("equivalent query keys differ:\n%s\n%s", first, second)
	}
	if first == huggingface.CacheKey(parsed) {
		t.Fatal("query-bearing request reused the query-free cache key")
	}
	if want := "secret-cursor"; strings.Contains(first, want) {
		t.Fatalf("cache key leaked raw query value %q: %s", want, first)
	}
	if !strings.HasPrefix(first, "huggingface/__query__/metadata/") {
		t.Fatalf("query key did not use isolated representation namespace: %s", first)
	}

	expandOne := huggingface.CacheKeyWithQuery(parsed, url.Values{
		"expand": {"likes", "downloads"},
	})
	expandTwo := huggingface.CacheKeyWithQuery(parsed, url.Values{
		"expand": {"downloads", "likes"},
	})
	if expandOne != expandTwo {
		t.Fatalf("equivalent repeated query values differ:\n%s\n%s", expandOne, expandTwo)
	}

	modelInfo := huggingface.ParseRequestPath("/api/models/google/flan-t5-base")
	lowerBoolean := huggingface.CacheKeyWithQuery(modelInfo, url.Values{"blobs": {"true"}})
	titleBoolean := huggingface.CacheKeyWithQuery(modelInfo, url.Values{"blobs": {"True"}})
	if lowerBoolean != titleBoolean {
		t.Fatalf("equivalent boolean query keys differ:\n%s\n%s", lowerBoolean, titleBoolean)
	}
}

func TestQueryCacheNamespaceCannotCollideWithAValidRouteKey(t *testing.T) {
	parsed := huggingface.ParseRequestPath("/api/models/acme/model/tree/main")
	queryKey := huggingface.CacheKeyWithQuery(parsed, url.Values{"cursor": {"next"}})
	parts := strings.Split(queryKey, "/")
	if len(parts) < 6 {
		t.Fatalf("unexpected query cache key: %q", queryKey)
	}
	// A route attempting to spell the reserved namespace is not recognized,
	// while the original base path remains visible only after the opaque hash.
	reservedRoute := huggingface.ParseRequestPath("/__query__/metadata/" + parts[3])
	if reservedRoute.Kind != huggingface.PathUnknown || huggingface.CacheKey(reservedRoute) != "" {
		t.Fatalf("reserved query namespace was reachable as a Hub route: %+v", reservedRoute)
	}
	if strings.HasPrefix(queryKey, huggingface.CacheKey(parsed)+"/") {
		t.Fatalf("query identity was appended inside a user-controlled tree path: %q", queryKey)
	}
}

func TestCacheKeyPreservesEncodedRevisionAndTreeSubpath(t *testing.T) {
	encodedFile := huggingface.ParseRequestPath("/acme/model/resolve/refs%2Fpr%2F1/a%3Fb")
	if got, want := huggingface.CacheKey(encodedFile), "huggingface/acme/model/resolve/refs%2Fpr%2F1/a%3Fb"; got != want {
		t.Fatalf("encoded file CacheKey = %q, want %q", got, want)
	}

	first := huggingface.ParseRequestPath("/api/models/acme/model/tree/main/coreml")
	second := huggingface.ParseRequestPath("/api/models/acme/model/tree/main/coreml/fill-mask")
	firstKey := huggingface.CacheKey(first)
	secondKey := huggingface.CacheKey(second)
	if firstKey == secondKey {
		t.Fatalf("tree subpaths collided at %q", firstKey)
	}
	if want := "huggingface/api/models/acme/model/tree/main/coreml/fill-mask"; secondKey != want {
		t.Fatalf("tree CacheKey = %q, want %q", secondKey, want)
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

func TestCacheKeyCanonicalizesRepositoryCaseAliases(t *testing.T) {
	modelAlias := huggingface.ParseRequestPath("/OpenAI/Whisper-Tiny/resolve/main/config.json")
	modelCanonical := huggingface.ParseRequestPath("/openai/whisper-tiny/resolve/main/config.json")
	if aliasKey, canonicalKey := huggingface.CacheKey(modelAlias), huggingface.CacheKey(modelCanonical); aliasKey != canonicalKey {
		t.Fatalf("model alias keys differ: %q != %q", aliasKey, canonicalKey)
	}

	datasetAlias := huggingface.ParseRequestPath("/datasets/Acme/My_Data/resolve/main/data.parquet")
	datasetCanonical := huggingface.ParseRequestPath("/datasets/acme/my_data/resolve/main/data.parquet")
	if aliasKey, canonicalKey := huggingface.CacheKey(datasetAlias), huggingface.CacheKey(datasetCanonical); aliasKey != canonicalKey {
		t.Fatalf("dataset alias keys differ: %q != %q", aliasKey, canonicalKey)
	}
}
