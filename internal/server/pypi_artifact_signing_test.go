package server

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestDerivePyPIArtifactSigningKeyIsStableForConfiguredSecret(t *testing.T) {
	const secret = "configured-test-secret-with-enough-entropy"
	first, err := derivePyPIArtifactSigningKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	second, err := derivePyPIArtifactSigningKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != sha256.Size || !bytes.Equal(first, second) {
		t.Fatalf("derived keys are not stable SHA-256-sized values")
	}
	if bytes.Equal(first, []byte(secret)) {
		t.Fatal("artifact signing reused raw JWT key material")
	}
}

func TestArtifactSigningKeyRejectsUnsafeRootSecrets(t *testing.T) {
	derivers := map[string]func(string) ([]byte, error){
		"pypi": derivePyPIArtifactSigningKey,
		"npm":  deriveNPMTarballSigningKey,
	}
	for name, derive := range derivers {
		for _, secret := range []string{"", "too-short", "  0123456789abcdef0123456789abcdef  "} {
			if _, err := derive(secret); err == nil {
				t.Fatalf("%s signing key derivation accepted unsafe root secret %q", name, secret)
			}
		}
	}
}

func TestArtifactSigningKeysAreEphemeralForLoopbackDevelopmentPlaceholder(t *testing.T) {
	pypi, err := derivePyPIArtifactSigningKey(developmentJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	npm, err := deriveNPMTarballSigningKey(developmentJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	secondNPM, err := deriveNPMTarballSigningKey(developmentJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(pypi) != sha256.Size || len(npm) != sha256.Size || len(secondNPM) != sha256.Size {
		t.Fatal("development artifact signing keys are not SHA-256-sized")
	}
	if bytes.Equal(pypi, npm) || bytes.Equal(npm, secondNPM) {
		t.Fatal("development artifact signing keys were reused")
	}
}

func TestNPMTarballSigningKeyIsStableAndDomainSeparated(t *testing.T) {
	const secret = "configured-test-secret-with-enough-entropy"
	first, err := deriveNPMTarballSigningKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	second, err := deriveNPMTarballSigningKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	pypi, err := derivePyPIArtifactSigningKey(secret)
	if err != nil {
		t.Fatal(err)
	}
	otherSecret, err := deriveNPMTarballSigningKey("different-configured-secret-with-enough-entropy")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != sha256.Size || !bytes.Equal(first, second) {
		t.Fatal("same persisted secret did not derive one stable npm key")
	}
	if bytes.Equal(first, pypi) {
		t.Fatal("npm and PyPI artifact tokens share one signing domain")
	}
	if bytes.Equal(first, otherSecret) {
		t.Fatal("different persisted secrets derived the same npm key")
	}
}

func TestNPMTarballSigningKeyIsRequiredOnlyWhenNPMIsActive(t *testing.T) {
	key, err := deriveActiveNPMTarballSigningKey(developmentJWTSecret, []string{"pypi", "cargo"})
	if err != nil {
		t.Fatalf("inactive npm rejected loopback development secret: %v", err)
	}
	if key != nil {
		t.Fatalf("inactive npm derived signing key %x, want nil", key)
	}

	key, err = deriveActiveNPMTarballSigningKey(developmentJWTSecret, []string{"pypi", "npm"})
	if err != nil {
		t.Fatalf("active npm rejected the loopback development secret: %v", err)
	}
	if len(key) != sha256.Size {
		t.Fatalf("active npm development signing key length = %d, want %d", len(key), sha256.Size)
	}

	key, err = deriveActiveNPMTarballSigningKey(
		"configured-test-secret-with-enough-entropy",
		[]string{"npm"},
	)
	if err != nil {
		t.Fatalf("active npm rejected strong JWT secret: %v", err)
	}
	if len(key) != sha256.Size {
		t.Fatalf("active npm signing key length = %d, want %d", len(key), sha256.Size)
	}

	if _, err := deriveActiveNPMTarballSigningKey("too-short", []string{"npm"}); err == nil {
		t.Fatal("active npm accepted a weak non-placeholder JWT secret")
	}
}
