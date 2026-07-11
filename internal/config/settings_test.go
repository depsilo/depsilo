package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSettingsSnapshotDefaultsAreCompleteAndCompact(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := SettingsSnapshotFromConfig(cfg)
	if got.Server.LogLevel != "info" || got.Cache.TTLIndex != "5m" || got.Cache.TTLBlob != "72h" || got.Auth.TokenTTL != "168h" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if len(AllSettingPaths()) != 11 || len(EditableSettingPaths()) != 6 || len(RestartSettingPaths()) != 5 {
		t.Fatalf("path counts = %d/%d/%d", len(AllSettingPaths()), len(EditableSettingPaths()), len(RestartSettingPaths()))
	}
}

func TestSettingPathOrdersAreCanonicalAndDefensive(t *testing.T) {
	tests := []struct {
		name string
		get  func() []SettingPath
		want []SettingPath
	}{
		{
			name: "all",
			get:  AllSettingPaths,
			want: []SettingPath{
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
			},
		},
		{
			name: "editable",
			get:  EditableSettingPaths,
			want: []SettingPath{
				SettingServerLogLevel,
				SettingCacheMaxSizeGB,
				SettingCacheTTLIndex,
				SettingCacheTTLBlob,
				SettingCacheLRUThreshold,
				SettingAuthTokenTTL,
			},
		},
		{
			name: "restart",
			get:  RestartSettingPaths,
			want: []SettingPath{
				SettingCacheMaxSizeGB,
				SettingCacheTTLIndex,
				SettingCacheTTLBlob,
				SettingCacheLRUThreshold,
				SettingAuthTokenTTL,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.get()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("paths = %v, want %v", got, tt.want)
			}
			got[0] = "mutated"
			if fresh := tt.get(); !reflect.DeepEqual(fresh, tt.want) {
				t.Fatalf("fresh paths changed through caller mutation: %v", fresh)
			}
		})
	}
}

func TestSettingsPatchEntriesUseCanonicalEditableOrder(t *testing.T) {
	logLevel := "warn"
	maxSize := 8
	ttlIndex := "10m"
	ttlBlob := "96h"
	lruThreshold := 80
	tokenTTL := "24h"

	patch := SettingsPatch{
		Server: &SettingsServerPatch{LogLevel: &logLevel},
		Cache: &SettingsCachePatch{
			MaxSizeGB:    &maxSize,
			TTLIndex:     &ttlIndex,
			TTLBlob:      &ttlBlob,
			LRUThreshold: &lruThreshold,
		},
		Auth: &SettingsAuthPatch{TokenTTL: &tokenTTL},
	}

	entries := patch.entries()
	got := make([]SettingPath, len(entries))
	for i, entry := range entries {
		got[i] = entry.path
	}
	if want := EditableSettingPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("entry paths = %v, want %v", got, want)
	}
}

func TestSettingsSnapshotCompactsMixedHourMinuteDurations(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "ninety minutes", duration: 90 * time.Minute, want: "1h30m"},
		{name: "eighty minutes", duration: 80 * time.Minute, want: "1h20m"},
		{name: "one hour ten minutes", duration: time.Hour + 10*time.Minute, want: "1h10m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SettingsSnapshotFromConfig(&Config{Cache: CacheConfig{TTLIndex: tt.duration}})
			if got.Cache.TTLIndex != tt.want {
				t.Fatalf("TTLIndex = %q, want %q", got.Cache.TTLIndex, tt.want)
			}
		})
	}
}

func TestLoadAcceptsMixedHourMinuteDurations(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "ninety minutes", raw: "90m", want: "1h30m"},
		{name: "eighty minutes", raw: "80m", want: "1h20m"},
		{name: "one hour ten minutes", raw: "1h10m", want: "1h10m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			contents := []byte("[cache]\nttl_index = \"" + tt.raw + "\"\n")
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEPSILO_CONFIG", path)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() rejected valid duration %q: %v", tt.raw, err)
			}
			if got := SettingsSnapshotFromConfig(cfg).Cache.TTLIndex; got != tt.want {
				t.Fatalf("TTLIndex = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateSettingsSnapshotRejectsInvalidEditableValues(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := SettingsSnapshotFromConfig(cfg)
	tests := []struct {
		name   string
		mutate func(*SettingsSnapshot)
	}{
		{"log level", func(s *SettingsSnapshot) { s.Server.LogLevel = "trace" }},
		{"max size", func(s *SettingsSnapshot) { s.Cache.MaxSizeGB = 0 }},
		{"index duration", func(s *SettingsSnapshot) { s.Cache.TTLIndex = "tomorrow" }},
		{"blob duration", func(s *SettingsSnapshot) { s.Cache.TTLBlob = "" }},
		{"lru low", func(s *SettingsSnapshot) { s.Cache.LRUThreshold = 0 }},
		{"lru high", func(s *SettingsSnapshot) { s.Cache.LRUThreshold = 101 }},
		{"never token", func(s *SettingsSnapshot) { s.Auth.TokenTTL = "never" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			tt.mutate(&candidate)
			if err := ValidateSettingsSnapshot(candidate); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadReadsSettingsWrittenToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[server]\nlog_level = \"warn\"\n[cache]\nmax_size_gb = 8\nttl_index = \"10m\"\nttl_blob = \"96h\"\nlru_threshold = 80\n[auth]\ntoken_ttl = \"24h\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := SettingsSnapshotFromConfig(cfg); !reflect.DeepEqual(got.Cache, SettingsCacheSnapshot{MaxSizeGB: 8, TTLIndex: "10m", TTLBlob: "96h", LRUThreshold: 80}) {
		t.Fatalf("cache = %+v", got.Cache)
	}
}
