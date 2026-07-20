package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadTracksOnlyFileExplicitOrdinaryUpstreams(t *testing.T) {
	setTestJWTSecret(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	document := []byte(`
[[pypi.upstreams]]
name = "primary"
url = "https://pypi.org"
priority = 1
`)
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ExplicitUpstreamEcosystems["pypi"] {
		t.Fatal("file-explicit pypi was not recorded")
	}
	if cfg.ExplicitUpstreamEcosystems["alpine"] {
		t.Fatal("defaulted alpine was recorded as file-explicit")
	}
	if len(cfg.Alpine.Upstreams) == 0 {
		t.Fatal("test did not retain the built-in Alpine runtime default")
	}
}

func TestExplicitUpstreamEcosystemsIgnoresEnvironmentOnlyValues(t *testing.T) {
	v := viper.New()
	v.SetEnvPrefix("DEPSILO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	t.Setenv("DEPSILO_PYPI_UPSTREAMS", `[{"name":"env","url":"https://env.example","priority":1}]`)
	if err := v.BindEnv("pypi.upstreams"); err != nil {
		t.Fatal(err)
	}
	if !v.IsSet("pypi.upstreams") {
		t.Fatal("test did not establish an environment-only upstream value")
	}
	if explicitUpstreamEcosystems(v)["pypi"] {
		t.Fatal("environment-only pypi was recorded as file-explicit")
	}
}
