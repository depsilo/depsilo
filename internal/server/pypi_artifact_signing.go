package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

const developmentJWTSecret = "change-me-in-production"

const (
	pypiArtifactSigningDomain = "depsilo/pypi-artifact-reference/v1"
	npmTarballSigningDomain   = "depsilo/npm-packument-tarball/v1"
)

// derivePyPIArtifactSigningKey separates artifact-reference signatures from
// JWT signatures. Configured deployments use the persistent JWT root; the
// documented loopback development placeholder produces an ephemeral key.
func derivePyPIArtifactSigningKey(jwtSecret string) ([]byte, error) {
	return deriveArtifactSigningKey(jwtSecret, pypiArtifactSigningDomain)
}

// deriveNPMTarballSigningKey gives npm packument tokens a domain-separated
// deployment key without sharing raw key material with JWTs or PyPI. The
// documented loopback development placeholder produces an ephemeral key.
func deriveNPMTarballSigningKey(jwtSecret string) ([]byte, error) {
	return deriveArtifactSigningKey(jwtSecret, npmTarballSigningDomain)
}

// deriveActiveNPMTarballSigningKey avoids imposing npm's signing requirements
// on configurations where npm is inactive. Active npm delegates to the common
// derivation policy, including its random-key compatibility mode for the exact
// loopback-only development placeholder.
func deriveActiveNPMTarballSigningKey(jwtSecret string, activeEcosystems []string) ([]byte, error) {
	for _, ecosystem := range activeEcosystems {
		if ecosystem == "npm" {
			return deriveNPMTarballSigningKey(jwtSecret)
		}
	}
	return nil, nil
}

func deriveArtifactSigningKey(jwtSecret, domain string) ([]byte, error) {
	if domain == "" {
		return nil, errors.New("artifact signing key domain is empty")
	}
	if jwtSecret == developmentJWTSecret {
		// config.Load permits this documented placeholder only on a loopback
		// listener. Preserve local-development startup without deriving a
		// forgeable key from public material: every process gets a fresh key,
		// and signed cache identities rotate with it.
		key := make([]byte, sha256.Size)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate ephemeral artifact signing key: %w", err)
		}
		return key, nil
	}
	if jwtSecret == "" ||
		jwtSecret != strings.TrimSpace(jwtSecret) || len([]byte(jwtSecret)) < sha256.Size {
		return nil, errors.New("auth.jwt_secret must be a persisted, non-placeholder secret of at least 32 bytes for artifact URL signing")
	}

	mac := hmac.New(sha256.New, []byte(jwtSecret))
	_, _ = mac.Write([]byte(domain))
	return mac.Sum(nil), nil
}
