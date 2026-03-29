package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 23333)
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "./data/repocache.db")
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.path", "./data/cache")
	v.SetDefault("cache.max_size_gb", 20)
	v.SetDefault("cache.ttl_index", "5m")
	v.SetDefault("cache.ttl_blob", "72h")
	v.SetDefault("cache.lru_threshold", 90)
	v.SetDefault("auth.enabled", true)
	v.SetDefault("auth.jwt_secret", "change-me-in-production")
	v.SetDefault("auth.token_ttl", "168h")

	// Config file path from env or default
	configPath := os.Getenv("REPOCACHE_CONFIG")
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("toml")
		v.AddConfigPath(".")
		v.AddConfigPath("/app")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		zap.L().Warn("config file not found, using defaults")
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Parse duration fields from string
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

	// Warn if JWT secret is still the default placeholder
	if cfg.Auth.JWTSecret == "change-me-in-production" {
		zap.L().Warn("⚠ auth.jwt_secret is using the default value — this is INSECURE for production. Please set a strong secret in your config file or via REPOCACHE_AUTH_JWT_SECRET environment variable.")
	}

	return cfg, nil
}
