package compilecache

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHTTPKeySupportsStockCCacheLayouts(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef01234567"
	for _, input := range []string{key, "/" + key, key[:2] + "/" + key[2:], "/" + key[:2] + "/" + key[2:]} {
		got, err := ParseHTTPKey(input)
		if err != nil {
			t.Fatalf("ParseHTTPKey(%q): %v", input, err)
		}
		if got != key {
			t.Fatalf("ParseHTTPKey(%q) = %q, want %q", input, got, key)
		}
	}
	const legacyKey = "9cdejbfdl00fmeavr8k681b9l0l0l5g56"
	for _, input := range []string{legacyKey, legacyKey[:2] + "/" + legacyKey[2:]} {
		got, err := ParseHTTPKey(input)
		if err != nil || got != legacyKey {
			t.Fatalf("ParseHTTPKey(%q) = %q, %v; want %q", input, got, err, legacyKey)
		}
	}
}

func TestParseHTTPKeyRejectsNonCanonicalPaths(t *testing.T) {
	for _, input := range []string{
		"", "abc", "../0123456789abcdef0123456789abcdef01234567",
		"0123456789ABCDEF0123456789abcdef01234567",
		"01//23456789abcdef0123456789abcdef01234567",
		"gg23456789abcdef0123456789abcdef01234567",
	} {
		if _, err := ParseHTTPKey(input); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParseHTTPKey(%q) error = %v, want ErrInvalidKey", input, err)
		}
	}
}

func TestNormalizeNamespace(t *testing.T) {
	for _, namespace := range []string{"team-a", "toolchain.gcc_13", "a", "123"} {
		got, err := NormalizeNamespace(namespace)
		if err != nil || got != namespace {
			t.Errorf("NormalizeNamespace(%q) = %q, %v", namespace, got, err)
		}
	}
	for _, namespace := range []string{"", "Team", "-team", "team-", "team/name", "../team", "a b"} {
		if _, err := NormalizeNamespace(namespace); !errors.Is(err, ErrInvalidNamespace) {
			t.Errorf("NormalizeNamespace(%q) error = %v, want ErrInvalidNamespace", namespace, err)
		}
	}
}

func TestParseSCCacheKeyAcceptsCanonicalWebDAVPath(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, input := range []string{"0/1/2/" + key, "/0/1/2/" + key} {
		got, err := ParseSCCacheKey(input)
		if err != nil {
			t.Fatalf("ParseSCCacheKey(%q): %v", input, err)
		}
		if got != key {
			t.Fatalf("ParseSCCacheKey(%q) = %q, want %q", input, got, key)
		}
	}
}

func TestParseSCCacheKeyRejectsNonCanonicalWebDAVPaths(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, input := range []string{
		key,
		"0/1/" + key,
		"0/1/2/" + key[:63],
		"0/1/3/" + key,
		"0/1/2/" + strings.ToUpper(key),
		"../1/2/" + key,
		"0//1/2/" + key,
		".sccache_check",
	} {
		if _, err := ParseSCCacheKey(input); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("ParseSCCacheKey(%q) error = %v, want ErrInvalidKey", input, err)
		}
	}
}

func TestArtifactIDValidatesProtocolSpecificKeys(t *testing.T) {
	ccache, err := ParseCCacheArtifact(testNamespace, testKeyA)
	if err != nil {
		t.Fatal(err)
	}
	if ccache.Protocol() != ProtocolCCache || ccache.Namespace() != testNamespace || ccache.Key() != testKeyA {
		t.Fatalf("ccache ArtifactID = protocol=%q namespace=%q key=%q", ccache.Protocol(), ccache.Namespace(), ccache.Key())
	}

	const sccacheKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sccache, err := ParseSCCacheArtifact(testNamespace, "0/1/2/"+sccacheKey)
	if err != nil {
		t.Fatal(err)
	}
	if sccache.Protocol() != ProtocolSCCache || sccache.Namespace() != testNamespace || sccache.Key() != sccacheKey {
		t.Fatalf("sccache ArtifactID = protocol=%q namespace=%q key=%q", sccache.Protocol(), sccache.Namespace(), sccache.Key())
	}

	if _, err := NewArtifactID(Protocol("future"), testNamespace, testKeyA); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("unknown protocol error = %v, want ErrInvalidProtocol", err)
	}
}

func TestObjectPathMatchesExactArtifactIdentity(t *testing.T) {
	ccache, err := ParseCCacheArtifact(testNamespace, testKeyA)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"v1/ccache/team-a/objects/01/23456789abcdef0123456789abcdef01234567/generation",
		"v1/team-a/objects/01/23456789abcdef0123456789abcdef01234567/legacy-generation",
	} {
		if !objectPathMatchesArtifact(ccache, path) {
			t.Errorf("valid ccache path %q did not match", path)
		}
	}
	for _, path := range []string{
		"v1/ccache/team-a/objects/11/23456789abcdef0123456789abcdef01234567/generation",
		"v1/ccache/team-a/objects/01/ffffffffffffffffffffffffffffffffffffff/generation",
		"v1/sccache/team-a/objects/01/23456789abcdef0123456789abcdef01234567/generation",
		"v1/ccache/team-b/objects/01/23456789abcdef0123456789abcdef01234567/generation",
	} {
		if objectPathMatchesArtifact(ccache, path) {
			t.Errorf("misbound path %q matched ccache artifact", path)
		}
	}
}
