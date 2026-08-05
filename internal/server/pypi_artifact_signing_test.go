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

func TestDerivePyPIArtifactSigningKeyDoesNotUsePublicDevelopmentSecret(t *testing.T) {
	first, err := derivePyPIArtifactSigningKey(developmentJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	second, err := derivePyPIArtifactSigningKey(developmentJWTSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != sha256.Size || len(second) != sha256.Size {
		t.Fatalf("ephemeral key lengths = %d and %d, want %d", len(first), len(second), sha256.Size)
	}
	if bytes.Equal(first, second) {
		t.Fatal("public development secret produced a stable forgeable artifact key")
	}
}
