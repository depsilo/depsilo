package pypi

import (
	"strings"
	"testing"
)

func TestSignedIndexCacheKeyVersionsSignedHTML(t *testing.T) {
	t.Parallel()
	first := signedIndexCacheKey("extra:torch", "Torch", []byte(strings.Repeat("a", 32)))
	second := signedIndexCacheKey("extra:torch", "Torch", []byte(strings.Repeat("b", 32)))
	if first == second {
		t.Fatal("rotated signing key reused the signed index cache key")
	}
	if !strings.Contains(first, "/_signed/"+externalArtifactTokenVersion+"/") {
		t.Fatalf("signed index key does not include token version: %q", first)
	}
	if pkg, ok := IndexPackageFromCacheKey("extra:torch", first); !ok || pkg != "torch" {
		t.Fatalf("parsed signed index key = %q, %v", pkg, ok)
	}
}

func TestIndexCacheKeyUsesPEP503ProjectIdentity(t *testing.T) {
	t.Parallel()
	if got, want := IndexCacheKey("pypi", "Django_rest.framework"), "pypi/simple/django-rest-framework/index.html"; got != want {
		t.Fatalf("IndexCacheKey = %q, want %q", got, want)
	}
}

func TestIndexPackageFromCacheKeyRejectsMalformedSignedNamespace(t *testing.T) {
	t.Parallel()
	validDigest := strings.Repeat("a", 64)
	invalid := []string{
		"other/simple/torch/index.html",
		"extra:torch/simple/../index.html",
		"extra:torch/simple/torch/_signed/vx/" + validDigest + "/index.html",
		"extra:torch/simple/torch/_signed/v1/not-a-digest/index.html",
		"extra:torch/simple/torch/_signed/v1/" + validDigest + "/nested/index.html",
	}
	for _, key := range invalid {
		if pkg, ok := IndexPackageFromCacheKey("extra:torch", key); ok {
			t.Errorf("malformed key %q parsed as %q", key, pkg)
		}
	}
}
