package config

import (
	"encoding/base64"
	"testing"
)

func TestNewSecureTokenUses256BitsOfRandomness(t *testing.T) {
	seen := make(map[string]struct{})
	for range 32 {
		token, err := NewSecureToken()
		if err != nil {
			t.Fatalf("NewSecureToken: %v", err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("decode token: %v", err)
		}
		if len(raw) != secureTokenBytes {
			t.Fatalf("decoded token length = %d, want %d", len(raw), secureTokenBytes)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatal("NewSecureToken returned a duplicate")
		}
		seen[token] = struct{}{}
	}
}

func TestResolveBootstrapTokenHonorsStrongEnvironmentValue(t *testing.T) {
	const configured = "test-bootstrap-token-0123456789"
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", configured)

	token, generated, err := resolveBootstrapToken()
	if err != nil {
		t.Fatalf("resolveBootstrapToken: %v", err)
	}
	if token != configured || generated {
		t.Fatalf("resolveBootstrapToken = (%q, %v), want (%q, false)", token, generated, configured)
	}
}

func TestResolveBootstrapTokenRejectsWeakEnvironmentValue(t *testing.T) {
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "too-short")
	if _, _, err := resolveBootstrapToken(); err == nil {
		t.Fatal("resolveBootstrapToken accepted a weak configured token")
	}
}
