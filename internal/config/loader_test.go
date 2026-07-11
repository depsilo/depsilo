package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadEnvironmentOverridesWizardStoragePaths(t *testing.T) {
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
}

func TestConfigExampleLoadsWithCurrentSchema(t *testing.T) {
	t.Setenv("DEPSILO_CONFIG", filepath.Join("..", "..", "config.example.toml"))

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
