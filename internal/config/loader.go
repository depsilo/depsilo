package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"depsilo/internal/ecosystem"
	"github.com/pelletier/go-toml/v2"
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
	// Keep this key out of setDefaults: its nil state distinguishes a new
	// default-off install from an older config with explicit thresholds.
	if err := v.BindEnv("supply_chain.min_release_age_enabled"); err != nil {
		return nil, fmt.Errorf("bind minimum release age environment override: %w", err)
	}

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
		// Config schema metadata belongs to the document, not the environment.
		// Pin the missing-document version so DEPSILO_CONFIG_VERSION cannot
		// fabricate or mask an on-disk schema version.
		v.Set("config_version", 0)
		zap.L().Warn("config file not found, using defaults — visit the web UI to run setup")

		// When no config file, use ~/.depsilo/ paths as defaults
		if usr, err := user.Current(); err == nil {
			depsiloDir := filepath.Join(usr.HomeDir, ".depsilo")
			v.Set("database.dsn", filepath.Join(depsiloDir, "data", "depsilo.db"))
			v.Set("storage.path", filepath.Join(depsiloDir, "data", "cache"))
			v.Set("compile_cache.storage.path", filepath.Join(depsiloDir, "data", "compile-cache"))
			if resolvedPath == "" {
				resolvedPath = filepath.Join(depsiloDir, "config.toml")
			}
		}
	} else {
		resolvedPath = v.ConfigFileUsed()
		if err := readValidatedConfig(v, resolvedPath); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	cfg, err := decodeViper(v)
	if err != nil {
		return nil, err
	}

	cfg.ExplicitUpstreamEcosystems = explicitUpstreamEcosystems(v)
	cfg.IsDefault = isDefault
	cfg.ConfigPath = resolvedPath
	if isDefault {
		bootstrapToken, generated, err := resolveBootstrapToken()
		if err != nil {
			return nil, err
		}
		cfg.BootstrapToken = bootstrapToken

		// The placeholder must never become a usable signing key, even during
		// setup or when an existing database survives a lost config file.
		if cfg.Auth.JWTSecret == "change-me-in-production" {
			cfg.Auth.JWTSecret, err = NewSecureToken()
			if err != nil {
				return nil, fmt.Errorf("generate temporary JWT secret: %w", err)
			}
		}

		if generated {
			zap.L().Warn("initial setup requires this one-time bootstrap token",
				zap.String("bootstrap_token", bootstrapToken))
		} else {
			zap.L().Info("initial setup is protected by DEPSILO_BOOTSTRAP_TOKEN")
		}
	}

	// License key from env (overrides config file)
	if envKey := os.Getenv("DEPSILO_LICENSE_KEY"); envKey != "" {
		cfg.License.Key = envKey
	}

	// A known signing key on a remotely reachable listener allows forged admin
	// JWTs. Keep loopback-only development compatible, but fail closed anywhere
	// the listener can accept remote traffic.
	if cfg.Auth.JWTSecret == "change-me-in-production" {
		if !isLoopbackHost(cfg.Server.Host) {
			return nil, errors.New("auth.jwt_secret must be changed before listening on a non-loopback address; set DEPSILO_AUTH_JWT_SECRET to a cryptographically random value")
		}
		zap.L().Warn("auth.jwt_secret is using the development placeholder; the server is restricted to loopback")
	}

	if err := ValidateSettingsSnapshot(SettingsSnapshotFromConfig(cfg)); err != nil {
		return nil, err
	}

	return cfg, nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveBootstrapToken() (token string, generated bool, err error) {
	if configured := os.Getenv("DEPSILO_BOOTSTRAP_TOKEN"); configured != "" {
		if configured != strings.TrimSpace(configured) || len(configured) < 24 {
			return "", false, fmt.Errorf("DEPSILO_BOOTSTRAP_TOKEN must be at least 24 characters and have no leading or trailing whitespace")
		}
		return configured, false, nil
	}
	token, err = NewSecureToken()
	if err != nil {
		return "", false, fmt.Errorf("generate bootstrap token: %w", err)
	}
	return token, true, nil
}

func explicitUpstreamEcosystems(v *viper.Viper) map[string]bool {
	out := make(map[string]bool)
	for _, definition := range ecosystem.StandardUpstreamDefinitions() {
		if v.InConfig(definition.Name + ".upstreams") {
			out[definition.Name] = true
		}
	}
	return out
}

func decodeViper(v *viper.Viper) (*Config, error) {
	documentVersion := v.GetInt("config_version")
	if err := validateConfigVersion(documentVersion); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := v.UnmarshalExact(cfg); err != nil {
		return nil, fmt.Errorf("decode config schema: %w; remove unknown keys or consult config.example.toml", err)
	}
	if cfg.ConfigVersion == 0 {
		// Files written before the version field was introduced are schema zero.
		// The first schema has no structural rewrite, so it is safe to migrate in
		// memory while preserving the operator-owned file byte-for-byte.
		cfg.ConfigVersion = CurrentConfigVersion
	}
	resolvedExtraIndexes, err := resolveExtraIndexPresets(cfg.ExtraIndexPresets, cfg.ExtraIndexes)
	if err != nil {
		return nil, err
	}
	cfg.ExtraIndexes = resolvedExtraIndexes
	if err := normalizeExtraIndexes(cfg.ExtraIndexes); err != nil {
		return nil, err
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
	if raw := v.GetString("compile_cache.upload_timeout"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse compile_cache.upload_timeout: %w", err)
		}
		cfg.CompileCache.UploadTimeout = d
	}
	if raw := v.GetString("compile_cache.download_timeout"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse compile_cache.download_timeout: %w", err)
		}
		cfg.CompileCache.DownloadTimeout = d
	}
	cfg.CompileCache.PublicURL = strings.TrimRight(strings.TrimSpace(cfg.CompileCache.PublicURL), "/")
	if err := validateCompileCacheConfig(cfg.CompileCache); err != nil {
		return nil, err
	}

	return cfg, nil
}

func validateConfigVersion(documentVersion int) error {
	if documentVersion > CurrentConfigVersion {
		return fmt.Errorf(
			"config version %d is newer than this binary supports (%d); upgrade Depsilo instead of starting with an older binary",
			documentVersion,
			CurrentConfigVersion,
		)
	}
	if documentVersion < 0 {
		return fmt.Errorf("config_version must be between 0 and %d", CurrentConfigVersion)
	}
	return nil
}

func validateCompileCacheConfig(cfg CompileCacheConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.MaxSizeGB <= 0 {
		return errors.New("compile_cache.max_size_gb must be greater than zero")
	}
	if cfg.MaxEntries <= 0 {
		return errors.New("compile_cache.max_entries must be greater than zero")
	}
	if cfg.MaxEntrySizeMB <= 0 {
		return errors.New("compile_cache.max_entry_size_mb must be greater than zero")
	}
	if cfg.NamespaceMaxSizeGB <= 0 {
		return errors.New("compile_cache.namespace_max_size_gb must be greater than zero")
	}
	if int64(cfg.NamespaceMaxSizeGB)*1024 < int64(cfg.MaxEntrySizeMB) {
		return errors.New("compile_cache.namespace_max_size_gb must be large enough for max_entry_size_mb")
	}
	if cfg.NamespaceMaxEntries <= 0 {
		return errors.New("compile_cache.namespace_max_entries must be greater than zero")
	}
	if cfg.MaxConcurrentUploads <= 0 {
		return errors.New("compile_cache.max_concurrent_uploads must be greater than zero")
	}
	if cfg.MaxQueuedUploads < 0 {
		return errors.New("compile_cache.max_queued_uploads must not be negative")
	}
	if cfg.UploadTimeout <= 0 || cfg.UploadTimeout > 24*time.Hour {
		return errors.New("compile_cache.upload_timeout must be greater than zero and at most 24h")
	}
	if cfg.MaxConcurrentDownloads <= 0 {
		return errors.New("compile_cache.max_concurrent_downloads must be greater than zero")
	}
	if cfg.DownloadTimeout <= 0 || cfg.DownloadTimeout > 24*time.Hour {
		return errors.New("compile_cache.download_timeout must be greater than zero and at most 24h")
	}
	if cfg.MaxInflightUploadSizeMB < cfg.MaxEntrySizeMB {
		return errors.New("compile_cache.max_inflight_upload_size_mb must be at least max_entry_size_mb")
	}
	if cfg.LRUThreshold < 1 || cfg.LRUThreshold > 100 {
		return errors.New("compile_cache.lru_threshold must be between 1 and 100")
	}
	publicURL, err := url.Parse(strings.TrimSpace(cfg.PublicURL))
	if err != nil || publicURL.Scheme != "http" && publicURL.Scheme != "https" || publicURL.Host == "" ||
		publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" ||
		publicURL.Path != "" && publicURL.Path != "/" {
		return errors.New("compile_cache.public_url must be an absolute http(s) origin without a path, query, fragment, or credentials")
	}
	if publicURL.Scheme == "http" && !isLoopbackHost(publicURL.Hostname()) && !cfg.AllowInsecureHTTP {
		return errors.New("compile_cache.public_url must use https for remote clients; set allow_insecure_http=true only for a trusted LAN/VPN")
	}
	switch cfg.Storage.Type {
	case "local":
		if strings.TrimSpace(cfg.Storage.Path) == "" {
			return errors.New("compile_cache.storage.path must not be empty for local storage")
		}
	case "s3":
		if strings.TrimSpace(cfg.Storage.Bucket) == "" {
			return errors.New("compile_cache.storage.bucket must not be empty for s3 storage")
		}
	default:
		return fmt.Errorf("compile_cache.storage.type must be local or s3, got %q", cfg.Storage.Type)
	}
	return nil
}

func decodeConfigDocument(data []byte) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	if len(data) > 0 {
		if err := pinDocumentConfigVersion(v, data); err != nil {
			return nil, err
		}
		if err := validateConfigDocumentSyntax(data); err != nil {
			return nil, err
		}
		v.SetConfigType("toml")
		if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	} else {
		v.Set("config_version", 0)
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

func readValidatedConfig(v *viper.Viper, path string) error {
	document, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := pinDocumentConfigVersion(v, document); err != nil {
		return err
	}
	if err := validateConfigDocumentSyntax(document); err != nil {
		return err
	}
	return v.ReadConfig(bytes.NewReader(document))
}

func pinDocumentConfigVersion(v *viper.Viper, document []byte) error {
	var metadata struct {
		ConfigVersion *int `toml:"config_version"`
	}
	if err := toml.Unmarshal(document, &metadata); err != nil {
		return fmt.Errorf("parse config metadata: %w", err)
	}
	version := 0
	if metadata.ConfigVersion != nil {
		version = *metadata.ConfigVersion
	}
	// Viper.Set has higher precedence than environment variables. This is
	// intentional: schema metadata describes the file itself and must not be
	// spoofable via DEPSILO_CONFIG_VERSION.
	v.Set("config_version", version)
	return validateConfigVersion(version)
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 23333)
	v.SetDefault("server.log_level", "info")
	v.SetDefault("server.trusted_proxies", []string{})
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("database.dsn", "./data/depsilo.db")
	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.path", "./data/cache")
	v.SetDefault("cache.max_size_gb", 20)
	v.SetDefault("cache.ttl_index", "5m")
	v.SetDefault("cache.ttl_blob", "72h")
	v.SetDefault("cache.lru_threshold", 90)
	v.SetDefault("compile_cache.enabled", false)
	v.SetDefault("compile_cache.public_url", "")
	v.SetDefault("compile_cache.allow_insecure_http", false)
	v.SetDefault("compile_cache.max_size_gb", 20)
	v.SetDefault("compile_cache.max_entries", 500000)
	v.SetDefault("compile_cache.max_entry_size_mb", 512)
	v.SetDefault("compile_cache.namespace_max_size_gb", 20)
	v.SetDefault("compile_cache.namespace_max_entries", 250000)
	v.SetDefault("compile_cache.max_concurrent_uploads", 8)
	v.SetDefault("compile_cache.max_queued_uploads", 32)
	v.SetDefault("compile_cache.max_inflight_upload_size_mb", 1024)
	v.SetDefault("compile_cache.upload_timeout", "15m")
	v.SetDefault("compile_cache.max_concurrent_downloads", 64)
	v.SetDefault("compile_cache.download_timeout", "15m")
	v.SetDefault("compile_cache.lru_threshold", 90)
	v.SetDefault("compile_cache.storage.type", "local")
	v.SetDefault("compile_cache.storage.path", "./data/compile-cache")
	v.SetDefault("upstream_updates.enabled", true)
	v.SetDefault("upstream_updates.check_interval", "1h")
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
