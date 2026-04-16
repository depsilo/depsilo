package config

import "time"

type Config struct {
	IsDefault  bool   `mapstructure:"-" json:"-"`  // true when no config file found
	ConfigPath string `mapstructure:"-" json:"-"`  // resolved path for config file
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Cache    CacheConfig    `mapstructure:"cache"`
	Auth     AuthConfig     `mapstructure:"auth"`
	PyPI     AdapterConfig  `mapstructure:"pypi"`
	APT      AdapterConfig  `mapstructure:"apt"`
	NPM      AdapterConfig  `mapstructure:"npm"`
	Go       AdapterConfig  `mapstructure:"go"`
	Cargo    AdapterConfig  `mapstructure:"cargo"`
	Maven    AdapterConfig  `mapstructure:"maven"`
	RubyGems AdapterConfig  `mapstructure:"rubygems"`
	Composer AdapterConfig  `mapstructure:"composer"`
	NuGet    AdapterConfig  `mapstructure:"nuget"`
	Conda    AdapterConfig  `mapstructure:"conda"`
	CRAN     AdapterConfig  `mapstructure:"cran"`
	Helm     AdapterConfig  `mapstructure:"helm"`
	Docker   DockerConfig   `mapstructure:"docker"`
	License  LicenseConfig  `mapstructure:"license"`
	Security SecurityConfig `mapstructure:"security"`
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
	Driver string `mapstructure:"driver"` // sqlite | postgres
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

type AdapterConfig struct {
	Upstreams []UpstreamConfig `mapstructure:"upstreams"`
}

type UpstreamConfig struct {
	Name     string `mapstructure:"name"`
	URL      string `mapstructure:"url"`
	Priority int    `mapstructure:"priority"`
	Proxy    string `mapstructure:"proxy"`
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
