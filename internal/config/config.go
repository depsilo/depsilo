package config

import "time"

type Config struct {
	IsDefault                  bool               `mapstructure:"-" json:"-"` // true when no config file found
	ConfigPath                 string             `mapstructure:"-" json:"-"` // resolved path for config file
	ExplicitUpstreamEcosystems map[string]bool    `mapstructure:"-" json:"-"`
	Server                     ServerConfig       `mapstructure:"server"`
	Database                   DatabaseConfig     `mapstructure:"database"`
	Storage                    StorageConfig      `mapstructure:"storage"`
	Cache                      CacheConfig        `mapstructure:"cache"`
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
	ExtraIndexes               []ExtraIndexConfig `mapstructure:"extra_indexes"`
	Webhooks                   []WebhookConfig    `mapstructure:"webhooks"`
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
	MinReleaseAge map[string]string `mapstructure:"min_release_age"`
	Mode          string            `mapstructure:"mode"`
	Allow         []string          `mapstructure:"allow"`
	FailClosed    *bool             `mapstructure:"fail_closed"`
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
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	LogLevel string `mapstructure:"log_level"`
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

type ExtraIndexConfig struct {
	Name      string           `mapstructure:"name"`
	Path      string           `mapstructure:"path"`
	Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}

type UpstreamConfig struct {
	Name          string `mapstructure:"name"`
	URL           string `mapstructure:"url"`
	Priority      int    `mapstructure:"priority"`
	Proxy         string `mapstructure:"proxy"`
	ProbeMode     string `mapstructure:"probe_mode"`
	ProbeInterval string `mapstructure:"probe_interval"`
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
