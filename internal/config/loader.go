package config

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func Load() (*Config, error) {
	v := viper.New()

	// Environment overrides via viper. DEPSILO_SERVER_PORT overrides
	// [server] port, DEPSILO_DATABASE_DRIVER overrides [database]
	// driver, etc. — the key replacer maps "server.port" → "_SERVER_PORT"
	// and the prefix prepends "DEPSILO". Standard 12-factor pattern.
	// Precedence (highest wins): CLI flags (which call os.Setenv) →
	// env vars → config file → SetDefault values below.
	v.SetEnvPrefix("DEPSILO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	// Config file path resolution
	configPath := os.Getenv("DEPSILO_CONFIG")
	isDefault := false
	resolvedPath := ""

	if configPath != "" {
		v.SetConfigFile(configPath)
		resolvedPath = configPath
	} else {
		v.SetConfigName("config")
		v.SetConfigType("toml")
		v.AddConfigPath(".")
		v.AddConfigPath("/app")
		// Also search ~/.depsilo/
		if usr, err := user.Current(); err == nil {
			depsiloDir := filepath.Join(usr.HomeDir, ".depsilo")
			v.AddConfigPath(depsiloDir)
			resolvedPath = filepath.Join(depsiloDir, "config.toml")
		}
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		isDefault = true
		zap.L().Warn("config file not found, using defaults — visit the web UI to run setup")

		// When no config file, use ~/.depsilo/ paths as defaults
		if usr, err := user.Current(); err == nil {
			depsiloDir := filepath.Join(usr.HomeDir, ".depsilo")
			v.Set("database.dsn", filepath.Join(depsiloDir, "data", "depsilo.db"))
			v.Set("storage.path", filepath.Join(depsiloDir, "data", "cache"))
			if resolvedPath == "" {
				resolvedPath = filepath.Join(depsiloDir, "config.toml")
			}
		}
	} else {
		resolvedPath = v.ConfigFileUsed()
		if err := readSanitizedConfig(v, resolvedPath); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	cfg, err := decodeViper(v)
	if err != nil {
		return nil, err
	}

	cfg.IsDefault = isDefault
	cfg.ConfigPath = resolvedPath

	// License key from env (overrides config file)
	if envKey := os.Getenv("DEPSILO_LICENSE_KEY"); envKey != "" {
		cfg.License.Key = envKey
	}

	// Warn if JWT secret is still the default placeholder
	if cfg.Auth.JWTSecret == "change-me-in-production" {
		zap.L().Warn("⚠ auth.jwt_secret is using the default value — this is INSECURE for production. Please set a strong secret in your config file or via DEPSILO_AUTH_JWT_SECRET environment variable.")
	}

	if err := ValidateSettingsSnapshot(SettingsSnapshotFromConfig(cfg)); err != nil {
		return nil, err
	}

	return cfg, nil
}

func decodeViper(v *viper.Viper) (*Config, error) {
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if raw := v.GetString("cache.ttl_index"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse cache.ttl_index: %w", err)
		}
		cfg.Cache.TTLIndex = d
	}
	if raw := v.GetString("cache.ttl_blob"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse cache.ttl_blob: %w", err)
		}
		cfg.Cache.TTLBlob = d
	}
	if raw := v.GetString("auth.token_ttl"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse auth.token_ttl: %w", err)
		}
		cfg.Auth.TokenTTL = d
	}
	if raw := v.GetString("access_log.batch_interval"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse access_log.batch_interval: %w", err)
		}
		cfg.AccessLog.BatchInterval = d
	}

	return cfg, nil
}

func decodeConfigDocument(data []byte) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	if len(data) > 0 {
		sanitized, err := sanitizeConfigDocumentForViper(data)
		if err != nil {
			return nil, err
		}
		v.SetConfigType("toml")
		if err := v.ReadConfig(bytes.NewReader(sanitized)); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	cfg, err := decodeViper(v)
	if err != nil {
		return nil, err
	}
	if err := ValidateSettingsSnapshot(SettingsSnapshotFromConfig(cfg)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func readSanitizedConfig(v *viper.Viper, path string) error {
	document, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sanitized, err := sanitizeConfigDocumentForViper(document)
	if err != nil {
		return err
	}
	return v.ReadConfig(bytes.NewReader(sanitized))
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 23333)
	v.SetDefault("server.log_level", "info")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "./data/depsilo.db")
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.path", "./data/cache")
	v.SetDefault("cache.max_size_gb", 20)
	v.SetDefault("cache.ttl_index", "5m")
	v.SetDefault("cache.ttl_blob", "72h")
	v.SetDefault("cache.lru_threshold", 90)
	v.SetDefault("auth.enabled", true)
	v.SetDefault("auth.jwt_secret", "change-me-in-production")
	v.SetDefault("auth.token_ttl", "168h")
	// Access log rollup. retention_days bounds the raw access_logs table
	// at 7 days of detail (the admin "recent logs" page); rollup retention
	// keeps a year of aggregated dashboards. Operators who upgrade and
	// want to keep the historical raw rows can override both to 0 in
	// config.toml to disable sweeping entirely. Rollup writes themselves
	// are on by default — flip to false to fall back to raw-only writes.
	v.SetDefault("access_log.retention_days", 7)
	v.SetDefault("access_log.batch_size", 100)
	v.SetDefault("access_log.batch_interval", "5s")
	v.SetDefault("access_log.rollup_enabled", true)
	v.SetDefault("access_log.rollup_retention_days", 365)
	v.SetDefault("access_log.backfill_on_start", true)
	v.SetDefault("alpine.upstreams", []map[string]any{
		{
			"name":     "tuna",
			"url":      "https://mirrors.tuna.tsinghua.edu.cn/alpine",
			"priority": 1,
		},
		{
			"name":     "official",
			"url":      "https://dl-cdn.alpinelinux.org/alpine",
			"priority": 2,
		},
	})
}
