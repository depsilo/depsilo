# Admin Settings Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Admin Settings a truthful control plane whose whitelisted edits are atomically persisted to `config.toml`, whose runtime effect is explicitly reported, and whose UI distinguishes effective, pending-restart, environment-blocked, read-only, stale, and failed states.

**Architecture:** `config.Store` owns the boundary between the file-configured snapshot and the process-effective snapshot. It reparses the current TOML for every mutation, uses the existing go-toml parser's source ranges to replace only selected scalar values, atomically writes a same-directory temporary file, and applies only `server.log_level` through the exact `zap.AtomicLevel` used by the process logger. The Gin handler is a strict DTO adapter over the Store; the React page sends only dirty fields and renders the Store's result arrays rather than inferring success from HTTP 200.

**Tech Stack:** Go 1.25.6, Gin 1.12, Viper 1.21, `github.com/pelletier/go-toml/v2` 2.2.4, zap 1.27, React 19, TypeScript 5.9, TanStack Query 5, Axios 1.14, Base UI wrappers from Plan 04, Playwright.

## Global Constraints

- Preserve all pre-existing dirty-worktree changes. In particular, read and retain the current edits in `internal/config/config.go`, `internal/config/loader_test.go`, `internal/api/router.go`, `internal/server/server.go`, `config.example.toml`, `web/src/admin/pages/Settings.tsx`, `web/src/lib/api.ts`, `web/src/i18n/en.ts`, and `web/src/i18n/zh.ts`; never use reset, checkout, clean, or whole-file restoration.
- Record the dirty-path list before Task 1. For the already dirty targets above, use the exact `git add -p -- ...` commands in each task and inspect `git diff --cached`; never stage a whole pre-existing file in a task commit.
- `config.toml` is the Settings persistence authority. Every update re-reads the current file under a process-local mutex and modifies only request fields.
- The only editable paths are `server.log_level`, `cache.max_size_gb`, `cache.ttl_index`, `cache.ttl_blob`, `cache.lru_threshold`, and `auth.token_ttl`.
- `server.host`, `server.port`, `database.driver`, `storage.type`, and `storage.path` remain read-only. Remove `auth.enabled` from the Admin form and PUT DTO without deleting an existing TOML key.
- Missing keys receive built-in defaults, so every field in `configured` and `effective` is present and non-null. Duration strings use valid Go duration syntax and the compact examples `5m`, `72h`, and `168h`.
- `sources` contains all eleven `SettingPath` keys with exactly `default`, `file`, or `env`; `overrides` maps environment-sourced paths to their exact `DEPSILO_*` variable names.
- Environment-overridden fields are still written to the file and included in `changed`, but only in `blocked_by_override`, never `applied_now` or `restart_required`.
- Only non-overridden `server.log_level` is hot-applied. Cache and auth fields remain at boot-effective values until restart.
- Reject invalid duration syntax, `cache.max_size_gb <= 0`, `cache.lru_threshold` outside `1..100`, unsupported `auth.token_ttl = "never"`, unsupported log levels, empty patches, and non-whitelisted JSON fields. Never partially write an invalid patch.
- Preserve untouched TOML bytes, including sections, key order, whitespace, comments, newline style, and file permission bits. Writes use a same-directory temporary file, file `fsync`, atomic rename, and directory `fsync`.
- Read-only files/directories return `409 CONFIG_READ_ONLY`; failed atomic writes return `500 CONFIG_WRITE_FAILED`; malformed/unreadable current files return `500 CONFIG_READ_FAILED`; setting validation returns `422 INVALID_SETTING`. Keep the existing `{code,message}` error shape.
- Backend tasks in this plan are independently executable. The final frontend task starts only after Plan 01 provides `usePrincipal().canWrite` and Plan 04 Tasks 1/3/4/5 provide the strict API fixture, labelled fields/Switch, feedback/toast, responsive Tabs, and `useMediaQuery`.
- Plan 02 exclusively owns Settings DTOs, `Settings.tsx`, Settings API typing, Settings i18n, and Settings-specific browser tests. Plan 04 owns shared components and Webhook; do not implement shared components here.
- Mobile Settings uses horizontal tabs below 768px and a 180px vertical rail at 768px and above. All Settings form grids use `grid-cols-1 sm:grid-cols-2`; the page must not create document-level horizontal scrolling at 320px or 390px.
- Do not add frontend runtime dependencies. Promote the already-transitive `go-toml/v2@v2.2.4` module to a direct Go dependency because production code imports its parser.
- Modified frontend files must add no ESLint errors. Do not attempt to clear the repository's historical lint baseline.

---

## File Structure

- `internal/config/settings.go` — create the canonical setting paths, typed snapshots/patches/results, validation, compact duration rendering, and deterministic ordering.
- `internal/config/loader.go` — reuse one decode path for startup and Store reads; add the missing `server.log_level = info` default.
- `internal/config/settings_test.go` — test defaults, validation, duration rendering, path completeness, and startup reload behavior.
- `internal/config/toml_patch.go` — index TOML through parser source ranges and apply byte-range edits without reserializing the document.
- `internal/config/toml_patch_test.go` — prove comments, spacing, unknown sections, CRLF, dotted keys, missing keys, and parse errors behave losslessly.
- `internal/config/atomic_write.go` — implement permission-aware writability probing and durable same-directory atomic replacement.
- `internal/config/atomic_write_test.go` — prove mode retention, inode replacement, temp cleanup, and non-mutation on failure.
- `internal/logging/production.go` — construct the production logger and return its shared `zap.AtomicLevel`.
- `internal/logging/production_test.go` — prove changing the returned level changes the constructed logger's enabled levels.
- `internal/config/store.go` — own configured/effective state, environment provenance, update classification, serialization, and typed Store errors.
- `internal/config/store_test.go` — cover legal/illegal patches, no-write failures, read-only paths, concurrency, atomic failure, comment retention, overrides, and reload-after-restart.
- `internal/api/admin/settings.go` — replace map responses and in-memory mutation with strict request/response structs over a small Store interface.
- `internal/api/admin/settings_test.go` — contract-test exact GET/PUT JSON and error/status mapping.
- `internal/api/router.go` — inject `*config.Store` into the Settings handler.
- `internal/server/server.go` — set the boot log level, construct the Store, and pass it through `api.Deps`.
- `internal/cli/serve.go`, `internal/cli/daemon.go`, `cmd/server/main_server.go`, `cmd/depsilo-tray/main.go` — construct the logger through `internal/logging` and pass the shared atomic level to `StartServer`.
- `go.mod`, `go.sum` — record `go-toml/v2@v2.2.4` as a direct dependency.
- `config.example.toml` — document the default `server.log_level` while preserving unrelated dirty edits.
- `web/src/lib/adminApi.types.ts` — add the exact Settings DTOs to the shared contract file created by Plan 01.
- `web/src/lib/api.ts` — type Settings Axios calls and remove the Settings `any`.
- `web/src/admin/pages/Settings.tsx` — render the typed configured/effective/status model and send only dirty fields.
- `web/src/i18n/en.ts`, `web/src/i18n/zh.ts` — add matched Settings status, source, error, and field-label copy.
- `web/e2e/admin-settings.spec.ts` — verify request shape, service-authored feedback, failures, permissions, stale/retry behavior, and 320/390/768 layouts.

## Cross-Plan Interfaces

The final task consumes these interfaces exactly; their implementation belongs to Plans 01 and 04:

```ts
// Plan 01
declare function usePrincipal(): { principal: Principal | undefined; canWrite: boolean }
// Settings consumes `canWrite`; the DTO field remains `principal.can_write` on the wire.

// Plan 04 Tasks 3/4/5
type FieldFeedbackProps = { label?: string; hint?: string; error?: string }
type SwitchV2Props = { label: string; checked: boolean; onCheckedChange(value: boolean): void; disabled?: boolean }
type InlineNoticeProps = { tone: 'success' | 'warning' | 'danger' | 'info'; title?: string; children: React.ReactNode }
type QueryErrorStateProps = { message: string; onRetry(): void }
declare function useAppToast(): { show(input: { tone: 'success' | 'danger' | 'warning'; message: string }): string; close(id?: string): void }
type TabItem = { key: string; label: string; icon?: React.ReactNode; disabled?: boolean; content: React.ReactNode }
type TabsV2Props = { items: TabItem[]; value: string; onValueChange(value: string): void; ariaLabel: string; orientation?: 'horizontal' | 'vertical' }
declare function useMediaQuery(query: string): boolean

// Plan 04 Task 1
declare function mockAdminApi(page: Page, overrides?: AdminApiOverrides): Promise<{ assertMatched(): void }>
```

### Task 1: Define the canonical Settings model and one validated config decoder

**Files:**
- Create: `internal/config/settings.go`
- Create: `internal/config/settings_test.go`
- Modify: `internal/config/loader.go`
- Modify: `config.example.toml`

**Interfaces:**
- Produces: `SettingPath`, `SettingSource`, `SettingsSnapshot`, `SettingsPatch`, `SettingsState`, and `SettingsUpdateResult` with the exact fields below.
- Produces: `AllSettingPaths()`, `EditableSettingPaths()`, `RestartSettingPaths()`, `SettingsSnapshotFromConfig(*Config)`, `ValidateSettingsSnapshot(SettingsSnapshot)`, and `decodeConfigDocument([]byte)`.
- Keeps: `Load() (*Config, error)` and all existing config structs/callers.

- [ ] **Step 1: Write failing model/default/validation tests**

Create `internal/config/settings_test.go` with these cases:

```go
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingsSnapshotDefaultsAreCompleteAndCompact(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil { t.Fatal(err) }
	got := SettingsSnapshotFromConfig(cfg)
	if got.Server.LogLevel != "info" || got.Cache.TTLIndex != "5m" || got.Cache.TTLBlob != "72h" || got.Auth.TokenTTL != "168h" {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if len(AllSettingPaths()) != 11 || len(EditableSettingPaths()) != 6 || len(RestartSettingPaths()) != 5 {
		t.Fatalf("path counts = %d/%d/%d", len(AllSettingPaths()), len(EditableSettingPaths()), len(RestartSettingPaths()))
	}
}

func TestValidateSettingsSnapshotRejectsInvalidEditableValues(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil { t.Fatal(err) }
	valid := SettingsSnapshotFromConfig(cfg)
	tests := []struct { name string; mutate func(*SettingsSnapshot) }{
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
			if err := ValidateSettingsSnapshot(candidate); err == nil { t.Fatal("expected validation error") }
		})
	}
}

func TestLoadReadsSettingsWrittenToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[server]\nlog_level = \"warn\"\n[cache]\nmax_size_gb = 8\nttl_index = \"10m\"\nttl_blob = \"96h\"\nlru_threshold = 80\n[auth]\ntoken_ttl = \"24h\"\n"), 0o600); err != nil { t.Fatal(err) }
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if got := SettingsSnapshotFromConfig(cfg); !reflect.DeepEqual(got.Cache, SettingsCacheSnapshot{MaxSizeGB: 8, TTLIndex: "10m", TTLBlob: "96h", LRUThreshold: 80}) {
		t.Fatalf("cache = %+v", got.Cache)
	}
}
```

- [ ] **Step 2: Run the tests and confirm the missing-type failure**

Run: `go test ./internal/config -run 'Test(SettingsSnapshot|ValidateSettings|LoadReadsSettings)'`

Expected: FAIL with undefined Settings types/functions.

- [ ] **Step 3: Add the complete domain types and deterministic orders**

Create `internal/config/settings.go` with this public surface (use private helpers for copying and comparisons):

```go
package config

import (
	"fmt"
	"strings"
	"time"
)

type SettingPath string
const (
	SettingServerHost SettingPath = "server.host"
	SettingServerPort SettingPath = "server.port"
	SettingServerLogLevel SettingPath = "server.log_level"
	SettingDatabaseDriver SettingPath = "database.driver"
	SettingStorageType SettingPath = "storage.type"
	SettingStoragePath SettingPath = "storage.path"
	SettingCacheMaxSizeGB SettingPath = "cache.max_size_gb"
	SettingCacheTTLIndex SettingPath = "cache.ttl_index"
	SettingCacheTTLBlob SettingPath = "cache.ttl_blob"
	SettingCacheLRUThreshold SettingPath = "cache.lru_threshold"
	SettingAuthTokenTTL SettingPath = "auth.token_ttl"
)

var allSettingPaths = []SettingPath{SettingServerHost, SettingServerPort, SettingServerLogLevel, SettingDatabaseDriver, SettingStorageType, SettingStoragePath, SettingCacheMaxSizeGB, SettingCacheTTLIndex, SettingCacheTTLBlob, SettingCacheLRUThreshold, SettingAuthTokenTTL}
var editableSettingPaths = []SettingPath{SettingServerLogLevel, SettingCacheMaxSizeGB, SettingCacheTTLIndex, SettingCacheTTLBlob, SettingCacheLRUThreshold, SettingAuthTokenTTL}
var restartSettingPaths = []SettingPath{SettingCacheMaxSizeGB, SettingCacheTTLIndex, SettingCacheTTLBlob, SettingCacheLRUThreshold, SettingAuthTokenTTL}

func clonePaths(in []SettingPath) []SettingPath { return append([]SettingPath{}, in...) }
func AllSettingPaths() []SettingPath { return clonePaths(allSettingPaths) }
func EditableSettingPaths() []SettingPath { return clonePaths(editableSettingPaths) }
func RestartSettingPaths() []SettingPath { return clonePaths(restartSettingPaths) }

type SettingSource string
const (
	SettingSourceDefault SettingSource = "default"
	SettingSourceFile SettingSource = "file"
	SettingSourceEnv SettingSource = "env"
)

type SettingsSnapshot struct {
	Server SettingsServerSnapshot `json:"server"`
	Database SettingsDatabaseSnapshot `json:"database"`
	Storage SettingsStorageSnapshot `json:"storage"`
	Cache SettingsCacheSnapshot `json:"cache"`
	Auth SettingsAuthSnapshot `json:"auth"`
}
type SettingsServerSnapshot struct { Host string `json:"host"`; Port int `json:"port"`; LogLevel string `json:"log_level"` }
type SettingsDatabaseSnapshot struct { Driver string `json:"driver"` }
type SettingsStorageSnapshot struct { Type string `json:"type"`; Path string `json:"path"` }
type SettingsCacheSnapshot struct { MaxSizeGB int `json:"max_size_gb"`; TTLIndex string `json:"ttl_index"`; TTLBlob string `json:"ttl_blob"`; LRUThreshold int `json:"lru_threshold"` }
type SettingsAuthSnapshot struct { TokenTTL string `json:"token_ttl"` }

type SettingsPatch struct { Server *SettingsServerPatch; Cache *SettingsCachePatch; Auth *SettingsAuthPatch }
type SettingsServerPatch struct { LogLevel *string }
type SettingsCachePatch struct { MaxSizeGB *int; TTLIndex *string; TTLBlob *string; LRUThreshold *int }
type SettingsAuthPatch struct { TokenTTL *string }

type SettingsState struct {
	Configured SettingsSnapshot `json:"configured"`
	Effective SettingsSnapshot `json:"effective"`
	PendingRestart []SettingPath `json:"pending_restart"`
	Overrides map[SettingPath]string `json:"overrides"`
	Sources map[SettingPath]SettingSource `json:"sources"`
	Editable []SettingPath `json:"editable"`
	ConfigWritable bool `json:"config_writable"`
}
type SettingsUpdateResult struct { SettingsState; Changed []SettingPath `json:"changed"`; AppliedNow []SettingPath `json:"applied_now"`; RestartRequired []SettingPath `json:"restart_required"`; BlockedByOverride []SettingPath `json:"blocked_by_override"` }

func compactDuration(d time.Duration) string {
	s := d.String()
	if strings.Contains(s, "m") && strings.HasSuffix(s, "0s") { s = strings.TrimSuffix(s, "0s") }
	if strings.Contains(s, "h") && strings.HasSuffix(s, "0m") { s = strings.TrimSuffix(s, "0m") }
	return s
}

func SettingsSnapshotFromConfig(c *Config) SettingsSnapshot {
	return SettingsSnapshot{
		Server: SettingsServerSnapshot{Host: c.Server.Host, Port: c.Server.Port, LogLevel: c.Server.LogLevel},
		Database: SettingsDatabaseSnapshot{Driver: c.Database.Driver},
		Storage: SettingsStorageSnapshot{Type: c.Storage.Type, Path: c.Storage.Path},
		Cache: SettingsCacheSnapshot{MaxSizeGB: c.Cache.MaxSizeGB, TTLIndex: compactDuration(c.Cache.TTLIndex), TTLBlob: compactDuration(c.Cache.TTLBlob), LRUThreshold: c.Cache.LRUThreshold},
		Auth: SettingsAuthSnapshot{TokenTTL: compactDuration(c.Auth.TokenTTL)},
	}
}

func ValidateSettingsSnapshot(s SettingsSnapshot) error {
	switch s.Server.LogLevel { case "debug", "info", "warn", "error": default: return fmt.Errorf("server.log_level must be debug, info, warn, or error") }
	if s.Cache.MaxSizeGB <= 0 { return fmt.Errorf("cache.max_size_gb must be greater than zero") }
	if _, err := time.ParseDuration(s.Cache.TTLIndex); err != nil { return fmt.Errorf("cache.ttl_index must be a Go duration: %w", err) }
	if _, err := time.ParseDuration(s.Cache.TTLBlob); err != nil { return fmt.Errorf("cache.ttl_blob must be a Go duration: %w", err) }
	if s.Cache.LRUThreshold < 1 || s.Cache.LRUThreshold > 100 { return fmt.Errorf("cache.lru_threshold must be between 1 and 100") }
	if s.Auth.TokenTTL == "never" { return fmt.Errorf("auth.token_ttl does not support never") }
	if _, err := time.ParseDuration(s.Auth.TokenTTL); err != nil { return fmt.Errorf("auth.token_ttl must be a Go duration: %w", err) }
	return nil
}
```

Add these unexported helpers in the same file; later tasks use their canonical order for every response array:

```go
type settingPatchEntry struct { path SettingPath; value any }

func (p SettingsPatch) empty() bool { return len(p.entries()) == 0 }
func (p SettingsPatch) entries() []settingPatchEntry {
	entries := make([]settingPatchEntry, 0, 6)
	if p.Server != nil && p.Server.LogLevel != nil { entries = append(entries, settingPatchEntry{SettingServerLogLevel, *p.Server.LogLevel}) }
	if p.Cache != nil {
		if p.Cache.MaxSizeGB != nil { entries = append(entries, settingPatchEntry{SettingCacheMaxSizeGB, *p.Cache.MaxSizeGB}) }
		if p.Cache.TTLIndex != nil { entries = append(entries, settingPatchEntry{SettingCacheTTLIndex, *p.Cache.TTLIndex}) }
		if p.Cache.TTLBlob != nil { entries = append(entries, settingPatchEntry{SettingCacheTTLBlob, *p.Cache.TTLBlob}) }
		if p.Cache.LRUThreshold != nil { entries = append(entries, settingPatchEntry{SettingCacheLRUThreshold, *p.Cache.LRUThreshold}) }
	}
	if p.Auth != nil && p.Auth.TokenTTL != nil { entries = append(entries, settingPatchEntry{SettingAuthTokenTTL, *p.Auth.TokenTTL}) }
	return entries
}
func patchFromEntries(entries []settingPatchEntry) SettingsPatch {
	var out SettingsPatch
	for _, entry := range entries {
		switch entry.path {
		case SettingServerLogLevel:
			value := entry.value.(string); if out.Server == nil { out.Server = &SettingsServerPatch{} }; out.Server.LogLevel = &value
		case SettingCacheMaxSizeGB:
			value := entry.value.(int); if out.Cache == nil { out.Cache = &SettingsCachePatch{} }; out.Cache.MaxSizeGB = &value
		case SettingCacheTTLIndex:
			value := entry.value.(string); if out.Cache == nil { out.Cache = &SettingsCachePatch{} }; out.Cache.TTLIndex = &value
		case SettingCacheTTLBlob:
			value := entry.value.(string); if out.Cache == nil { out.Cache = &SettingsCachePatch{} }; out.Cache.TTLBlob = &value
		case SettingCacheLRUThreshold:
			value := entry.value.(int); if out.Cache == nil { out.Cache = &SettingsCachePatch{} }; out.Cache.LRUThreshold = &value
		case SettingAuthTokenTTL:
			value := entry.value.(string); if out.Auth == nil { out.Auth = &SettingsAuthPatch{} }; out.Auth.TokenTTL = &value
		}
	}
	return out
}
```

- [ ] **Step 4: Refactor loader decoding and add the log default**

In `internal/config/loader.go`, extract the existing unmarshal and four explicit duration parses into `decodeViper(v *viper.Viper) (*Config, error)`. Add:

```go
func decodeConfigDocument(data []byte) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	if len(data) > 0 {
		v.SetConfigType("toml")
		if err := v.ReadConfig(bytes.NewReader(data)); err != nil { return nil, fmt.Errorf("parse config: %w", err) }
	}
	cfg, err := decodeViper(v)
	if err != nil { return nil, err }
	if err := ValidateSettingsSnapshot(SettingsSnapshotFromConfig(cfg)); err != nil { return nil, err }
	return cfg, nil
}
```

`Load()` must call `decodeViper(v)`, retain `IsDefault`, `ConfigPath`, license environment override, and JWT warning behavior, then call `ValidateSettingsSnapshot` before returning. Add this exact default in `setDefaults`:

```go
v.SetDefault("server.log_level", "info")
```

Add `log_level = "info"` beneath `port` in `config.example.toml`; do not disturb its unrelated supply-chain edits.

- [ ] **Step 5: Run config tests**

Run: `go test ./internal/config`

Expected: PASS, including the pre-existing environment-path and writer tests.

- [ ] **Step 6: Commit the typed model**

```bash
git add internal/config/settings.go internal/config/settings_test.go internal/config/loader.go
git add -p -- config.example.toml
git commit -m "feat(config): define validated admin settings model"
```

### Task 2: Build lossless TOML patching and durable atomic replacement

**Files:**
- Create: `internal/config/toml_patch.go`
- Create: `internal/config/toml_patch_test.go`
- Create: `internal/config/atomic_write.go`
- Create: `internal/config/atomic_write_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `SettingsPatch.entries()` from Task 1.
- Produces: `patchSettingsDocument(document []byte, patch SettingsPatch) ([]byte, map[SettingPath]bool, error)`; the map reports all explicit setting paths after patching.
- Produces: `atomicFileWriter.Write(path string, data []byte, mode fs.FileMode) error` and `configWritable(path string) bool`.

- [ ] **Step 1: Write failing lossless-patch tests**

Create `internal/config/toml_patch_test.go`. Use a byte-for-byte expected document, not substring-only assertions:

```go
func TestPatchSettingsDocumentPreservesUntouchedBytes(t *testing.T) {
	input := []byte("# operator header\r\n[server]\r\nhost  = '127.0.0.1' # keep\r\nlog_level = \"info\" # level\r\n\r\n[cache]\r\nmax_size_gb = 20\r\nttl_index = \"5m\"\r\n# blob policy\r\nttl_blob   = \"72h\"\r\nlru_threshold = 90\r\n\r\n[custom]\r\nverbatim = { x = 1 }\r\n")
	level, blob := "warn", "96h"
	got, explicit, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}, Cache: &SettingsCachePatch{TTLBlob: &blob}})
	if err != nil { t.Fatal(err) }
	want := []byte("# operator header\r\n[server]\r\nhost  = '127.0.0.1' # keep\r\nlog_level = \"warn\" # level\r\n\r\n[cache]\r\nmax_size_gb = 20\r\nttl_index = \"5m\"\r\n# blob policy\r\nttl_blob   = \"96h\"\r\nlru_threshold = 90\r\n\r\n[custom]\r\nverbatim = { x = 1 }\r\n")
	if !bytes.Equal(got, want) { t.Fatalf("patched document:\n%s\nwant:\n%s", got, want) }
	if !explicit[SettingServerLogLevel] || !explicit[SettingCacheTTLBlob] { t.Fatalf("explicit = %#v", explicit) }
}

func TestPatchSettingsDocumentAddsOnlyMissingKeys(t *testing.T) {
	input := []byte("[server]\nhost = \"0.0.0.0\"\n\n[custom]\nkeep = true\n")
	level, ttl := "debug", "24h"
	got, _, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}, Auth: &SettingsAuthPatch{TokenTTL: &ttl}})
	if err != nil { t.Fatal(err) }
	want := "auth.token_ttl = \"24h\"\n[server]\nhost = \"0.0.0.0\"\nlog_level = \"debug\"\n\n[custom]\nkeep = true\n"
	if string(got) != want { t.Fatalf("got:\n%s\nwant:\n%s", got, want) }
}

func TestPatchSettingsDocumentRejectsMalformedCurrentTOML(t *testing.T) {
	level := "warn"
	if _, _, err := patchSettingsDocument([]byte("[server\n"), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}}); err == nil { t.Fatal("expected parse error") }
}
```

Also add cases for root dotted keys (`server.log_level = "info"`), a missing key beside comments, an existing inline-table scalar replacement, and an inline table that seals a missing child (must return an error rather than emit invalid TOML).

- [ ] **Step 2: Run and confirm the missing patcher failure**

Run: `go test ./internal/config -run TestPatchSettingsDocument`

Expected: FAIL with undefined `patchSettingsDocument`.

- [ ] **Step 3: Promote and use the existing parser dependency**

Run: `go get github.com/pelletier/go-toml/v2@v2.2.4`

Expected: `go.mod` lists `go-toml/v2 v2.2.4` in the direct `require` block; no unrelated version changes.

Implement `internal/config/toml_patch.go` around `unstable.Parser`, never regexes. The implementation must:

```go
type documentEdit struct { start, end int; replacement []byte }
type settingsDocumentIndex struct {
	values map[SettingPath]unstable.Range
	explicit map[SettingPath]bool
	sectionEnds map[string]int
	sealedInline map[string]bool
	rootEnd int
	newline string
}

func patchSettingsDocument(document []byte, patch SettingsPatch) ([]byte, map[SettingPath]bool, error) {
	index, err := indexSettingsDocument(document)
	if err != nil { return nil, nil, err }
	edits := make([]documentEdit, 0, len(patch.entries()))
	missingBySection := map[string][]settingPatchEntry{}
	for _, entry := range patch.entries() {
		if raw, ok := index.values[entry.path]; ok {
			edits = append(edits, documentEdit{start: int(raw.Offset), end: int(raw.Offset + raw.Length), replacement: renderSettingValue(entry)})
			continue
		}
		section, _ := splitSettingPath(entry.path)
		if index.sealedInline[section] { return nil, nil, fmt.Errorf("cannot add %s: %s is an inline table", entry.path, section) }
		missingBySection[section] = append(missingBySection[section], entry)
	}
	for _, section := range []string{"server", "cache", "auth"} {
		entries := missingBySection[section]
		if len(entries) == 0 { continue }
		at, inSection := index.sectionEnds[section]
		var b strings.Builder
		for _, entry := range entries {
			_, key := splitSettingPath(entry.path)
			if !inSection { key = string(entry.path); at = index.rootEnd }
			fmt.Fprintf(&b, "%s = %s%s", key, renderSettingValue(entry), index.newline)
		}
		edits = append(edits, documentEdit{start: at, end: at, replacement: []byte(b.String())})
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), document...)
	for _, edit := range edits { out = append(append(append([]byte(nil), out[:edit.start]...), edit.replacement...), out[edit.end:]...) }
	if _, err := decodeConfigDocument(out); err != nil { return nil, nil, fmt.Errorf("validate patched config: %w", err) }
	after, err := indexSettingsDocument(out)
	if err != nil { return nil, nil, err }
	return out, after.explicit, nil
}
```

`indexSettingsDocument` must walk every parser expression, carry the current standard table path, recursively walk inline-table `KeyValue` children, and copy each value's `unstable.Range`. Track each relevant section's insertion point as the end of its last key-value line, before trailing blank lines/comments; set `rootEnd` to the first table header (or EOF), so a missing section is added as a root dotted key without redefining an implicit/inline table. Detect `\r\n` once and use it for additions. `renderSettingValue` uses `strconv.Quote` for the three string fields and `strconv.Itoa` for integers. `splitSettingPath` uses `strings.Cut` and rejects paths outside the canonical constants. Apply edits from highest offset to lowest so source ranges never shift before use.

- [ ] **Step 4: Write failing atomic-write tests**

Create `internal/config/atomic_write_test.go`:

```go
func TestOSAtomicFileWriterReplacesAndPreservesMode(t *testing.T) {
	dir := t.TempDir(); path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil { t.Fatal(err) }
	before, _ := os.Stat(path)
	if err := (osAtomicFileWriter{}).Write(path, []byte("new"), before.Mode()); err != nil { t.Fatal(err) }
	after, err := os.Stat(path); if err != nil { t.Fatal(err) }
	data, _ := os.ReadFile(path)
	if string(data) != "new" || after.Mode().Perm() != 0o640 { t.Fatalf("data/mode = %q/%o", data, after.Mode().Perm()) }
	if os.SameFile(before, after) { t.Fatal("expected rename to replace inode") }
	matches, _ := filepath.Glob(filepath.Join(dir, ".config.toml.tmp-*")); if len(matches) != 0 { t.Fatalf("leftover temp files: %v", matches) }
}

func TestConfigWritableHonorsReadOnlyBits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil { t.Fatal(err) }
	if configWritable(path) { t.Fatal("read-only file reported writable") }
}
```

- [ ] **Step 5: Implement durable replacement**

Create `internal/config/atomic_write.go` with `atomicFileWriter`, production `osAtomicFileWriter`, and this exact order:

```go
func (osAtomicFileWriter) Write(path string, data []byte, mode fs.FileMode) (err error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil { return err }
	tmpName := tmp.Name()
	defer func() { tmp.Close(); if err != nil { os.Remove(tmpName) } }()
	if err = tmp.Chmod(mode.Perm()); err != nil { return err }
	if _, err = tmp.Write(data); err != nil { return err }
	if err = tmp.Sync(); err != nil { return err }
	if err = tmp.Close(); err != nil { return err }
	if err = os.Rename(tmpName, path); err != nil { return err }
	d, err := os.Open(dir)
	if err != nil { return err }
	defer d.Close()
	return d.Sync()
}
```

`configWritable` first rejects an existing file with no write bits and a directory with no write bits, then creates/closes/removes a probe temp file in the same directory. Missing parent directories are not created by Admin Settings and report false.

- [ ] **Step 6: Run patch/write tests and race tests**

Run: `go test -race ./internal/config -run 'Test(PatchSettingsDocument|OSAtomicFileWriter|ConfigWritable)'`

Expected: PASS.

- [ ] **Step 7: Commit the persistence primitives**

```bash
git add go.mod go.sum internal/config/toml_patch.go internal/config/toml_patch_test.go internal/config/atomic_write.go internal/config/atomic_write_test.go
git commit -m "feat(config): add lossless atomic settings persistence"
```

### Task 3: Construct every server logger with one shared `zap.AtomicLevel`

**Files:**
- Create: `internal/logging/production.go`
- Create: `internal/logging/production_test.go`
- Modify: `internal/server/server.go`
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/daemon.go`
- Modify: `cmd/server/main_server.go`
- Modify: `cmd/depsilo-tray/main.go`

**Interfaces:**
- Produces: `logging.NewProduction() (*zap.Logger, zap.AtomicLevel, error)`.
- Changes: `server.StartServer(ctx context.Context, logLevel zap.AtomicLevel) (*http.Server, error)`.
- Guarantees: the level passed to `StartServer`, later passed to `config.Store`, is the same level enabler embedded in the installed logger core.

- [ ] **Step 1: Write the failing logger identity test**

```go
package logging

import (
	"testing"
	"go.uber.org/zap"
)

func TestNewProductionSharesAtomicLevelWithCore(t *testing.T) {
	logger, level, err := NewProduction()
	if err != nil { t.Fatal(err) }
	defer logger.Sync()
	if logger.Core().Enabled(zap.DebugLevel) { t.Fatal("debug unexpectedly enabled") }
	level.SetLevel(zap.DebugLevel)
	if !logger.Core().Enabled(zap.DebugLevel) { t.Fatal("atomic level did not update logger core") }
}
```

Run: `go test ./internal/logging`

Expected: FAIL because the package/function does not exist.

- [ ] **Step 2: Implement the shared production constructor**

```go
package logging

import "go.uber.org/zap"

func NewProduction() (*zap.Logger, zap.AtomicLevel, error) {
	level := zap.NewAtomicLevelAt(zap.InfoLevel)
	cfg := zap.NewProductionConfig()
	cfg.Level = level
	logger, err := cfg.Build()
	return logger, level, err
}
```

- [ ] **Step 3: Thread the level through all four in-process entry points**

Replace every `zap.NewProduction()` with `logging.NewProduction()`, retain each existing `zap.ReplaceGlobals(logger)` and `defer logger.Sync()`, and call `server.StartServer(ctx, logLevel)`. Add the `depsilo/internal/logging` import. Do this in `RunServe`, foreground `runStart`, `cmd/server`, and `cmd/depsilo-tray`; daemon subprocess behavior is unchanged.

At the start of `server.StartServer`, after `config.Load()` succeeds and before the first config-loaded log, parse and apply the boot-effective value:

```go
parsed, err := zap.ParseAtomicLevel(cfg.Server.LogLevel)
if err != nil { return nil, fmt.Errorf("parse server.log_level: %w", err) }
logLevel.SetLevel(parsed.Level())
```

Do not call `zap.L().Core().Enabled(...)`; it is a predicate and never mutates a logger.

- [ ] **Step 4: Run focused build/tests**

Run: `go test ./internal/logging ./internal/cli ./internal/server ./cmd/server ./cmd/depsilo-tray`

Expected: PASS and all `StartServer` call sites compile.

- [ ] **Step 5: Commit logger wiring**

```bash
git add internal/logging/production.go internal/logging/production_test.go internal/cli/serve.go internal/cli/daemon.go cmd/server/main_server.go cmd/depsilo-tray/main.go
git add -p -- internal/server/server.go
git commit -m "feat(logging): share atomic server log level"
```

### Task 4: Implement `config.Store` configured/effective and result semantics

**Files:**
- Create: `internal/config/store.go`
- Create: `internal/config/store_test.go`

**Interfaces:**
- Consumes: Task 1 model, Task 2 patch/writer, and Task 3 shared level.
- Produces: `NewStore(path string, effective *Config, logLevel zap.AtomicLevel) *Store`.
- Produces: `(*Store).Snapshot(ctx context.Context) (SettingsState, error)` and `(*Store).Update(ctx context.Context, patch SettingsPatch) (SettingsUpdateResult, error)`.
- Produces: `StoreError{Code,Err}` with codes `INVALID_SETTING`, `CONFIG_READ_ONLY`, `CONFIG_READ_FAILED`, and `CONFIG_WRITE_FAILED`.

- [ ] **Step 1: Write the failing Store behavior suite**

Create `internal/config/store_test.go` with this complete setup and suite. It uses only the standard library and zap; do not import `go-cmp`:

```go
package config

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

const storeConfigFixture = `# operator header
[server]
host = "127.0.0.1"
port = 23333
log_level = "info" # keep level comment

[database]
driver = "sqlite"
dsn = "./data/depsilo.db"

[storage]
type = "local"
path = "./data/cache"

[cache]
max_size_gb = 20
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90

[auth]
enabled = true
jwt_secret = "test-secret"
token_ttl = "168h"

[custom]
untouched = "preserve me" # keep custom comment
`

var settingsEnvironmentNames = []string{
	"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_SERVER_LOG_LEVEL",
	"DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_TYPE", "DEPSILO_STORAGE_PATH",
	"DEPSILO_CACHE_MAX_SIZE_GB", "DEPSILO_CACHE_TTL_INDEX", "DEPSILO_CACHE_TTL_BLOB",
	"DEPSILO_CACHE_LRU_THRESHOLD", "DEPSILO_AUTH_TOKEN_TTL",
}

func clearSettingsEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range settingsEnvironmentNames { t.Setenv(name, "") }
}

func writeStoreFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(storeConfigFixture), 0o640); err != nil { t.Fatal(err) }
	return path
}

func loadStoreFixture(t *testing.T, path string) (*Config, zap.AtomicLevel) {
	t.Helper()
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	parsed, err := zap.ParseAtomicLevel(cfg.Server.LogLevel)
	if err != nil { t.Fatal(err) }
	return cfg, parsed
}

func newStoreFixture(t *testing.T) (string, *Store, zap.AtomicLevel) {
	t.Helper()
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	return path, NewStore(path, cfg, level), level
}

func assertPaths(t *testing.T, got, want []SettingPath) {
	t.Helper()
	if !reflect.DeepEqual(got, want) { t.Fatalf("paths = %#v, want %#v", got, want) }
}

func assertStoreCode(t *testing.T, err error, want StoreErrorCode) {
	t.Helper()
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != want { t.Fatalf("error = %v, want code %s", err, want) }
}

type failingWriter struct{ err error }
func (w failingWriter) Write(string, []byte, fs.FileMode) error { return w.err }

func TestStoreUpdatePersistsClassifiesAndPreservesComments(t *testing.T) {
	path, store, level := newStoreFixture(t)
	logLevel, blobTTL := "debug", "96h"
	result, err := store.Update(context.Background(), SettingsPatch{
		Server: &SettingsServerPatch{LogLevel: &logLevel},
		Cache: &SettingsCachePatch{TTLBlob: &blobTTL},
	})
	if err != nil { t.Fatal(err) }
	assertPaths(t, result.Changed, []SettingPath{SettingServerLogLevel, SettingCacheTTLBlob})
	assertPaths(t, result.AppliedNow, []SettingPath{SettingServerLogLevel})
	assertPaths(t, result.RestartRequired, []SettingPath{SettingCacheTTLBlob})
	assertPaths(t, result.BlockedByOverride, []SettingPath{})
	assertPaths(t, result.PendingRestart, []SettingPath{SettingCacheTTLBlob})
	if result.Configured.Cache.TTLBlob != "96h" || result.Effective.Cache.TTLBlob != "72h" || result.Effective.Server.LogLevel != "debug" { t.Fatalf("result = %+v", result) }
	if level.Level() != zap.DebugLevel { t.Fatalf("atomic level = %s", level.Level()) }
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	if !bytes.Contains(data, []byte(`untouched = "preserve me" # keep custom comment`)) || !bytes.Contains(data, []byte(`log_level = "debug" # keep level comment`)) { t.Fatalf("comments changed:\n%s", data) }
	info, err := os.Stat(path); if err != nil { t.Fatal(err) }
	if info.Mode().Perm() != 0o640 { t.Fatalf("mode = %o", info.Mode().Perm()) }
}

func TestStoreUpdateRejectsInvalidPatchWithoutWriting(t *testing.T) {
	tests := []struct{ name string; patch func() SettingsPatch }{
		{"max size", func() SettingsPatch { value := 0; return SettingsPatch{Cache:&SettingsCachePatch{MaxSizeGB:&value}} }},
		{"index ttl", func() SettingsPatch { value := "bad"; return SettingsPatch{Cache:&SettingsCachePatch{TTLIndex:&value}} }},
		{"lru", func() SettingsPatch { value := 101; return SettingsPatch{Cache:&SettingsCachePatch{LRUThreshold:&value}} }},
		{"token never", func() SettingsPatch { value := "never"; return SettingsPatch{Auth:&SettingsAuthPatch{TokenTTL:&value}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, store, level := newStoreFixture(t)
			before, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
			_, err = store.Update(context.Background(), tt.patch())
			assertStoreCode(t, err, StoreInvalidSetting)
			after, readErr := os.ReadFile(path); if readErr != nil { t.Fatal(readErr) }
			if !bytes.Equal(after, before) { t.Fatalf("invalid patch changed disk:\n%s", after) }
			if level.Level() != zap.InfoLevel { t.Fatalf("level changed to %s", level.Level()) }
		})
	}
}

func TestStoreUpdateReadOnlyFile(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0o440); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chmod(path, 0o640) })
	value := "24h"
	_, err := store.Update(context.Background(), SettingsPatch{Auth:&SettingsAuthPatch{TokenTTL:&value}})
	assertStoreCode(t, err, StoreConfigReadOnly)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) { t.Fatal("read-only update changed disk") }
}

func TestStoreUpdateAtomicFailureLeavesFileAndEffectiveUntouched(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	store := newStore(path, cfg, level, failingWriter{err: errors.New("rename failed")})
	before, _ := os.ReadFile(path)
	value := "debug"
	_, err := store.Update(context.Background(), SettingsPatch{Server:&SettingsServerPatch{LogLevel:&value}})
	assertStoreCode(t, err, StoreConfigWriteFailed)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) { t.Fatal("failed atomic write changed disk") }
	state, err := store.Snapshot(context.Background()); if err != nil { t.Fatal(err) }
	if state.Effective.Server.LogLevel != "info" || level.Level() != zap.InfoLevel { t.Fatalf("effective/level changed: %+v %s", state.Effective, level.Level()) }
}

func TestStoreConcurrentUpdatesMergeDistinctFields(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	maxSize, tokenTTL := 40, "24h"
	patches := []SettingsPatch{{Cache:&SettingsCachePatch{MaxSizeGB:&maxSize}}, {Auth:&SettingsAuthPatch{TokenTTL:&tokenTTL}}}
	errs := make(chan error, len(patches))
	var wg sync.WaitGroup
	for _, patch := range patches {
		patch := patch; wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Update(context.Background(), patch); errs <- err }()
	}
	wg.Wait(); close(errs)
	for err := range errs { if err != nil { t.Fatal(err) } }
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	cfg, err := decodeConfigDocument(data); if err != nil { t.Fatal(err) }
	if cfg.Cache.MaxSizeGB != 40 || cfg.Auth.TokenTTL != 24*time.Hour { t.Fatalf("merged config = %+v %+v", cfg.Cache, cfg.Auth) }
}

func TestStoreEnvironmentOverrideIsBlockedButPersisted(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	t.Setenv("DEPSILO_SERVER_LOG_LEVEL", "debug")
	cfg, level := loadStoreFixture(t, path)
	store := NewStore(path, cfg, level)
	value := "error"
	result, err := store.Update(context.Background(), SettingsPatch{Server:&SettingsServerPatch{LogLevel:&value}})
	if err != nil { t.Fatal(err) }
	assertPaths(t, result.Changed, []SettingPath{SettingServerLogLevel})
	assertPaths(t, result.AppliedNow, []SettingPath{})
	assertPaths(t, result.RestartRequired, []SettingPath{})
	assertPaths(t, result.BlockedByOverride, []SettingPath{SettingServerLogLevel})
	if result.Configured.Server.LogLevel != "error" || result.Effective.Server.LogLevel != "debug" || result.Sources[SettingServerLogLevel] != SettingSourceEnv || result.Overrides[SettingServerLogLevel] != "DEPSILO_SERVER_LOG_LEVEL" { t.Fatalf("result = %+v", result) }
	if level.Level() != zap.DebugLevel { t.Fatalf("override level changed to %s", level.Level()) }
	data, _ := os.ReadFile(path); if !bytes.Contains(data, []byte(`log_level = "error"`)) { t.Fatalf("file not updated:\n%s", data) }
}

func TestStoreSnapshotSourcesCoverEveryPath(t *testing.T) {
	_, store, _ := newStoreFixture(t)
	state, err := store.Snapshot(context.Background()); if err != nil { t.Fatal(err) }
	if state.Sources == nil || state.Overrides == nil || state.PendingRestart == nil || state.Editable == nil { t.Fatalf("nil collection in %+v", state) }
	if len(state.Sources) != len(AllSettingPaths()) { t.Fatalf("sources = %#v", state.Sources) }
	for _, path := range AllSettingPaths() { if state.Sources[path] != SettingSourceFile { t.Fatalf("source[%s] = %s", path, state.Sources[path]) } }
}

func TestStoreUpdatedFileLoadsAfterRestart(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	value := "24h"
	if _, err := store.Update(context.Background(), SettingsPatch{Auth:&SettingsAuthPatch{TokenTTL:&value}}); err != nil { t.Fatal(err) }
	t.Setenv("DEPSILO_CONFIG", path)
	reloaded, err := Load(); if err != nil { t.Fatal(err) }
	if reloaded.Auth.TokenTTL != 24*time.Hour { t.Fatalf("reloaded token ttl = %s", reloaded.Auth.TokenTTL) }
}
```

- [ ] **Step 2: Run and verify the missing Store failure**

Run: `go test -race ./internal/config -run TestStore`

Expected: FAIL with undefined `Store`/`NewStore`.

- [ ] **Step 3: Implement Store errors, provenance, and snapshots**

Use this exact state owner in `internal/config/store.go`:

```go
type StoreErrorCode string
const (
	StoreInvalidSetting StoreErrorCode = "INVALID_SETTING"
	StoreConfigReadOnly StoreErrorCode = "CONFIG_READ_ONLY"
	StoreConfigReadFailed StoreErrorCode = "CONFIG_READ_FAILED"
	StoreConfigWriteFailed StoreErrorCode = "CONFIG_WRITE_FAILED"
)
type StoreError struct { Code StoreErrorCode; Err error }
func (e *StoreError) Error() string { return e.Err.Error() }
func (e *StoreError) Unwrap() error { return e.Err }

type Store struct {
	mu sync.Mutex
	path string
	effective SettingsSnapshot
	overrides map[SettingPath]string
	logLevel zap.AtomicLevel
	writer atomicFileWriter
}

func NewStore(path string, effective *Config, level zap.AtomicLevel) *Store {
	return newStore(path, effective, level, osAtomicFileWriter{})
}
```

Capture non-empty environment variables once at construction, matching Viper's boot-effective state, with this complete mapping:

```go
var settingEnvNames = map[SettingPath]string{
	SettingServerHost: "DEPSILO_SERVER_HOST", SettingServerPort: "DEPSILO_SERVER_PORT", SettingServerLogLevel: "DEPSILO_SERVER_LOG_LEVEL",
	SettingDatabaseDriver: "DEPSILO_DATABASE_DRIVER", SettingStorageType: "DEPSILO_STORAGE_TYPE", SettingStoragePath: "DEPSILO_STORAGE_PATH",
	SettingCacheMaxSizeGB: "DEPSILO_CACHE_MAX_SIZE_GB", SettingCacheTTLIndex: "DEPSILO_CACHE_TTL_INDEX", SettingCacheTTLBlob: "DEPSILO_CACHE_TTL_BLOB",
	SettingCacheLRUThreshold: "DEPSILO_CACHE_LRU_THRESHOLD", SettingAuthTokenTTL: "DEPSILO_AUTH_TOKEN_TTL",
}
```

`Snapshot` takes the mutex, honors `ctx.Err()`, reads and decodes the current file (or defaults if it does not exist), gets explicit paths from `indexSettingsDocument`, and returns non-nil maps/slices. Source precedence is `env > file > default`. `pending_restart` iterates `restartSettingPaths`, excludes override paths, and compares configured with the captured process-effective snapshot. `config_writable` comes from Task 2's probe.

- [ ] **Step 4: Implement serialized update and exact classifications**

`Update` must execute in this order under `mu`:

1. Reject an empty patch as `StoreInvalidSetting`.
2. Read/decode/index the current file; map read/parse failures to `StoreConfigReadFailed`.
3. Filter out fields already explicitly present with the same semantic value; a missing key set to its default still counts as changed because its source becomes `file`.
4. Patch only the remaining request fields and validate the complete decoded document.
5. If no fields changed, return the current state with four allocated empty result slices and do not touch the file.
6. Return `StoreConfigReadOnly` if the permission/probe check fails.
7. Call the atomic writer with the original permission bits (or `0644` for a missing file); map every failure to `StoreConfigWriteFailed`.
8. Only after persistence succeeds, apply a changed, non-overridden log level with `s.logLevel.SetLevel(parsed.Level())` and update `s.effective.Server.LogLevel`.
9. Build result lists in canonical editable order: overridden changes -> blocked; non-overridden log -> applied; all other non-overridden changes -> restart required.
10. Build `pending_restart` from all current configured/effective differences, not merely this request's changes.

Keep effective cache/auth values unchanged in memory. Never mutate the startup `*Config`, which is read concurrently by existing runtime components.

- [ ] **Step 5: Run Store race tests and full config tests**

Run:

```bash
go test -race ./internal/config -run TestStore -count=10
go test -race ./internal/config
```

Expected: PASS with no race reports; the concurrent test is stable across ten runs.

- [ ] **Step 6: Commit Store semantics**

```bash
git add internal/config/store.go internal/config/store_test.go
git commit -m "feat(config): add settings state store"
```

### Task 5: Expose the strict Settings HTTP contract and wire the Store

**Files:**
- Modify: `internal/api/admin/settings.go`
- Create: `internal/api/admin/settings_test.go`
- Modify: `internal/api/router.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `config.Store.Snapshot/Update`.
- Produces: exact Go DTOs `AdminSettingsResponse`, `UpdateAdminSettingsRequest`, and `UpdateAdminSettingsResponse` matching the TypeScript contract in Task 6.
- Changes: `NewSettingsHandler(store settingsStore) *SettingsHandler`; the private interface has the same two methods as `config.Store` for deterministic handler tests.
- Changes: `api.Deps` gains `ConfigStore *config.Store`.

- [ ] **Step 1: Write failing GET/PUT contract tests**

Create `internal/api/admin/settings_test.go` in package `admin` with this complete real-Store fixture, strict-body table, and Store-error stub:

```go
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

const settingsHandlerConfig = `# handler fixture
[server]
host = "127.0.0.1"
port = 23333
log_level = "info"
[database]
driver = "sqlite"
dsn = "./data/depsilo.db"
[storage]
type = "local"
path = "./data/cache"
[cache]
max_size_gb = 20
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90
[auth]
enabled = true
jwt_secret = "test-secret"
token_ttl = "168h"
[custom]
keep = "untouched"
`

var settingsHandlerEnv = []string{
	"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_SERVER_LOG_LEVEL",
	"DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_TYPE", "DEPSILO_STORAGE_PATH",
	"DEPSILO_CACHE_MAX_SIZE_GB", "DEPSILO_CACHE_TTL_INDEX", "DEPSILO_CACHE_TTL_BLOB",
	"DEPSILO_CACHE_LRU_THRESHOLD", "DEPSILO_AUTH_TOKEN_TTL",
}

func newSettingsHandlerFixture(t *testing.T) (*gin.Engine, string, zap.AtomicLevel) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	for _, name := range settingsHandlerEnv { t.Setenv(name, "") }
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(settingsHandlerConfig), 0o640); err != nil { t.Fatal(err) }
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := config.Load(); if err != nil { t.Fatal(err) }
	level, err := zap.ParseAtomicLevel(cfg.Server.LogLevel); if err != nil { t.Fatal(err) }
	handler := NewSettingsHandler(config.NewStore(path, cfg, level))
	router := gin.New()
	router.GET("/settings", handler.Get)
	router.PUT("/settings", handler.Update)
	return router, path, level
}

func performSettingsRequest(router http.Handler, method, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/settings", strings.NewReader(body))
	if body != "" { request.Header.Set("Content-Type", "application/json") }
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertSettingPaths(t *testing.T, got, want []config.SettingPath) {
	t.Helper()
	if !reflect.DeepEqual(got, want) { t.Fatalf("paths = %#v, want %#v", got, want) }
}

func responseCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct { Code string `json:"code"` }
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil { t.Fatalf("decode error response: %v; body=%s", err, recorder.Body.String()) }
	return response.Code
}

func TestSettingsGetReturnsCompleteConfiguredEffectiveContract(t *testing.T) {
	router, path, _ := newSettingsHandlerFixture(t)
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	data = bytes.Replace(data, []byte(`ttl_blob = "72h"`), []byte(`ttl_blob = "96h"`), 1)
	if err := os.WriteFile(path, data, 0o640); err != nil { t.Fatal(err) }

	recorder := performSettingsRequest(router, http.MethodGet, "")
	if recorder.Code != http.StatusOK { t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String()) }
	var response AdminSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	if response.Configured.Server.Host != "127.0.0.1" || response.Configured.Server.Port != 23333 || response.Configured.Database.Driver != "sqlite" || response.Configured.Storage.Type != "local" || response.Configured.Storage.Path != "./data/cache" { t.Fatalf("configured identity = %+v", response.Configured) }
	if response.Configured.Cache.TTLBlob != "96h" || response.Effective.Cache.TTLBlob != "72h" || response.Configured.Auth.TokenTTL != "168h" { t.Fatalf("configured/effective = %+v / %+v", response.Configured, response.Effective) }
	assertSettingPaths(t, response.PendingRestart, []config.SettingPath{config.SettingCacheTTLBlob})
	if response.Sources == nil || response.Overrides == nil || response.Editable == nil || len(response.Sources) != 11 || len(response.Editable) != 6 || !response.ConfigWritable { t.Fatalf("metadata = %+v", response) }

	var top map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &top); err != nil { t.Fatal(err) }
	var configured map[string]json.RawMessage
	if err := json.Unmarshal(top["configured"], &configured); err != nil { t.Fatal(err) }
	var auth map[string]json.RawMessage
	if err := json.Unmarshal(configured["auth"], &auth); err != nil { t.Fatal(err) }
	if _, exists := auth["enabled"]; exists { t.Fatalf("auth.enabled leaked in %s", configured["auth"]) }
}

func TestSettingsPutReturnsCompleteAppliedAndRestartContract(t *testing.T) {
	router, path, level := newSettingsHandlerFixture(t)
	body := `{"server":{"log_level":"debug"},"cache":{"ttl_blob":"96h"}}`
	recorder := performSettingsRequest(router, http.MethodPut, body)
	if recorder.Code != http.StatusOK { t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String()) }
	var response UpdateAdminSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	assertSettingPaths(t, response.Changed, []config.SettingPath{config.SettingServerLogLevel, config.SettingCacheTTLBlob})
	assertSettingPaths(t, response.AppliedNow, []config.SettingPath{config.SettingServerLogLevel})
	assertSettingPaths(t, response.RestartRequired, []config.SettingPath{config.SettingCacheTTLBlob})
	assertSettingPaths(t, response.BlockedByOverride, []config.SettingPath{})
	assertSettingPaths(t, response.PendingRestart, []config.SettingPath{config.SettingCacheTTLBlob})
	if response.Configured.Server.LogLevel != "debug" || response.Effective.Server.LogLevel != "debug" || response.Configured.Cache.TTLBlob != "96h" || response.Effective.Cache.TTLBlob != "72h" { t.Fatalf("response = %+v", response) }
	if response.Configured.Server.Host != "127.0.0.1" || response.Configured.Database.Driver != "sqlite" || response.Configured.Storage.Path != "./data/cache" || response.Configured.Auth.TokenTTL != "168h" || len(response.Sources) != 11 || len(response.Editable) != 6 { t.Fatalf("incomplete response = %+v", response) }
	if level.Level() != zap.DebugLevel { t.Fatalf("level = %s", level.Level()) }
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	if !bytes.Contains(data, []byte(`log_level = "debug"`)) || !bytes.Contains(data, []byte(`ttl_blob = "96h"`)) || !bytes.Contains(data, []byte(`keep = "untouched"`)) { t.Fatalf("disk =\n%s", data) }
}

func TestSettingsPutRejectsEmptyUnknownAndTrailingJSON(t *testing.T) {
	tests := []string{
		`{}`,
		`{"auth":{"enabled":false}}`,
		`{"server":{"host":"0.0.0.0"}}`,
		`{"cache":{"ttl_blob":"96h"}} {"cache":{"ttl_blob":"24h"}}`,
		`{`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			router, path, _ := newSettingsHandlerFixture(t)
			before, _ := os.ReadFile(path)
			recorder := performSettingsRequest(router, http.MethodPut, body)
			if recorder.Code != http.StatusBadRequest || responseCode(t, recorder) != "BAD_REQUEST" { t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String()) }
			after, _ := os.ReadFile(path); if !bytes.Equal(after, before) { t.Fatal("bad request changed disk") }
		})
	}
}

func TestSettingsPutRejectsInvalidValuesWithoutDiskMutation(t *testing.T) {
	tests := []string{
		`{"cache":{"ttl_index":"bad"}}`,
		`{"cache":{"max_size_gb":0}}`,
		`{"cache":{"lru_threshold":101}}`,
		`{"auth":{"token_ttl":"never"}}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			router, path, _ := newSettingsHandlerFixture(t)
			before, _ := os.ReadFile(path)
			recorder := performSettingsRequest(router, http.MethodPut, body)
			if recorder.Code != http.StatusUnprocessableEntity || responseCode(t, recorder) != "INVALID_SETTING" { t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String()) }
			after, _ := os.ReadFile(path); if !bytes.Equal(after, before) { t.Fatal("invalid setting changed disk") }
		})
	}
}

type stubSettingsStore struct {
	snapshot config.SettingsState
	update config.SettingsUpdateResult
	snapshotErr error
	updateErr error
}
func (s *stubSettingsStore) Snapshot(context.Context) (config.SettingsState, error) { return s.snapshot, s.snapshotErr }
func (s *stubSettingsStore) Update(context.Context, config.SettingsPatch) (config.SettingsUpdateResult, error) { return s.update, s.updateErr }

func TestSettingsHandlerMapsStoreErrors(t *testing.T) {
	tests := []struct {
		name, method string
		err *config.StoreError
		status int
		code string
	}{
		{"get read failure", http.MethodGet, &config.StoreError{Code:config.StoreConfigReadFailed, Err:errors.New("read failed")}, 500, "CONFIG_READ_FAILED"},
		{"put invalid", http.MethodPut, &config.StoreError{Code:config.StoreInvalidSetting, Err:errors.New("invalid")}, 422, "INVALID_SETTING"},
		{"put readonly", http.MethodPut, &config.StoreError{Code:config.StoreConfigReadOnly, Err:errors.New("readonly")}, 409, "CONFIG_READ_ONLY"},
		{"put read failure", http.MethodPut, &config.StoreError{Code:config.StoreConfigReadFailed, Err:errors.New("read failed")}, 500, "CONFIG_READ_FAILED"},
		{"put write failure", http.MethodPut, &config.StoreError{Code:config.StoreConfigWriteFailed, Err:errors.New("write failed")}, 500, "CONFIG_WRITE_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubSettingsStore{}
			if tt.method == http.MethodGet { stub.snapshotErr = tt.err } else { stub.updateErr = tt.err }
			handler := NewSettingsHandler(stub)
			router := gin.New(); router.GET("/settings", handler.Get); router.PUT("/settings", handler.Update)
			body := ""; if tt.method == http.MethodPut { body = `{"server":{"log_level":"debug"}}` }
			recorder := performSettingsRequest(router, tt.method, body)
			if recorder.Code != tt.status || responseCode(t, recorder) != tt.code { t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String()) }
		})
	}
}
```

- [ ] **Step 2: Run and verify the old-contract failures**

Run: `go test ./internal/api/admin -run TestSettings`

Expected: FAIL because the old handler constructor/response/mutation contract differs and has no tests package yet.

- [ ] **Step 3: Replace the handler with explicit DTOs and strict JSON decoding**

Use these exact structs in `internal/api/admin/settings.go`:

```go
type AdminSettingsResponse struct {
	Configured config.SettingsSnapshot `json:"configured"`
	Effective config.SettingsSnapshot `json:"effective"`
	PendingRestart []config.SettingPath `json:"pending_restart"`
	Overrides map[config.SettingPath]string `json:"overrides"`
	Sources map[config.SettingPath]config.SettingSource `json:"sources"`
	Editable []config.SettingPath `json:"editable"`
	ConfigWritable bool `json:"config_writable"`
}
type UpdateAdminSettingsRequest struct {
	Server *struct { LogLevel *string `json:"log_level"` } `json:"server"`
	Cache *struct { MaxSizeGB *int `json:"max_size_gb"`; TTLIndex *string `json:"ttl_index"`; TTLBlob *string `json:"ttl_blob"`; LRUThreshold *int `json:"lru_threshold"` } `json:"cache"`
	Auth *struct { TokenTTL *string `json:"token_ttl"` } `json:"auth"`
}
type UpdateAdminSettingsResponse struct {
	AdminSettingsResponse
	Changed []config.SettingPath `json:"changed"`
	AppliedNow []config.SettingPath `json:"applied_now"`
	RestartRequired []config.SettingPath `json:"restart_required"`
	BlockedByOverride []config.SettingPath `json:"blocked_by_override"`
}
```

Decode through `json.Decoder.DisallowUnknownFields()`, require EOF after the first value, convert pointer fields to `config.SettingsPatch`, and reject a patch with no leaf field. `Get` calls `Snapshot(c.Request.Context())`; `Update` calls `Update`. A single error writer maps Store codes to the statuses fixed in Global Constraints, while malformed/unknown JSON stays `400 BAD_REQUEST`.

- [ ] **Step 4: Wire one Store instance through server and router**

After applying the boot log level in `StartServer`, construct:

```go
settingsStore := config.NewStore(cfg.ConfigPath, cfg, logLevel)
```

Add `ConfigStore *config.Store` to `api.Deps`, pass it in the existing `api.Deps{...}` literal, and replace:

```go
settingsHandler := admin.NewSettingsHandler(deps.ConfigStore)
```

Do not repurpose `deps.Config`; existing startup/runtime consumers continue using the boot-effective config.

- [ ] **Step 5: Run API/config/server race tests**

Run:

```bash
go test -race ./internal/config ./internal/api/admin ./internal/server
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit the HTTP control plane**

```bash
git add internal/api/admin/settings.go internal/api/admin/settings_test.go
git add -p -- internal/api/router.go internal/server/server.go
git commit -m "feat(admin): expose truthful settings contract"
```

### Task 6: Integrate the typed Settings status UI after Plan 04 foundations

**Depends on:** Plan 01 `usePrincipal().canWrite`; Plan 04 Task 1 fixture, Task 3 fields, Task 4 notices/toast, and Task 5 Tabs/`useMediaQuery`. Do not start this task until those commits are present.

**Files:**
- Modify: `web/src/lib/adminApi.types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/admin/pages/Settings.tsx`
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh.ts`
- Create: `web/e2e/admin-settings.spec.ts`

**Interfaces:**
- Consumes: Plan 01/04 interfaces listed under Cross-Plan Interfaces.
- Produces: exact TypeScript DTOs matching Task 5.
- Produces: `adminApi.getSettings(): Promise<AxiosResponse<AdminSettingsResponse>>` and typed `updateSettings(request)`.
- Keeps: Webhook behavior/implementation owned by Plan 04; Settings only renders `<WebhookTab />` as one tab panel.

- [ ] **Step 1: Write failing Settings browser contracts**

Create `web/e2e/admin-settings.spec.ts` using `test`, `expect`, and `mockAdminApi` from `./fixtures/admin-api`. Cover these exact cases:

```ts
test('sends only dirty settings and renders server-authored restart status', async ({ page }) => {
  let requestBody: unknown
  await page.route('**/api/v1/admin/settings', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    requestBody = route.request().postDataJSON()
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(updateResponse) })
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /缓存策略/ }).click()
  await page.getByLabel(/索引 TTL/).fill('10m')
  await page.getByRole('button', { name: /^保存$/ }).click()
  expect(requestBody).toEqual({ cache: { ttl_index: '10m' } })
  await expect(page.getByText(/重启后生效/)).toBeVisible()
})

test('keeps dirty input and never reports success after 422', async ({ page }) => {
  await mockAdminApi(page, { 'PUT /api/v1/admin/settings': { status: 422, body: { code: 'INVALID_SETTING', message: 'bad ttl' } } })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /缓存策略/ }).click()
  const ttl = page.getByLabel(/索引 TTL/)
  await ttl.fill('bad')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect(page.getByRole('alert')).toContainText('bad ttl')
  await expect(ttl).toHaveValue('bad')
  await expect(page.getByText(/^已保存$/)).toHaveCount(0)
})
```

Define `updateResponse` in the spec as a complete snapshot plus `changed`, `applied_now`, `restart_required`, and `blocked_by_override`; never use a partial cast. Add tests for an environment override hint naming `DEPSILO_SERVER_LOG_LEVEL`, a `config_writable:false` response, a principal fixture whose wire DTO has `can_write:false` (consumed by the page as `usePrincipal().canWrite === false`), initial 500 -> Retry -> success, cached data retained on refetch failure, and widths 320/390/768. At 320/390 assert `documentElement.scrollWidth === innerWidth` and horizontal tab orientation; at 768 assert vertical orientation and a 180px tab rail.

- [ ] **Step 2: Run and confirm current DTO/layout/feedback failures**

Run: `cd web && npm run test:e2e -- admin-settings.spec.ts`

Expected: FAIL because Settings uses `Record<string, any>`, fixed desktop layout, submits all fields including `auth.enabled`, and treats completion as success without service result arrays.

- [ ] **Step 3: Add the exact TypeScript contract and Axios generics**

Append this Settings section to Plan 01's `web/src/lib/adminApi.types.ts`:

```ts
export interface AdminSettingsSnapshot {
  server: { host: string; port: number; log_level: 'debug' | 'info' | 'warn' | 'error' }
  database: { driver: string }
  storage: { type: string; path: string }
  cache: { max_size_gb: number; ttl_index: string; ttl_blob: string; lru_threshold: number }
  auth: { token_ttl: string }
}
export type SettingPath =
  | 'server.host' | 'server.port' | 'server.log_level'
  | 'database.driver' | 'storage.type' | 'storage.path'
  | 'cache.max_size_gb' | 'cache.ttl_index' | 'cache.ttl_blob'
  | 'cache.lru_threshold' | 'auth.token_ttl'
export type EditableSettingPath =
  | 'server.log_level' | 'cache.max_size_gb' | 'cache.ttl_index'
  | 'cache.ttl_blob' | 'cache.lru_threshold' | 'auth.token_ttl'
export type SettingSource = 'default' | 'file' | 'env'
export interface AdminSettingsResponse {
  configured: AdminSettingsSnapshot
  effective: AdminSettingsSnapshot
  pending_restart: EditableSettingPath[]
  overrides: Partial<Record<SettingPath, string>>
  sources: Record<SettingPath, SettingSource>
  editable: EditableSettingPath[]
  config_writable: boolean
}
export interface UpdateAdminSettingsRequest {
  server?: { log_level?: AdminSettingsSnapshot['server']['log_level'] }
  cache?: { max_size_gb?: number; ttl_index?: string; ttl_blob?: string; lru_threshold?: number }
  auth?: { token_ttl?: string }
}
export interface UpdateAdminSettingsResponse extends AdminSettingsResponse {
  changed: EditableSettingPath[]
  applied_now: EditableSettingPath[]
  restart_required: EditableSettingPath[]
  blocked_by_override: EditableSettingPath[]
}
```

In `web/src/lib/api.ts`, import the types and replace the two calls:

```ts
getSettings: () => api.get<AdminSettingsResponse>('/admin/settings'),
updateSettings: (data: UpdateAdminSettingsRequest) => api.put<UpdateAdminSettingsResponse>('/admin/settings', data),
```

- [ ] **Step 4: Replace Settings page state and mutation logic**

Read the dirty `Settings.tsx` first and retain the already-removed PostgreSQL option. Replace its flat `Record<string, any>` with this local draft and pure patch builder:

```ts
interface SettingsDraft {
  logLevel: AdminSettingsSnapshot['server']['log_level']
  maxSizeGB: string
  ttlIndex: string
  ttlBlob: string
  lruThreshold: string
  tokenTTL: string
}
const draftFrom = (s: AdminSettingsSnapshot): SettingsDraft => ({
  logLevel: s.server.log_level, maxSizeGB: String(s.cache.max_size_gb),
  ttlIndex: s.cache.ttl_index, ttlBlob: s.cache.ttl_blob,
  lruThreshold: String(s.cache.lru_threshold), tokenTTL: s.auth.token_ttl,
})
function buildPatch(draft: SettingsDraft, base: AdminSettingsSnapshot): UpdateAdminSettingsRequest | null {
  const request: UpdateAdminSettingsRequest = {}
  if (draft.logLevel !== base.server.log_level) request.server = { log_level: draft.logLevel }
  const cache: NonNullable<UpdateAdminSettingsRequest['cache']> = {}
  if (draft.maxSizeGB !== String(base.cache.max_size_gb)) cache.max_size_gb = Number(draft.maxSizeGB)
  if (draft.ttlIndex !== base.cache.ttl_index) cache.ttl_index = draft.ttlIndex
  if (draft.ttlBlob !== base.cache.ttl_blob) cache.ttl_blob = draft.ttlBlob
  if (draft.lruThreshold !== String(base.cache.lru_threshold)) cache.lru_threshold = Number(draft.lruThreshold)
  if (Object.keys(cache).length) request.cache = cache
  if (draft.tokenTTL !== base.auth.token_ttl) request.auth = { token_ttl: draft.tokenTTL }
  return Object.keys(request).length ? request : null
}
```

Use a typed `useQuery` returning `response.data` and `useMutation` returning the update response data. Do not overwrite a dirty draft during a background refetch. On success, put the complete response into `['admin','settings']`, reset the draft from `configured`, clear the inline error, and retain the mutation response for visible result notices. On error, keep the draft and show the server's `{code,message}` in `InlineNotice tone="danger"`; do not close anything or show a success toast.

Derive toast tone/copy from service arrays: blocked or restart results use warning; applied-only uses success; empty `changed` says no changes. Render persistent `pending_restart`, per-field environment override hints with the exact variable from `overrides`, the effective value when it differs, and `config_writable=false`. Destructure `const { canWrite } = usePrincipal()` and disable editable controls and Save when `!canWrite`, `!config_writable`, or mutation pending. Remove the auth switch, JWT secret, and `never` option; render token TTL as a text `InputV2` so arbitrary valid Go durations remain editable.

- [ ] **Step 5: Compose responsive tabs using only Plan 04 wrappers**

Build `TabsV2` items for General, Cache, Storage, Auth, and Webhooks; each item's `content` is its section. Plan 04's wrapper owns the horizontal list and the vertical `180px + minmax(0,1fr)` root layout, so Settings renders exactly one wrapper:

```tsx
const desktopTabs = useMediaQuery('(min-width: 768px)')
<TabsV2
  items={tabs}
  value={activeTab}
  onValueChange={value => setActiveTab(value as TabKey)}
  ariaLabel={t('settings.tabsLabel')}
  orientation={desktopTabs ? 'vertical' : 'horizontal'}
/>
```

There must be exactly one `tablist`. Every form grid is `grid grid-cols-1 gap-4 sm:grid-cols-2`. Stack the save bar on mobile with `flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-between`. Use read-only fields for host, port, database driver, storage type, and storage path; do not synthesize S3 credentials absent from the DTO.

- [ ] **Step 6: Add matched Settings copy**

Add the same leaf keys to `en.ts` and `zh.ts`: `tabsLabel`, `configuredValue`, `effectiveValue`, `sourceDefault`, `sourceFile`, `sourceEnv`, `envOverride`, `pendingRestartTitle`, `pendingRestartField`, `appliedNowTitle`, `restartRequiredTitle`, `blockedOverrideTitle`, `configReadOnlyTitle`, `configReadOnlyBody`, `readOnlyPrincipal`, `stale`, `loadError`, `saveError`, `noChanges`, `durationHint`, and labels for every canonical setting path. Replace the false existing `hotReloadNote` copy with log-level-only immediate semantics.

- [ ] **Step 7: Run Settings browser/type/i18n gates**

Run:

```bash
cd web
npm run test:e2e -- admin-settings.spec.ts
npm run type-check
npm run build
npx eslint src/admin/pages/Settings.tsx src/lib/api.ts src/lib/adminApi.types.ts
cd ..
python3 scripts/i18n-audit.py
```

Expected: all commands PASS; browser test records no unmatched `/api/v1/**` request or console error.

- [ ] **Step 8: Commit the Settings frontend**

```bash
git add web/src/lib/adminApi.types.ts web/e2e/admin-settings.spec.ts
git add -p -- web/src/lib/api.ts web/src/admin/pages/Settings.tsx web/src/i18n/en.ts web/src/i18n/zh.ts
git commit -m "fix(admin): show truthful settings application state"
```

### Task 7: Final Settings verification and dirty-worktree audit

**Files:**
- Verify only; no planned source changes.

**Interfaces:**
- Confirms: persistence, API, logger, UI, race safety, and cross-plan integration all satisfy this plan together.

- [ ] **Step 1: Re-read every dirty target before resolving integration conflicts**

Run:

```bash
git status --short
git diff --check
git diff -- internal/config/config.go internal/config/loader_test.go config.example.toml web/src/admin/pages/Settings.tsx web/src/lib/api.ts web/src/i18n/en.ts web/src/i18n/zh.ts
```

Expected: no whitespace errors; the pre-existing database-driver comment, environment storage-path test, removed PostgreSQL UI option, API comment changes, pricing/i18n edits, and supply-chain example edits remain present.

- [ ] **Step 2: Run the required backend gates**

Run:

```bash
go test -race ./internal/config ./internal/upstream ./internal/middleware ./internal/api/...
go test ./...
```

Expected: PASS with no race reports.

- [ ] **Step 3: Run the required frontend gates**

Run:

```bash
cd web
npm run type-check
npm run build
npm run test:e2e
cd ..
python3 scripts/i18n-audit.py
```

Expected: PASS. Plan 04's visual matrix additionally verifies Settings at 320, 390, 768, 1024, and 1440 in light/dark and zh/en.

- [ ] **Step 4: Perform a manual atomic persistence smoke test**

Start Depsilo with a temporary copied config and `DEPSILO_SERVER_LOG_LEVEL` unset. Through the authenticated Settings UI, set log level to debug and blob TTL to `96h`. Verify the response/UI reports log level applied now and blob TTL pending restart; inspect the file to confirm only the two values changed and comments/mode remain. Restart against that file and verify `effective.cache.ttl_blob` becomes `96h` and `pending_restart` clears. Repeat once with `DEPSILO_SERVER_LOG_LEVEL=error` and verify the file changes but the UI reports the log field blocked by that exact override.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-10-admin-remediation-02-settings-control-plane.md`. Two execution options:

1. **Subagent-Driven (recommended)** - dispatch a fresh subagent per task, run two-stage review between tasks, and keep each task's commit independently reviewable. Required sub-skill: `superpowers:subagent-driven-development`.
2. **Inline Execution** - execute tasks in this session in batches with review checkpoints. Required sub-skill: `superpowers:executing-plans`.
