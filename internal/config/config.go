package config

import (
	"fmt"
	"strings"
	"time"
)

type Config struct {
	ConfigVersion              int                `mapstructure:"config_version" json:"-"`
	IsDefault                  bool               `mapstructure:"-" json:"-"` // true when no config file found
	ConfigPath                 string             `mapstructure:"-" json:"-"` // resolved path for config file
	BootstrapToken             string             `mapstructure:"-" json:"-"` // ephemeral first-run authorization token
	BootstrapTokenGenerated    bool               `mapstructure:"-" json:"-"` // safe to reveal in the first-run console summary
	ExplicitUpstreamEcosystems map[string]bool    `mapstructure:"-" json:"-"`
	Server                     ServerConfig       `mapstructure:"server"`
	Database                   DatabaseConfig     `mapstructure:"database"`
	Storage                    StorageConfig      `mapstructure:"storage"`
	Cache                      CacheConfig        `mapstructure:"cache"`
	CompileCache               CompileCacheConfig `mapstructure:"compile_cache"`
	AccessLog                  AccessLogConfig    `mapstructure:"access_log"`
	Auth                       AuthConfig         `mapstructure:"auth"`
	PyPI                       AdapterConfig      `mapstructure:"pypi"`
	APT                        AdapterConfig      `mapstructure:"apt"`
	NPM                        AdapterConfig      `mapstructure:"npm"`
	Go                         AdapterConfig      `mapstructure:"go"`
	Cargo                      AdapterConfig      `mapstructure:"cargo"`
	Maven                      AdapterConfig      `mapstructure:"maven"`
	RubyGems                   AdapterConfig      `mapstructure:"rubygems"`
	Composer                   AdapterConfig      `mapstructure:"composer"`
	NuGet                      AdapterConfig      `mapstructure:"nuget"`
	Conda                      AdapterConfig      `mapstructure:"conda"`
	CRAN                       AdapterConfig      `mapstructure:"cran"`
	Alpine                     AdapterConfig      `mapstructure:"alpine"`
	Helm                       AdapterConfig      `mapstructure:"helm"`
	HuggingFace                AdapterConfig      `mapstructure:"huggingface"`
	Docker                     DockerConfig       `mapstructure:"docker"`
	License                    LicenseConfig      `mapstructure:"license"`
	Security                   SecurityConfig     `mapstructure:"security"`
	ExtraIndexPresets          ExtraIndexPresets  `mapstructure:"extra_index_presets"`
	ExtraIndexes               []ExtraIndexConfig `mapstructure:"extra_indexes"`
	Webhooks                   []WebhookConfig    `mapstructure:"webhooks"`
	// Custom is an explicit operator-owned extension table. Depsilo preserves
	// it when patching managed settings but never interprets its contents.
	Custom map[string]any `mapstructure:"custom" json:"-"`
	// UpstreamUpdates controls proactive conditional checks for validated
	// PyPI-compatible metadata already in the local cache. It never prefetches
	// artifacts or guesses at changes when an adapter lacks validators.
	UpstreamUpdates UpstreamUpdatesConfig `mapstructure:"upstream_updates"`
	// SupplyChain: minimum-release-age quarantine, malicious blocklist,
	// and tamper detection. The policy struct lives in internal/quarantine to keep
	// duration parsing + allow-list semantics next to the code that
	// consumes them; this config carries the raw operator-facing shape.
	SupplyChain SupplyChainConfig `mapstructure:"supply_chain"`
}

// SupplyChainConfig is the TOML/YAML shape the operator writes. It
// intentionally mirrors quarantine.Config field-for-field — we don't
// re-export it directly so blocklist, tamper detection, and future
// freeze/snapshot features can own sub-blocks without reshaping the
// quarantine package's public API.
type SupplyChainConfig struct {
	// MinReleaseAgeEnabled is a tri-state migration switch. Nil keeps an
	// existing explicit threshold table enabled, while an entirely empty
	// configuration defaults the age gate off. Explicit false always wins.
	MinReleaseAgeEnabled *bool             `mapstructure:"min_release_age_enabled"`
	MinReleaseAge        map[string]string `mapstructure:"min_release_age"`
	Mode                 string            `mapstructure:"mode"`
	Allow                []string          `mapstructure:"allow"`
	FailClosed           *bool             `mapstructure:"fail_closed"`
	// Blocklist mirrors blocklist.Config field-for-field — same
	// convention as the fields above (config carries the raw operator
	// shape; the domain package owns semantics and defaults).
	Blocklist BlocklistConfig `mapstructure:"blocklist"`
	// TamperDetection: content-integrity tracking of immutable
	// artifacts. Enabled by default.
	TamperDetection TamperConfig `mapstructure:"tamper_detection"`
}

// BlocklistConfig is [supply_chain.blocklist]: the known-malicious
// package blocklist (DIRECTION Task 2). Enabled defaults to true;
// sync failures degrade rather than break the proxy.
type BlocklistConfig struct {
	Enabled      *bool  `mapstructure:"enabled"`
	SyncInterval string `mapstructure:"sync_interval"` // default 6h
	MirrorURL    string `mapstructure:"mirror_url"`    // default: official OSV bucket
	Proxy        string `mapstructure:"proxy"`         // HTTP(S) proxy for sync fetches
	Mode         string `mapstructure:"mode"`          // block | warn; default block
}

// TamperConfig is [supply_chain.tamper_detection] (DIRECTION T1).
// Enabled defaults to true; disabling detaches persistence, comparison,
// and alerting. The shared storage reader still computes its streaming hash.
type TamperConfig struct {
	Enabled *bool `mapstructure:"enabled"`
	// ImmutableThresholdSeconds overrides the TTL at/above which an
	// artifact is treated as immutable for tamper detection. 0 (unset)
	// means "derive from cache.ttl_blob" — see server assembly. Tests
	// and unusual deployments set it explicitly.
	ImmutableThresholdSeconds int `mapstructure:"immutable_threshold_seconds"`
}

// IsEnabled applies the default-true semantics.
func (c TamperConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// ImmutableThresholdOverride returns the configured override, or 0 when
// unset (meaning the server should derive the default from ttl_blob).
func (c TamperConfig) ImmutableThresholdOverride() time.Duration {
	if c.ImmutableThresholdSeconds > 0 {
		return time.Duration(c.ImmutableThresholdSeconds) * time.Second
	}
	return 0
}

type WebhookConfig struct {
	Name     string `mapstructure:"name"`
	Platform string `mapstructure:"platform"`
	URL      string `mapstructure:"url"`
	Events   string `mapstructure:"events"`
	Enabled  bool   `mapstructure:"enabled"`
}

type LicenseConfig struct {
	Key string `mapstructure:"key"`
}

type ServerConfig struct {
	Host           string   `mapstructure:"host"`
	Port           int      `mapstructure:"port"`
	LogLevel       string   `mapstructure:"log_level"`
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // sqlite (the only implemented driver)
	DSN    string `mapstructure:"dsn"`
}

type StorageConfig struct {
	Type      string `mapstructure:"type"` // local | s3
	Path      string `mapstructure:"path"`
	Bucket    string `mapstructure:"bucket"`
	Endpoint  string `mapstructure:"endpoint"`
	Region    string `mapstructure:"region"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
}

type CacheConfig struct {
	MaxSizeGB    int           `mapstructure:"max_size_gb"`
	TTLIndex     time.Duration `mapstructure:"ttl_index"`
	TTLBlob      time.Duration `mapstructure:"ttl_blob"`
	LRUThreshold int           `mapstructure:"lru_threshold"`
}

// CompileCacheConfig controls the ccache/sccache-compatible compiler cache. Its
// storage is separate from StorageConfig so compiler artifacts cannot distort
// package-cache capacity accounting or retention.
type CompileCacheConfig struct {
	Enabled                 bool          `mapstructure:"enabled"`
	PublicURL               string        `mapstructure:"public_url"`
	AllowInsecureHTTP       bool          `mapstructure:"allow_insecure_http"`
	MaxSizeGB               int           `mapstructure:"max_size_gb"`
	MaxEntries              int64         `mapstructure:"max_entries"`
	MaxEntrySizeMB          int           `mapstructure:"max_entry_size_mb"`
	NamespaceMaxSizeGB      int           `mapstructure:"namespace_max_size_gb"`
	NamespaceMaxEntries     int64         `mapstructure:"namespace_max_entries"`
	MaxConcurrentUploads    int           `mapstructure:"max_concurrent_uploads"`
	MaxQueuedUploads        int           `mapstructure:"max_queued_uploads"`
	MaxInflightUploadSizeMB int           `mapstructure:"max_inflight_upload_size_mb"`
	UploadTimeout           time.Duration `mapstructure:"upload_timeout"`
	MaxConcurrentDownloads  int           `mapstructure:"max_concurrent_downloads"`
	DownloadTimeout         time.Duration `mapstructure:"download_timeout"`
	LRUThreshold            int           `mapstructure:"lru_threshold"`
	Storage                 StorageConfig `mapstructure:"storage"`
}

type AuthConfig struct {
	Enabled   bool          `mapstructure:"enabled"`
	JWTSecret string        `mapstructure:"jwt_secret"`
	TokenTTL  time.Duration `mapstructure:"token_ttl"`
}

// AccessLogConfig tunes the access-log rollup pipeline.
//
// Behavior summary:
//   - When RollupEnabled is true, every proxy request is buffered through a
//     channel and aggregated in memory before being upserted into the
//     access_log_hourly + access_log_package_daily tables in batches. Raw
//     rows continue to be written to access_logs so the admin detail page
//     keeps working.
//   - RetentionDays and RollupRetentionDays drive a background sweeper that
//     deletes rows older than the configured horizon. Both default to 0,
//     which DISABLES the sweep so an operator who upgrades and immediately
//     panics doesn't lose history. A later commit raises the defaults to
//     the spec-recommended 7d/365d once the rollout has soaked.
//   - BatchSize / BatchInterval gate when the recorder flushes its buffer.
//     Flush triggers on whichever happens first.
//   - BackfillOnStart drives a one-shot INSERT...SELECT from access_logs
//     into the rollup tables when they look empty, so the first dashboard
//     after rollup is enabled isn't blank.
type AccessLogConfig struct {
	RetentionDays       int           `mapstructure:"retention_days"`
	BatchSize           int           `mapstructure:"batch_size"`
	BatchInterval       time.Duration `mapstructure:"batch_interval"`
	RollupEnabled       bool          `mapstructure:"rollup_enabled"`
	RollupRetentionDays int           `mapstructure:"rollup_retention_days"`
	BackfillOnStart     bool          `mapstructure:"backfill_on_start"`
}

type AdapterConfig struct {
	Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}

// ExtraIndexPresets controls the third-party Python index families bundled
// with Depsilo. An absent enabled value keeps the built-in catalog enabled;
// operators can disable the catalog wholesale or suppress individual entries.
type ExtraIndexPresets struct {
	Enabled  *bool    `mapstructure:"enabled"`
	Disabled []string `mapstructure:"disabled"`
}

type ExtraIndexConfig struct {
	Name       string           `mapstructure:"name"`
	Kind       string           `mapstructure:"kind"`
	Path       string           `mapstructure:"path"`
	SimplePath string           `mapstructure:"simple_path"`
	Upstreams  []UpstreamConfig `mapstructure:"upstreams"`
}

type UpstreamConfig struct {
	Name          string `mapstructure:"name"`
	URL           string `mapstructure:"url"`
	Priority      int    `mapstructure:"priority"`
	Proxy         string `mapstructure:"proxy"`
	ProbeMode     string `mapstructure:"probe_mode"`
	ProbeInterval string `mapstructure:"probe_interval"`
}

// UpstreamUpdatesConfig controls the optional package-metadata watcher.
// CheckInterval is deliberately a string so "off" remains an operator-facing
// value alongside normal Go durations such as "1h" and "30m".
type UpstreamUpdatesConfig struct {
	Enabled       *bool  `mapstructure:"enabled"`
	CheckInterval string `mapstructure:"check_interval"`
}

// IsEnabled keeps proactive checks on by default, matching the other
// supply-chain background jobs.
func (c UpstreamUpdatesConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// ParseUpdateCheckInterval accepts normal Go durations and the operator-facing
// "off" / "0" values used to disable proactive metadata checks globally.
func ParseUpdateCheckInterval(raw string) (interval time.Duration, enabled bool, err error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		raw = "1h"
	}
	if raw == "off" || raw == "0" {
		return 0, false, nil
	}
	interval, err = time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		if err == nil {
			err = fmt.Errorf("must be greater than zero")
		}
		return 0, false, fmt.Errorf("invalid update check interval %q: %w", raw, err)
	}
	return interval, true, nil
}

type DockerConfig struct {
	DefaultRegistry string           `mapstructure:"default_registry"`
	Registries      []RegistryConfig `mapstructure:"registries"`
}

type RegistryConfig struct {
	Name     string `mapstructure:"name"`
	URL      string `mapstructure:"url"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Proxy    string `mapstructure:"proxy"`
}

type SecurityConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	OSVURL       string        `mapstructure:"osv_api_url"`
	ScanInterval time.Duration `mapstructure:"scan_interval"`
	CheckTTL     time.Duration `mapstructure:"check_ttl"`
	Proxy        string        `mapstructure:"proxy"`
}
