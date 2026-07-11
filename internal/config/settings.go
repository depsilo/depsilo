package config

import (
	"fmt"
	"strings"
	"time"
)

type SettingPath string

const (
	SettingServerHost        SettingPath = "server.host"
	SettingServerPort        SettingPath = "server.port"
	SettingServerLogLevel    SettingPath = "server.log_level"
	SettingDatabaseDriver    SettingPath = "database.driver"
	SettingStorageType       SettingPath = "storage.type"
	SettingStoragePath       SettingPath = "storage.path"
	SettingCacheMaxSizeGB    SettingPath = "cache.max_size_gb"
	SettingCacheTTLIndex     SettingPath = "cache.ttl_index"
	SettingCacheTTLBlob      SettingPath = "cache.ttl_blob"
	SettingCacheLRUThreshold SettingPath = "cache.lru_threshold"
	SettingAuthTokenTTL      SettingPath = "auth.token_ttl"
)

var allSettingPaths = []SettingPath{
	SettingServerHost,
	SettingServerPort,
	SettingServerLogLevel,
	SettingDatabaseDriver,
	SettingStorageType,
	SettingStoragePath,
	SettingCacheMaxSizeGB,
	SettingCacheTTLIndex,
	SettingCacheTTLBlob,
	SettingCacheLRUThreshold,
	SettingAuthTokenTTL,
}

var editableSettingPaths = []SettingPath{
	SettingServerLogLevel,
	SettingCacheMaxSizeGB,
	SettingCacheTTLIndex,
	SettingCacheTTLBlob,
	SettingCacheLRUThreshold,
	SettingAuthTokenTTL,
}

var restartSettingPaths = []SettingPath{
	SettingCacheMaxSizeGB,
	SettingCacheTTLIndex,
	SettingCacheTTLBlob,
	SettingCacheLRUThreshold,
	SettingAuthTokenTTL,
}

func clonePaths(in []SettingPath) []SettingPath { return append([]SettingPath{}, in...) }
func AllSettingPaths() []SettingPath            { return clonePaths(allSettingPaths) }
func EditableSettingPaths() []SettingPath       { return clonePaths(editableSettingPaths) }
func RestartSettingPaths() []SettingPath        { return clonePaths(restartSettingPaths) }

type SettingSource string

const (
	SettingSourceDefault SettingSource = "default"
	SettingSourceFile    SettingSource = "file"
	SettingSourceEnv     SettingSource = "env"
)

type SettingsSnapshot struct {
	Server   SettingsServerSnapshot   `json:"server"`
	Database SettingsDatabaseSnapshot `json:"database"`
	Storage  SettingsStorageSnapshot  `json:"storage"`
	Cache    SettingsCacheSnapshot    `json:"cache"`
	Auth     SettingsAuthSnapshot     `json:"auth"`
}

type SettingsServerSnapshot struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	LogLevel string `json:"log_level"`
}

type SettingsDatabaseSnapshot struct {
	Driver string `json:"driver"`
}

type SettingsStorageSnapshot struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type SettingsCacheSnapshot struct {
	MaxSizeGB    int    `json:"max_size_gb"`
	TTLIndex     string `json:"ttl_index"`
	TTLBlob      string `json:"ttl_blob"`
	LRUThreshold int    `json:"lru_threshold"`
}

type SettingsAuthSnapshot struct {
	TokenTTL string `json:"token_ttl"`
}

type SettingsPatch struct {
	Server *SettingsServerPatch
	Cache  *SettingsCachePatch
	Auth   *SettingsAuthPatch
}

type SettingsServerPatch struct {
	LogLevel *string
}

type SettingsCachePatch struct {
	MaxSizeGB    *int
	TTLIndex     *string
	TTLBlob      *string
	LRUThreshold *int
}

type SettingsAuthPatch struct {
	TokenTTL *string
}

type SettingsState struct {
	Configured     SettingsSnapshot              `json:"configured"`
	Effective      SettingsSnapshot              `json:"effective"`
	PendingRestart []SettingPath                 `json:"pending_restart"`
	Overrides      map[SettingPath]string        `json:"overrides"`
	Sources        map[SettingPath]SettingSource `json:"sources"`
	Editable       []SettingPath                 `json:"editable"`
	ConfigWritable bool                          `json:"config_writable"`
}

type SettingsUpdateResult struct {
	SettingsState
	Changed           []SettingPath `json:"changed"`
	AppliedNow        []SettingPath `json:"applied_now"`
	RestartRequired   []SettingPath `json:"restart_required"`
	BlockedByOverride []SettingPath `json:"blocked_by_override"`
}

func compactDuration(d time.Duration) string {
	s := d.String()
	if d != 0 && d%time.Hour == 0 {
		return strings.TrimSuffix(s, "0m0s")
	}
	if d != 0 && d%time.Minute == 0 {
		return strings.TrimSuffix(s, "0s")
	}
	return s
}

func SettingsSnapshotFromConfig(c *Config) SettingsSnapshot {
	return SettingsSnapshot{
		Server: SettingsServerSnapshot{
			Host:     c.Server.Host,
			Port:     c.Server.Port,
			LogLevel: c.Server.LogLevel,
		},
		Database: SettingsDatabaseSnapshot{Driver: c.Database.Driver},
		Storage:  SettingsStorageSnapshot{Type: c.Storage.Type, Path: c.Storage.Path},
		Cache: SettingsCacheSnapshot{
			MaxSizeGB:    c.Cache.MaxSizeGB,
			TTLIndex:     compactDuration(c.Cache.TTLIndex),
			TTLBlob:      compactDuration(c.Cache.TTLBlob),
			LRUThreshold: c.Cache.LRUThreshold,
		},
		Auth: SettingsAuthSnapshot{TokenTTL: compactDuration(c.Auth.TokenTTL)},
	}
}

func ValidateSettingsSnapshot(s SettingsSnapshot) error {
	switch s.Server.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("server.log_level must be debug, info, warn, or error")
	}
	if s.Cache.MaxSizeGB <= 0 {
		return fmt.Errorf("cache.max_size_gb must be greater than zero")
	}
	if _, err := time.ParseDuration(s.Cache.TTLIndex); err != nil {
		return fmt.Errorf("cache.ttl_index must be a Go duration: %w", err)
	}
	if _, err := time.ParseDuration(s.Cache.TTLBlob); err != nil {
		return fmt.Errorf("cache.ttl_blob must be a Go duration: %w", err)
	}
	if s.Cache.LRUThreshold < 1 || s.Cache.LRUThreshold > 100 {
		return fmt.Errorf("cache.lru_threshold must be between 1 and 100")
	}
	if s.Auth.TokenTTL == "never" {
		return fmt.Errorf("auth.token_ttl does not support never")
	}
	if _, err := time.ParseDuration(s.Auth.TokenTTL); err != nil {
		return fmt.Errorf("auth.token_ttl must be a Go duration: %w", err)
	}
	return nil
}

type settingPatchEntry struct {
	path  SettingPath
	value any
}

func (p SettingsPatch) empty() bool { return len(p.entries()) == 0 }

func (p SettingsPatch) entries() []settingPatchEntry {
	entries := make([]settingPatchEntry, 0, 6)
	if p.Server != nil && p.Server.LogLevel != nil {
		entries = append(entries, settingPatchEntry{SettingServerLogLevel, *p.Server.LogLevel})
	}
	if p.Cache != nil {
		if p.Cache.MaxSizeGB != nil {
			entries = append(entries, settingPatchEntry{SettingCacheMaxSizeGB, *p.Cache.MaxSizeGB})
		}
		if p.Cache.TTLIndex != nil {
			entries = append(entries, settingPatchEntry{SettingCacheTTLIndex, *p.Cache.TTLIndex})
		}
		if p.Cache.TTLBlob != nil {
			entries = append(entries, settingPatchEntry{SettingCacheTTLBlob, *p.Cache.TTLBlob})
		}
		if p.Cache.LRUThreshold != nil {
			entries = append(entries, settingPatchEntry{SettingCacheLRUThreshold, *p.Cache.LRUThreshold})
		}
	}
	if p.Auth != nil && p.Auth.TokenTTL != nil {
		entries = append(entries, settingPatchEntry{SettingAuthTokenTTL, *p.Auth.TokenTTL})
	}
	return entries
}

func patchFromEntries(entries []settingPatchEntry) SettingsPatch {
	var out SettingsPatch
	for _, entry := range entries {
		switch entry.path {
		case SettingServerLogLevel:
			value := entry.value.(string)
			if out.Server == nil {
				out.Server = &SettingsServerPatch{}
			}
			out.Server.LogLevel = &value
		case SettingCacheMaxSizeGB:
			value := entry.value.(int)
			if out.Cache == nil {
				out.Cache = &SettingsCachePatch{}
			}
			out.Cache.MaxSizeGB = &value
		case SettingCacheTTLIndex:
			value := entry.value.(string)
			if out.Cache == nil {
				out.Cache = &SettingsCachePatch{}
			}
			out.Cache.TTLIndex = &value
		case SettingCacheTTLBlob:
			value := entry.value.(string)
			if out.Cache == nil {
				out.Cache = &SettingsCachePatch{}
			}
			out.Cache.TTLBlob = &value
		case SettingCacheLRUThreshold:
			value := entry.value.(int)
			if out.Cache == nil {
				out.Cache = &SettingsCachePatch{}
			}
			out.Cache.LRUThreshold = &value
		case SettingAuthTokenTTL:
			value := entry.value.(string)
			if out.Auth == nil {
				out.Auth = &SettingsAuthPatch{}
			}
			out.Auth.TokenTTL = &value
		}
	}
	return out
}
