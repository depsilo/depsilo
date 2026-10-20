package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSigningKeySnapshotPreservesSupportedConfigForms(t *testing.T) {
	for _, document := range []string{
		"config_version = 1\n# header\n[auth]\nenabled = true\n[custom]\nkeep = 'unchanged' # comment\n",
		"auth = {enabled = true}\n[custom]\nkeep = 'unchanged' # comment\n",
		"auth = {}\n[custom]\nkeep = 'unchanged' # comment\n",
		"auth.enabled = true\n[custom]\nkeep = 'unchanged' # comment\n",
		"[custom]\nkeep = 'unchanged' # comment\n",
	} {
		t.Run(document, func(t *testing.T) {
			configPath, _ := managedSecretFixture(t)
			cfg, err := LoadForServer()
			if err != nil {
				t.Fatal(err)
			}
			updated, err := SnapshotJWTSecret([]byte(document), configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(updated), "[custom]\nkeep = 'unchanged' # comment\n") {
				t.Fatal("portable backup modified unrelated config")
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, updated, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEPSILO_CONFIG", path)
			restored, err := Load()
			if err != nil || restored.Auth.JWTSecret != cfg.Auth.JWTSecret {
				t.Fatal("portable config lost its original signing identity")
			}
		})
	}
}
