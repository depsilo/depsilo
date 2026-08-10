package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("ConfigVersion = %d, want %d", cfg.ConfigVersion, CurrentConfigVersion)
	}
	if cfg.Server.Port != 23333 {
		t.Fatalf("Server.Port = %d, want 23333", cfg.Server.Port)
	}
	if cfg.SupplyChain.MinReleaseAgeEnabled == nil || *cfg.SupplyChain.MinReleaseAgeEnabled {
		t.Fatal("config.example.toml must explicitly default minimum release age to disabled")
	}

	currentDefaultUpstreams := []struct {
		ecosystem string
		upstreams []UpstreamConfig
		index     int
		name      string
		url       string
		priority  int
	}{
		{ecosystem: "cargo", upstreams: cfg.Cargo.Upstreams, index: 0, name: "rsproxy", url: "https://rsproxy.cn/index/", priority: 1},
		{ecosystem: "maven", upstreams: cfg.Maven.Upstreams, index: 1, name: "central", url: "https://repo.maven.apache.org/maven2/", priority: 2},
		{ecosystem: "rubygems", upstreams: cfg.RubyGems.Upstreams, index: 0, name: "tuna", url: "https://mirrors.tuna.tsinghua.edu.cn/rubygems/", priority: 1},
		{ecosystem: "composer", upstreams: cfg.Composer.Upstreams, index: 0, name: "aliyun", url: "https://mirrors.aliyun.com/composer/", priority: 1},
	}
	for _, expected := range currentDefaultUpstreams {
		t.Run(expected.ecosystem+"/"+expected.name, func(t *testing.T) {
			if len(expected.upstreams) <= expected.index {
				t.Fatalf("%s defaults contain %d upstreams, want index %d", expected.ecosystem, len(expected.upstreams), expected.index)
			}
			upstream := expected.upstreams[expected.index]
			if upstream.Name != expected.name || upstream.URL != expected.url || upstream.Priority != expected.priority {
				t.Fatalf("%s default upstream %d = %#v, want name=%q url=%q priority=%d", expected.ecosystem, expected.index, upstream, expected.name, expected.url, expected.priority)
			}
		})
	}
}

func TestLoadRejectsUnknownConfigKey(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte("config_version = 1\n[server]\nhost = \"127.0.0.1\"\nprot = 23333\n")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted an unknown config key")
	}
	if !strings.Contains(err.Error(), "prot") || !strings.Contains(err.Error(), "unknown keys") {
		t.Fatalf("Load error = %q, want the unknown key named", err)
	}
}

func TestLoadRejectsConfigFromNewerBinary(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte("config_version = 999\n[server]\nhost = \"127.0.0.1\"\n")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted config from a newer binary")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") {
		t.Fatalf("Load error = %q, want actionable downgrade refusal", err)
	}
}

func TestLoadConfigVersionCannotBeOverriddenByEnvironment(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte("config_version = 999\n[server]\nhost = \"127.0.0.1\"\n")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_CONFIG_VERSION", "1")

	_, err := Load()
	if err == nil {
		t.Fatal("Load allowed an environment variable to mask the file schema version")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") {
		t.Fatalf("Load error = %q, want file-version downgrade refusal", err)
	}
}

func TestLoadRejectsFutureVersionBeforeDecodingFutureKeys(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	document := []byte("config_version = 999\n[future_feature]\nenabled = true\n")
	if err := os.WriteFile(configPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted config from a newer binary")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") || strings.Contains(err.Error(), "invalid keys") {
		t.Fatalf("Load error = %q, want version refusal before future-key decoding", err)
	}
}

func TestLoadMigratesUnversionedConfigInMemory(t *testing.T) {
	setTestJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[server]\nhost = \"127.0.0.1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load legacy config: %v", err)
	}
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("ConfigVersion = %d, want in-memory migration to %d", cfg.ConfigVersion, CurrentConfigVersion)
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
