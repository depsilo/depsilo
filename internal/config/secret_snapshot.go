package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// SnapshotJWTSecret makes a portable backup configuration. Its inline secret
// preserves the instance's signing identity without adding a third restore
// target. Only the archive's private snapshot is changed, never the live file.
func SnapshotJWTSecret(document []byte, configPath string) ([]byte, error) {
	if _, err := decodeConfigDocument(document); err != nil {
		return nil, err
	}
	var source struct {
		Auth struct {
			JWTSecret *string `toml:"jwt_secret"`
		} `toml:"auth"`
	}
	if err := toml.Unmarshal(document, &source); err != nil {
		return nil, err
	}
	secret := os.Getenv("DEPSILO_AUTH_JWT_SECRET")
	if secret != "" {
		if !validSigningSecret(secret) {
			return nil, errors.New("cannot back up an invalid DEPSILO_AUTH_JWT_SECRET")
		}
	} else if source.Auth.JWTSecret != nil {
		// Preserve legacy documents byte-for-byte, including old weak values.
		return document, nil
	} else {
		path, err := ManagedJWTSecretPath(configPath)
		if err != nil {
			return nil, err
		}
		secret, err = readManagedJWTSecret(path)
		if err != nil {
			return nil, fmt.Errorf("cannot back up instance signing key: %w", err)
		}
	}
	updated, err := patchConfigString(document, "auth", "jwt_secret", secret)
	if err != nil {
		return nil, err
	}
	if _, err := decodeConfigDocument(updated); err != nil {
		return nil, errors.New("cannot encode signing key in backup configuration")
	}
	return updated, nil
}
