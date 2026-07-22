package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func setTestJWTSecret(t *testing.T) {
	t.Helper()
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")
}

func TestLoadEnvironmentOverridesWizardStoragePaths(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte(`[database]
driver = "sqlite"
dsn = "./data/depsilo.db"

[storage]
type = "local"
path = "./data/cache"
`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_DATABASE_DSN", "/root/.depsilo/data/depsilo.db")
	t.Setenv("DEPSILO_STORAGE_PATH", "/root/.depsilo/data/cache")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.Database.DSN, "/root/.depsilo/data/depsilo.db"; got != want {
		t.Errorf("Database.DSN = %q, want %q", got, want)
	}
	if got, want := cfg.Storage.Path, "/root/.depsilo/data/cache"; got != want {
		t.Errorf("Storage.Path = %q, want %q", got, want)
	}
	if cfg.SupplyChain.MinReleaseAgeEnabled != nil {
		t.Fatal("omitted minimum release age switch must remain nil for compatibility resolution")
	}
}

func TestConfigExampleLoadsWithCurrentSchema(t *testing.T) {
	t.Setenv("DEPSILO_CONFIG", filepath.Join("..", "..", "config.example.toml"))
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load config.example.toml: %v", err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("Database.Driver = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.Server.Port != 23333 {
		t.Fatalf("Server.Port = %d, want 23333", cfg.Server.Port)
	}
	if cfg.SupplyChain.MinReleaseAgeEnabled == nil || *cfg.SupplyChain.MinReleaseAgeEnabled {
		t.Fatal("config.example.toml must explicitly default minimum release age to disabled")
	}
}

func TestLoadMinimumReleaseAgeEnvironmentOverrideWithoutConfigKey(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[server]\nhost = \"127.0.0.1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_SUPPLY_CHAIN_MIN_RELEASE_AGE_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SupplyChain.MinReleaseAgeEnabled == nil || !*cfg.SupplyChain.MinReleaseAgeEnabled {
		t.Fatal("environment-only minimum release age override was not decoded")
	}
}

func TestLoadMinimumReleaseAgeEnvironmentOverrideWinsOverConfig(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte("[server]\nhost = \"127.0.0.1\"\n[supply_chain]\nmin_release_age_enabled = true\n")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_SUPPLY_CHAIN_MIN_RELEASE_AGE_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SupplyChain.MinReleaseAgeEnabled == nil || *cfg.SupplyChain.MinReleaseAgeEnabled {
		t.Fatal("environment false did not override config true")
	}
}

func TestLoadRejectsPlaceholderJWTSecretOnRemoteListener(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte(`[server]
host = "0.0.0.0"

[auth]
jwt_secret = "change-me-in-production"
`)
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a known JWT secret on a remote listener")
	}
}

func TestLoadAllowsPlaceholderJWTSecretForLoopbackDevelopment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte(`[server]
host = "127.0.0.1"

[auth]
jwt_secret = "change-me-in-production"
`)
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	if _, err := Load(); err != nil {
		t.Fatalf("Load loopback config: %v", err)
	}
}

func TestLoadAllowsRemoteListenerWithJWTSecretEnvironmentOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte(`[server]
host = "0.0.0.0"

[auth]
jwt_secret = "change-me-in-production"
`)
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != "test-only-0123456789abcdef0123456789abcdef" {
		t.Fatalf("JWT secret environment override was not applied")
	}
}

func TestSetDefaultsIncludesAlpineUpstreams(t *testing.T) {
	v := viper.New()
	setDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal defaults: %v", err)
	}

	if len(cfg.Alpine.Upstreams) != 2 {
		t.Fatalf("len(cfg.Alpine.Upstreams) = %d, want 2", len(cfg.Alpine.Upstreams))
	}

	first := cfg.Alpine.Upstreams[0]
	if first.Name != "tuna" {
		t.Errorf("first.Name = %q, want tuna", first.Name)
	}
	if first.URL != "https://mirrors.tuna.tsinghua.edu.cn/alpine" {
		t.Errorf("first.URL = %q, want TUNA Alpine mirror", first.URL)
	}
	if first.Priority != 1 {
		t.Errorf("first.Priority = %d, want 1", first.Priority)
	}

	second := cfg.Alpine.Upstreams[1]
	if second.Name != "official" {
		t.Errorf("second.Name = %q, want official", second.Name)
	}
	if second.URL != "https://dl-cdn.alpinelinux.org/alpine" {
		t.Errorf("second.URL = %q, want official Alpine mirror", second.URL)
	}
	if second.Priority != 2 {
		t.Errorf("second.Priority = %d, want 2", second.Priority)
	}
}
