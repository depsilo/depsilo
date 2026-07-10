# Admin Dynamic Upstream Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the database authoritative for active non-Docker upstreams so Admin CRUD atomically changes the pools used by the next proxy request and remains correct across restarts.

**Architecture:** A one-time `ControlPlaneState` migration seeds ordinary ecosystem upstreams and persists the ordered active-ecosystem set. A process-owned `upstream.Registry` serializes mutations per ecosystem, prepares immutable `atomic.Pointer[poolSnapshot]` replacements and worker plans before the database commit, then publishes the already-built snapshot with one infallible store. Server assembly creates adapters and routes only for active ecosystems; Docker and extra-index pools remain config-owned.

**Tech Stack:** Go 1.25.6, Gin 1.12, GORM 1.31 with SQLite, `sync/atomic`, `net/http/httptest`, zap, React 19, TypeScript 5.9, Axios 1.14, TanStack Query 5.

## Global Constraints

- `docs/superpowers/specs/2026-07-10-admin-control-plane-ui-remediation-design.md` is the sole semantic authority for this plan; do not broaden behavior from legacy code or convenience assumptions.
- Preserve every pre-existing dirty-worktree change. Before editing, run `git diff -- internal/config/config.go internal/api/router.go internal/server/server.go web/src/lib/api.ts config.example.toml README.md docs/README_zh.md` and reread every affected function or documentation section; never use reset, checkout, clean, or whole-file replacement from `HEAD`.
- The starting worktree already modifies `internal/config/config.go`, `internal/api/router.go`, `internal/server/server.go`, `web/src/lib/api.ts`, `config.example.toml`, `README.md`, and `docs/README_zh.md`. Use the exact partial-stage commands in Tasks 1 and 7-9 for those seven paths; never stage a whole dirty file.
- Plan 01 owns the Principal/read-write route groups and creates `web/src/lib/adminApi.types.ts`; merge into those results rather than restoring today's router or API client.
- Ordinary upstream authority moves to the database. `config.toml` supplies first-seed defaults and can activate a previously inactive supported ecosystem only after an operator edits config and restarts.
- Only ordinary upstream lists explicitly present in `config.toml` are seed candidates. Viper defaults and environment-only values do not activate an ecosystem; in particular, the loader's built-in Alpine defaults must not make Alpine active.
- The active ecosystem order is exactly: `pypi`, `apt`, `npm`, `go`, `cargo`, `maven`, `rubygems`, `composer`, `nuget`, `conda`, `cran`, `alpine`, `helm`, `huggingface`.
- Docker never enters `upstream.Registry`; `/v2` continues to use `config.Docker`. `extra_indexes` continue to build config-owned pools on every start and are not returned by Admin upstream CRUD.
- Inactive ecosystems have no Pool, Adapter, standard route, or project-scoped route. Admin cannot activate them and returns `409 ECOSYSTEM_NOT_ACTIVE`.
- Every active ecosystem must contain at least one database upstream. For example, an empty active PyPI ecosystem fails startup with the exact message `active ecosystem pypi has no upstreams`; deleting the final source returns `409 LAST_UPSTREAM`.
- `Pool` identity is stable for the process lifetime. Adapters and selectors retain the same `*Pool`; mutations replace only its immutable snapshot through one `atomic.Pointer.Store`.
- Every mutation uses one per-ecosystem mutex and the sequence validate -> transaction mutation/read -> prebuild snapshot/clients/worker plan -> commit -> infallible swap -> cancel/start workers -> invariant check.
- A network-unhealthy manual check is a successful registry operation: return HTTP 200, persist the result with the correct `upstream_id`, and expose the failure string in `check.error`.
- Use the existing `{code,message}` error shape and existing `/api/v1/admin/upstreams` routes. Do not add `/v2` or return GORM models as API DTOs.
- UI appearance, Base UI primitives, responsive layout, icon styling, Tooltip, Toast, and shared error-state visuals belong exclusively to Plan 04. This plan changes only typed data flow and truthful runtime semantics on `Upstreams.tsx`.
- Backend gates are `go test -race ./internal/upstream ./internal/api/admin ./internal/api/public ./internal/server` and `go test ./...`.
- Frontend gates are `cd web && npm run type-check`, targeted `npm exec eslint` for `Upstreams.tsx` and the two Admin type files, an Upstream-method `any` scan in the historical-baseline `api.ts`, and `npm run build`; touched code must not add ESLint errors.

---

## File Structure

- `internal/db/control_plane.go` — create: persistent `ControlPlaneState` model only.
- `internal/db/repository.go` — modify: migrate `ControlPlaneState` with existing models.
- `internal/config/config.go` — modify carefully: retain which ordinary upstream lists were explicitly present in `config.toml`.
- `internal/config/loader.go` — modify after Plan 02: populate explicit ordinary-ecosystem metadata without treating Viper defaults or environment values as seed input.
- `internal/config/explicit_upstreams_test.go` — create: prove the built-in Alpine default is not an explicit seed source.
- `internal/upstream/bootstrap.go` — create: seed marker, active JSON, legacy-row reconciliation, and ordered active result.
- `internal/upstream/bootstrap_test.go` — create: first seed, old DB, deleted-row restart, new ecosystem activation, Docker/extra exclusion, and zero-source tests.
- `internal/upstream/pool.go` — modify: immutable snapshot pointer, record/config builders, stable Pool identity, and locked health state.
- `internal/upstream/pool_test.go` — create: snapshot replacement and concurrent read/report tests.
- `internal/upstream/selector.go` — modify: select only from `Pool.Snapshot()` and `Upstream.IsHealthy()`.
- `internal/upstream/health.go` — modify: shared proxy-aware probe, locked health update, `upstream_id` persistence, and config-owned worker compatibility.
- `internal/upstream/registry.go` — create: active pools, reads, per-ecosystem locks, worker handles, Start/Close, and degraded state.
- `internal/upstream/registry_mutation.go` — create: validated Create/Update/Delete, prepared transaction publication, recovery, and Check.
- `internal/upstream/registry_test.go` — create: active-pool construction and worker lifecycle coverage.
- `internal/upstream/registry_mutation_test.go` — create: transaction, publish/reload, worker replacement, commit-failure, and concurrent mutation coverage.
- `internal/upstream/registry_check_test.go` — create: proxy-aware manual checks and dynamic latency-log identity.
- `internal/api/public/now.go` — modify: snapshot/locked health rollup.
- `internal/api/public/stats.go` — modify: snapshot/locked health and latency grouping by upstream identity.
- `internal/api/admin/dashboard.go` — modify: snapshot/locked health and direct runtime IDs.
- `internal/api/admin/latency.go` — modify: query history by `upstream_id`.
- `internal/notify/scheduler.go` — modify: snapshot/locked health.
- `internal/api/admin/upstream_contract.go` — create: exact request/response DTOs and registry-error mapping.
- `internal/api/admin/upstream.go` — modify: delegate every operation to Registry.
- `internal/api/admin/upstream_test.go` — create: DTO, status, error, last-source, and unhealthy-check contracts.
- `internal/api/router.go` — modify carefully: inject Registry into the Plan 01 read/write route groups.
- `internal/server/server.go` — modify carefully: fixed startup order, active-only routes, registry lifecycle, Docker separation, and config-owned extras.
- `internal/server/upstream_registry_integration_test.go` — create: real `httptest` upstream and proxy request switching.
- `web/src/lib/adminApi.types.ts` — modify after Plan 01: exact Upstream DTOs.
- `web/src/lib/adminApi.types.type-test.ts` — modify after Plan 01: compile-time exactness/no-`any` assertions.
- `web/src/lib/api.ts` — modify carefully: typed Upstream Axios methods.
- `web/src/admin/pages/Upstreams.tsx` — modify: active-only choices and response-driven query cache updates without visual redesign.
- `config.example.toml` — modify: one-time seed/database-authority comments for ordinary upstreams.
- `README.md` — modify: English migration behavior.
- `docs/README_zh.md` — modify: Chinese migration behavior.

---

### Task 1: Persist Seed State and Reconcile the Active Ecosystem Set

**Files:**
- Create: `internal/db/control_plane.go`
- Modify: `internal/db/repository.go:72-110`
- Modify carefully: `internal/config/config.go:5-35`
- Modify after Plan 02: `internal/config/loader.go:15-105`
- Create: `internal/config/explicit_upstreams_test.go`
- Create: `internal/upstream/bootstrap.go`
- Create: `internal/upstream/bootstrap_test.go`

**Interfaces:**
- Produces: `db.ControlPlaneState{Key string, Value string, UpdatedAt time.Time}`.
- Produces: constants `SeedMarkerKey`, `ActiveEcosystemsKey`.
- Produces: `Config.ExplicitUpstreamEcosystems map[string]bool` populated only through `viper.InConfig`.
- Produces: package-private ordered `supportedEcosystems` and `type SeedSource struct { Ecosystem string; Upstreams []config.UpstreamConfig }`; callers pass only file-explicit ordinary ecosystem lists.
- Produces: `type BootstrapResult struct { ActiveEcosystems []string }`.
- Produces: `func ReconcileBootstrap(database *gorm.DB, sources []SeedSource) (BootstrapResult, error)`.

- [ ] **Step 1: Write failing migration and seed tests**

```go
// internal/config/explicit_upstreams_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTracksOnlyFileExplicitOrdinaryUpstreams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	document := []byte(`
[[pypi.upstreams]]
name = "primary"
url = "https://pypi.org"
priority = 1
`)
	if err := os.WriteFile(path, document, 0o600); err != nil { t.Fatal(err) }
	t.Setenv("DEPSILO_CONFIG", path)

	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if !cfg.ExplicitUpstreamEcosystems["pypi"] { t.Fatal("file-explicit pypi was not recorded") }
	if cfg.ExplicitUpstreamEcosystems["alpine"] { t.Fatal("defaulted alpine was recorded as file-explicit") }
	if len(cfg.Alpine.Upstreams) == 0 { t.Fatal("test did not retain the built-in Alpine runtime default") }
}
```

```go
// internal/upstream/bootstrap_test.go
package upstream

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"depsilo/internal/config"
	dbmodel "depsilo/internal/db"
	"gorm.io/gorm"
)

func bootstrapDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := dbmodel.Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil { t.Fatal(err) }
	if err := dbmodel.AutoMigrate(database); err != nil { t.Fatal(err) }
	return database
}

func source(name string, upstreams ...config.UpstreamConfig) SeedSource {
	return SeedSource{Ecosystem: name, Upstreams: upstreams}
}

func TestReconcileBootstrap_FirstSeedMergesLegacyRowsAndWritesBothStates(t *testing.T) {
	database := bootstrapDB(t)
	legacy := dbmodel.UpstreamRecord{AdapterType: "pypi", Name: "legacy", URL: "https://legacy.example", Priority: 1}
	if err := database.Create(&legacy).Error; err != nil { t.Fatal(err) }

	got, err := ReconcileBootstrap(database, []SeedSource{
		source("pypi", config.UpstreamConfig{Name: "legacy", URL: "https://config-must-not-overwrite.example", Priority: 9}, config.UpstreamConfig{Name: "fallback", URL: "https://fallback.example", Priority: 2}),
		source("npm", config.UpstreamConfig{Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1}),
	})
	if err != nil { t.Fatal(err) }
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(got.ActiveEcosystems, want) { t.Fatalf("active=%v want=%v", got.ActiveEcosystems, want) }

	var rows []dbmodel.UpstreamRecord
	if err := database.Order("adapter_type, name").Find(&rows).Error; err != nil { t.Fatal(err) }
	if len(rows) != 3 { t.Fatalf("rows=%d want=3", len(rows)) }
	var persistedLegacy dbmodel.UpstreamRecord
	if err := database.Where("adapter_type = ? AND name = ?", "pypi", "legacy").First(&persistedLegacy).Error; err != nil { t.Fatal(err) }
	if persistedLegacy.URL != "https://legacy.example" || persistedLegacy.Priority != 1 { t.Fatalf("legacy row overwritten: %#v", persistedLegacy) }

	var marker, activeState dbmodel.ControlPlaneState
	if err := database.First(&marker, "key = ?", SeedMarkerKey).Error; err != nil { t.Fatal(err) }
	if marker.Value != "true" { t.Fatalf("marker=%q", marker.Value) }
	if err := database.First(&activeState, "key = ?", ActiveEcosystemsKey).Error; err != nil { t.Fatal(err) }
	var stored []string
	if err := json.Unmarshal([]byte(activeState.Value), &stored); err != nil { t.Fatal(err) }
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(stored, want) { t.Fatalf("stored=%v want=%v", stored, want) }
}

func TestReconcileBootstrap_SeededRestartDoesNotRestoreDeletedActiveRow(t *testing.T) {
	database := bootstrapDB(t)
	sources := []SeedSource{source("pypi",
		config.UpstreamConfig{Name: "primary", URL: "https://one.example", Priority: 1},
		config.UpstreamConfig{Name: "secondary", URL: "https://two.example", Priority: 2},
	)}
	if _, err := ReconcileBootstrap(database, sources); err != nil { t.Fatal(err) }
	if err := database.Where("adapter_type = ? AND name = ?", "pypi", "secondary").Delete(&dbmodel.UpstreamRecord{}).Error; err != nil { t.Fatal(err) }
	if _, err := ReconcileBootstrap(database, sources); err != nil { t.Fatal(err) }
	var count int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count)
	if count != 1 { t.Fatalf("deleted config row was restored; count=%d", count) }
}

func TestReconcileBootstrap_SeededConfigActivatesOnlyNewEcosystem(t *testing.T) {
	database := bootstrapDB(t)
	if _, err := ReconcileBootstrap(database, []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})}); err != nil { t.Fatal(err) }

	got, err := ReconcileBootstrap(database, []SeedSource{
		source("pypi", config.UpstreamConfig{Name: "must-not-import", URL: "https://must-not-import.example", Priority: 2}),
		source("npm", config.UpstreamConfig{Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1}),
	})
	if err != nil { t.Fatal(err) }
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(got.ActiveEcosystems, want) { t.Fatalf("active=%v want=%v", got.ActiveEcosystems, want) }
	var npmCount, pypiCount int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "npm").Count(&npmCount)
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&pypiCount)
	if npmCount != 1 || pypiCount != 1 { t.Fatalf("npm=%d pypi=%d", npmCount, pypiCount) }
}

func TestReconcileBootstrap_ActiveEcosystemWithoutRowsFailsAndDoesNotReimport(t *testing.T) {
	database := bootstrapDB(t)
	sources := []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})}
	if _, err := ReconcileBootstrap(database, sources); err != nil { t.Fatal(err) }
	if err := database.Where("adapter_type = ?", "pypi").Delete(&dbmodel.UpstreamRecord{}).Error; err != nil { t.Fatal(err) }
	_, err := ReconcileBootstrap(database, sources)
	if err == nil || err.Error() != "active ecosystem pypi has no upstreams" { t.Fatalf("err=%v", err) }
	var count int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count)
	if count != 0 { t.Fatalf("active config was reimported; count=%d", count) }
}

func TestReconcileBootstrap_IgnoresDockerAndExtraRecords(t *testing.T) {
	database := bootstrapDB(t)
	for _, row := range []dbmodel.UpstreamRecord{
		{AdapterType: "docker", Name: "hub", URL: "https://registry-1.docker.io", Priority: 1},
		{AdapterType: "extra:private", Name: "private", URL: "https://private.example", Priority: 1},
	} { if err := database.Create(&row).Error; err != nil { t.Fatal(err) } }
	got, err := ReconcileBootstrap(database, []SeedSource{source("pypi")})
	if err != nil { t.Fatal(err) }
	if len(got.ActiveEcosystems) != 0 { t.Fatalf("active=%v", got.ActiveEcosystems) }
	var state dbmodel.ControlPlaneState
	if err := database.First(&state, "key = ?", ActiveEcosystemsKey).Error; err != nil { t.Fatal(err) }
	var stored []string
	if err := json.Unmarshal([]byte(state.Value), &stored); err != nil { t.Fatal(err) }
	if len(stored) != 0 { t.Fatalf("stored=%v", stored) }
}
```

- [ ] **Step 2: Run the focused tests and verify the missing model/API failure**

Run: `go test ./internal/config ./internal/upstream -run 'TestLoadTracksOnlyFileExplicitOrdinaryUpstreams|TestReconcileBootstrap' -count=1`

Expected: FAIL with undefined `Config.ExplicitUpstreamEcosystems`, `dbmodel.ControlPlaneState`, `SeedSource`, and `ReconcileBootstrap`.

- [ ] **Step 3: Add the model and migration**

```go
// internal/db/control_plane.go
package db

import "time"

type ControlPlaneState struct {
	Key       string    `gorm:"primaryKey;size:128" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}
```

Add `&ControlPlaneState{},` immediately after `&UpstreamRecord{},` in `db.AutoMigrate`.

- [ ] **Step 4: Record only file-explicit ordinary upstream lists**

Add this non-serialized loader metadata to `Config`:

```go
ExplicitUpstreamEcosystems map[string]bool `mapstructure:"-" json:"-"`
```

After Plan 02's `cfg, err := decodeViper(v)` succeeds inside `Load`, and before assigning `IsDefault`/`ConfigPath`, populate it with:

```go
func explicitUpstreamEcosystems(v *viper.Viper) map[string]bool {
	out := make(map[string]bool)
	for _, ecosystem := range []string{
		"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems",
		"composer", "nuget", "conda", "cran", "alpine", "helm", "huggingface",
	} {
		if v.InConfig(ecosystem + ".upstreams") { out[ecosystem] = true }
	}
	return out
}

cfg.ExplicitUpstreamEcosystems = explicitUpstreamEcosystems(v)
```

`InConfig` is mandatory here: `IsSet` would include Viper's built-in Alpine default and environment overrides, which the design does not define as `config.toml` seed data. Do not add this metadata in `decodeConfigDocument`; Plan 02's Settings Store parses configured/effective settings, not boot seed provenance.

- [ ] **Step 5: Implement transactional seed and active JSON reconciliation**

```go
// internal/upstream/bootstrap.go
package upstream

import (
	"encoding/json"
	"errors"
	"fmt"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"gorm.io/gorm"
)

const (
	SeedMarkerKey       = "upstreams_seeded_v1"
	ActiveEcosystemsKey = "upstreams_active_ecosystems_v1"
)

var supportedEcosystems = [...]string{
	"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems",
	"composer", "nuget", "conda", "cran", "alpine", "helm", "huggingface",
}

type SeedSource struct {
	Ecosystem string
	Upstreams []config.UpstreamConfig
}

type BootstrapResult struct { ActiveEcosystems []string }

func ReconcileBootstrap(database *gorm.DB, sources []SeedSource) (BootstrapResult, error) {
	explicit, err := indexSeedSources(sources)
	if err != nil { return BootstrapResult{}, err }
	var active []string
	err = database.Transaction(func(tx *gorm.DB) error {
		var marker db.ControlPlaneState
		markerErr := tx.First(&marker, "key = ?", SeedMarkerKey).Error
		if markerErr != nil && !errors.Is(markerErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load seed marker: %w", markerErr)
		}
		seeded := markerErr == nil && marker.Value == "true"
		if seeded {
			var state db.ControlPlaneState
			if err := tx.First(&state, "key = ?", ActiveEcosystemsKey).Error; err != nil { return fmt.Errorf("load active ecosystems: %w", err) }
			var stored []string
			if err := json.Unmarshal([]byte(state.Value), &stored); err != nil { return fmt.Errorf("decode active ecosystems: %w", err) }
			active, err = canonicalActive(stored)
			if err != nil { return err }
		} else {
			for _, ecosystem := range supportedEcosystems {
				var count int64
				if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", ecosystem).Count(&count).Error; err != nil { return err }
				src, configured := explicit[ecosystem]
				if count > 0 || configured && len(src.Upstreams) > 0 { active = append(active, ecosystem) }
				if configured {
					if err := insertMissingConfigRows(tx, src); err != nil { return err }
				}
			}
		}

		if seeded {
			activeSet := make(map[string]bool, len(active))
			for _, name := range active { activeSet[name] = true }
			for _, ecosystem := range supportedEcosystems {
				src, configured := explicit[ecosystem]
				if !configured || activeSet[ecosystem] || len(src.Upstreams) == 0 { continue }
				if err := insertMissingConfigRows(tx, src); err != nil { return err }
				activeSet[ecosystem] = true
			}
			active = active[:0]
			for _, ecosystem := range supportedEcosystems {
				if activeSet[ecosystem] { active = append(active, ecosystem) }
			}
		}

		for _, ecosystem := range active {
			var count int64
			if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", ecosystem).Count(&count).Error; err != nil { return err }
			if count == 0 { return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem) }
		}
		encoded, err := json.Marshal(active)
		if err != nil { return err }
		if err := tx.Save(&db.ControlPlaneState{Key: SeedMarkerKey, Value: "true"}).Error; err != nil { return err }
		return tx.Save(&db.ControlPlaneState{Key: ActiveEcosystemsKey, Value: string(encoded)}).Error
	})
	return BootstrapResult{ActiveEcosystems: append([]string(nil), active...)}, err
}

func indexSeedSources(sources []SeedSource) (map[string]SeedSource, error) {
	supported := make(map[string]bool, len(supportedEcosystems))
	for _, ecosystem := range supportedEcosystems { supported[ecosystem] = true }
	out := make(map[string]SeedSource, len(sources))
	for _, src := range sources {
		if !supported[src.Ecosystem] { return nil, fmt.Errorf("unsupported seed ecosystem %q", src.Ecosystem) }
		if _, exists := out[src.Ecosystem]; exists { return nil, fmt.Errorf("duplicate seed ecosystem %q", src.Ecosystem) }
		out[src.Ecosystem] = src
	}
	return out, nil
}

func canonicalActive(stored []string) ([]string, error) {
	wanted := make(map[string]bool, len(stored))
	supported := make(map[string]bool, len(supportedEcosystems))
	for _, ecosystem := range supportedEcosystems { supported[ecosystem] = true }
	for _, ecosystem := range stored {
		if !supported[ecosystem] { return nil, fmt.Errorf("unsupported active ecosystem %q", ecosystem) }
		if wanted[ecosystem] { return nil, fmt.Errorf("duplicate active ecosystem %q", ecosystem) }
		wanted[ecosystem] = true
	}
	active := make([]string, 0, len(wanted))
	for _, ecosystem := range supportedEcosystems {
		if wanted[ecosystem] { active = append(active, ecosystem) }
	}
	return active, nil
}

func insertMissingConfigRows(tx *gorm.DB, src SeedSource) error {
	for _, item := range src.Upstreams {
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ?", src.Ecosystem, item.Name).Count(&count).Error; err != nil { return err }
		if count != 0 { continue }
		mode := item.ProbeMode
		if mode == "" { mode = "active" }
		interval := item.ProbeInterval
		if interval == "" { interval = DefaultProbeInterval.String() }
		record := db.UpstreamRecord{AdapterType: src.Ecosystem, Name: item.Name, URL: item.URL, Proxy: item.Proxy, Priority: item.Priority, ProbeMode: mode, ProbeInterval: interval, Healthy: true, SuccessRate: 1}
		if err := tx.Create(&record).Error; err != nil { return fmt.Errorf("seed %s/%s: %w", src.Ecosystem, item.Name, err) }
	}
	return nil
}
```

- [ ] **Step 6: Run migration/seed tests**

Run: `go test ./internal/config ./internal/upstream -run 'TestLoadTracksOnlyFileExplicitOrdinaryUpstreams|TestReconcileBootstrap' -count=1`

Expected: PASS; the Alpine loader test proves defaults are not seed input, the seeded-restart and zero-source tests prove deleted rows are not reintroduced, and the new-ecosystem test proves only an inactive file-configured ecosystem is imported.

- [ ] **Step 7: Commit**

```bash
git add internal/db/control_plane.go internal/db/repository.go internal/config/loader.go internal/config/explicit_upstreams_test.go internal/upstream/bootstrap.go internal/upstream/bootstrap_test.go
git add -p -- internal/config/config.go
git commit -m "feat(upstream): persist registry seed state"
```

---

### Task 2: Replace Mutable Pool Slices with Atomic Immutable Snapshots

**Files:**
- Modify: `internal/upstream/pool.go`
- Modify: `internal/upstream/selector.go`
- Modify: `internal/upstream/health.go`
- Create: `internal/upstream/pool_test.go`
- Modify: `internal/api/public/now.go:97-113`
- Modify: `internal/api/public/stats.go:278-295`
- Modify: `internal/api/admin/dashboard.go:79-103`
- Modify: `internal/notify/scheduler.go:68-96`

**Interfaces:**
- Produces: `func NewPoolFromRecords(records []db.UpstreamRecord) (*Pool, error)`.
- Produces: `func (p *Pool) Snapshot() []*Upstream` and package-private `func (p *Pool) Replace(*poolSnapshot)`.
- Produces: `type HealthSnapshot struct { Healthy bool; AvgLatency time.Duration; SuccessRate float64; LastCheckedAt time.Time }` and `func (u *Upstream) HealthSnapshot() HealthSnapshot`.
- Produces: `ProbeResult` and locked `func (u *Upstream) applyProbe(ProbeResult)` for the existing checker and Task 3's worker refactor.
- Preserves: `NewPool([]config.UpstreamConfig)` for config-owned extra indexes.

- [ ] **Step 1: Write failing atomic snapshot and health race tests**

```go
// internal/upstream/pool_test.go
package upstream

import (
	"context"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestPoolReplaceChangesExistingSelectorWithoutChangingPoolIdentity(t *testing.T) {
	pool, err := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	if err != nil { t.Fatal(err) }
	selector := NewPrioritySelector(pool)
	next, err := buildPoolSnapshot([]db.UpstreamRecord{{ID: 2, AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 1, Healthy: true}}, pool.load())
	if err != nil { t.Fatal(err) }
	pool.Replace(next)
	got, err := selector.Select(context.Background())
	if err != nil { t.Fatal(err) }
	if got.ID != 2 { t.Fatalf("selected id=%d want=2", got.ID) }
}

func TestPoolSnapshotCannotBeMutatedByCaller(t *testing.T) {
	pool, _ := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	copy := pool.Snapshot()
	copy[0] = nil
	if pool.Snapshot()[0] == nil { t.Fatal("caller mutated live snapshot") }
}

func TestUpstreamHealthStateConcurrentReadWrite(t *testing.T) {
	pool, _ := NewPoolFromRecords([]db.UpstreamRecord{{ID: 1, AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, Healthy: true}})
	u := pool.Snapshot()[0]
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); for n := 0; n < 1000; n++ { u.Report(time.Millisecond, n%2 == 0) } }()
		go func() { defer wg.Done(); for n := 0; n < 1000; n++ { _ = u.HealthSnapshot() } }()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run under the race detector and verify failure**

Run: `go test -race ./internal/upstream -run 'TestPool|TestUpstreamHealth' -count=1`

Expected: FAIL because `NewPoolFromRecords`, `buildPoolSnapshot`, `Pool.Replace`, and `HealthSnapshot` do not exist.

- [ ] **Step 3: Implement the snapshot and locked health core**

```go
// replacement core in internal/upstream/pool.go
type healthState struct {
	healthy       bool
	avgLatency    time.Duration
	successRate   float64
	totalReqs     int64
	successReqs   int64
	lastCheckedAt time.Time
}

type Upstream struct {
	ID            uint
	AdapterType   string
	Name          string
	URL           string
	Proxy         string
	Priority      int
	ProbeMode     string
	ProbeInterval time.Duration
	CreatedAt     time.Time
	UpdatedAt     time.Time
	client        *http.Client
	mu            sync.RWMutex
	health        healthState
}

type HealthSnapshot struct {
	Healthy       bool
	AvgLatency    time.Duration
	SuccessRate   float64
	LastCheckedAt time.Time
}

type ProbeResult struct {
	Healthy   bool
	Latency   time.Duration
	CheckedAt time.Time
	Err       error
}

type poolSnapshot struct {
	upstreams []*Upstream
	byID      map[uint]*Upstream
}

type Pool struct { snapshot atomic.Pointer[poolSnapshot] }

func (p *Pool) load() *poolSnapshot { return p.snapshot.Load() }

func (p *Pool) Snapshot() []*Upstream {
	current := p.load()
	if current == nil { return nil }
	return append([]*Upstream(nil), current.upstreams...)
}

func (p *Pool) Replace(next *poolSnapshot) { p.snapshot.Store(next) }

func (p *Pool) Find(id uint) (*Upstream, bool) {
	current := p.load()
	if current == nil { return nil, false }
	u, ok := current.byID[id]
	return u, ok
}

func (u *Upstream) HealthSnapshot() HealthSnapshot {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return HealthSnapshot{Healthy: u.health.healthy, AvgLatency: u.health.avgLatency, SuccessRate: u.health.successRate, LastCheckedAt: u.health.lastCheckedAt}
}

func (u *Upstream) IsHealthy() bool { return u.HealthSnapshot().Healthy }
func (u *Upstream) AvgLatency() time.Duration { return u.HealthSnapshot().AvgLatency }
func (u *Upstream) SuccessRate() float64 { return u.HealthSnapshot().SuccessRate }

func (u *Upstream) Report(latency time.Duration, success bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.health.totalReqs++
	if success { u.health.successReqs++ }
	u.health.successRate = float64(u.health.successReqs) / float64(u.health.totalReqs)
	if u.health.avgLatency == 0 { u.health.avgLatency = latency } else { u.health.avgLatency = (u.health.avgLatency*7 + latency*3) / 10 }
	u.health.healthy = u.health.successRate > 0.3
}

func (u *Upstream) applyProbe(result ProbeResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.health.totalReqs++
	if result.Healthy { u.health.successReqs++ }
	u.health.successRate = float64(u.health.successReqs) / float64(u.health.totalReqs)
	u.health.healthy = result.Healthy
	u.health.lastCheckedAt = result.CheckedAt
	if result.Latency > 0 {
		if u.health.avgLatency == 0 { u.health.avgLatency = result.Latency } else { u.health.avgLatency = (u.health.avgLatency*7 + result.Latency*3) / 10 }
	}
}

func buildPoolSnapshot(records []db.UpstreamRecord, previous *poolSnapshot) (*poolSnapshot, error) {
	next := &poolSnapshot{upstreams: make([]*Upstream, 0, len(records)), byID: make(map[uint]*Upstream, len(records))}
	for _, record := range records {
		if previous != nil {
			if existing := previous.byID[record.ID]; existing != nil && existing.sameConfig(record) {
				next.upstreams = append(next.upstreams, existing)
				next.byID[record.ID] = existing
				continue
			}
		}
		u, err := newUpstreamFromRecord(record)
		if err != nil { return nil, err }
		next.upstreams = append(next.upstreams, u)
		next.byID[u.ID] = u
	}
	return next, nil
}

func NewPoolFromRecords(records []db.UpstreamRecord) (*Pool, error) {
	next, err := buildPoolSnapshot(records, nil)
	if err != nil { return nil, err }
	p := &Pool{}
	p.Replace(next)
	return p, nil
}

func normalizeRecordProbe(record db.UpstreamRecord) (string, time.Duration, error) {
	mode := record.ProbeMode
	if mode == "" { mode = "active" }
	if mode != "active" && mode != "passive" { return "", 0, fmt.Errorf("invalid probe mode %q", mode) }
	intervalText := record.ProbeInterval
	if intervalText == "" { intervalText = DefaultProbeInterval.String() }
	interval, err := time.ParseDuration(intervalText)
	if err != nil || interval <= 0 { return "", 0, fmt.Errorf("invalid probe interval %q", intervalText) }
	return mode, interval, nil
}

func newUpstreamFromRecord(record db.UpstreamRecord) (*Upstream, error) {
	mode, interval, err := normalizeRecordProbe(record)
	if err != nil { return nil, err }
	client, err := buildClient(record.Proxy)
	if err != nil { return nil, fmt.Errorf("build client for %s: %w", record.Name, err) }
	return &Upstream{
		ID: record.ID, AdapterType: record.AdapterType, Name: record.Name, URL: record.URL,
		Proxy: record.Proxy, Priority: record.Priority, ProbeMode: mode, ProbeInterval: interval,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, client: client,
		health: healthState{healthy: record.Healthy, avgLatency: time.Duration(record.AvgLatencyMs) * time.Millisecond, successRate: record.SuccessRate, lastCheckedAt: record.LastCheckedAt},
	}, nil
}

func (u *Upstream) sameConfig(record db.UpstreamRecord) bool {
	mode, interval, err := normalizeRecordProbe(record)
	return err == nil && u.ID == record.ID && u.AdapterType == record.AdapterType &&
		u.Name == record.Name && u.URL == record.URL && u.Proxy == record.Proxy &&
		u.Priority == record.Priority && u.ProbeMode == mode && u.ProbeInterval == interval
}

func NewPool(configs []config.UpstreamConfig) (*Pool, error) {
	records := make([]db.UpstreamRecord, 0, len(configs))
	for _, item := range configs {
		records = append(records, db.UpstreamRecord{
			Name: item.Name, URL: item.URL, Proxy: item.Proxy, Priority: item.Priority,
			ProbeMode: item.ProbeMode, ProbeInterval: item.ProbeInterval,
			Healthy: true, SuccessRate: 1,
		})
	}
	return NewPoolFromRecords(records)
}
```

Keep `Fetch`, `FetchURL`, and `FetchWithHeaders` behavior unchanged; all three continue to call the exact locked `Report` above. Config-owned extras intentionally receive ID 0 records and never use Registry lookup. Timestamps and mutable health are not client/worker identity, so `sameConfig` excludes them.

- [ ] **Step 4: Migrate selectors and all direct runtime health readers**

```go
// internal/upstream/selector.go
func (s *PrioritySelector) Select(_ context.Context) (*Upstream, error) {
	ups := s.pool.Snapshot()
	sort.Slice(ups, func(i, j int) bool { return ups[i].Priority < ups[j].Priority })
	for _, u := range ups {
		if u.IsHealthy() { return u, nil }
	}
	return nil, fmt.Errorf("all upstreams are unhealthy")
}
```

In the existing `health.go`, iterate `pool.Snapshot()` in `RestoreFromDB` and `StartHealthCheck`. `RestoreFromDB` takes `u.mu`, writes `u.health.avgLatency`, `u.health.healthy`, and `u.health.lastCheckedAt`, then unlocks. The existing network checker constructs `ProbeResult{Healthy: healthy, Latency: latency, CheckedAt: time.Now().UTC(), Err: err}` and calls `u.applyProbe(result)` in both success and failure branches; it never writes health fields directly. Task 3 then centralizes worker ownership and synchronous persistence.

In `now.go`, `stats.go`, `dashboard.go`, and `notify/scheduler.go`, replace `pool.Upstreams()` with `pool.Snapshot()` and replace direct health reads with one local `health := u.HealthSnapshot()` per item. Do not call three health getters separately for one DTO because that could mix samples from different writes.

- [ ] **Step 5: Run focused and consumer tests**

Run: `go test -race ./internal/upstream ./internal/api/public ./internal/api/admin ./internal/notify -count=1`

Expected: PASS with no race report and no remaining direct `.Healthy` access outside `internal/upstream`.

Run: `rg -n '\.Upstreams\(\)|\.Healthy' internal --glob '*.go'`

Expected: no mutable-pool read; remaining `.Healthy` matches are DTO/database/test fields, not `*upstream.Upstream` reads.

- [ ] **Step 6: Commit**

```bash
git add internal/upstream/pool.go internal/upstream/selector.go internal/upstream/health.go internal/upstream/pool_test.go internal/api/public/now.go internal/api/public/stats.go internal/api/admin/dashboard.go internal/notify/scheduler.go
git commit -m "refactor(upstream): publish immutable pool snapshots"
```

---

### Task 3: Build Registry-Owned Pools and Worker Lifecycle

**Files:**
- Create: `internal/upstream/registry.go`
- Modify: `internal/upstream/health.go`
- Create: `internal/upstream/registry_test.go`

**Interfaces:**
- Produces: `func NewRegistry(database *gorm.DB, active []string) (*Registry, error)`.
- Produces: `func (r *Registry) Start(ctx context.Context)`, `func (r *Registry) Close()`, `func (r *Registry) Pools() map[string]*Pool`, `func (r *Registry) ActiveEcosystems() []string`.
- Produces: `func (r *Registry) List() []RuntimeUpstream`, `func (r *Registry) Get(id uint) (RuntimeUpstream, error)`, and `func (r *Registry) WorkerRunning(id uint) bool`.
- Produces sentinels `ErrNotFound`, `ErrEcosystemNotActive`, `ErrInvalidUpstream`, `ErrImmutableEcosystem`, `ErrConflict`, `ErrLastUpstream`, `ErrReconcileFailed`.

- [ ] **Step 1: Write failing startup and worker lifecycle tests**

```go
// internal/upstream/registry_test.go
package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestNewRegistryRejectsActiveEcosystemWithoutRows(t *testing.T) {
	database := bootstrapDB(t)
	_, err := NewRegistry(database, []string{"pypi"})
	if err == nil || err.Error() != "active ecosystem pypi has no upstreams" { t.Fatalf("err=%v", err) }
}

func TestRegistryBuildsOnlyActivePools(t *testing.T) {
	database := bootstrapDB(t)
	for _, row := range []db.UpstreamRecord{
		{AdapterType: "pypi", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
		{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true},
	} { if err := database.Create(&row).Error; err != nil { t.Fatal(err) } }
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	if _, ok := registry.Pools()["pypi"]; !ok { t.Fatal("missing active pool") }
	if _, ok := registry.Pools()["npm"]; ok { t.Fatal("inactive pool was built") }
}

func TestRegistryCloseStopsEveryWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "one", URL: server.URL, Priority: 1, ProbeMode: "active", ProbeInterval: "10ms", Healthy: true}
	if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	ctx, cancel := context.WithCancel(context.Background())
	registry.Start(ctx)
	handle := registry.workers[record.ID]
	cancel()
	registry.Close()
	select { case <-handle.done: case <-time.After(time.Second): t.Fatal("worker did not exit") }
}
```

- [ ] **Step 2: Run and verify Registry is missing**

Run: `go test ./internal/upstream -run 'TestNewRegistry|TestRegistry' -count=1`

Expected: FAIL with undefined `NewRegistry` and Registry fields/methods.

- [ ] **Step 3: Implement Registry reads and lifecycle**

```go
// core of internal/upstream/registry.go
var (
	ErrNotFound             = errors.New("upstream not found")
	ErrEcosystemNotActive   = errors.New("ecosystem not active")
	ErrInvalidUpstream      = errors.New("invalid upstream")
	ErrImmutableEcosystem   = errors.New("immutable ecosystem")
	ErrConflict             = errors.New("upstream conflict")
	ErrLastUpstream         = errors.New("last upstream")
	ErrReconcileFailed      = errors.New("registry reconcile failed")
)

type RuntimeUpstream struct {
	ID uint; AdapterType, Name, URL, Proxy string; Priority int
	ProbeMode, ProbeInterval string
	Healthy bool; AvgLatencyMS int64; SuccessRate float64
	LastCheckedAt time.Time; WorkerRunning bool
	CreatedAt, UpdatedAt time.Time
}

type workerHandle struct { cancel context.CancelFunc; done chan struct{} }

type Registry struct {
	db *gorm.DB
	active []string
	pools map[string]*Pool
	mutationLocks map[string]*sync.Mutex
	workersMu sync.Mutex
	workers map[uint]workerHandle
	ctx context.Context
	cancel context.CancelFunc
	started bool
	degradedMu sync.RWMutex
	degraded map[string]error
}

func NewRegistry(database *gorm.DB, active []string) (*Registry, error) {
	ordered, err := canonicalActive(active)
	if err != nil { return nil, err }
	r := &Registry{db: database, active: ordered, pools: make(map[string]*Pool), mutationLocks: make(map[string]*sync.Mutex), workers: make(map[uint]workerHandle), degraded: make(map[string]error)}
	for _, ecosystem := range ordered {
		var records []db.UpstreamRecord
		if err := database.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil { return nil, err }
		if len(records) == 0 { return nil, fmt.Errorf("active ecosystem %s has no upstreams", ecosystem) }
		pool, err := NewPoolFromRecords(records)
		if err != nil { return nil, fmt.Errorf("build %s pool: %w", ecosystem, err) }
		r.pools[ecosystem] = pool
		r.mutationLocks[ecosystem] = &sync.Mutex{}
	}
	return r, nil
}

func (r *Registry) Start(parent context.Context) {
	r.workersMu.Lock()
	defer r.workersMu.Unlock()
	if r.started { return }
	r.ctx, r.cancel = context.WithCancel(parent)
	r.started = true
	for _, pool := range r.pools { for _, u := range pool.Snapshot() { r.startWorkerLocked(u) } }
}

func (r *Registry) Close() {
	r.workersMu.Lock()
	if r.cancel != nil { r.cancel() }
	r.started = false
	handles := make([]workerHandle, 0, len(r.workers))
	for _, h := range r.workers { h.cancel(); handles = append(handles, h) }
	r.workers = make(map[uint]workerHandle)
	r.workersMu.Unlock()
	for _, h := range handles { <-h.done }
}

func (r *Registry) startWorkerLocked(u *Upstream) {
	if !r.started || u.ID == 0 || u.ProbeMode != "active" { return }
	ctx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	r.workers[u.ID] = workerHandle{cancel: cancel, done: done}
	go func() { defer close(done); runUpstreamHealthCheck(ctx, u, r.db, u.ProbeInterval) }()
}

func (r *Registry) Pools() map[string]*Pool {
	out := make(map[string]*Pool, len(r.pools))
	for ecosystem, pool := range r.pools { out[ecosystem] = pool }
	return out
}

func (r *Registry) ActiveEcosystems() []string {
	return append([]string(nil), r.active...)
}

func (r *Registry) WorkerRunning(id uint) bool {
	r.workersMu.Lock()
	defer r.workersMu.Unlock()
	_, ok := r.workers[id]
	return ok
}

func (r *Registry) runtimeUpstream(u *Upstream) RuntimeUpstream {
	health := u.HealthSnapshot()
	return RuntimeUpstream{
		ID: u.ID, AdapterType: u.AdapterType, Name: u.Name, URL: u.URL, Proxy: u.Proxy,
		Priority: u.Priority, ProbeMode: u.ProbeMode, ProbeInterval: u.ProbeInterval.String(),
		Healthy: health.Healthy, AvgLatencyMS: health.AvgLatency.Milliseconds(),
		SuccessRate: health.SuccessRate, LastCheckedAt: health.LastCheckedAt,
		WorkerRunning: r.WorkerRunning(u.ID), CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (r *Registry) List() []RuntimeUpstream {
	out := make([]RuntimeUpstream, 0)
	for _, ecosystem := range r.active {
		for _, u := range r.pools[ecosystem].Snapshot() { out = append(out, r.runtimeUpstream(u)) }
	}
	return out
}

func (r *Registry) Get(id uint) (RuntimeUpstream, error) {
	for _, ecosystem := range r.active {
		if u, ok := r.pools[ecosystem].Find(id); ok { return r.runtimeUpstream(u), nil }
	}
	return RuntimeUpstream{}, ErrNotFound
}
```

The copied map/slice prevent callers from changing Registry ownership. `List/Get` use timestamps copied into the immutable `Upstream`; they do not perform an N+1 database join. `WorkerRunning` reads only under `workersMu`.

- [ ] **Step 4: Make health probing proxy-aware and persist one locked sample**

```go
// internal/upstream/health.go
func probe(ctx context.Context, u *Upstream) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.URL, nil)
	if err != nil { return ProbeResult{CheckedAt: time.Now().UTC(), Err: err} }
	req.Header.Set("User-Agent", "depsilo/0.1")
	start := time.Now()
	resp, err := u.client.Do(req)
	result := ProbeResult{Latency: time.Since(start), CheckedAt: time.Now().UTC(), Err: err}
	if err == nil {
		resp.Body.Close()
		result.Healthy = resp.StatusCode < 500
		if !result.Healthy { result.Err = fmt.Errorf("upstream returned status %d", resp.StatusCode) }
	}
	return result
}

func persistProbe(database *gorm.DB, u *Upstream, result ProbeResult) error {
	if database == nil { return nil }
	health := u.HealthSnapshot()
	return database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.UpstreamRecord{}).Where("id = ?", u.ID).Updates(map[string]any{"healthy": health.Healthy, "avg_latency_ms": health.AvgLatency.Milliseconds(), "success_rate": health.SuccessRate, "last_checked_at": health.LastCheckedAt}).Error; err != nil { return err }
		return tx.Create(&db.UpstreamLatencyLog{UpstreamID: u.ID, Name: u.Name, LatencyMs: result.Latency.Milliseconds(), Healthy: result.Healthy, CreatedAt: result.CheckedAt}).Error
	})
}

func runUpstreamHealthCheck(ctx context.Context, u *Upstream, database *gorm.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := probe(ctx, u)
			u.applyProbe(result)
			if err := persistProbe(database, u, result); err != nil {
				zap.L().Warn("persist upstream probe", zap.Uint("upstream_id", u.ID), zap.Error(err))
			}
		}
	}
}
```

Network failure changes health and logs but does not terminate the worker. Keep `StartHealthCheck` only for config-owned extra pools, iterate `Snapshot()`, and start `runUpstreamHealthCheck` only for `ProbeMode == "active"` entries. Add `fmt` and zap imports required by the exact code above.

- [ ] **Step 5: Run lifecycle tests under race**

Run: `go test -race ./internal/upstream -run 'TestNewRegistry|TestRegistry' -count=1`

Expected: PASS; cancellation closes every captured `done` channel and only `pypi` gets a Pool.

- [ ] **Step 6: Commit**

```bash
git add internal/upstream/registry.go internal/upstream/registry_test.go internal/upstream/health.go
git commit -m "feat(upstream): own pools and probe workers in registry"
```

---

### Task 4: Make Registry Mutations Transactional and Infallibly Publish Prepared State

**Files:**
- Create: `internal/upstream/registry_mutation.go`
- Modify: `internal/upstream/registry.go`
- Create: `internal/upstream/registry_mutation_test.go`

**Interfaces:**
- Produces: `type MutationInput struct { AdapterType, Name, URL, Proxy string; Priority int; ProbeMode, ProbeInterval string }`.
- Produces: `func (r *Registry) Create(context.Context, MutationInput) (RuntimeUpstream, error)`.
- Produces: `func (r *Registry) Update(context.Context, uint, MutationInput) (RuntimeUpstream, error)`.
- Produces: `func (r *Registry) Delete(context.Context, uint) (RuntimeUpstream, error)`.
- Produces: `func (r *Registry) Check(context.Context, uint) (RuntimeUpstream, ProbeResult, error)`.

- [ ] **Step 1: Write failing mutation atomicity tests**

```go
// internal/upstream/registry_mutation_test.go
package upstream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func registryFixture(t *testing.T, ecosystem string, count int) (*gorm.DB, *Registry) {
	t.Helper()
	database := bootstrapDB(t)
	for i := 0; i < count; i++ {
		record := db.UpstreamRecord{
			AdapterType: ecosystem,
			Name: fmt.Sprintf("source-%d", i+1),
			URL: fmt.Sprintf("https://source-%d.example", i+1),
			Priority: i + 1,
			ProbeMode: "passive",
			ProbeInterval: "30m",
			Healthy: true,
			SuccessRate: 1,
		}
		if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	}
	registry, err := NewRegistry(database, []string{ecosystem})
	if err != nil { t.Fatal(err) }
	return database, registry
}

func TestRegistryMutationsPublishCommittedSnapshot(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	ctx := context.Background()
	created, err := registry.Create(ctx, MutationInput{AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m"})
	if err != nil { t.Fatal(err) }
	if _, ok := registry.Pools()["pypi"].Find(created.ID); !ok { t.Fatal("created row absent from runtime pool") }

	updated, err := registry.Update(ctx, created.ID, MutationInput{AdapterType: "pypi", Name: "two", URL: "https://changed.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "1h"})
	if err != nil { t.Fatal(err) }
	if updated.URL != "https://changed.example" { t.Fatalf("url=%q", updated.URL) }

	deleted, err := registry.Delete(ctx, created.ID)
	if err != nil { t.Fatal(err) }
	if deleted.ID != created.ID { t.Fatalf("deleted=%d", deleted.ID) }
	if _, ok := registry.Pools()["pypi"].Find(created.ID); ok { t.Fatal("deleted row remains in runtime pool") }
	var count int64
	database.Model(&db.UpstreamRecord{}).Where("id = ?", created.ID).Count(&count)
	if count != 0 { t.Fatal("deleted row remains in database") }
}

func TestRegistryDeleteLastUpstreamLeavesDBAndPoolUnchanged(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	before := registry.Pools()["pypi"].Snapshot()
	_, err := registry.Delete(context.Background(), before[0].ID)
	if !errors.Is(err, ErrLastUpstream) { t.Fatalf("err=%v", err) }
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0].ID != before[0].ID { t.Fatalf("snapshot changed: %#v", got) }
}

func TestRegistryRejectsInactiveAndImmutableEcosystem(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	_, err := registry.Create(context.Background(), MutationInput{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrEcosystemNotActive) { t.Fatalf("create err=%v", err) }
	id := registry.Pools()["pypi"].Snapshot()[0].ID
	_, err = registry.Update(context.Background(), id, MutationInput{AdapterType: "npm", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrImmutableEcosystem) { t.Fatalf("update err=%v", err) }
}

func TestRegistryConcurrentMutationsRemainConsistent(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, _ = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: fmt.Sprintf("mirror-%02d", i), URL: fmt.Sprintf("https://mirror-%02d.example", i), Priority: i + 2, ProbeMode: "passive", ProbeInterval: "30m"}) }(i)
	}
	wg.Wait()
	if err := registry.verify("pypi"); err != nil { t.Fatal(err) }
}

func TestRegistryConflictRollsBackWithoutPublishing(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	before := registry.Pools()["pypi"].Snapshot()
	_, err := registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: "source-1", URL: "https://duplicate.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrConflict) { t.Fatalf("err=%v", err) }
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0] != before[0] { t.Fatalf("snapshot changed: %#v", got) }
	var count int64
	database.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count)
	if count != 1 { t.Fatalf("count=%d", count) }
}

func TestRegistryCommitFailureLeavesDBPoolAndWorkersUnpublished(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil { t.Fatal(err) }
	if err := database.Model(&record).Updates(map[string]any{"probe_mode": "active", "probe_interval": "1h"}).Error; err != nil { t.Fatal(err) }
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	registry.Start(context.Background())
	defer registry.Close()

	beforePool := registry.Pools()["pypi"].Snapshot()
	beforeWorker := registry.workers[record.ID]
	injected := errors.New("injected commit failure")
	registry.commit = func(*gorm.DB) error { return injected }
	_, err = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 2, ProbeMode: "active", ProbeInterval: "1h"})
	if !errors.Is(err, injected) { t.Fatalf("err=%v", err) }

	var count int64
	if err := database.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count).Error; err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("committed rows=%d want=1", count) }
	afterPool := registry.Pools()["pypi"].Snapshot()
	if len(afterPool) != 1 || afterPool[0] != beforePool[0] { t.Fatalf("pool published after commit failure: %#v", afterPool) }
	if len(registry.workers) != 1 || registry.workers[record.ID].done != beforeWorker.done { t.Fatal("worker plan published after commit failure") }
	select { case <-beforeWorker.done: t.Fatal("existing worker was stopped"); default: }
}

func TestRegistryPublishMismatchReloadsCommittedDatabase(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var records []db.UpstreamRecord
	if err := database.Where("adapter_type = ?", "pypi").Find(&records).Error; err != nil { t.Fatal(err) }
	wrong, err := buildPoolSnapshot(nil, registry.Pools()["pypi"].load())
	if err != nil { t.Fatal(err) }
	current := registry.Pools()["pypi"].load()
	if err := registry.publish("pypi", preparedMutation{next: wrong, workers: planWorkers(current, wrong)}); err != nil { t.Fatal(err) }
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0].ID != records[0].ID { t.Fatalf("reload=%#v", got) }
}

func TestRegistryPublishReturnsReconcileFailureWhenReloadFails(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var records []db.UpstreamRecord
	if err := database.Where("adapter_type = ?", "pypi").Find(&records).Error; err != nil { t.Fatal(err) }
	wrong, _ := buildPoolSnapshot(nil, registry.Pools()["pypi"].load())
	sqlDB, err := database.DB()
	if err != nil { t.Fatal(err) }
	if err := sqlDB.Close(); err != nil { t.Fatal(err) }
	current := registry.Pools()["pypi"].load()
	err = registry.publish("pypi", preparedMutation{next: wrong, workers: planWorkers(current, wrong)})
	if !errors.Is(err, ErrReconcileFailed) { t.Fatalf("err=%v", err) }
	if registry.degradedError("pypi") == nil { t.Fatal("ecosystem was not marked degraded") }
}

func TestRegistryIntervalUpdateStopsOldWorkerAndStartsReplacement(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil { t.Fatal(err) }
	if err := database.Model(&record).Updates(map[string]any{"probe_mode":"active", "probe_interval":"1h"}).Error; err != nil { t.Fatal(err) }
	registry, _ = NewRegistry(database, []string{"pypi"})
	registry.Start(context.Background())
	defer registry.Close()
	old := registry.workers[record.ID]
	_, err := registry.Update(context.Background(), record.ID, MutationInput{AdapterType:"pypi", Name:record.Name, URL:record.URL, Priority:record.Priority, ProbeMode:"active", ProbeInterval:"2h"})
	if err != nil { t.Fatal(err) }
	select { case <-old.done: case <-time.After(time.Second): t.Fatal("old worker still running") }
	if next := registry.workers[record.ID]; next.done == old.done { t.Fatal("worker was not replaced") }
}
```

- [ ] **Step 2: Run and verify mutation methods are absent**

Run: `go test -race ./internal/upstream -run 'TestRegistryMutations|TestRegistryDelete|TestRegistryRejects|TestRegistryConcurrent|TestRegistryConflict|TestRegistryCommit|TestRegistryPublish|TestRegistryInterval' -count=1`

Expected: FAIL with undefined `MutationInput` and Registry mutation methods.

- [ ] **Step 3: Implement exact validation and prepared mutation publication**

Add this testable commit boundary to `Registry`:

```go
commit func(*gorm.DB) error
```

Initialize it in `NewRegistry` as `commit: func(tx *gorm.DB) error { return tx.Commit().Error }`. Production therefore uses an actual SQL transaction commit; the test replaces only that final operation after the mutation, record read, client construction, snapshot construction, and worker-plan construction have succeeded.

```go
// core of internal/upstream/registry_mutation.go
type MutationInput struct {
	AdapterType, Name, URL, Proxy string
	Priority int
	ProbeMode, ProbeInterval string
}

type preparedMutation struct {
	next *poolSnapshot
	workers workerPlan
	resultID uint
}

type workerPlan struct {
	stop []uint
	start []*Upstream
}

func validateMutation(input MutationInput) (MutationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.URL = strings.TrimSpace(input.URL)
	input.Proxy = strings.TrimSpace(input.Proxy)
	if input.Name == "" || len(input.Name) > 128 || input.Priority <= 0 { return input, fmt.Errorf("%w: name and positive priority are required", ErrInvalidUpstream) }
	parsed, err := url.ParseRequestURI(input.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") { return input, fmt.Errorf("%w: invalid url", ErrInvalidUpstream) }
	if input.Proxy != "" {
		proxy, err := url.ParseRequestURI(input.Proxy)
		if err != nil || proxy.Host == "" || (proxy.Scheme != "http" && proxy.Scheme != "https") { return input, fmt.Errorf("%w: invalid proxy", ErrInvalidUpstream) }
	}
	if input.ProbeMode != "active" && input.ProbeMode != "passive" { return input, fmt.Errorf("%w: probe_mode must be active or passive", ErrInvalidUpstream) }
	interval, err := time.ParseDuration(input.ProbeInterval)
	if err != nil || interval <= 0 { return input, fmt.Errorf("%w: probe_interval must be a positive Go duration", ErrInvalidUpstream) }
	input.ProbeInterval = interval.String()
	return input, nil
}

func (r *Registry) runTransaction(ctx context.Context, apply func(*gorm.DB) error) (err error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil { return tx.Error }
	committed := false
	defer func() {
		if !committed { _ = tx.Rollback().Error }
	}()
	if err := apply(tx); err != nil { return err }
	if err := r.commit(tx); err != nil { return err }
	committed = true
	return nil
}

func (r *Registry) prepareAndCommit(ctx context.Context, ecosystem string, apply func(*gorm.DB) (uint, error)) (preparedMutation, error) {
	pool := r.pools[ecosystem]
	previous := pool.load()
	var prepared preparedMutation
	err := r.runTransaction(ctx, func(tx *gorm.DB) error {
		id, err := apply(tx)
		if err != nil { return err }
		var records []db.UpstreamRecord
		if err := tx.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil { return err }
		next, err := buildPoolSnapshot(records, previous)
		if err != nil { return err }
		prepared = preparedMutation{next: next, workers: planWorkers(previous, next), resultID: id}
		return nil
	})
	return prepared, err
}

func (r *Registry) publish(ecosystem string, prepared preparedMutation) error {
	r.pools[ecosystem].Replace(prepared.next) // a single infallible atomic store
	r.applyWorkerPlan(prepared.workers)       // only cancel/start; no construction failure
	if err := r.verify(ecosystem); err == nil {
		r.clearDegraded(ecosystem)
		return nil
	}
	if err := r.reloadEcosystem(ecosystem); err != nil {
		r.markDegraded(ecosystem, err)
		return fmt.Errorf("%w: %v", ErrReconcileFailed, err)
	}
	r.clearDegraded(ecosystem)
	return nil
}

func sameWorkerConfig(a, b *Upstream) bool {
	return a.ID == b.ID && a.URL == b.URL && a.Proxy == b.Proxy &&
		a.ProbeMode == b.ProbeMode && a.ProbeInterval == b.ProbeInterval
}

func planWorkers(previous, next *poolSnapshot) workerPlan {
	plan := workerPlan{}
	if previous != nil {
		for id, old := range previous.byID {
			current := next.byID[id]
			if current == nil || !sameWorkerConfig(old, current) { plan.stop = append(plan.stop, id) }
		}
	}
	for id, current := range next.byID {
		old := (*Upstream)(nil)
		if previous != nil { old = previous.byID[id] }
		if current.ProbeMode == "active" && (old == nil || !sameWorkerConfig(old, current)) {
			plan.start = append(plan.start, current)
		}
	}
	return plan
}

func (r *Registry) applyWorkerPlan(plan workerPlan) {
	r.workersMu.Lock()
	stopped := make([]workerHandle, 0, len(plan.stop))
	for _, id := range plan.stop {
		if handle, ok := r.workers[id]; ok {
			handle.cancel()
			stopped = append(stopped, handle)
			delete(r.workers, id)
		}
	}
	r.workersMu.Unlock()
	for _, handle := range stopped { <-handle.done }
	r.workersMu.Lock()
	defer r.workersMu.Unlock()
	for _, current := range plan.start { r.startWorkerLocked(current) }
}

func snapshotMatches(snapshot *poolSnapshot, records []db.UpstreamRecord) bool {
	if snapshot == nil || len(snapshot.upstreams) != len(records) { return false }
	for i, record := range records {
		mode := record.ProbeMode
		if mode == "" { mode = "active" }
		intervalText := record.ProbeInterval
		if intervalText == "" { intervalText = DefaultProbeInterval.String() }
		interval, err := time.ParseDuration(intervalText)
		if err != nil || interval <= 0 { return false }
		current := snapshot.upstreams[i]
		if current.ID != record.ID || current.AdapterType != record.AdapterType ||
			current.Name != record.Name || current.URL != record.URL || current.Proxy != record.Proxy ||
			current.Priority != record.Priority || current.ProbeMode != mode || current.ProbeInterval != interval {
			return false
		}
	}
	return true
}

func (r *Registry) verify(ecosystem string) error {
	var records []db.UpstreamRecord
	if err := r.db.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil { return err }
	if len(records) == 0 { return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem) }
	if !snapshotMatches(r.pools[ecosystem].load(), records) { return fmt.Errorf("runtime snapshot differs from committed %s records", ecosystem) }
	return nil
}

func (r *Registry) reloadEcosystem(ecosystem string) error {
	var records []db.UpstreamRecord
	if err := r.db.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil { return err }
	if len(records) == 0 { return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem) }
	pool := r.pools[ecosystem]
	previous := pool.load()
	next, err := buildPoolSnapshot(records, previous)
	if err != nil { return err }
	workers := planWorkers(previous, next)
	pool.Replace(next)
	r.applyWorkerPlan(workers)
	return r.verify(ecosystem)
}

func (r *Registry) markDegraded(ecosystem string, err error) {
	r.degradedMu.Lock()
	r.degraded[ecosystem] = err
	r.degradedMu.Unlock()
	zap.L().Error("upstream registry ecosystem degraded", zap.String("ecosystem", ecosystem), zap.Error(err))
}

func (r *Registry) clearDegraded(ecosystem string) {
	r.degradedMu.Lock()
	defer r.degradedMu.Unlock()
	delete(r.degraded, ecosystem)
}

func (r *Registry) degradedError(ecosystem string) error {
	r.degradedMu.RLock()
	defer r.degradedMu.RUnlock()
	return r.degraded[ecosystem]
}
```

The invariant deliberately compares `r.pools[ecosystem].load()`, the live snapshot used by selectors, against a fresh ordered read of committed records. It never compares `prepared.next` with the records used to build that same value. Recovery reload builds both the replacement snapshot and its worker plan from the database, swaps, applies workers, and performs the same live-versus-committed check once more.

- [ ] **Step 4: Implement Create, Update, Delete, and Check without abbreviated branches**

```go
func (r *Registry) Create(ctx context.Context, input MutationInput) (RuntimeUpstream, error) {
	lock := r.mutationLocks[input.AdapterType]
	if lock == nil { return RuntimeUpstream{}, ErrEcosystemNotActive }
	input, err := validateMutation(input)
	if err != nil { return RuntimeUpstream{}, err }
	lock.Lock(); defer lock.Unlock()
	prepared, err := r.prepareAndCommit(ctx, input.AdapterType, func(tx *gorm.DB) (uint, error) {
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ?", input.AdapterType, input.Name).Count(&count).Error; err != nil { return 0, err }
		if count != 0 { return 0, ErrConflict }
		record := db.UpstreamRecord{AdapterType: input.AdapterType, Name: input.Name, URL: input.URL, Proxy: input.Proxy, Priority: input.Priority, ProbeMode: input.ProbeMode, ProbeInterval: input.ProbeInterval, Healthy: true, SuccessRate: 1}
		if err := tx.Create(&record).Error; err != nil { return 0, err }
		return record.ID, nil
	})
	if err != nil { return RuntimeUpstream{}, err }
	if err := r.publish(input.AdapterType, prepared); err != nil { return RuntimeUpstream{}, err }
	return r.Get(prepared.resultID)
}

func (r *Registry) Update(ctx context.Context, id uint, input MutationInput) (RuntimeUpstream, error) {
	var current db.UpstreamRecord
	if err := r.db.First(&current, id).Error; errors.Is(err, gorm.ErrRecordNotFound) { return RuntimeUpstream{}, ErrNotFound } else if err != nil { return RuntimeUpstream{}, err }
	if input.AdapterType != current.AdapterType { return RuntimeUpstream{}, ErrImmutableEcosystem }
	lock := r.mutationLocks[current.AdapterType]
	if lock == nil { return RuntimeUpstream{}, ErrEcosystemNotActive }
	input, err := validateMutation(input)
	if err != nil { return RuntimeUpstream{}, err }
	lock.Lock(); defer lock.Unlock()
	prepared, err := r.prepareAndCommit(ctx, current.AdapterType, func(tx *gorm.DB) (uint, error) {
		var record db.UpstreamRecord
		if err := tx.First(&record, id).Error; errors.Is(err, gorm.ErrRecordNotFound) { return 0, ErrNotFound } else if err != nil { return 0, err }
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ? AND id <> ?", current.AdapterType, input.Name, id).Count(&count).Error; err != nil { return 0, err }
		if count != 0 { return 0, ErrConflict }
		err := tx.Model(&record).Updates(map[string]any{"name": input.Name, "url": input.URL, "proxy": input.Proxy, "priority": input.Priority, "probe_mode": input.ProbeMode, "probe_interval": input.ProbeInterval}).Error
		if err != nil { return 0, err }
		return id, nil
	})
	if err != nil { return RuntimeUpstream{}, err }
	if err := r.publish(current.AdapterType, prepared); err != nil { return RuntimeUpstream{}, err }
	return r.Get(id)
}

func (r *Registry) Delete(ctx context.Context, id uint) (RuntimeUpstream, error) {
	before, err := r.Get(id)
	if err != nil { return RuntimeUpstream{}, err }
	lock := r.mutationLocks[before.AdapterType]
	if lock == nil { return RuntimeUpstream{}, ErrEcosystemNotActive }
	lock.Lock(); defer lock.Unlock()
	prepared, err := r.prepareAndCommit(ctx, before.AdapterType, func(tx *gorm.DB) (uint, error) {
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", before.AdapterType).Count(&count).Error; err != nil { return 0, err }
		if count <= 1 { return 0, ErrLastUpstream }
		result := tx.Delete(&db.UpstreamRecord{}, id)
		if result.Error != nil { return 0, result.Error }
		if result.RowsAffected == 0 { return 0, ErrNotFound }
		return id, nil
	})
	if err != nil { return RuntimeUpstream{}, err }
	if err := r.publish(before.AdapterType, prepared); err != nil { return RuntimeUpstream{}, err }
	return before, nil
}

func (r *Registry) Check(ctx context.Context, id uint) (RuntimeUpstream, ProbeResult, error) {
	resource, err := r.Get(id)
	if err != nil { return RuntimeUpstream{}, ProbeResult{}, err }
	lock := r.mutationLocks[resource.AdapterType]
	lock.Lock(); defer lock.Unlock()
	u, ok := r.pools[resource.AdapterType].Find(id)
	if !ok { return RuntimeUpstream{}, ProbeResult{}, ErrNotFound }
	result := probe(ctx, u)
	u.applyProbe(result)
	if err := persistProbe(r.db, u, result); err != nil { return RuntimeUpstream{}, ProbeResult{}, err }
	updated, err := r.Get(id)
	return updated, result, err
}
```

- [ ] **Step 5: Run mutation and full Registry race tests**

Run: `go test -race ./internal/upstream -count=1`

Expected: PASS with DB records equal to every published snapshot, no worker leak, and no race.

- [ ] **Step 6: Commit**

```bash
git add internal/upstream/registry_mutation.go internal/upstream/registry.go internal/upstream/registry_mutation_test.go
git commit -m "feat(upstream): atomically reconcile registry mutations"
```

---

### Task 5: Migrate Health History, Selectors, and Stats to Runtime Identity

**Files:**
- Modify: `internal/upstream/health.go`
- Modify: `internal/api/admin/latency.go`
- Modify: `internal/api/public/stats.go`
- Modify: `internal/api/public/now.go`
- Modify: `internal/api/admin/dashboard.go`
- Modify: `internal/notify/scheduler.go`
- Modify: `internal/upstream/registry_mutation_test.go`
- Create: `internal/upstream/registry_check_test.go`

**Interfaces:**
- Consumes: `Pool.Snapshot`, `Upstream.HealthSnapshot`, and `Upstream.ID` from Tasks 2-4.
- Produces: every dynamic latency log with nonzero `UpstreamID`; admin history filters by ID.
- Preserves: public `/latency-series` response keyed by upstream name for Portal compatibility.

- [ ] **Step 1: Add failing identity and concurrent reader tests**

```go
// internal/upstream/registry_check_test.go
package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"depsilo/internal/db"
)

func TestRegistryCheckPersistsUpstreamIDEvenWhenNetworkIsUnhealthy(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "down", URL: "http://127.0.0.1:1", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	registry, _ := NewRegistry(database, []string{"pypi"})
	resource, check, err := registry.Check(context.Background(), record.ID)
	if err != nil { t.Fatal(err) }
	if resource.Healthy || check.Healthy || check.Err == nil { t.Fatalf("resource=%#v check=%#v", resource, check) }
	var log db.UpstreamLatencyLog
	if err := database.Order("id desc").First(&log).Error; err != nil { t.Fatal(err) }
	if log.UpstreamID != record.ID { t.Fatalf("upstream_id=%d want=%d", log.UpstreamID, record.ID) }
}

func TestRegistryCheckUsesConfiguredProxyClient(t *testing.T) {
	var proxyHits atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		proxyHits.Add(1)
		if request.Method != http.MethodHead || request.URL.Host != "origin.invalid" { t.Errorf("request=%s %s", request.Method, request.URL.String()) }
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "proxied", URL: "http://origin.invalid", Proxy: proxy.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	runtime, result, err := registry.Check(context.Background(), record.ID)
	if err != nil { t.Fatal(err) }
	if proxyHits.Load() != 1 || !result.Healthy || !runtime.Healthy { t.Fatalf("proxy_hits=%d runtime=%#v result=%#v", proxyHits.Load(), runtime, result) }
}
```

```go
// append these functions to internal/upstream/registry_mutation_test.go

func TestRegistryReadersAndMutationsAreRaceFree(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 2)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); for n := 0; n < 500; n++ { for _, pool := range registry.Pools() { for _, u := range pool.Snapshot() { _ = u.HealthSnapshot() } }; _ = registry.List() } }()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); for n := 0; n < 50; n++ { _, _ = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: fmt.Sprintf("r-%d-%d", i, n), URL: "https://example.invalid", Priority: n + 3, ProbeMode: "passive", ProbeInterval: "30m"}) } }(i)
	}
	wg.Wait()
	if err := registry.verify("pypi"); err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run the race tests and observe the identity regression**

Run: `go test -race ./internal/upstream -run 'TestRegistryCheckPersists|TestRegistryCheckUsesConfiguredProxyClient|TestRegistryReaders' -count=1`

Expected: the identity test FAILS if any probe path still writes `upstream_id=0`; race detector must remain clean after Task 4.

- [ ] **Step 3: Query latency by ID and aggregate public series without name collisions**

```go
// internal/api/admin/latency.go query replacement
h.db.Model(&db.UpstreamLatencyLog{}).
	Select("created_at as time, latency_ms, healthy").
	Where("upstream_id = ? AND created_at >= ?", upstream.ID, since).
	Order("datetime(created_at) ASC").
	Find(&points)
```

Use this identity query and key in `StatsHandler.allUpstreamLatencySeries`; retain the existing 90-bucket output loop and name-keyed response shape:

```go
type bucketRow struct {
	UpstreamID uint
	Name string
	Bucket int64
	AvgLat, AvgHP float64
	Requests int64
}

var rows []bucketRow
h.db.Table("upstream_latency_logs AS l").
	Select(fmt.Sprintf(`l.upstream_id,
		COALESCE(NULLIF(u.name, ''), l.name) AS name,
		(CAST(strftime('%%s', l.created_at) AS INTEGER) / %d) * %d AS bucket,
		AVG(l.latency_ms) AS avg_lat,
		AVG(CASE WHEN l.healthy = 1 THEN 1.0 ELSE 0.0 END) AS avg_hp,
		COUNT(*) AS requests`, latencyIntervalMin*60, latencyIntervalMin*60)).
	Joins("LEFT JOIN upstream_records AS u ON u.id = l.upstream_id").
	Where("datetime(l.created_at) >= datetime(?)", since.UTC()).
	Group("l.upstream_id, COALESCE(NULLIF(u.name, ''), l.name), bucket").
	Order("name, bucket ASC").
	Scan(&rows)

type seriesKey struct { upstreamID uint; name string }
type bucketKey struct { series seriesKey; bucket int64 }
```

Index each row by `bucketKey{seriesKey{row.UpstreamID, row.Name}, row.Bucket}`. ID 0 extra-index rows remain separated by name; nonzero rows follow the current DB name across a rename. Emit `result[series.name]` so Portal's public response remains unchanged. Standard runtime entries get IDs directly from Pool snapshots rather than an `idByName` database join.

- [ ] **Step 4: Make every read take one coherent health snapshot**

```go
for _, u := range pool.Snapshot() {
	health := u.HealthSnapshot()
	upstreams = append(upstreams, gin.H{
		"id": u.ID, "name": u.Name, "adapter": name, "url": u.URL,
		"healthy": health.Healthy,
		"avg_latency_ms": health.AvgLatency.Milliseconds(),
		"success_rate": health.SuccessRate,
	})
}
```

Use that pattern in public stats, Now rollup, Admin dashboard, and notifier. Selector remains the only component that sorts, and it sorts the copied `Snapshot()` slice.

- [ ] **Step 5: Run comprehensive upstream consumers under race**

Run: `go test -race ./internal/upstream ./internal/api/admin ./internal/api/public ./internal/notify -count=1`

Expected: PASS; no dynamic probe log has ID 0, no mutable slice iteration remains, and no race is reported.

- [ ] **Step 6: Commit**

```bash
git add internal/upstream/health.go internal/api/admin/latency.go internal/api/public/stats.go internal/api/public/now.go internal/api/admin/dashboard.go internal/notify/scheduler.go internal/upstream/registry_mutation_test.go internal/upstream/registry_check_test.go
git commit -m "fix(upstream): key health and stats by runtime upstream"
```

---

### Task 6: Expose the Exact Admin Upstream HTTP Contract

**Files:**
- Create: `internal/api/admin/upstream_contract.go`
- Modify: `internal/api/admin/upstream.go`
- Create: `internal/api/admin/upstream_test.go`
- Consume unchanged: `internal/api/admin/credentials.go` from Plan 01

**Interfaces:**
- Consumes: Registry CRUD/Check interfaces from Task 4.
- Produces: exact `upstreamMutationRequest`, `adminUpstreamResponse`, `upstreamListResponse`, `deleteUpstreamResponse`, and `checkUpstreamResponse` DTOs.
- Produces exact error mappings: 400 `BAD_REQUEST`; 404 `NOT_FOUND`; 409 `CONFLICT`, `LAST_UPSTREAM`, `ECOSYSTEM_NOT_ACTIVE`; 422 `INVALID_UPSTREAM`, `IMMUTABLE_ECOSYSTEM`; 500 `REGISTRY_RECONCILE_FAILED`.
- Preserves Plan 01's server-side credential rule: `middleware.PrincipalFromContext` plus `principalCanViewCredentials` and `maskURLUserInfo` mask URL userinfo and proxy credentials in readonly List/Create/Update/Check responses; write-capable principals receive operational values.

- [ ] **Step 1: Write failing contract tests**

```go
// internal/api/admin/upstream_test.go
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
)

func upstreamRouterFixtureWithPrincipal(t *testing.T, count int, canWrite bool, firstURL, firstProxy string) (*upstream.Registry, *gin.Engine) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "upstream-handler.db"))
	if err != nil { t.Fatal(err) }
	if err := db.AutoMigrate(database); err != nil { t.Fatal(err) }
	for i := 0; i < count; i++ {
		urlValue := fmt.Sprintf("https://source-%d.example", i+1)
		proxyValue := ""
		if i == 0 && firstURL != "" { urlValue = firstURL }
		if i == 0 { proxyValue = firstProxy }
		record := db.UpstreamRecord{AdapterType:"pypi", Name:fmt.Sprintf("source-%d", i+1), URL:urlValue, Proxy:proxyValue, Priority:i+1, ProbeMode:"passive", ProbeInterval:"30m", Healthy:true, SuccessRate:1}
		if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	}
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	handler := NewUpstreamHandler(registry)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyPrincipal, middleware.Principal{ID:1, Username:"operator", Role:"admin", Enabled:true, AuthMethod:middleware.AuthMethodJWT, CanWrite:canWrite})
		c.Next()
	})
	router.GET("/upstreams", handler.List)
	router.POST("/upstreams", handler.Create)
	router.PUT("/upstreams/:id", handler.Update)
	router.DELETE("/upstreams/:id", handler.Delete)
	router.POST("/upstreams/:id/check", handler.Check)
	return registry, router
}

func upstreamRouterFixture(t *testing.T, count int) (*upstream.Registry, *gin.Engine) {
	return upstreamRouterFixtureWithPrincipal(t, count, true, "", "")
}

func upstreamRouterFixtureWithURL(t *testing.T, firstURL string) (*upstream.Registry, *gin.Engine) {
	return upstreamRouterFixtureWithPrincipal(t, 1, true, firstURL, "")
}

func performJSON(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" { request.Header.Set("Content-Type", "application/json") }
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestUpstreamHandlerListEnvelopeAndCreateContract(t *testing.T) {
	registry, router := upstreamRouterFixture(t, 1)
	body := `{"adapter_type":"pypi","name":"secondary","url":"https://secondary.example","proxy":"","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w := performJSON(router, http.MethodPost, "/upstreams", body)
	if w.Code != http.StatusCreated { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var created adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil { t.Fatal(err) }
	if created.ID == 0 || created.AdapterType != "pypi" || created.WorkerRunning || created.LastCheckedAt != nil { t.Fatalf("created=%#v", created) }
	if _, ok := registry.Pools()["pypi"].Find(created.ID); !ok { t.Fatal("HTTP success did not publish runtime source") }

	w = performJSON(router, http.MethodGet, "/upstreams", "")
	if w.Code != http.StatusOK { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var list upstreamListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil { t.Fatal(err) }
	if list.Total != 2 || len(list.Items) != 2 { t.Fatalf("list=%#v", list) }
}

func TestUpstreamHandlerExactErrors(t *testing.T) {
	_, router := upstreamRouterFixture(t, 1)
	cases := []struct{ method, path, body string; status int; code string }{
		{http.MethodPost, "/upstreams", `{`, 400, "BAD_REQUEST"},
		{http.MethodDelete, "/upstreams/not-a-number", ``, 400, "BAD_REQUEST"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"npm","name":"x","url":"https://x.example","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 409, "ECOSYSTEM_NOT_ACTIVE"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"pypi","name":"bad","url":"file:///tmp/source","proxy":"","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 422, "INVALID_UPSTREAM"},
		{http.MethodPost, "/upstreams", `{"adapter_type":"pypi","name":"source-1","url":"https://duplicate.example","proxy":"","priority":2,"probe_mode":"passive","probe_interval":"30m"}`, 409, "CONFLICT"},
		{http.MethodPut, "/upstreams/1", `{"adapter_type":"npm","name":"x","url":"https://x.example","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 422, "IMMUTABLE_ECOSYSTEM"},
		{http.MethodDelete, "/upstreams/1", ``, 409, "LAST_UPSTREAM"},
		{http.MethodPut, "/upstreams/999", `{"adapter_type":"pypi","name":"x","url":"https://x.example","proxy":"","priority":1,"probe_mode":"passive","probe_interval":"30m"}`, 404, "NOT_FOUND"},
	}
	for _, tc := range cases {
		w := performJSON(router, tc.method, tc.path, tc.body)
		var response struct { Code string `json:"code"` }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
		if w.Code != tc.status || response.Code != tc.code { t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String()) }
	}
}

func TestWriteUpstreamErrorMatrix(t *testing.T) {
	cases := []struct { err error; status int; code string }{
		{upstream.ErrNotFound, 404, "NOT_FOUND"},
		{upstream.ErrConflict, 409, "CONFLICT"},
		{upstream.ErrLastUpstream, 409, "LAST_UPSTREAM"},
		{upstream.ErrEcosystemNotActive, 409, "ECOSYSTEM_NOT_ACTIVE"},
		{upstream.ErrImmutableEcosystem, 422, "IMMUTABLE_ECOSYSTEM"},
		{upstream.ErrInvalidUpstream, 422, "INVALID_UPSTREAM"},
		{upstream.ErrReconcileFailed, 500, "REGISTRY_RECONCILE_FAILED"},
		{errors.New("database unavailable"), 500, "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		writeUpstreamError(context, tc.err)
		var response struct { Code string `json:"code"` }
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil { t.Fatal(err) }
		if recorder.Code != tc.status || response.Code != tc.code { t.Fatalf("err=%v status=%d body=%s", tc.err, recorder.Code, recorder.Body.String()) }
	}
}

func TestUpstreamHandlerDeleteResponseIsExact(t *testing.T) {
	registry, router := upstreamRouterFixture(t, 2)
	id := registry.Pools()["pypi"].Snapshot()[1].ID
	w := performJSON(router, http.MethodDelete, fmt.Sprintf("/upstreams/%d", id), "")
	if w.Code != http.StatusOK { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	if len(response) != 2 || uint(response["deleted_id"].(float64)) != id || response["adapter_type"] != "pypi" { t.Fatalf("response=%#v", response) }
}

func TestUpstreamHandlerCheckReturns200ForUnhealthyNetwork(t *testing.T) {
	_, router := upstreamRouterFixtureWithURL(t, "http://127.0.0.1:1")
	w := performJSON(router, http.MethodPost, "/upstreams/1/check", "")
	if w.Code != http.StatusOK { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var response checkUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	if response.Check.Healthy { t.Fatal("check unexpectedly healthy") }
	if response.Check.Error == nil || *response.Check.Error == "" { t.Fatal("missing network error") }
}

func TestUpstreamHandlerMasksCredentialsForReadonlyResponses(t *testing.T) {
	_, readonly := upstreamRouterFixtureWithPrincipal(t, 1, false, "http://source-user:source-secret@source.example", "http://proxy-user:proxy-secret@proxy.example:8080")
	w := performJSON(readonly, http.MethodGet, "/upstreams", "")
	var list upstreamListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil { t.Fatal(err) }
	if list.Items[0].URL != "http://***:***@source.example" || list.Items[0].Proxy != "http://***:***@proxy.example:8080" { t.Fatalf("readonly=%#v", list.Items[0]) }

	createBody := `{"adapter_type":"pypi","name":"masked","url":"https://alice:secret@masked.example","proxy":"http://bob:secret@proxy.example","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w = performJSON(readonly, http.MethodPost, "/upstreams", createBody)
	var created adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil { t.Fatal(err) }
	if created.URL != "https://***:***@masked.example" || created.Proxy != "http://***:***@proxy.example" { t.Fatalf("created=%#v", created) }

	updateBody := `{"adapter_type":"pypi","name":"masked","url":"http://carol:secret@127.0.0.1:1","proxy":"http://dave:secret@127.0.0.1:1","priority":2,"probe_mode":"passive","probe_interval":"30m"}`
	w = performJSON(readonly, http.MethodPut, fmt.Sprintf("/upstreams/%d", created.ID), updateBody)
	var updated adminUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil { t.Fatal(err) }
	if updated.URL != "http://***:***@127.0.0.1:1" || updated.Proxy != "http://***:***@127.0.0.1:1" { t.Fatalf("updated=%#v", updated) }

	w = performJSON(readonly, http.MethodPost, fmt.Sprintf("/upstreams/%d/check", created.ID), "")
	var checked checkUpstreamResponse
	if err := json.Unmarshal(w.Body.Bytes(), &checked); err != nil { t.Fatal(err) }
	if checked.Upstream.URL != "http://***:***@127.0.0.1:1" || checked.Upstream.Proxy != "http://***:***@127.0.0.1:1" { t.Fatalf("checked=%#v", checked.Upstream) }

	_, writable := upstreamRouterFixtureWithPrincipal(t, 1, true, "http://source-user:source-secret@source.example", "http://proxy-user:proxy-secret@proxy.example:8080")
	w = performJSON(writable, http.MethodGet, "/upstreams", "")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil { t.Fatal(err) }
	if list.Items[0].URL != "http://source-user:source-secret@source.example" { t.Fatalf("writable url=%q", list.Items[0].URL) }
}
```

- [ ] **Step 2: Run and verify legacy DTO/status failures**

Run: `go test ./internal/api/admin -run 'TestUpstreamHandler|TestWriteUpstreamError' -count=1`

Expected: FAIL because List returns a bare GORM array, Create is DB-only, Delete has the wrong body, and handler construction takes `*gorm.DB`.

- [ ] **Step 3: Add exact DTOs and error mapping**

```go
// internal/api/admin/upstream_contract.go
type upstreamMutationRequest struct {
	AdapterType string `json:"adapter_type" binding:"required"`
	Name string `json:"name" binding:"required"`
	URL string `json:"url" binding:"required"`
	Proxy string `json:"proxy"`
	Priority int `json:"priority" binding:"required"`
	ProbeMode string `json:"probe_mode" binding:"required"`
	ProbeInterval string `json:"probe_interval" binding:"required"`
}

func (request upstreamMutationRequest) toMutation() upstream.MutationInput {
	return upstream.MutationInput{
		AdapterType: request.AdapterType, Name: request.Name, URL: request.URL,
		Proxy: request.Proxy, Priority: request.Priority, ProbeMode: request.ProbeMode,
		ProbeInterval: request.ProbeInterval,
	}
}

type adminUpstreamResponse struct {
	ID uint `json:"id"`; AdapterType string `json:"adapter_type"`; Name string `json:"name"`; URL string `json:"url"`; Proxy string `json:"proxy"`; Priority int `json:"priority"`
	ProbeMode string `json:"probe_mode"`; ProbeInterval string `json:"probe_interval"`
	Healthy bool `json:"healthy"`; AvgLatencyMS int64 `json:"avg_latency_ms"`; SuccessRate float64 `json:"success_rate"`; LastCheckedAt *time.Time `json:"last_checked_at"`; WorkerRunning bool `json:"worker_running"`
	CreatedAt time.Time `json:"created_at"`; UpdatedAt time.Time `json:"updated_at"`
}

type upstreamListResponse struct { Items []adminUpstreamResponse `json:"items"`; Total int `json:"total"` }
type deleteUpstreamResponse struct { DeletedID uint `json:"deleted_id"`; AdapterType string `json:"adapter_type"` }
type checkResultResponse struct { Healthy bool `json:"healthy"`; LatencyMS int64 `json:"latency_ms"`; CheckedAt time.Time `json:"checked_at"`; Error *string `json:"error"` }
type checkUpstreamResponse struct { Upstream adminUpstreamResponse `json:"upstream"`; Check checkResultResponse `json:"check"` }

func writeUpstreamError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "INTERNAL_ERROR"
	switch {
	case errors.Is(err, upstream.ErrNotFound): status, code = 404, "NOT_FOUND"
	case errors.Is(err, upstream.ErrConflict): status, code = 409, "CONFLICT"
	case errors.Is(err, upstream.ErrLastUpstream): status, code = 409, "LAST_UPSTREAM"
	case errors.Is(err, upstream.ErrEcosystemNotActive): status, code = 409, "ECOSYSTEM_NOT_ACTIVE"
	case errors.Is(err, upstream.ErrImmutableEcosystem): status, code = 422, "IMMUTABLE_ECOSYSTEM"
	case errors.Is(err, upstream.ErrInvalidUpstream): status, code = 422, "INVALID_UPSTREAM"
	case errors.Is(err, upstream.ErrReconcileFailed): status, code = 500, "REGISTRY_RECONCILE_FAILED"
	}
	c.JSON(status, gin.H{"code": code, "message": err.Error()})
}
```

Map zero `LastCheckedAt` to JSON `null`, not year 1. Keep Plan 01's credential helper in the mapper:

```go
func mapAdminUpstream(item upstream.RuntimeUpstream, canViewCredentials bool) adminUpstreamResponse {
	urlValue, proxyValue := item.URL, item.Proxy
	if !canViewCredentials {
		urlValue = maskURLUserInfo(urlValue)
		proxyValue = maskURLUserInfo(proxyValue)
	}
	var checkedAt *time.Time
	if !item.LastCheckedAt.IsZero() { value := item.LastCheckedAt; checkedAt = &value }
	return adminUpstreamResponse{
		ID:item.ID, AdapterType:item.AdapterType, Name:item.Name, URL:urlValue, Proxy:proxyValue,
		Priority:item.Priority, ProbeMode:item.ProbeMode, ProbeInterval:item.ProbeInterval,
		Healthy:item.Healthy, AvgLatencyMS:item.AvgLatencyMS, SuccessRate:item.SuccessRate,
		LastCheckedAt:checkedAt, WorkerRunning:item.WorkerRunning,
		CreatedAt:item.CreatedAt, UpdatedAt:item.UpdatedAt,
	}
}
```

- [ ] **Step 4: Replace DB handler operations with Registry operations**

```go
type UpstreamHandler struct { registry *upstream.Registry }
func NewUpstreamHandler(registry *upstream.Registry) *UpstreamHandler { return &UpstreamHandler{registry: registry} }

func (h *UpstreamHandler) List(c *gin.Context) {
	items := h.registry.List()
	response := make([]adminUpstreamResponse, 0, len(items))
	canViewCredentials := principalCanViewCredentials(c)
	for _, item := range items { response = append(response, mapAdminUpstream(item, canViewCredentials)) }
	c.JSON(http.StatusOK, upstreamListResponse{Items: response, Total: len(response)})
}

func (h *UpstreamHandler) Create(c *gin.Context) {
	var request upstreamMutationRequest
	if err := c.ShouldBindJSON(&request); err != nil { c.JSON(400, gin.H{"code":"BAD_REQUEST", "message":err.Error()}); return }
	item, err := h.registry.Create(c.Request.Context(), request.toMutation())
	if err != nil { writeUpstreamError(c, err); return }
	c.JSON(http.StatusCreated, mapAdminUpstream(item, principalCanViewCredentials(c)))
}

func parseUpstreamID(c *gin.Context) (uint, bool) {
	parsed, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || parsed == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid upstream id"})
		return 0, false
	}
	return uint(parsed), true
}

func (h *UpstreamHandler) Update(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok { return }
	var request upstreamMutationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	item, err := h.registry.Update(c.Request.Context(), id, request.toMutation())
	if err != nil { writeUpstreamError(c, err); return }
	c.JSON(http.StatusOK, mapAdminUpstream(item, principalCanViewCredentials(c)))
}

func (h *UpstreamHandler) Delete(c *gin.Context) {
	id, ok := parseUpstreamID(c); if !ok { return }
	item, err := h.registry.Delete(c.Request.Context(), id)
	if err != nil { writeUpstreamError(c, err); return }
	c.JSON(http.StatusOK, deleteUpstreamResponse{DeletedID: item.ID, AdapterType: item.AdapterType})
}

func (h *UpstreamHandler) Check(c *gin.Context) {
	id, ok := parseUpstreamID(c); if !ok { return }
	item, result, err := h.registry.Check(c.Request.Context(), id)
	if err != nil { writeUpstreamError(c, err); return }
	var errorText *string
	if result.Err != nil { text := result.Err.Error(); errorText = &text }
	c.JSON(http.StatusOK, checkUpstreamResponse{Upstream: mapAdminUpstream(item, principalCanViewCredentials(c)), Check: checkResultResponse{Healthy: result.Healthy, LatencyMS: result.Latency.Milliseconds(), CheckedAt: result.CheckedAt, Error: errorText}})
}
```

Add `strconv` to `internal/api/admin/upstream.go` imports for `parseUpstreamID`. The Update handler is a full replacement of every editable field; do not expose a GET-by-ID route.

- [ ] **Step 5: Run exact handler contract tests**

Run: `go test ./internal/api/admin -run 'TestUpstreamHandler|TestWriteUpstreamError' -count=1`

Expected: PASS with Create 201, List envelope, Delete 200 exact body, and unhealthy Check 200.

- [ ] **Step 6: Commit**

```bash
git add internal/api/admin/upstream_contract.go internal/api/admin/upstream_test.go
git add -p -- internal/api/admin/upstream.go
git commit -m "fix(api): expose runtime upstream registry contract"
```

---

### Task 7: Assemble Active-Only Routes and Prove the Next Proxy Request Switches

**Files:**
- Modify: `internal/api/router.go:30-63,126-162`
- Modify: `internal/server/server.go:53-177,358-501,614-642`
- Create: `internal/server/upstream_registry_integration_test.go`

**Interfaces:**
- `api.Deps` consumes `UpstreamRegistry *upstream.Registry` while retaining `Pools` for stats/warmup consumers.
- Preserves Plan 02's `StartServer(ctx context.Context, logLevel zap.AtomicLevel)` and the one `ConfigStore *config.Store` built from that same `logLevel`.
- Server produces only active standard Pools/Adapters/routes; project routes use the same handlers and Pool pointers.
- Extra indexes continue through `NewPool(config)` and `syncConfigOwnedUpstreams`; Docker continues through `dockeradapter.New`.

- [ ] **Step 1: Write failing assembly and real proxy-switch tests**

```go
// internal/server/upstream_registry_integration_test.go
package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/adapter/pypi"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func serverTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	if err := db.AutoMigrate(database); err != nil { t.Fatal(err) }
	return database
}

func testCacheManager(t *testing.T, database *gorm.DB) *cache.Manager {
	t.Helper()
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil { t.Fatal(err) }
	return cache.NewManager(storage, database, cache.NewEventBus(), time.Hour)
}

func requestUniquePyPIPath(t *testing.T, handler http.Handler, path string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != http.StatusOK { t.Fatalf("GET %s: status=%d body=%s", path, recorder.Code, recorder.Body.String()) }
}

func TestStandardEcosystemDefinitionsHaveExactOrderRoutesAndFactories(t *testing.T) {
	cfg := &config.Config{}
	definitions := standardEcosystemDefinitions(cfg)
	got := make([][2]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.factory == nil { t.Fatalf("nil factory for %s", definition.name) }
		got = append(got, [2]string{definition.name, definition.route})
	}
	want := [][2]string{
		{"pypi", "/pypi"}, {"apt", "/apt"}, {"npm", "/npm"}, {"go", "/go"},
		{"cargo", "/crates"}, {"maven", "/maven"}, {"rubygems", "/rubygems"},
		{"composer", "/composer"}, {"nuget", "/nuget"}, {"conda", "/conda"},
		{"cran", "/cran"}, {"alpine", "/alpine"}, {"helm", "/helm"},
		{"huggingface", "/huggingface"},
	}
	if !reflect.DeepEqual(got, want) { t.Fatalf("definitions=%v want=%v", got, want) }
}

func TestSeedSourcesExcludeDefaultedAlpineAndActiveDefinitionsExcludeInactive(t *testing.T) {
	cfg := &config.Config{
		ExplicitUpstreamEcosystems: map[string]bool{"pypi": true},
		PyPI: config.AdapterConfig{Upstreams: []config.UpstreamConfig{{Name: "pypi", URL: "https://pypi.example", Priority: 1}}},
		Alpine: config.AdapterConfig{Upstreams: []config.UpstreamConfig{{Name: "built-in", URL: "https://dl-cdn.alpinelinux.org/alpine", Priority: 1}}},
	}
	definitions := standardEcosystemDefinitions(cfg)
	result, err := upstream.ReconcileBootstrap(serverTestDB(t), seedSources(definitions))
	if err != nil { t.Fatal(err) }
	if want := []string{"pypi"}; !reflect.DeepEqual(result.ActiveEcosystems, want) { t.Fatalf("active=%v want=%v", result.ActiveEcosystems, want) }
	active, err := activeDefinitions(definitions, result.ActiveEcosystems)
	if err != nil { t.Fatal(err) }
	if len(active) != 1 || active[0].name != "pypi" { t.Fatalf("definitions=%#v", active) }
}

func TestRegisterActiveAdaptersAddsOnlyStandardAndProjectPyPIRoutes(t *testing.T) {
	database := serverTestDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "primary", URL: "https://pypi.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	definitions, err := activeDefinitions(standardEcosystemDefinitions(&config.Config{}), []string{"pypi"})
	if err != nil { t.Fatal(err) }
	engine := gin.New()
	project := engine.Group("/p/:slug")
	if err := registerActiveAdapters(engine, project, definitions, registry.Pools(), testCacheManager(t, database), config.CacheConfig{}, database); err != nil { t.Fatal(err) }
	paths := make([]string, 0)
	for _, route := range engine.Routes() { paths = append(paths, route.Path) }
	if !containsString(paths, "/pypi/simple/") || !containsString(paths, "/p/:slug/pypi/simple/") { t.Fatalf("paths=%v", paths) }
	for _, path := range paths {
		if strings.Contains(path, "/npm") || strings.Contains(path, "/v2") { t.Fatalf("inactive/config-owned route registered: %s", path) }
	}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items { if item == wanted { return true } }
	return false
}

func TestRegistryUpdateChangesNextRealPyPIProxyRequest(t *testing.T) {
	var firstHits, secondHits atomic.Int64
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { firstHits.Add(1); w.Header().Set("Content-Type", "text/html"); _, _ = io.WriteString(w, `<a href="first.whl">first</a>`) }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { secondHits.Add(1); w.Header().Set("Content-Type", "text/html"); _, _ = io.WriteString(w, `<a href="second.whl">second</a>`) }))
	defer second.Close()

	database := serverTestDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "primary", URL: first.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil { t.Fatal(err) }
	registry, err := upstream.NewRegistry(database, []string{"pypi"})
	if err != nil { t.Fatal(err) }
	pool := registry.Pools()["pypi"]
	engine := gin.New()
	cacheMgr := testCacheManager(t, database)
	handler := pypi.New(cacheMgr, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLIndex: time.Nanosecond}, database)
	handler.Register(engine.Group("/pypi"))

	requestUniquePyPIPath(t, engine, "/pypi/simple/before/")
	_, err = registry.Update(context.Background(), record.ID, upstream.MutationInput{AdapterType: "pypi", Name: "primary", URL: second.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if err != nil { t.Fatal(err) }
	if registry.Pools()["pypi"] != pool { t.Fatal("registry replaced the stable Pool pointer") }
	requestUniquePyPIPath(t, engine, "/pypi/simple/after/")
	if firstHits.Load() != 1 || secondHits.Load() != 1 { t.Fatalf("first=%d second=%d", firstHits.Load(), secondHits.Load()) }
}
```

- [ ] **Step 2: Run and verify assembly helpers/runtime switching are absent**

Run: `go test ./internal/server -run 'TestStandardEcosystemDefinitions|TestSeedSources|TestRegisterActiveAdapters|TestRegistryUpdateChanges' -count=1`

Expected: FAIL because exact definitions, explicit-only seed assembly, and active filtering do not exist and current pools are built directly from config.

- [ ] **Step 3: Refactor startup into the specified order**

```go
type adapterFactory func(*cache.Manager, upstream.Selector, config.CacheConfig, *gorm.DB) adapter.Adapter

type ecosystemDef struct {
	name string
	route string
	upstreams []config.UpstreamConfig
	explicit bool
	factory adapterFactory
}

func standardEcosystemDefinitions(cfg *config.Config) []ecosystemDef {
	explicit := cfg.ExplicitUpstreamEcosystems
	return []ecosystemDef{
		{"pypi", "/pypi", cfg.PyPI.Upstreams, explicit["pypi"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return pypi.New(cm, s, cc, database) }},
		{"apt", "/apt", cfg.APT.Upstreams, explicit["apt"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return apt.New(cm, s, cc, database) }},
		{"npm", "/npm", cfg.NPM.Upstreams, explicit["npm"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return npm.New(cm, s, cc, database) }},
		{"go", "/go", cfg.Go.Upstreams, explicit["go"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return goproxy.New(cm, s, cc, database) }},
		{"cargo", "/crates", cfg.Cargo.Upstreams, explicit["cargo"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return cargo.New(cm, s, cc, database) }},
		{"maven", "/maven", cfg.Maven.Upstreams, explicit["maven"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return maven.New(cm, s, cc, database) }},
		{"rubygems", "/rubygems", cfg.RubyGems.Upstreams, explicit["rubygems"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return rubygems.New(cm, s, cc, database) }},
		{"composer", "/composer", cfg.Composer.Upstreams, explicit["composer"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return composer.New(cm, s, cc, database) }},
		{"nuget", "/nuget", cfg.NuGet.Upstreams, explicit["nuget"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return nuget.New(cm, s, cc, database) }},
		{"conda", "/conda", cfg.Conda.Upstreams, explicit["conda"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return conda.New(cm, s, cc, database) }},
		{"cran", "/cran", cfg.CRAN.Upstreams, explicit["cran"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return cran.New(cm, s, cc, database) }},
		{"alpine", "/alpine", cfg.Alpine.Upstreams, explicit["alpine"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return alpine.New(cm, s, cc, database) }},
		{"helm", "/helm", cfg.Helm.Upstreams, explicit["helm"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return helm.New(cm, s, cc, database) }},
		{"huggingface", "/huggingface", cfg.HuggingFace.Upstreams, explicit["huggingface"], func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter { return huggingface.New(cm, s, cc, database) }},
	}
}

func seedSources(definitions []ecosystemDef) []upstream.SeedSource {
	sources := make([]upstream.SeedSource, 0, len(definitions))
	for _, definition := range definitions {
		if definition.explicit { sources = append(sources, upstream.SeedSource{Ecosystem: definition.name, Upstreams: definition.upstreams}) }
	}
	return sources
}

func activeDefinitions(definitions []ecosystemDef, active []string) ([]ecosystemDef, error) {
	byName := make(map[string]ecosystemDef, len(definitions))
	for _, definition := range definitions { byName[definition.name] = definition }
	result := make([]ecosystemDef, 0, len(active))
	for _, ecosystem := range active {
		definition, ok := byName[ecosystem]
		if !ok { return nil, fmt.Errorf("active ecosystem %q has no compiled adapter", ecosystem) }
		result = append(result, definition)
	}
	return result, nil
}

func registerActiveAdapters(root *gin.Engine, project *gin.RouterGroup, definitions []ecosystemDef, pools map[string]*upstream.Pool, cacheMgr *cache.Manager, cacheConfig config.CacheConfig, database *gorm.DB) error {
	for _, definition := range definitions {
		pool := pools[definition.name]
		if pool == nil { return fmt.Errorf("active ecosystem %s has no pool", definition.name) }
		handler := definition.factory(cacheMgr, upstream.NewPrioritySelector(pool), cacheConfig, database)
		handler.Register(root.Group(definition.route))
		handler.Register(project.Group(definition.route))
	}
	return nil
}

definitions := standardEcosystemDefinitions(cfg)
bootstrap, err := upstream.ReconcileBootstrap(database, seedSources(definitions))
if err != nil { return nil, fmt.Errorf("reconcile upstream control plane: %w", err) }
registry, err := upstream.NewRegistry(database, bootstrap.ActiveEcosystems)
if err != nil { return nil, fmt.Errorf("build upstream registry: %w", err) }
pools := registry.Pools()
activeDefs, err := activeDefinitions(definitions, bootstrap.ActiveEcosystems)
if err != nil { return nil, err }
```

Put the definition/bootstrap/Registry block immediately after `db.AutoMigrate` and before any adapter construction. Remove the ordinary `syncUpstreams`, `NewPool(config)`, `RestoreFromDB`, and `StartHealthCheck` loops. Build `ecosystemNames` from `activeDefs`, construct the project group with its existing middleware, then call `registerActiveAdapters(r, projectGroup, activeDefs, pools, cacheMgr, cfg.Cache, database)`. The helper registers both standard and `/p/:slug` routes and uses the same Registry-owned Pool pointer for each pair.

Keep Docker construction and `/v2` registration unchanged and separate. For each extra index, continue `NewPool(idx.Upstreams)` on every start, call `syncConfigOwnedUpstreams(database, "extra:"+idx.Name, idx.Upstreams)`, restore its history, and launch `StartHealthCheck`; never add those Pools to Registry or Admin List. Rename the existing helper and retain its full create/update body:

```go
func syncConfigOwnedUpstreams(database *gorm.DB, adapterType string, upstreams []config.UpstreamConfig) {
	for _, item := range upstreams {
		mode := item.ProbeMode
		if mode == "" { mode = "active" }
		interval := item.ProbeInterval
		if interval == "" { interval = upstream.DefaultProbeInterval.String() }
		var record db.UpstreamRecord
		result := database.Where("name = ? AND adapter_type = ?", item.Name, adapterType).First(&record)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			record = db.UpstreamRecord{AdapterType: adapterType, Name: item.Name, URL: item.URL, Proxy: item.Proxy, Priority: item.Priority, ProbeMode: mode, ProbeInterval: interval, Healthy: true, SuccessRate: 1}
			if err := database.Create(&record).Error; err != nil { zap.L().Warn("failed to sync config-owned upstream", zap.String("name", item.Name), zap.Error(err)) }
			continue
		}
		if result.Error != nil { zap.L().Warn("failed to read config-owned upstream", zap.String("name", item.Name), zap.Error(result.Error)); continue }
		if err := database.Model(&record).Updates(map[string]any{"url": item.URL, "proxy": item.Proxy, "priority": item.Priority, "probe_mode": mode, "probe_interval": interval}).Error; err != nil {
			zap.L().Warn("failed to update config-owned upstream", zap.String("name", item.Name), zap.Error(err))
		}
	}
}
```

After all ordinary and extra handlers are registered, call `registry.Start(ctx)` exactly once before starting `ListenAndServe`. Register `registry.Close` in `srv.RegisterOnShutdown` so every dynamic worker is cancelled and joined.

- [ ] **Step 4: Inject Registry through the already capability-split router**

```go
// internal/api/router.go
// Add adjacent to Pools in the existing Deps declaration:
UpstreamRegistry *upstream.Registry

upstreamHandler := admin.NewUpstreamHandler(deps.UpstreamRegistry)
adminRead.GET("/upstreams", upstreamHandler.List)
adminWrite.POST("/upstreams", upstreamHandler.Create)
adminWrite.PUT("/upstreams/:id", upstreamHandler.Update)
adminWrite.DELETE("/upstreams/:id", upstreamHandler.Delete)
adminWrite.POST("/upstreams/:id/check", upstreamHandler.Check)
adminRead.GET("/upstreams/:id/latency", latencyHandler.GetLatencyHistory)
```

Use Plan 01's exact `adminRead` and `adminWrite` groups. Do not recreate `adminGroup.Use(AdminRequired())` and do not alter permission semantics. In the existing `api.Deps` literal inside `StartServer`, preserve every Plan 02 field and add the Registry:

```go
Config:           cfg,
ConfigStore:      settingsStore,
Pools:            pools,
UpstreamRegistry: registry,
Ecosystems:       ecosystemNames,
```

The top of the function must remain `func StartServer(ctx context.Context, logLevel zap.AtomicLevel) (*http.Server, error)`. Preserve Plan 02's `zap.ParseAtomicLevel`, `logLevel.SetLevel`, and `settingsStore := config.NewStore(cfg.ConfigPath, cfg, logLevel)` statements; do not create a second `zap.AtomicLevel` or a second Settings Store while moving startup blocks.

- [ ] **Step 5: Run real proxy and server tests**

Run: `go test -race ./internal/server ./internal/api/... -run 'TestStandardEcosystemDefinitions|TestSeedSources|TestRegisterActiveAdapters|TestRegistryUpdateChanges|TestUpstreamHandler|TestWriteUpstreamError' -count=1`

Expected: PASS; two unique real proxy requests hit first then second upstream without rebuilding the adapter or Pool pointer.

- [ ] **Step 6: Commit**

```bash
git add -p -- internal/api/router.go internal/server/server.go
git add internal/server/upstream_registry_integration_test.go
git commit -m "feat(server): assemble active upstream registry routes"
```

---

### Task 8: Type the Upstream API End to End in TypeScript

**Files:**
- Modify: `web/src/lib/adminApi.types.ts`
- Modify: `web/src/lib/adminApi.types.type-test.ts`
- Modify: `web/src/lib/api.ts:1-68`

**Interfaces:**
- Produces exact `UpstreamMutationRequest`, `AdminUpstream`, `AdminUpstreamListResponse`, `DeleteUpstreamResponse`, `UpstreamCheckResult`, and `CheckUpstreamResponse`.
- Produces typed Axios methods with no Upstream-path `any`.

- [ ] **Step 1: Add failing compile-time contract assertions**

```ts
// append to web/src/lib/adminApi.types.type-test.ts
import type {
  AdminUpstream, AdminUpstreamListResponse, CheckUpstreamResponse,
  DeleteUpstreamResponse, UpstreamMutationRequest,
} from './adminApi.types'

type UpstreamIsAny<T> = 0 extends (1 & T) ? true : false

export type UpstreamListContract = Assert<Equal<ResponseData<typeof adminApi.listUpstreams>, AdminUpstreamListResponse>>
export type UpstreamCreateContract = Assert<Equal<ResponseData<typeof adminApi.createUpstream>, AdminUpstream>>
export type UpstreamUpdateContract = Assert<Equal<ResponseData<typeof adminApi.updateUpstream>, AdminUpstream>>
export type UpstreamDeleteContract = Assert<Equal<ResponseData<typeof adminApi.deleteUpstream>, DeleteUpstreamResponse>>
export type UpstreamCheckContract = Assert<Equal<ResponseData<typeof adminApi.checkUpstream>, CheckUpstreamResponse>>
export type UpstreamRequestNotAny = Assert<Equal<UpstreamIsAny<Parameters<typeof adminApi.createUpstream>[0]>, false>>
const validMutation: UpstreamMutationRequest = { adapter_type: 'pypi', name: 'primary', url: 'https://pypi.org', proxy: '', priority: 1, probe_mode: 'active', probe_interval: '30m' }
void validMutation
```

`adminApi`, `Equal`, `Assert`, and `ResponseData` already exist in this Plan 01-created file. Reuse them unchanged; do not redeclare or re-import them.

- [ ] **Step 2: Run type-check and verify Upstream DTOs are missing**

Run: `cd web && npm run type-check`

Expected: FAIL with missing exported Upstream types and non-exact Axios response types.

- [ ] **Step 3: Add the exact DTOs**

```ts
// web/src/lib/adminApi.types.ts
export interface UpstreamMutationRequest {
  adapter_type: string
  name: string
  url: string
  proxy: string
  priority: number
  probe_mode: 'active' | 'passive'
  probe_interval: string
}

export interface AdminUpstream extends UpstreamMutationRequest {
  id: number
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  last_checked_at: string | null
  worker_running: boolean
  created_at: string
  updated_at: string
}

export interface AdminUpstreamListResponse { items: AdminUpstream[]; total: number }
export interface DeleteUpstreamResponse { deleted_id: number; adapter_type: string }
export interface UpstreamCheckResult { healthy: boolean; latency_ms: number; checked_at: string; error: string | null }
export interface CheckUpstreamResponse { upstream: AdminUpstream; check: UpstreamCheckResult }
```

- [ ] **Step 4: Type all Axios methods**

```ts
import type {
  AdminUpstream, AdminUpstreamListResponse, CheckUpstreamResponse,
  DeleteUpstreamResponse, UpstreamMutationRequest,
} from './adminApi.types'

listUpstreams: () => api.get<AdminUpstreamListResponse>('/admin/upstreams'),
createUpstream: (data: UpstreamMutationRequest) => api.post<AdminUpstream>('/admin/upstreams', data),
updateUpstream: (id: number, data: UpstreamMutationRequest) => api.put<AdminUpstream>(`/admin/upstreams/${id}`, data),
deleteUpstream: (id: number) => api.delete<DeleteUpstreamResponse>(`/admin/upstreams/${id}`),
checkUpstream: (id: number) => api.post<CheckUpstreamResponse>(`/admin/upstreams/${id}/check`),
```

- [ ] **Step 5: Run type-check and build**

Run: `cd web && npm run type-check && npm exec eslint -- src/lib/adminApi.types.ts src/lib/adminApi.types.type-test.ts && ! rg -n '(list|create|update|delete|check)Upstream.*any' src/lib/api.ts && npm run build`

Expected: PASS; compile-time assertions prove response data and mutation input are exact and non-`any`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/adminApi.types.ts web/src/lib/adminApi.types.type-test.ts
git add -p -- web/src/lib/api.ts
git commit -m "refactor(web): type upstream registry API"
```

---

### Task 9: Make Upstreams Consume Runtime Responses and Document Migration Semantics

**Files:**
- Modify: `web/src/admin/pages/Upstreams.tsx`
- Modify: `config.example.toml:43-196`
- Modify: `README.md`
- Modify: `docs/README_zh.md`

**Interfaces:**
- Consumes: typed methods and DTOs from Task 8.
- Produces: active ecosystem options derived from `AdminUpstreamListResponse.items`; Docker is absent.
- Produces: query-cache helpers `upsertRuntimeUpstream`, `removeRuntimeUpstream`, and `replaceRuntimeList` using exact DTOs.
- Leaves: all responsive, primitive, IconButton, Tooltip, Toast, and styling changes to Plan 04.

- [ ] **Step 1: Add compile-time runtime-cache helpers before changing the page**

```ts
// add near the top of web/src/admin/pages/Upstreams.tsx
import type { AdminUpstream, AdminUpstreamListResponse, UpstreamMutationRequest } from '@/lib/adminApi.types'

const runtimeEcosystemOrder = [
  'pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems',
  'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'huggingface',
] as const
const runtimeEcosystemRank = new Map<string, number>(runtimeEcosystemOrder.map((name, index) => [name, index] as const))

function upsertRuntimeUpstream(
  current: AdminUpstreamListResponse | undefined,
  upstream: AdminUpstream,
): AdminUpstreamListResponse {
  const items = current?.items ?? []
  const index = items.findIndex((item) => item.id === upstream.id)
  const next = index < 0 ? [...items, upstream] : items.map((item) => item.id === upstream.id ? upstream : item)
  next.sort((a, b) => (runtimeEcosystemRank.get(a.adapter_type) ?? Number.MAX_SAFE_INTEGER) - (runtimeEcosystemRank.get(b.adapter_type) ?? Number.MAX_SAFE_INTEGER) || a.priority - b.priority || a.id - b.id)
  return { items: next, total: next.length }
}

function removeRuntimeUpstream(
  current: AdminUpstreamListResponse | undefined,
  deletedID: number,
): AdminUpstreamListResponse {
  const items = (current?.items ?? []).filter((item) => item.id !== deletedID)
	return { items, total: items.length }
}

function replaceRuntimeList(
  current: AdminUpstreamListResponse | undefined,
  replacements: AdminUpstream[],
): AdminUpstreamListResponse {
  let next = current ?? { items: [], total: 0 }
  for (const replacement of replacements) next = upsertRuntimeUpstream(next, replacement)
  return next
}
```

Replace the local form type with `UpstreamMutationRequest` and add a compile-time assertion in the same file:

```ts
const emptyForm = (ecosystem: string): UpstreamMutationRequest => ({
  adapter_type: ecosystem,
  name: '', url: '', proxy: '', priority: 1,
  probe_mode: 'active', probe_interval: '30m',
})
```

- [ ] **Step 2: Run type-check and expose every legacy `any`/fallback**

Run: `cd web && npm run type-check`

Expected: FAIL until query data, forms, mutation inputs, and `openEdit` stop using `any`; the legacy `data?.data?.items || data?.data || []` union is not assignable to the exact envelope.

- [ ] **Step 3: Consume only the runtime envelope and active ecosystems**

```ts
const { data, isLoading } = useQuery({
  queryKey: ['admin', 'upstreams'],
  queryFn: async () => (await adminApi.listUpstreams()).data,
})
const allUpstreams = data?.items ?? []
const activeEcosystems = Array.from(new Set(allUpstreams.map((item) => item.adapter_type)))
const [form, setForm] = useState<UpstreamMutationRequest>(() => emptyForm(''))

function openCreate() {
  const ecosystem = activeEcosystems[0]
  if (!ecosystem) return
  setForm(emptyForm(ecosystem))
  setEditId(null)
  setDialogOpen(true)
}

function closeDialog() {
  setDialogOpen(false)
  setEditId(null)
  setForm(emptyForm(''))
  setUrlError('')
}

function openEdit(item: UpstreamItem) {
  const runtime = allUpstreams.find((candidate) => candidate.id === item.id)
  if (!runtime) return
  setEditId(runtime.id)
  setForm({
    adapter_type: runtime.adapter_type, name: runtime.name, url: runtime.url,
    proxy: runtime.proxy, priority: runtime.priority, probe_mode: runtime.probe_mode,
    probe_interval: runtime.probe_interval,
  })
  setDialogOpen(true)
}

const upstreamItems: UpstreamItem[] = allUpstreams.map((item) => ({
  id: item.id,
  name: item.name,
  adapter: item.adapter_type,
  healthy: item.healthy,
  avg_latency_ms: item.avg_latency_ms,
  success_rate: item.success_rate,
  url: item.url,
  proxy: item.proxy,
  priority: item.priority,
}))
```

The create ecosystem selector renders `activeEcosystems`; it must not include a hard-coded Docker or inactive ecosystem. Edit keeps `adapter_type` disabled and submits the unchanged value.

Remove the `ECOSYSTEMS` constant and every local `any`. Pass `openEdit` directly as `openEdit(item)`. In `handleSubmit`, use `updateMutation.mutate({ id: editId, request: form })` when `editId !== null`; otherwise use `createMutation.mutate(form)`. Cast only the probe select value to `UpstreamMutationRequest['probe_mode']`, because DOM select values are strings. Keep the existing form markup/classes for Plan 04.

- [ ] **Step 4: Update cache from each server runtime response**

```ts
const createMutation = useMutation({
  mutationFn: (request: UpstreamMutationRequest) => adminApi.createUpstream(request),
  onSuccess: ({ data: runtime }) => {
    queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
    closeDialog()
  },
})

const updateMutation = useMutation({
  mutationFn: ({ id, request }: { id: number; request: UpstreamMutationRequest }) => adminApi.updateUpstream(id, request),
  onSuccess: ({ data: runtime }) => {
    queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
    closeDialog()
  },
})

const deleteMutation = useMutation({
  mutationFn: (id: number) => adminApi.deleteUpstream(id),
  onSuccess: ({ data }) => {
    queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => removeRuntimeUpstream(current, data.deleted_id))
    setDeleteTarget(null)
  },
})

async function checkOne(id: number) {
  const { data: result } = await adminApi.checkUpstream(id)
  queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, result.upstream))
}

async function checkAll() {
  setChecking(true)
  try {
    const settled = await Promise.allSettled(allUpstreams.map((item) => adminApi.checkUpstream(item.id)))
    const fulfilled = settled.flatMap((result) => result.status === 'fulfilled' ? [result.value.data.upstream] : [])
    queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => replaceRuntimeList(current, fulfilled))
  } finally {
    setChecking(false)
  }
}
```

Keep mutation dialogs open on error by closing only in `onSuccess`. `checkAll` leaves rejected entries unchanged and applies every fulfilled runtime resource; because a network-unhealthy probe is HTTP 200, its `result.value.data.upstream` is applied rather than treated as a request failure. Plan 04 will add localized inline/Toast presentation for rejected registry operations. Do not change classes, layout, icons, primitives, breakpoints, or visible styling in this task.

- [ ] **Step 5: Document one-time seed, DB authority, activation, Docker, and extras**

Add this valid TOML comment block to `config.example.toml` immediately before the first ordinary upstream list:

```toml
# On the first start after upgrading, Depsilo imports ordinary ecosystem upstreams
# into the database and records the active ecosystems. After that seed, Admin and
# the database are authoritative: deleting or editing an upstream is not overwritten
# by later restarts. Adding upstreams in config for a previously inactive supported
# ecosystem activates it on the next restart. Docker registries and extra indexes
# remain config-owned and are not managed by Admin Upstream CRUD.
```

Add this exact English paragraph to the `README.md` Admin configuration documentation:

```text
On the first start after upgrading, Depsilo imports ordinary ecosystem upstreams into the database and records the active ecosystems. After that seed, Admin and the database are authoritative: deleting or editing an upstream is not overwritten by later restarts. Adding upstreams in config for a previously inactive supported ecosystem activates that ecosystem on the next restart. Docker registries and extra indexes remain config-owned and are not managed by Admin Upstream CRUD.
```

Add the equivalent Chinese block to `docs/README_zh.md`:

```text
升级后的首次启动会把普通生态的上游源导入数据库，并记录已激活生态。完成首次导入后，Admin 与数据库成为权威来源；通过 Admin 删除或修改的上游源不会在重启时被配置文件覆盖。若要启用此前未激活的受支持生态，需要先在配置文件中添加该生态的上游源并重启。Docker Registry 与额外索引仍由配置文件管理，不属于 Admin 上游源 CRUD。
```

- [ ] **Step 6: Run frontend, documentation, and repository gates**

Run:

```bash
cd web
npm run type-check
npm exec eslint -- src/admin/pages/Upstreams.tsx src/lib/adminApi.types.ts src/lib/adminApi.types.type-test.ts
! rg -n '(list|create|update|delete|check)Upstream.*any' src/lib/api.ts
npm run build
cd ..
rg -n "database.*authoritative|数据库成为权威|Docker.*extra" config.example.toml README.md docs/README_zh.md
go test -race ./internal/upstream ./internal/api/admin ./internal/api/public ./internal/server
go test ./...
```

Expected: all commands PASS; Upstreams has no Upstream-path `any`, docs describe all four migration boundaries, and the full Go suite passes.

- [ ] **Step 7: Commit**

```bash
git add web/src/admin/pages/Upstreams.tsx
git add -p -- config.example.toml README.md docs/README_zh.md
git commit -m "fix(admin): reflect live upstream registry state"
```

---

## Final Verification

- [ ] Confirm the migration creates both `upstreams_seeded_v1=true` and valid ordered `upstreams_active_ecosystems_v1` JSON in the same transaction.
- [ ] Confirm a seeded active ecosystem never imports config rows again, while a newly configured inactive supported ecosystem imports once and becomes active.
- [ ] Confirm Docker and an example `extra:private` pool are absent from Registry and Admin List but continue to serve their config-owned routes.
- [ ] Confirm an active ecosystem with zero DB rows stops startup; the PyPI case must say `active ecosystem pypi has no upstreams`.
- [ ] Confirm every adapter and selector retains the same Pool pointer while Create/Update/Delete changes the next request through one snapshot Store.
- [ ] Confirm Update of proxy or interval rebuilds the client and restarts exactly one worker; Delete cancels and joins the removed worker.
- [ ] Confirm a failed transaction leaves the DB and Pool unchanged; a post-swap invariant failure runs one DB rebuild and returns `REGISTRY_RECONCILE_FAILED` only if that rebuild also fails.
- [ ] Confirm Check uses the upstream proxy client, writes the correct `upstream_id`, and returns 200 with `check.healthy=false` for network failure.
- [ ] Confirm List/Create/Update/Delete/Check DTOs and all status/error codes match the design exactly.
- [ ] Confirm only active standard and project-scoped routes exist; Docker and extra-index routes preserve their prior config ownership.
- [ ] Confirm `go test -race ./internal/upstream ./internal/api/admin ./internal/api/public ./internal/server` and `go test ./...` pass.
- [ ] Confirm TypeScript type-check, targeted ESLint, and the production build pass and Plan 03 makes no visual primitive/layout/icon styling changes.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-10-admin-remediation-03-upstream-registry.md`. Two execution options:

1. **Subagent-Driven (recommended)** - dispatch a fresh subagent per task, run two-stage review between tasks, and keep each task's commit independently reviewable. Required sub-skill: `superpowers:subagent-driven-development`.
2. **Inline Execution** - execute tasks in this session in batches with review checkpoints. Required sub-skill: `superpowers:executing-plans`.
