package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func setTestJWTSecret(t *testing.T) {
	t.Helper()
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")
}

func TestLoadMissingConfigUsesHomeStateDefaultsWithoutBlockingEnvironmentOverrides(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("DEPSILO_CONFIG", "")
	t.Setenv("DEPSILO_SERVER_HOST", "")
	t.Setenv("DEPSILO_DATABASE_DSN", filepath.Join(home, "custom", "database.db"))
	t.Setenv("DEPSILO_STORAGE_PATH", filepath.Join(home, "custom", "package-cache"))
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_PATH", filepath.Join(home, "custom", "compile-cache"))
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "test-bootstrap-token-0123456789")
	setTestJWTSecret(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load without config: %v", err)
	}
	if !cfg.IsDefault {
		t.Fatal("Load without config did not report default setup state")
	}
	if cfg.BootstrapTokenGenerated {
		t.Fatal("explicit DEPSILO_BOOTSTRAP_TOKEN was marked safe to reveal")
	}
	if got, want := cfg.ConfigPath, filepath.Join(home, ".depsilo", "config.toml"); got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
	if got, want := cfg.Server.Host, "127.0.0.1"; got != want {
		t.Errorf("Server.Host = %q, want %q", got, want)
	}
	if got, want := cfg.Database.DSN, filepath.Join(home, "custom", "database.db"); got != want {
		t.Errorf("Database.DSN = %q, want environment override %q", got, want)
	}
	if got, want := cfg.Storage.Path, filepath.Join(home, "custom", "package-cache"); got != want {
		t.Errorf("Storage.Path = %q, want environment override %q", got, want)
	}
	if got, want := cfg.CompileCache.Storage.Path, filepath.Join(home, "custom", "compile-cache"); got != want {
		t.Errorf("CompileCache.Storage.Path = %q, want environment override %q", got, want)
	}
}

func TestLoadMissingConfigPlacesAllLocalStateUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Chdir(t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("DEPSILO_CONFIG", "")
	t.Setenv("DEPSILO_SERVER_HOST", "")
	t.Setenv("DEPSILO_DATABASE_DSN", "")
	t.Setenv("DEPSILO_STORAGE_PATH", "")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_PATH", "")
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "")
	setTestJWTSecret(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load without config: %v", err)
	}
	if !cfg.BootstrapTokenGenerated || len(cfg.BootstrapToken) < 24 {
		t.Fatalf("automatic bootstrap token was not marked for the first-run summary")
	}
	stateDir := filepath.Join(home, ".depsilo")
	if got, want := cfg.Database.DSN, filepath.Join(stateDir, "data", "depsilo.db"); got != want {
		t.Errorf("Database.DSN = %q, want %q", got, want)
	}
	if got, want := cfg.Storage.Path, filepath.Join(stateDir, "data", "cache"); got != want {
		t.Errorf("Storage.Path = %q, want %q", got, want)
	}
	if got, want := cfg.CompileCache.Storage.Path, filepath.Join(stateDir, "data", "compile-cache"); got != want {
		t.Errorf("CompileCache.Storage.Path = %q, want %q", got, want)
	}
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

func TestLoadPackageS3FieldsFromEnvironment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("config_version = 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_STORAGE_TYPE", "s3")
	t.Setenv("DEPSILO_STORAGE_BUCKET", "env-package-bucket")
	t.Setenv("DEPSILO_STORAGE_ENDPOINT", "https://s3.env.example.test")
	t.Setenv("DEPSILO_STORAGE_REGION", "eu-west-1")
	t.Setenv("DEPSILO_STORAGE_ACCESS_KEY", "env-package-access")
	secret := "env-package-secret-not-for-logs"
	t.Setenv("DEPSILO_STORAGE_SECRET_KEY", secret)

	core, observedLogs := observer.New(zap.DebugLevel)
	restoreLogger := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restoreLogger)

	cfg, err := Load()
	if err != nil {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("Load exposed the S3 secret in its error")
		}
		t.Fatalf("Load package S3 environment: %v", err)
	}
	if cfg.Storage.Type != "s3" ||
		cfg.Storage.Bucket != "env-package-bucket" ||
		cfg.Storage.Endpoint != "https://s3.env.example.test" ||
		cfg.Storage.Region != "eu-west-1" ||
		cfg.Storage.AccessKey != "env-package-access" ||
		cfg.Storage.SecretKey != secret {
		t.Fatal("Load did not apply every package S3 environment field")
	}
	for _, entry := range observedLogs.All() {
		if strings.Contains(entry.Message+fmt.Sprint(entry.ContextMap()), secret) {
			t.Fatal("Load exposed the S3 secret in logs")
		}
	}
}

func TestLoadCompileCacheS3FieldsFromEnvironment(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("config_version = 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_COMPILE_CACHE_ENABLED", "true")
	t.Setenv("DEPSILO_COMPILE_CACHE_PUBLIC_URL", "http://127.0.0.1:23333")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_TYPE", "s3")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_BUCKET", "env-compile-bucket")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_ENDPOINT", "https://s3.env.example.test")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_REGION", "ap-southeast-1")
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_ACCESS_KEY", "env-compile-access")
	secret := "env-compile-secret-not-for-logs"
	t.Setenv("DEPSILO_COMPILE_CACHE_STORAGE_SECRET_KEY", secret)

	core, observedLogs := observer.New(zap.DebugLevel)
	restoreLogger := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restoreLogger)

	cfg, err := Load()
	if err != nil {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("Load exposed the compiler-cache S3 secret in its error")
		}
		t.Fatalf("Load compiler-cache S3 environment: %v", err)
	}
	storage := cfg.CompileCache.Storage
	if storage.Type != "s3" ||
		storage.Bucket != "env-compile-bucket" ||
		storage.Endpoint != "https://s3.env.example.test" ||
		storage.Region != "ap-southeast-1" ||
		storage.AccessKey != "env-compile-access" ||
		storage.SecretKey != secret {
		t.Fatal("Load did not apply every compiler-cache S3 environment field")
	}
	for _, entry := range observedLogs.All() {
		if strings.Contains(entry.Message+fmt.Sprint(entry.ContextMap()), secret) {
			t.Fatal("Load exposed the compiler-cache S3 secret in logs")
		}
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

func TestLoadRejectsWeakJWTSecretOnRemoteListener(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{name: "empty", secret: ""},
		{name: "short", secret: "guessable-secret"},
		{name: "surrounding whitespace", secret: "  0123456789abcdef0123456789abcdef  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.toml")
			document := []byte(fmt.Sprintf(`[server]
host = "0.0.0.0"

[auth]
jwt_secret = %q
`, tt.secret))
			if err := os.WriteFile(configPath, document, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEPSILO_CONFIG", configPath)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "auth.jwt_secret") {
				t.Fatalf("Load error = %v, want auth.jwt_secret rejection", err)
			}
		})
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
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("Server.Host = %q, want loopback default", cfg.Server.Host)
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
