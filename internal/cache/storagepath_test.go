package cache

import (
	"strings"
	"testing"
)

// A case-folding root resolves paths without regard to case, so two cache keys
// that differ only in case must never map onto one storage path.
func TestStoragePathKeepsCaseDistinctKeysDistinct(t *testing.T) {
	keys := []string{
		"npm-exact-v1/Express/metadata.json",
		"npm-exact-v1/express/metadata.json",
		"npm-exact-v1/EXPRESS/metadata.json",
		"npm-exact-v1/@Scope/Name/metadata.json",
		"huggingface/models/openai/Whisper/index.json",
		"huggingface/models/openai/whisper/index.json",
		"pypi/simple/requests/index.html",
	}
	byFoldedPath := make(map[string]string, len(keys))
	for _, key := range keys {
		path := storagePath(key, true)
		folded := strings.ToLower(path)
		if existing, ok := byFoldedPath[folded]; ok {
			t.Fatalf("keys %q and %q share storage path %q on a case-folding root", existing, key, path)
		}
		byFoldedPath[folded] = key
	}
}

// Lowercase keys dominate the cache, and a case-sensitive root is already
// safe: both must keep the historical layout so existing objects stay
// reachable.
func TestStoragePathLeavesSafeKeysAndCaseSensitiveRootsAlone(t *testing.T) {
	for _, key := range []string{
		"pypi/simple/requests/index.html",
		"pypi/files/requests-2.31.0-py3-none-any.whl",
		"npm-exact-v1/express/metadata.json",
		"v1/ccache/objects/ab/cdef/0123456789abcdef",
	} {
		if path := storagePath(key, true); path != key {
			t.Fatalf("storagePath(%q, folding) = %q, want the key unchanged", key, path)
		}
		if path := storagePath(key, false); path != key {
			t.Fatalf("storagePath(%q, sensitive) = %q, want the key unchanged", key, path)
		}
	}
	for _, key := range []string{
		"npm-exact-v1/Express/metadata.json",
		"huggingface/models/openai/Whisper/index.json",
	} {
		if path := storagePath(key, false); path != key {
			t.Fatalf("storagePath(%q, sensitive) = %q, want the key unchanged", key, path)
		}
	}
}

func TestStoragePathEscapesUnsafeBytesAndKeepsEscapesInjective(t *testing.T) {
	cases := map[string]string{
		"npm-exact-v1/Express/metadata.json":    "npm-exact-v1/%45xpress/metadata.json",
		"npm-exact-v1/aB/metadata.json":         "npm-exact-v1/a%42/metadata.json",
		"npm-exact-v1/%45xpress/metadata.json":  "npm-exact-v1/%2545xpress/metadata.json",
		"huggingface/models/Ünicode/index.json": "huggingface/models/%C3%9Cnicode/index.json",
		"npm-exact-v1/a b/metadata.json":        "npm-exact-v1/a%20b/metadata.json",
	}
	for key, want := range cases {
		if path := storagePath(key, true); path != want {
			t.Fatalf("storagePath(%q, folding) = %q, want %q", key, path, want)
		}
	}
	// A literal escape sequence must not be able to impersonate an escaped key.
	if storagePath("npm-exact-v1/Express/metadata.json", true) == storagePath("npm-exact-v1/%45xpress/metadata.json", true) {
		t.Fatal("escaped key collides with a literal key that spells the escape")
	}
}

func TestStoragePathBoundsOverlongSegmentsWithADigest(t *testing.T) {
	segment := strings.Repeat("A", 200)
	path := storagePath("npm-exact-v1/"+segment+"/metadata.json", true)
	segments := strings.Split(path, "/")
	if len(segments) != 3 {
		t.Fatalf("storagePath(%q) = %q, want the key's three segments", segment, path)
	}
	derived := segments[1]
	if !strings.HasPrefix(derived, "~") || len(derived) != 65 {
		t.Fatalf("derived segment = %q, want a 64-hex digest behind ~", derived)
	}
	// Two keys that fold together through the digest form still differ.
	other := storagePath("npm-exact-v1/"+strings.Repeat("a", 200)+"/metadata.json", true)
	if other == path {
		t.Fatalf("case-distinct long segments collapsed to %q", path)
	}
}

// Listing walks the storage tree, so a listed path must decode back into the
// logical key it was derived from.
func TestStoragePathRoundTripsThroughListedPaths(t *testing.T) {
	for _, key := range []string{
		"npm-exact-v1/Express/metadata.json",
		"npm-exact-v1/@Scope/Name/metadata.json",
		"huggingface/models/openai/Whisper/resolve/main/Model.json",
		"pypi/simple/requests/index.html",
		"npm-exact-v1/%45xpress/metadata.json",
	} {
		path := storagePath(key, true)
		listed, ok := storageKeyFromPath(path, true)
		if !ok {
			t.Fatalf("storageKeyFromPath(%q) failed for key %q", path, key)
		}
		if listed != key {
			t.Fatalf("listed key = %q, want %q (storage path %q)", listed, key, path)
		}
	}
	// The digest fallback cannot be decoded; callers keep the path instead.
	long := "npm-exact-v1/" + strings.Repeat("A", 200) + "/metadata.json"
	if _, ok := storageKeyFromPath(storagePath(long, true), true); ok {
		t.Fatal("digest-backed path reported a decodable key")
	}
}

// The compiler cache shares this storage and reconciles its objects by
// comparing listed paths with the paths recorded in its own metadata, so its
// keys must keep mapping onto themselves.
func TestStoragePathLeavesCompilerCacheLayoutAlone(t *testing.T) {
	for _, path := range []string{
		"v1/ccache/ci-builder/objects/ab/cdef0123/0123456789abcdef0123456789abcdef",
		"v1/sccache/ci_builder/objects/01/23456789abcdef/0011ff",
		"v1/probes/0123456789abcdef",
	} {
		if derived := storagePath(path, true); derived != path {
			t.Fatalf("storagePath(%q, folding) = %q, want the compiler-cache path unchanged", path, derived)
		}
	}
}
