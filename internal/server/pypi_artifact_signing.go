package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

const developmentJWTSecret = "change-me-in-production"

// derivePyPIArtifactSigningKey separates artifact-reference signatures from
// JWT signatures even though both are rooted in the deployment's persistent
// secret. Loopback development may still use the documented placeholder; in
// that case use an ephemeral random key instead of exposing a forgeable token
// format with a public key.
func derivePyPIArtifactSigningKey(jwtSecret string) ([]byte, error) {
	if jwtSecret == "" || jwtSecret == developmentJWTSecret {
		key := make([]byte, sha256.Size)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate PyPI artifact signing key: %w", err)
		}
		return key, nil
	}

	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte("depsilo/pypi-artifact-reference/v1"))
	return mac.Sum(nil), nil
}
