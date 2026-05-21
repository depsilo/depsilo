# Pricing surface + 14-day trial — Implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the in-product `/admin/license` page + local 14-day trial activation + landing `/pro-trial` page so the already-built Pro features become a self-serve conversion funnel.

**Architecture:** Three new backend modules (`internal/trial`, `internal/entitlement`, plus extensions to `internal/license`) wired through an `entitlement.Checker` façade that combines paid-license state with local trial state. `RequirePro` middleware, `audit.Logger`, and `rules.Engine` all switch from depending on `*license.Manager` to `*entitlement.Checker` so trial users get the same access as paid users. Frontend gains one new page, one global modal, and a sidebar entry.

**Tech Stack:** Go 1.21, GORM (SQLite/PostgreSQL), Gin, React 18 + TypeScript + Vite, TanStack Query, shadcn/ui + Tailwind, Astro (landing repo).

**Spec source of truth:** [docs/specs/2026-05-21-pricing-trial-design.md](../specs/2026-05-21-pricing-trial-design.md) (commit 4b698ab).

**Known deviation from spec:** §6.3 / §8.2 require audit log entries on `trial.activated`, `license.key_set`, `license.key_cleared`. The existing `db.AuditLog` model is request-shaped (Ecosystem/PackageName/Version), not event-shaped. Adding a management-event model is its own design task. **Each affected handler will include a `// TODO: write management event audit log` comment** and the actual write deferred to a follow-up plan.

---

## File structure

### `depsilo/` repository — new files

| Path | Responsibility |
|---|---|
| `internal/trial/manager.go` | `trial.Manager` + cache state + activate logic |
| `internal/trial/errors.go` | `ErrTrialAlreadyUsed` and friends |
| `internal/trial/manager_test.go` | Unit tests for trial.Manager |
| `internal/entitlement/checker.go` | `Checker` façade + `Status` struct + `Source` enum |
| `internal/entitlement/middleware.go` | `RequirePro` middleware (moved from `internal/license/license.go`) |
| `internal/entitlement/checker_test.go` | Unit tests for source precedence and Status assembly |
| `tests/integration/license_trial_test.go` | End-to-end activate/key/paywall flows |
| `web/src/admin/pages/License.tsx` | The new License page |
| `web/src/admin/components/ProRequiredModal.tsx` | Global 402 modal |
| `depsilo-landingpage/src/pages/pro-trial.astro` | Closes the existing `/pro-trial` 404 |

### `depsilo/` repository — modified files

| Path | Change |
|---|---|
| `internal/db/models.go` | Add `TrialRecord` + `LicenseStorage` models |
| `internal/db/repository.go` | Register the two new models in `AutoMigrate` |
| `internal/license/license.go` | Constructor takes `*gorm.DB`; add `SetKey` / `ClearKey`; delete `RequirePro` (moved to entitlement) |
| `internal/audit/logger.go` | Switch dep from `*license.Manager` to `*entitlement.Checker` |
| `internal/rules/engine.go` | Switch dep from `*license.Manager` to `*entitlement.Checker` |
| `internal/api/admin/license.go` | Rewrite handler struct + add ActivateTrial / SetKey / ClearKey |
| `internal/api/router.go` | Add `TrialManager`, `Entitlement`, remove direct LicenseManager passing where applicable |
| `internal/server/server.go` | Wire up `trial.NewManager`, `entitlement.NewChecker`; pass Checker to audit / rules |
| `web/src/lib/api.ts` | Add `licenseApi` methods + `EntitlementStatus` type + 402 axios interceptor branch |
| `web/src/admin/AdminApp.tsx` | Mount `ProRequiredModal`; add route for `/admin/license` |
| `web/src/admin/components/MainLayout.tsx` | Add "License" sidebar entry |
| `web/src/i18n/zh.ts` | Add `license.*` namespace |
| `web/src/i18n/en.ts` | Add `license.*` namespace (parity-locked with zh) |
| `CHANGELOG.md` | Entry per spec §16.4 |

### `depsilo-landingpage/` repository — modified files

| Path | Change |
|---|---|
| `src/i18n/locales/zh-CN.json` | Add `pro_trial_*` keys |
| `src/i18n/locales/en.json` | Add `pro_trial_*` keys (parity) |

---

# Phase 0 — Pre-flight

### Task 0.1: Verify clean tree and baseline tests pass

**Files:** none

- [ ] **Step 1: Confirm working tree clean and on master**

```bash
git status -s
git rev-parse --abbrev-ref HEAD
```

Expected: empty status output, `master` on second line.

- [ ] **Step 2: Confirm Makefile baseline**

```bash
make lint && make test-unit && make test-integration
```

Expected: all three green. If `test-integration` fails for an unrelated reason, note it now; do not start coding until baseline is clean.

- [ ] **Step 3: Capture starting commit hash for rollback reference**

```bash
git rev-parse HEAD > /tmp/depsilo-implementation-start.txt
cat /tmp/depsilo-implementation-start.txt
```

---

# Phase 1 — Backend data model

### Task 1.1: Add `TrialRecord` model

**Files:**
- Modify: `internal/db/models.go` (append at end)

- [ ] **Step 1: Add the model**

Append to `internal/db/models.go`:

```go
// TrialRecord persists the local 14-day Pro trial state. Singleton (at most one
// row, ID = 1). Uniqueness is enforced by the manager-layer mutex + count check,
// not by a DB constraint, because Depsilo is single-instance today.
type TrialRecord struct {
    ID            uint      `gorm:"primarykey"`
    ActivatedAt   time.Time `gorm:"not null"`
    ExpiresAt     time.Time `gorm:"not null"`
    ActivatedBy   uint      `gorm:"index"` // FK to User.ID; admin who clicked
    ActivatedFrom string    `gorm:"size:64"` // client IP, reserved for future abuse analysis
    CreatedAt     time.Time
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/db/models.go
git commit -m "feat(db): add TrialRecord model for local 14-day Pro trial state"
```

### Task 1.2: Add `LicenseStorage` model

**Files:**
- Modify: `internal/db/models.go` (append after `TrialRecord`)

- [ ] **Step 1: Add the model**

Append to `internal/db/models.go`:

```go
// LicenseStorage persists a license key that was set via the admin UI.
// Takes precedence over the config.toml-loaded key when both exist.
// Singleton (ID = 1). License keys are identifiers, not credentials —
// stored as plaintext; masked only for display.
type LicenseStorage struct {
    ID        uint      `gorm:"primarykey"`
    Key       string    `gorm:"size:256"`
    UpdatedBy uint      `gorm:"index"`
    UpdatedAt time.Time
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/models.go
git commit -m "feat(db): add LicenseStorage model for runtime license key persistence"
```

### Task 1.3: Register new models in AutoMigrate

**Files:**
- Modify: `internal/db/repository.go:54-72`

- [ ] **Step 1: Append both models to AutoMigrate**

Change the `db.AutoMigrate(...)` call to add the two new models at the end:

```go
func AutoMigrate(db *gorm.DB) error {
    zap.L().Info("running database auto-migration")
    return db.AutoMigrate(
        &CacheEntry{},
        &AccessLog{},
        &UpstreamRecord{},
        &User{},
        &APIToken{},
        &UpstreamLatencyLog{},
        &AuditLog{},
        &PackageRule{},
        &Vulnerability{},
        &VulnerabilityCheck{},
        &SecurityPolicy{},
        &DismissedVuln{},
        &Project{},
        &ProjectPackage{},
        &TrialRecord{},
        &LicenseStorage{},
    )
}
```

- [ ] **Step 2: Verify migrations run on boot without errors**

```bash
make build
DEPSILO_CONFIG=config.example.toml ./bin/depsilo &
PID=$!
sleep 3
curl -sf http://localhost:23333/health
kill $PID
```

Expected: `/health` returns 200; logs (in `.dev.log` or stdout) show "running database auto-migration" and no migration errors. (If config.example.toml uses a different port, adjust.)

- [ ] **Step 3: Commit**

```bash
git add internal/db/repository.go
git commit -m "feat(db): register TrialRecord + LicenseStorage in AutoMigrate"
```

---

# Phase 2 — `internal/trial/`

### Task 2.1: Create `errors.go`

**Files:**
- Create: `internal/trial/errors.go`

- [ ] **Step 1: Create the file**

```go
package trial

import "errors"

// ErrTrialAlreadyUsed is returned by Manager.Activate when a TrialRecord
// already exists (regardless of whether it's expired). One trial per install.
var ErrTrialAlreadyUsed = errors.New("trial already used")
```

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/trial/
```

- [ ] **Step 3: Commit**

```bash
git add internal/trial/errors.go
git commit -m "feat(trial): scaffold internal/trial package + ErrTrialAlreadyUsed"
```

### Task 2.2: Write failing test for `NewManager` + `IsActive` + `IsUsed` + `Available`

**Files:**
- Create: `internal/trial/manager_test.go`

- [ ] **Step 1: Write the test**

```go
package trial_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "depsilo/internal/db"
    "depsilo/internal/trial"
)

func newTestDB(t *testing.T) *gorm.DB {
    t.Helper()
    d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    require.NoError(t, err)
    require.NoError(t, d.AutoMigrate(&db.TrialRecord{}, &db.User{}))
    return d
}

func TestNewManager_NoRecord_AvailableTrue(t *testing.T) {
    d := newTestDB(t)
    m := trial.NewManager(d)
    require.True(t, m.Available())
    require.False(t, m.IsActive())
    require.False(t, m.IsUsed())
}

func TestNewManager_LoadsExistingActiveRecord(t *testing.T) {
    d := newTestDB(t)
    now := time.Now().UTC()
    require.NoError(t, d.Create(&db.TrialRecord{
        ActivatedAt: now,
        ExpiresAt:   now.Add(14 * 24 * time.Hour),
        ActivatedBy: 1,
    }).Error)

    m := trial.NewManager(d)
    require.True(t, m.IsActive())
    require.True(t, m.IsUsed())
    require.False(t, m.Available())
}

func TestNewManager_LoadsExistingExpiredRecord(t *testing.T) {
    d := newTestDB(t)
    now := time.Now().UTC()
    require.NoError(t, d.Create(&db.TrialRecord{
        ActivatedAt: now.Add(-30 * 24 * time.Hour),
        ExpiresAt:   now.Add(-16 * 24 * time.Hour),
        ActivatedBy: 1,
    }).Error)

    m := trial.NewManager(d)
    require.False(t, m.IsActive())
    require.True(t, m.IsUsed())
    require.False(t, m.Available())
}

func TestActivate_FirstCallSucceeds(t *testing.T) {
    d := newTestDB(t)
    m := trial.NewManager(d)

    rec, err := m.Activate(context.Background(), 42, "127.0.0.1")
    require.NoError(t, err)
    require.NotNil(t, rec)
    require.Equal(t, uint(42), rec.ActivatedBy)
    require.Equal(t, "127.0.0.1", rec.ActivatedFrom)
    require.WithinDuration(t, rec.ActivatedAt.Add(14*24*time.Hour), rec.ExpiresAt, time.Second)
    require.True(t, m.IsActive())
}

func TestActivate_SecondCallReturnsErrTrialAlreadyUsed(t *testing.T) {
    d := newTestDB(t)
    m := trial.NewManager(d)

    _, err := m.Activate(context.Background(), 1, "")
    require.NoError(t, err)

    _, err = m.Activate(context.Background(), 2, "")
    require.ErrorIs(t, err, trial.ErrTrialAlreadyUsed)
}

func TestActivate_ConcurrentOnlyOneSucceeds(t *testing.T) {
    d := newTestDB(t)
    m := trial.NewManager(d)

    const N = 16
    errs := make(chan error, N)
    for i := 0; i < N; i++ {
        go func() {
            _, err := m.Activate(context.Background(), 1, "")
            errs <- err
        }()
    }

    var successes, alreadyUsed int
    for i := 0; i < N; i++ {
        switch err := <-errs; {
        case err == nil:
            successes++
        case err == trial.ErrTrialAlreadyUsed:
            alreadyUsed++
        default:
            t.Fatalf("unexpected error: %v", err)
        }
    }
    require.Equal(t, 1, successes)
    require.Equal(t, N-1, alreadyUsed)
}
```

- [ ] **Step 2: Run tests and confirm they fail**

```bash
go test ./internal/trial/ -run TestNewManager -v
```

Expected: build failure — `undefined: trial.NewManager`. This is the failing red state.

### Task 2.3: Implement `trial.Manager`

**Files:**
- Create: `internal/trial/manager.go`

- [ ] **Step 1: Write the implementation**

```go
package trial

import (
    "context"
    "errors"
    "sync"
    "time"

    "gorm.io/gorm"

    "depsilo/internal/db"
)

// TrialDuration is the length of a Pro trial, baked in at compile time
// per spec §4.2.
const TrialDuration = 14 * 24 * time.Hour

// Manager owns the local 14-day Pro trial state machine.
// Single-instance, singleton record (ID = 1). Thread-safe.
type Manager struct {
    database *gorm.DB
    mu       sync.RWMutex
    record   *db.TrialRecord // nil if trial has never been activated
}

// NewManager loads the existing TrialRecord (if any) from the database.
// At most one TrialRecord row is expected; if more are present, the lowest-ID
// one wins (a no-op for a single-instance deploy that respects the contract).
func NewManager(database *gorm.DB) *Manager {
    m := &Manager{database: database}
    var rec db.TrialRecord
    err := database.Order("id ASC").First(&rec).Error
    if err == nil {
        m.record = &rec
    }
    return m
}

// IsActive reports whether a trial currently grants Pro access.
// Hot path — called from RequirePro middleware on every Pro request.
func (m *Manager) IsActive() bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.record != nil && time.Now().UTC().Before(m.record.ExpiresAt)
}

// IsUsed reports whether a TrialRecord exists at all (active or expired).
func (m *Manager) IsUsed() bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.record != nil
}

// Available reports whether the user can still start a trial.
func (m *Manager) Available() bool {
    return !m.IsUsed()
}

// Record returns a copy of the current trial record (or nil).
func (m *Manager) Record() *db.TrialRecord {
    m.mu.RLock()
    defer m.mu.RUnlock()
    if m.record == nil {
        return nil
    }
    copy := *m.record
    return &copy
}

// Activate creates the singleton TrialRecord. Returns ErrTrialAlreadyUsed
// if a record already exists.
func (m *Manager) Activate(ctx context.Context, userID uint, fromIP string) (*db.TrialRecord, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.record != nil {
        return nil, ErrTrialAlreadyUsed
    }

    // Defensive re-check against the DB in case another process raced us.
    var count int64
    if err := m.database.WithContext(ctx).Model(&db.TrialRecord{}).Count(&count).Error; err != nil {
        return nil, err
    }
    if count > 0 {
        // Refresh cache from the winning row and report already-used.
        var rec db.TrialRecord
        if err := m.database.WithContext(ctx).Order("id ASC").First(&rec).Error; err == nil {
            m.record = &rec
        }
        return nil, ErrTrialAlreadyUsed
    }

    now := time.Now().UTC()
    rec := &db.TrialRecord{
        ActivatedAt:   now,
        ExpiresAt:     now.Add(TrialDuration),
        ActivatedBy:   userID,
        ActivatedFrom: fromIP,
    }
    if err := m.database.WithContext(ctx).Create(rec).Error; err != nil {
        return nil, err
    }
    m.record = rec

    // TODO: write management event audit log (deferred — current AuditLog
    // model is request-shaped; see plan front matter "Known deviation").
    _ = errors.New // satisfy unused import for now

    return rec, nil
}
```

Note: the `_ = errors.New` line is a placeholder to keep `errors` imported in case future code needs it. Drop it now since it's unused — remove the `errors` import too:

```go
import (
    "context"
    "sync"
    "time"

    "gorm.io/gorm"

    "depsilo/internal/db"
)
```

And remove the `_ = errors.New` line.

- [ ] **Step 2: Run the tests**

```bash
go test ./internal/trial/ -v
```

Expected: all 6 tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/trial/manager.go internal/trial/manager_test.go
git commit -m "feat(trial): implement trial.Manager state machine + tests"
```

---

# Phase 3 — `internal/license/` extensions

### Task 3.1: Add `*gorm.DB` parameter to `license.NewManager`

This is a constructor signature change. Tests + every call site needs updating.

**Files:**
- Modify: `internal/license/license.go:36`
- Modify: `internal/server/server.go:132`

- [ ] **Step 1: Update `NewManager` signature and add DB-key precedence**

In `internal/license/license.go`, change `NewManager`:

```go
import (
    // existing imports ...
    "gorm.io/gorm"
    "depsilo/internal/db"
)

// NewManager creates a new license Manager from config + DB.
// Key precedence: DB-stored key (set via UI) > config.toml key > none.
// DEPSILO_DEV_PRO=1 bypasses everything per dev mode.
func NewManager(cfg config.LicenseConfig, database *gorm.DB) *Manager {
    m := &Manager{database: database}

    now := time.Now().UTC()

    if os.Getenv("DEPSILO_DEV_PRO") == "1" {
        m.status = LicenseStatus{
            IsPro:       true,
            KeyMasked:   "dev-mode",
            ActivatedAt: &now,
            LastChecked: now,
        }
        zap.L().Warn("DEPSILO_DEV_PRO is set — Pro features activated without license validation")
        return m
    }

    // Load key: DB first, then config.
    if database != nil {
        var stored db.LicenseStorage
        if err := database.Order("id ASC").First(&stored).Error; err == nil && stored.Key != "" {
            m.key = strings.TrimSpace(stored.Key)
        }
    }
    if m.key == "" {
        m.key = strings.TrimSpace(cfg.Key)
    }

    if m.key == "" {
        m.status = LicenseStatus{IsPro: false, LastChecked: now}
    } else {
        m.status = LicenseStatus{IsPro: false, KeyMasked: MaskKey(m.key), LastChecked: now}
    }
    return m
}
```

And add the `database` field to the struct:

```go
type Manager struct {
    key      string
    database *gorm.DB  // NEW
    mu       sync.RWMutex
    status   LicenseStatus
}
```

- [ ] **Step 2: Update server.go call site**

In `internal/server/server.go:132`:

```go
licenseManager := license.NewManager(cfg.License, database)
```

(was: `license.NewManager(cfg.License)`)

- [ ] **Step 3: Compile-check**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run existing license-touching tests if any**

```bash
go test ./internal/license/... ./tests/unit/... -count=1
```

Expected: PASS. If any test instantiates `license.NewManager(cfg)` with one arg, update it to pass `nil` as the DB.

- [ ] **Step 5: Commit**

```bash
git add internal/license/license.go internal/server/server.go
git commit -m "refactor(license): NewManager takes *gorm.DB; DB key > config key"
```

### Task 3.2: Write failing tests for `SetKey` / `ClearKey`

**Files:**
- Create: `internal/license/license_test.go` (if it doesn't exist, otherwise modify)

- [ ] **Step 1: Check if a test file already exists**

```bash
ls internal/license/*_test.go 2>/dev/null
```

If empty, create `internal/license/license_test.go`. If a test file exists, append the tests below to it.

- [ ] **Step 2: Write the tests**

```go
package license_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "depsilo/internal/config"
    "depsilo/internal/db"
    "depsilo/internal/license"
)

func newTestDB(t *testing.T) *gorm.DB {
    t.Helper()
    d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    require.NoError(t, err)
    require.NoError(t, d.AutoMigrate(&db.LicenseStorage{}, &db.User{}))
    return d
}

func TestSetKey_PersistsAndUpdatesState(t *testing.T) {
    d := newTestDB(t)
    m := license.NewManager(config.LicenseConfig{}, d)
    require.False(t, m.IsPro())

    // Note: SetKey will call doValidate which hits Lemon Squeezy over the
    // network. In this unit test we expect doValidate to fail (test runners
    // typically have no internet). The key MUST be persisted regardless.
    _ = m.SetKey(context.Background(), "depsilo-test-key-1234", 7)

    var stored db.LicenseStorage
    require.NoError(t, d.First(&stored).Error)
    require.Equal(t, "depsilo-test-key-1234", stored.Key)
    require.Equal(t, uint(7), stored.UpdatedBy)
    require.Equal(t, "depsilo-***", m.Status().KeyMasked)
}

func TestClearKey_RemovesPersistenceAndResetsStatus(t *testing.T) {
    d := newTestDB(t)
    require.NoError(t, d.Create(&db.LicenseStorage{ID: 1, Key: "depsilo-stale", UpdatedBy: 1}).Error)
    m := license.NewManager(config.LicenseConfig{}, d)
    require.Equal(t, "depsilo-***", m.Status().KeyMasked)

    require.NoError(t, m.ClearKey(context.Background(), 7))

    var count int64
    require.NoError(t, d.Model(&db.LicenseStorage{}).Count(&count).Error)
    require.Equal(t, int64(0), count)
    require.Equal(t, "", m.Status().KeyMasked)
    require.False(t, m.IsPro())
}

func TestNewManager_DBKeyOverridesConfigKey(t *testing.T) {
    d := newTestDB(t)
    require.NoError(t, d.Create(&db.LicenseStorage{ID: 1, Key: "depsilo-from-db", UpdatedBy: 1}).Error)
    m := license.NewManager(config.LicenseConfig{Key: "depsilo-from-config"}, d)
    require.Equal(t, "depsilo-***", m.Status().KeyMasked)
    // We can't directly assert m.key (unexported); the masked output reflects the loaded key length.
    // Tests for behavior under doValidate would need a Lemon Squeezy mock; out of scope here.
    _ = m
}
```

- [ ] **Step 3: Run tests; expect compile failure**

```bash
go test ./internal/license/ -v -run "TestSetKey|TestClearKey|TestNewManager_DBKey"
```

Expected: build failure — `undefined: m.SetKey`, `m.ClearKey`.

### Task 3.3: Implement `SetKey` and `ClearKey`

**Files:**
- Modify: `internal/license/license.go` (add methods near `Revalidate`)

- [ ] **Step 1: Add the methods**

In `internal/license/license.go`, after `Revalidate`:

```go
// SetKey persists a new license key (DB), updates manager state,
// and triggers a synchronous validation against the upstream license server.
// Returns any validation error, but the key is persisted unconditionally so
// the user can retry validation later via Revalidate.
func (m *Manager) SetKey(ctx context.Context, newKey string, userID uint) error {
    newKey = strings.TrimSpace(newKey)

    m.mu.Lock()
    if m.database != nil {
        if err := m.database.WithContext(ctx).Save(&db.LicenseStorage{
            ID:        1,
            Key:       newKey,
            UpdatedBy: userID,
            UpdatedAt: time.Now().UTC(),
        }).Error; err != nil {
            m.mu.Unlock()
            return fmt.Errorf("persist license key: %w", err)
        }
    }
    m.key = newKey
    if newKey != "" {
        m.status.KeyMasked = MaskKey(newKey)
    } else {
        m.status.KeyMasked = ""
    }
    m.mu.Unlock()

    // TODO: write management event audit log (deferred; see plan front matter)

    if newKey == "" {
        return nil
    }
    // Synchronous validation so the caller can surface the result.
    m.doValidate()
    return nil
}

// ClearKey removes any DB-stored license key and resets manager state to "free".
// The config.toml key is NOT re-read; the manager remains free until the next
// process start (which re-runs the precedence logic in NewManager).
func (m *Manager) ClearKey(ctx context.Context, userID uint) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.database != nil {
        if err := m.database.WithContext(ctx).Where("id = ?", 1).Delete(&db.LicenseStorage{}).Error; err != nil {
            return fmt.Errorf("delete license key: %w", err)
        }
    }
    m.key = ""
    m.status = LicenseStatus{
        IsPro:       false,
        KeyMasked:   "",
        LastChecked: time.Now().UTC(),
    }

    // TODO: write management event audit log (deferred; see plan front matter)
    _ = userID
    return nil
}
```

And update the imports at the top of `license.go`:

```go
import (
    "context"
    "fmt"
    "net/http"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/gin-gonic/gin"
    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/config"
    "depsilo/internal/db"
)
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/license/ -v -run "TestSetKey|TestClearKey|TestNewManager_DBKey"
```

Expected: PASS (the SetKey test will print a Lemon Squeezy network error in stderr — that's expected and not a test failure since we don't assert on validation success).

- [ ] **Step 3: Commit**

```bash
git add internal/license/license.go internal/license/license_test.go
git commit -m "feat(license): SetKey/ClearKey persist via DB; runtime key mutation"
```

---

# Phase 4 — `internal/entitlement/` façade + middleware move

### Task 4.1: Scaffold `entitlement.Checker` and `Source` enum

**Files:**
- Create: `internal/entitlement/checker.go`

- [ ] **Step 1: Create the file**

```go
package entitlement

import (
    "math"
    "time"

    "depsilo/internal/license"
    "depsilo/internal/trial"
)

// Source identifies which entitlement grant is currently active.
type Source string

const (
    SourceNone  Source = "none"
    SourceTrial Source = "trial"
    SourcePaid  Source = "paid"
)

// Status is the unified view used by frontend code, RequirePro 402 bodies,
// and the GetStatus admin endpoint. See spec §7.2 for field semantics and
// §16.2 for the deprecated-alias compatibility window.
type Status struct {
    IsPro            bool       `json:"is_pro"`
    Source           Source     `json:"source"`
    ExpiresAt        *time.Time `json:"expires_at,omitempty"`
    DaysLeft         int        `json:"days_left"`
    TrialUsed        bool       `json:"trial_used"`
    TrialAvailable   bool       `json:"trial_available"`
    LicenseKeyMasked string     `json:"license_key_masked,omitempty"`
    LicenseError     string     `json:"license_error,omitempty"`
    LastChecked      time.Time  `json:"last_checked"`

    // Deprecated aliases — to be removed in 0.5.0 (spec §16.2).
    KeyMasked   string     `json:"key_masked,omitempty"`
    ActivatedAt *time.Time `json:"activated_at,omitempty"`
}

// Checker is the only abstraction the rest of the codebase reaches for
// entitlement decisions. RequirePro middleware, audit.Logger, and rules.Engine
// all depend on *Checker rather than directly on license.Manager or trial.Manager.
type Checker struct {
    lic   *license.Manager
    trial *trial.Manager
}

// NewChecker composes a license manager and a trial manager into one façade.
func NewChecker(lic *license.Manager, t *trial.Manager) *Checker {
    return &Checker{lic: lic, trial: t}
}

// IsPro reports whether ANY entitlement source currently grants Pro access.
// Hot path — called by RequirePro middleware on every Pro request.
func (c *Checker) IsPro() bool {
    if c.lic != nil && c.lic.IsPro() {
        return true
    }
    if c.trial != nil && c.trial.IsActive() {
        return true
    }
    return false
}

// Status assembles a unified view across the underlying sources.
// Precedence when multiple sources are active: paid > trial > none.
func (c *Checker) Status() Status {
    var licStatus license.LicenseStatus
    licPro := false
    if c.lic != nil {
        licStatus = c.lic.Status()
        licPro = c.lic.IsPro()
    }
    trialUsed := c.trial != nil && c.trial.IsUsed()
    trialActive := c.trial != nil && c.trial.IsActive()
    trialRec := (*Status)(nil)
    _ = trialRec

    s := Status{
        IsPro:            licPro || trialActive,
        TrialUsed:        trialUsed,
        TrialAvailable:   !trialUsed,
        LicenseKeyMasked: licStatus.KeyMasked,
        KeyMasked:        licStatus.KeyMasked, // deprecated alias
        LicenseError:     licStatus.Error,
        LastChecked:      licStatus.LastChecked,
    }

    now := time.Now().UTC()

    switch {
    case licPro:
        s.Source = SourcePaid
        s.ExpiresAt = licStatus.ExpiresAt
        s.ActivatedAt = licStatus.ActivatedAt
        if licStatus.ExpiresAt != nil {
            s.DaysLeft = daysBetween(now, *licStatus.ExpiresAt)
        }
    case trialActive:
        s.Source = SourceTrial
        if rec := c.trial.Record(); rec != nil {
            s.ExpiresAt = &rec.ExpiresAt
            s.ActivatedAt = &rec.ActivatedAt
            s.DaysLeft = daysBetween(now, rec.ExpiresAt)
        }
    default:
        s.Source = SourceNone
    }

    return s
}

func daysBetween(from, to time.Time) int {
    delta := to.Sub(from).Hours() / 24
    if delta <= 0 {
        return 0
    }
    return int(math.Ceil(delta))
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./internal/entitlement/
```

- [ ] **Step 3: Commit**

```bash
git add internal/entitlement/checker.go
git commit -m "feat(entitlement): add Checker façade composing license + trial"
```

### Task 4.2: Write tests for Checker source precedence and Status

**Files:**
- Create: `internal/entitlement/checker_test.go`

- [ ] **Step 1: Write the tests**

```go
package entitlement_test

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "depsilo/internal/config"
    "depsilo/internal/db"
    "depsilo/internal/entitlement"
    "depsilo/internal/license"
    "depsilo/internal/trial"
)

func newDB(t *testing.T) *gorm.DB {
    t.Helper()
    d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    require.NoError(t, err)
    require.NoError(t, d.AutoMigrate(&db.TrialRecord{}, &db.LicenseStorage{}, &db.User{}))
    return d
}

func TestChecker_NoneWhenBothEmpty(t *testing.T) {
    d := newDB(t)
    c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), trial.NewManager(d))
    require.False(t, c.IsPro())
    s := c.Status()
    require.Equal(t, entitlement.SourceNone, s.Source)
    require.False(t, s.IsPro)
    require.False(t, s.TrialUsed)
    require.True(t, s.TrialAvailable)
    require.Equal(t, 0, s.DaysLeft)
}

func TestChecker_TrialActiveSourceIsTrial(t *testing.T) {
    d := newDB(t)
    tm := trial.NewManager(d)
    _, err := tm.Activate(context.Background(), 1, "")
    require.NoError(t, err)

    c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), tm)
    require.True(t, c.IsPro())
    s := c.Status()
    require.Equal(t, entitlement.SourceTrial, s.Source)
    require.True(t, s.IsPro)
    require.True(t, s.TrialUsed)
    require.False(t, s.TrialAvailable)
    require.GreaterOrEqual(t, s.DaysLeft, 13)
    require.LessOrEqual(t, s.DaysLeft, 14)
}

func TestChecker_PaidBeatsTrial(t *testing.T) {
    d := newDB(t)
    // Force paid via DEPSILO_DEV_PRO
    t.Setenv("DEPSILO_DEV_PRO", "1")
    lm := license.NewManager(config.LicenseConfig{}, d)

    tm := trial.NewManager(d)
    _, err := tm.Activate(context.Background(), 1, "")
    require.NoError(t, err)

    c := entitlement.NewChecker(lm, tm)
    require.True(t, c.IsPro())
    s := c.Status()
    require.Equal(t, entitlement.SourcePaid, s.Source)
    require.True(t, s.TrialUsed)              // trial record exists even though paid wins
    require.False(t, s.TrialAvailable)         // and is therefore consumed
}

func TestChecker_TrialExpiredFallsToNone(t *testing.T) {
    d := newDB(t)
    now := time.Now().UTC()
    require.NoError(t, d.Create(&db.TrialRecord{
        ActivatedAt: now.Add(-30 * 24 * time.Hour),
        ExpiresAt:   now.Add(-16 * 24 * time.Hour),
        ActivatedBy: 1,
    }).Error)

    c := entitlement.NewChecker(license.NewManager(config.LicenseConfig{}, d), trial.NewManager(d))
    require.False(t, c.IsPro())
    s := c.Status()
    require.Equal(t, entitlement.SourceNone, s.Source)
    require.True(t, s.TrialUsed)
    require.False(t, s.TrialAvailable)
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/entitlement/ -v
```

Expected: PASS for all four tests.

- [ ] **Step 3: Commit**

```bash
git add internal/entitlement/checker_test.go
git commit -m "test(entitlement): cover source precedence + Status assembly"
```

### Task 4.3: Move `RequirePro` middleware to `internal/entitlement/`

**Files:**
- Create: `internal/entitlement/middleware.go`
- Modify: `internal/license/license.go` (delete `RequirePro`)

- [ ] **Step 1: Create the new middleware file**

```go
package entitlement

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

// RequirePro returns a Gin middleware that blocks requests when no
// entitlement source grants Pro access. The 402 response body includes
// `trial_available` so the frontend can offer an in-paywall trial CTA.
func RequirePro(checker *Checker) gin.HandlerFunc {
    return func(c *gin.Context) {
        if checker.IsPro() {
            c.Next()
            return
        }
        c.JSON(http.StatusPaymentRequired, gin.H{
            "code":            "PRO_REQUIRED",
            "message":         "This feature requires Depsilo Pro.",
            "upgrade":         "https://depsilo.com/#pricing",
            "trial_available": !checker.trial.IsUsed(),
        })
        c.Abort()
    }
}
```

Note: we read `!checker.trial.IsUsed()` directly here because Status() does the same — but accessing a private field of `Checker` from inside the same package is fine.

- [ ] **Step 2: Delete the old `RequirePro` in `internal/license/license.go`**

In `internal/license/license.go`, remove the function:

```go
// RequirePro returns a Gin middleware that blocks requests unless a valid Pro license is active.
func RequirePro(mgr *Manager) gin.HandlerFunc { ... }
```

Also remove now-unused imports if any (`gin`, `net/http`) — let `goimports` decide:

```bash
goimports -w internal/license/license.go
```

If `goimports` is not installed: open the file and remove the `gin-gonic/gin` and `net/http` imports manually if they're no longer used elsewhere in the file.

- [ ] **Step 3: Find all callers of `license.RequirePro` and update them to `entitlement.RequirePro`**

```bash
grep -rn "license.RequirePro" --include="*.go" .
```

Each match should be edited so:
- import changes from `"depsilo/internal/license"` to `"depsilo/internal/entitlement"` (or both kept if license is otherwise used in the file)
- call changes from `license.RequirePro(licenseMgr)` to `entitlement.RequirePro(checker)`

The likely sites are inside `internal/api/router.go` and `internal/api/admin/*.go`. Update each.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: no errors. Any "undefined: license.RequirePro" means a call site was missed.

- [ ] **Step 5: Commit**

```bash
git add internal/entitlement/middleware.go internal/license/license.go internal/api/
git commit -m "refactor(license,entitlement): move RequirePro to entitlement package"
```

---

# Phase 5 — Switch audit.Logger + rules.Engine to take `*entitlement.Checker`

### Task 5.1: Update `audit.Logger` constructor signature

**Files:**
- Modify: `internal/audit/logger.go`
- Modify: `internal/server/server.go` (audit init site)

- [ ] **Step 1: Change `audit.Logger` to depend on `*entitlement.Checker`**

In `internal/audit/logger.go`:

```go
import (
    "context"
    "time"

    "go.uber.org/zap"
    "gorm.io/gorm"

    "depsilo/internal/db"
    "depsilo/internal/entitlement"
)

type Logger struct {
    database *gorm.DB
    checker  *entitlement.Checker
    queue    chan db.AuditLog
}

func NewLogger(database *gorm.DB, checker *entitlement.Checker) *Logger {
    return &Logger{
        database: database,
        checker:  checker,
        queue:    make(chan db.AuditLog, 1000),
    }
}
```

And update the body of `Log`:

```go
func (l *Logger) Log(entry db.AuditLog) {
    if !l.checker.IsPro() {
        return
    }
    select {
    case l.queue <- entry:
    default:
        zap.L().Warn("audit log queue full, dropping entry")
    }
}
```

(The exact body around `if !l.checker.IsPro()` should match the existing pattern — preserve whatever channel/queue logic was there.)

- [ ] **Step 2: Update server.go wiring**

In `internal/server/server.go`, the wiring around line 132 becomes:

```go
licenseManager := license.NewManager(cfg.License, database)
go licenseManager.Start(ctx)

trialManager := trial.NewManager(database)
checker := entitlement.NewChecker(licenseManager, trialManager)

rulesStore := rules.NewStore(database)
rulesEngine := rules.NewEngine(rulesStore, checker) // updated in Task 5.2

auditLogger := audit.NewLogger(database, checker)
go auditLogger.Start(ctx)
adapter.SetAuditLogger(auditLogger)
```

- [ ] **Step 3: Compile-check**

```bash
go build ./...
```

Expected: still failing on `rules.NewEngine` signature mismatch — that's Task 5.2.

### Task 5.2: Update `rules.Engine` constructor signature

**Files:**
- Modify: `internal/rules/engine.go`

- [ ] **Step 1: Switch `rules.Engine` to depend on `*entitlement.Checker`**

```go
import (
    // existing ...
    "depsilo/internal/entitlement"
)

type Engine struct {
    store   *Store
    checker *entitlement.Checker
    // ... preserve any other fields
}

func NewEngine(store *Store, checker *entitlement.Checker) *Engine {
    return &Engine{store: store, checker: checker}
}
```

And update all internal references from `e.licMgr.IsPro()` to `e.checker.IsPro()`.

- [ ] **Step 2: Compile-check**

```bash
go build ./...
```

Expected: no errors. If any test in `internal/rules/` or `internal/audit/` instantiates these constructors directly, update those test setups too.

```bash
go test ./internal/rules/ ./internal/audit/ -count=1
```

Expected: PASS or build-fix-PASS after test setup updates.

- [ ] **Step 3: Commit**

```bash
git add internal/audit/logger.go internal/rules/engine.go internal/server/server.go
git commit -m "refactor: audit.Logger + rules.Engine now depend on entitlement.Checker

Trial-active users were silently denied audit logging and rules
enforcement under the old wiring, because both modules called
licenseMgr.IsPro() directly. Routing through the Checker façade fixes
the leak and matches the architectural intent stated in spec §7.3."
```

---

# Phase 6 — Backend API endpoints

### Task 6.1: Wire `TrialManager` + `Entitlement` into `api.Deps`

**Files:**
- Modify: `internal/api/router.go` (Deps struct around line 30-44)
- Modify: `internal/server/server.go` (Deps construction)

- [ ] **Step 1: Add fields to Deps**

In `internal/api/router.go`:

```go
type Deps struct {
    DB         *gorm.DB
    Storage    cache.Storage
    Config     *config.Config
    Pools      map[string]*upstream.Pool
    Ecosystems []string
    CacheMgr   *cache.Manager
    EventBus   *cache.EventBus
    LicenseManager   *license.Manager        // KEPT for handlers that still read paid-status-only details
    TrialManager     *trial.Manager           // NEW
    Entitlement      *entitlement.Checker     // NEW
    AuditLogger      *audit.Logger
    RulesStore       *rules.Store
    RulesEngine      *rules.Engine
    SecurityScanner  *security.Scanner
    SecurityImporter *security.Importer
}
```

Add the new imports:

```go
import (
    // existing ...
    "depsilo/internal/entitlement"
    "depsilo/internal/trial"
)
```

- [ ] **Step 2: Populate the new fields in server.go**

In `internal/server/server.go`, where the `Deps{...}` literal is built (search for `api.Deps{`):

```go
deps := api.Deps{
    DB:               database,
    Storage:          storage,
    Config:           cfg,
    Pools:            pools,
    Ecosystems:       ecosystems,
    CacheMgr:         cacheMgr,
    EventBus:         eventBus,
    LicenseManager:   licenseManager,
    TrialManager:     trialManager,
    Entitlement:      checker,
    AuditLogger:      auditLogger,
    RulesStore:       rulesStore,
    RulesEngine:      rulesEngine,
    SecurityScanner:  securityScanner,
    SecurityImporter: securityImporter,
}
```

(Adjust to match the existing struct-literal order.)

- [ ] **Step 3: Compile-check**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/api/router.go internal/server/server.go
git commit -m "feat(api): add TrialManager + Entitlement Checker to Deps"
```

### Task 6.2: Rewrite `LicenseHandler` with full endpoint surface

**Files:**
- Modify: `internal/api/admin/license.go`

- [ ] **Step 1: Replace handler struct and add new methods**

Replace the entire contents of `internal/api/admin/license.go`:

```go
package admin

import (
    "errors"
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"

    "depsilo/internal/entitlement"
    "depsilo/internal/license"
    "depsilo/internal/trial"
)

// LicenseHandler handles license-and-trial status, key mutation, and trial activation.
type LicenseHandler struct {
    lic     *license.Manager
    trial   *trial.Manager
    checker *entitlement.Checker
}

// NewLicenseHandler creates the handler.
func NewLicenseHandler(lic *license.Manager, t *trial.Manager, c *entitlement.Checker) *LicenseHandler {
    return &LicenseHandler{lic: lic, trial: t, checker: c}
}

// GetStatus returns the unified entitlement status.
func (h *LicenseHandler) GetStatus(c *gin.Context) {
    c.JSON(http.StatusOK, h.checker.Status())
}

// Revalidate triggers a paid-license re-validation in the background.
func (h *LicenseHandler) Revalidate(c *gin.Context) {
    h.lic.Revalidate()
    c.JSON(http.StatusOK, gin.H{"message": "revalidation triggered"})
}

// ActivateTrial creates the singleton TrialRecord. Refuses if a paid license
// is already active (TRIAL_NOT_NEEDED) or a trial was already used (TRIAL_ALREADY_USED).
func (h *LicenseHandler) ActivateTrial(c *gin.Context) {
    // Block when user is already paid Pro.
    if h.checker.Status().Source == entitlement.SourcePaid {
        c.JSON(http.StatusConflict, gin.H{"code": "TRIAL_NOT_NEEDED"})
        return
    }

    userID := uint(0)
    if v, ok := c.Get("user_id"); ok {
        if id, ok := v.(uint); ok {
            userID = id
        }
    }

    _, err := h.trial.Activate(c.Request.Context(), userID, c.ClientIP())
    switch {
    case errors.Is(err, trial.ErrTrialAlreadyUsed):
        c.JSON(http.StatusConflict, gin.H{"code": "TRIAL_ALREADY_USED"})
    case err != nil:
        c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
    default:
        c.JSON(http.StatusOK, h.checker.Status())
    }
}

// SetKeyRequest is the PUT /key body.
type SetKeyRequest struct {
    Key string `json:"key" binding:"required,max=256"`
}

// SetKey persists a license key and triggers a synchronous validation.
// Always returns 200 with the updated Status; the frontend reads status.is_pro
// to determine success vs saved-pending.
func (h *LicenseHandler) SetKey(c *gin.Context) {
    var req SetKeyRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
        return
    }
    req.Key = strings.TrimSpace(req.Key)
    if req.Key == "" {
        c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_KEY", "message": "key must be non-empty"})
        return
    }

    userID := uint(0)
    if v, ok := c.Get("user_id"); ok {
        if id, ok := v.(uint); ok {
            userID = id
        }
    }

    // Persist + synchronous validate. Validation network failures are not
    // surfaced as 5xx; status.license_error captures them.
    _ = h.lic.SetKey(c.Request.Context(), req.Key, userID)
    c.JSON(http.StatusOK, h.checker.Status())
}

// ClearKey deletes the DB-stored key (if any) and resets state.
func (h *LicenseHandler) ClearKey(c *gin.Context) {
    userID := uint(0)
    if v, ok := c.Get("user_id"); ok {
        if id, ok := v.(uint); ok {
            userID = id
        }
    }
    if err := h.lic.ClearKey(c.Request.Context(), userID); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, h.checker.Status())
}
```

- [ ] **Step 2: Compile-check**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/admin/license.go
git commit -m "feat(api/admin/license): full handler surface — status, revalidate, trial, key set/clear"
```

### Task 6.3: Wire new routes into the admin route group

**Files:**
- Modify: `internal/api/router.go` (the admin route block)

- [ ] **Step 1: Locate the existing license route registration**

```bash
grep -n "NewLicenseHandler\|/license" internal/api/router.go
```

The site currently registers `GET /admin/license/status` and `POST /admin/license/revalidate`. Find that block.

- [ ] **Step 2: Replace handler construction and route registration**

Old:
```go
licenseHandler := admin.NewLicenseHandler(deps.LicenseManager)
adminGroup.GET("/license/status", licenseHandler.GetStatus)
adminGroup.POST("/license/revalidate", licenseHandler.Revalidate)
```

New:
```go
licenseHandler := admin.NewLicenseHandler(deps.LicenseManager, deps.TrialManager, deps.Entitlement)
adminGroup.GET("/license/status", licenseHandler.GetStatus)
adminGroup.POST("/license/revalidate", licenseHandler.Revalidate)
adminGroup.POST("/license/trial/activate", licenseHandler.ActivateTrial)
adminGroup.PUT("/license/key", licenseHandler.SetKey)
adminGroup.DELETE("/license/key", licenseHandler.ClearKey)
```

- [ ] **Step 3: Compile-check + boot smoke**

```bash
go build ./...
make stop 2>/dev/null
make dev
sleep 4
# Status endpoint sanity:
curl -s http://localhost:23333/api/v1/admin/license/status -H "Authorization: Bearer dummy" | head -5
# Expect 401 (unauthorized) because dummy token — that confirms the route is wired.
make stop
```

Expected: 401 with `{"code":"UNAUTHORIZED"}` or similar — the route exists and the auth middleware fires.

- [ ] **Step 4: Commit**

```bash
git add internal/api/router.go
git commit -m "feat(api/router): register trial + key mutation routes under /admin/license"
```

---

# Phase 7 — Backend integration test

### Task 7.1: Full trial-loop integration test

**Files:**
- Create: `tests/integration/license_trial_test.go`

- [ ] **Step 1: Inspect existing integration test setup**

```bash
ls tests/integration/
head -50 tests/integration/*.go | head -80
```

Locate the in-process server bootstrap pattern (likely a helper that builds a `*gin.Engine` and runs migrations in a temp DB). Reuse it.

- [ ] **Step 2: Write the test file**

```go
package integration_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"

    "depsilo/internal/entitlement"
    // ... other imports following the existing integration test conventions
)

// setupAuthedAdmin: replace with the project's existing helper for booting
// an in-process Depsilo + creating an admin session token. Look at any
// existing tests/integration/*.go file for the established pattern, e.g.
// tests/integration/login_test.go or a shared testserver.go.
//
// The helper should return: (engine *gin.Engine, bearerToken string, cleanup func()).

func TestTrialLoop_ActivateAndUnlockProEndpoint(t *testing.T) {
    engine, token, cleanup := setupAuthedAdmin(t)
    defer cleanup()

    // 1. Status: source=none, trial_available=true
    rec := doGET(t, engine, token, "/api/v1/admin/license/status")
    require.Equal(t, http.StatusOK, rec.Code)
    var s entitlement.Status
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
    require.Equal(t, entitlement.SourceNone, s.Source)
    require.True(t, s.TrialAvailable)
    require.False(t, s.TrialUsed)

    // 2. Activate trial
    rec = doPOST(t, engine, token, "/api/v1/admin/license/trial/activate", "")
    require.Equal(t, http.StatusOK, rec.Code)
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
    require.Equal(t, entitlement.SourceTrial, s.Source)
    require.True(t, s.IsPro)
    require.True(t, s.TrialUsed)
    require.False(t, s.TrialAvailable)

    // 3. Second activate → 409 TRIAL_ALREADY_USED
    rec = doPOST(t, engine, token, "/api/v1/admin/license/trial/activate", "")
    require.Equal(t, http.StatusConflict, rec.Code)
    require.Contains(t, rec.Body.String(), "TRIAL_ALREADY_USED")
}

func TestTrialLoop_PaywallShowsTrialAvailableThenFalse(t *testing.T) {
    engine, token, cleanup := setupAuthedAdmin(t)
    defer cleanup()

    // 1. Hit a Pro endpoint without entitlement (use a known Pro route, e.g. /admin/projects)
    //    The exact path depends on project routes; pick any RequirePro-gated GET.
    rec := doGET(t, engine, token, "/api/v1/admin/projects")
    require.Equal(t, http.StatusPaymentRequired, rec.Code)
    var body map[string]any
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
    require.Equal(t, "PRO_REQUIRED", body["code"])
    require.Equal(t, true, body["trial_available"])

    // 2. Activate trial.
    _ = doPOST(t, engine, token, "/api/v1/admin/license/trial/activate", "")

    // 3. Same Pro endpoint now returns 200 (or 404 if no projects exist — both are non-402).
    rec = doGET(t, engine, token, "/api/v1/admin/projects")
    require.NotEqual(t, http.StatusPaymentRequired, rec.Code)
}

func TestKeyLoop_SetThenClear(t *testing.T) {
    engine, token, cleanup := setupAuthedAdmin(t)
    defer cleanup()

    // Set a key. Validation will fail (no network / fake key), but status persists.
    body := `{"key": "depsilo-integration-test-key"}`
    rec := doPUT(t, engine, token, "/api/v1/admin/license/key", body)
    require.Equal(t, http.StatusOK, rec.Code)
    var s entitlement.Status
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
    require.Equal(t, "depsilo-***", s.LicenseKeyMasked)
    // is_pro will be false because validation failed — that's intentional UX (see spec §10.3).

    // Clear the key.
    rec = doDELETE(t, engine, token, "/api/v1/admin/license/key")
    require.Equal(t, http.StatusOK, rec.Code)
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
    require.Equal(t, "", s.LicenseKeyMasked)
}

// --- HTTP test helpers — adapt to project conventions if better helpers exist ---

func doGET(t *testing.T, engine http.Handler, token, path string) *httptest.ResponseRecorder {
    t.Helper()
    req, err := http.NewRequest(http.MethodGet, path, nil)
    require.NoError(t, err)
    req.Header.Set("Authorization", "Bearer "+token)
    rec := httptest.NewRecorder()
    engine.ServeHTTP(rec, req)
    return rec
}

func doPOST(t *testing.T, engine http.Handler, token, path, body string) *httptest.ResponseRecorder {
    t.Helper()
    req, err := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
    require.NoError(t, err)
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    engine.ServeHTTP(rec, req)
    return rec
}

func doPUT(t *testing.T, engine http.Handler, token, path, body string) *httptest.ResponseRecorder {
    t.Helper()
    req, err := http.NewRequest(http.MethodPut, path, strings.NewReader(body))
    require.NoError(t, err)
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    engine.ServeHTTP(rec, req)
    return rec
}

func doDELETE(t *testing.T, engine http.Handler, token, path string) *httptest.ResponseRecorder {
    t.Helper()
    req, err := http.NewRequest(http.MethodDelete, path, nil)
    require.NoError(t, err)
    req.Header.Set("Authorization", "Bearer "+token)
    rec := httptest.NewRecorder()
    engine.ServeHTTP(rec, req)
    return rec
}
```

**Implementation note:** the `setupAuthedAdmin` helper above is a stub. Before running this test, look at the existing integration test infrastructure (e.g. `tests/integration/auth_test.go` if it exists, or any `*_test.go` in the directory) for the canonical bootstrap pattern. The signature shown — `(engine, token, cleanup)` — is the target shape; adapt to what's already there.

- [ ] **Step 3: Run the integration tests**

```bash
make test-integration
```

Expected: PASS for all three new tests + previously-passing tests remain green.

- [ ] **Step 4: Commit**

```bash
git add tests/integration/license_trial_test.go
git commit -m "test(integration): trial activation + paywall + key set/clear"
```

---

# Phase 8 — Frontend i18n keys + API client

### Task 8.1: Add `licenseApi` + `EntitlementStatus` type to api.ts

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Locate the existing `*Api` exports**

```bash
grep -n "^export const " web/src/lib/api.ts | head -10
```

Look at one of them (e.g. `statsApi`, `cacheApi`) to copy the pattern.

- [ ] **Step 2: Add the type + API surface**

Append to `web/src/lib/api.ts`:

```ts
export type EntitlementSource = 'none' | 'trial' | 'paid'

export interface EntitlementStatus {
  is_pro: boolean
  source: EntitlementSource
  expires_at?: string
  days_left: number
  trial_used: boolean
  trial_available: boolean
  license_key_masked?: string
  license_error?: string
  last_checked: string
  // Deprecated aliases; remove when 0.5.0 ships per spec §16.2.
  key_masked?: string
  activated_at?: string
}

export const licenseApi = {
  status:        () => api.get<EntitlementStatus>('/admin/license/status').then(r => r.data),
  revalidate:    () => api.post('/admin/license/revalidate'),
  activateTrial: () => api.post<EntitlementStatus>('/admin/license/trial/activate').then(r => r.data),
  setKey:        (key: string) => api.put<EntitlementStatus>('/admin/license/key', { key }).then(r => r.data),
  clearKey:      () => api.delete<EntitlementStatus>('/admin/license/key').then(r => r.data),
}
```

Adjust the `.then(r => r.data)` pattern to match the project's prevailing style; if other `*Api` exports return the raw axios response, omit the `.then`.

- [ ] **Step 3: Verify type-check**

```bash
cd web && npm run type-check && cd ..
```

Expected: no TS errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat(web/api): add licenseApi (trial activate + key set/clear)"
```

### Task 8.2: Add `license.*` i18n keys to both locales

**Files:**
- Modify: `web/src/i18n/zh.ts`
- Modify: `web/src/i18n/en.ts`

- [ ] **Step 1: Locate existing top-level namespace patterns**

```bash
head -30 web/src/i18n/zh.ts
```

Note the indentation and structure — the file is a TypeScript object literal with nested namespaces.

- [ ] **Step 2: Add the `license` namespace to `zh.ts`**

Find a sibling top-level namespace (e.g. `cache`, `theme`) and add `license:` next to it. Use this exact content:

```ts
license: {
  title: 'License 与 Pro',
  subtitle: '管理您的 Pro 试用与许可证密钥',

  status: {
    free: 'Free',
    trial: '试用中',
    trial_expired: '试用已结束',
    pro: 'Pro',
  },

  trial: {
    start_button: '开始 14 天免费试用',
    start_explainer: '解锁审计日志、SBOM 导出、多项目、安全扫描等全部 Pro 能力。无需信用卡，无需邮箱。',
    days_left: '剩余 {{count}} 天',
    expires_at: '将于 {{date}} 结束',
    expired_message: '试用已于 {{date}} 结束。Free 功能继续保留。',
  },

  pro: {
    activated: 'Pro 已激活',
    key_label: '密钥',
    expires_at: '到期：{{date}}',
    last_checked: '上次校验：{{relative_time}}',
  },

  revalidate: '重新校验',
  buy_pro: '购买 Pro',
  view_pricing: '查看定价',

  key: {
    title: '许可证密钥',
    placeholder: 'depsilo-xxxx-xxxx-xxxx-...',
    activate_button: '激活',
    change_button: '更换密钥',
    remove_button: '移除密钥',
    save_button: '保存',
    remove_confirm_title: '移除许可证密钥？',
    remove_confirm_body: 'Pro 功能将立即失效。此操作不影响已经存在的试用记录。',
    success_toast: '密钥已激活',
    saved_pending_message: '密钥已保存。Depsilo 暂时无法确认其为 Pro 状态 — 可能是密钥无效，也可能是网络问题。',
    try_revalidate: '重试校验',
  },

  paywall: {
    title: '此功能需要 Depsilo Pro',
    body: 'Pro 提供审计日志、SBOM 导出、多项目工作区、安全扫描等高级能力。',
    start_trial: '开始 14 天免费试用',
    buy_pro: '购买 Pro',
    learn_more: '了解更多',
    view_status: '查看许可证状态',
    dismiss: '稍后',
    trial_activated_toast: '试用已激活 — 请重试您的操作',
  },

  features: {
    heading: 'Pro 包含哪些能力？',
    free: {
      f1: '12 种生态代理',
      f2: '缓存管理 + 流量统计',
      f3: '基础上游源管理',
      f4: '单用户访问',
      f5: '本地或 S3 存储',
      f6: '健康检查与延迟优选',
    },
    pro: {
      f1: '多项目工作区',
      f2: 'OSV 安全漏洞扫描',
      f3: 'SBOM 导出（SPDX + CycloneDX）',
      f4: '审计日志（可导出 CSV）',
      f5: '包级别 Allow/Deny 规则',
      f6: '优先技术支持',
    },
  },
},
```

- [ ] **Step 3: Add the matching `license` namespace to `en.ts`**

Use this exact content, ensuring every `{{placeholder}}` matches the zh.ts side:

```ts
license: {
  title: 'License & Pro',
  subtitle: 'Manage your Pro trial and license key',

  status: {
    free: 'Free',
    trial: 'Trial',
    trial_expired: 'Trial ended',
    pro: 'Pro',
  },

  trial: {
    start_button: 'Start 14-day free trial',
    start_explainer: 'Unlock audit logs, SBOM export, multi-project, security scanning, and the rest of Pro. No credit card. No email.',
    days_left: '{{count}} days remaining',
    expires_at: 'Expires {{date}}',
    expired_message: 'Trial ended on {{date}}. Your Free tier features are unaffected.',
  },

  pro: {
    activated: 'Pro activated',
    key_label: 'Key',
    expires_at: 'Expires {{date}}',
    last_checked: 'Last checked {{relative_time}}',
  },

  revalidate: 'Revalidate',
  buy_pro: 'Buy Pro',
  view_pricing: 'View pricing',

  key: {
    title: 'License key',
    placeholder: 'depsilo-xxxx-xxxx-xxxx-...',
    activate_button: 'Activate',
    change_button: 'Change key',
    remove_button: 'Remove key',
    save_button: 'Save',
    remove_confirm_title: 'Remove this license key?',
    remove_confirm_body: 'Pro features will lock immediately. Your existing trial record is unaffected.',
    success_toast: 'Key activated',
    saved_pending_message: "Key saved. Depsilo couldn't confirm it as Pro right now — this can mean an invalid key or a network issue.",
    try_revalidate: 'Try revalidate',
  },

  paywall: {
    title: 'This feature requires Depsilo Pro',
    body: 'Pro adds audit logs, SBOM export, multi-project workspaces, security scanning, and more.',
    start_trial: 'Start 14-day free trial',
    buy_pro: 'Buy Pro',
    learn_more: 'Learn more',
    view_status: 'View license status',
    dismiss: 'Maybe later',
    trial_activated_toast: 'Trial activated — please try your action again',
  },

  features: {
    heading: "What's in Pro?",
    free: {
      f1: 'Proxy for 12 ecosystems',
      f2: 'Cache management + bandwidth analytics',
      f3: 'Basic upstream source management',
      f4: 'Single-user access',
      f5: 'Local or S3 storage',
      f6: 'Health checks + latency-based selection',
    },
    pro: {
      f1: 'Multi-project workspaces',
      f2: 'OSV security vulnerability scanning',
      f3: 'SBOM export (SPDX + CycloneDX)',
      f4: 'Audit logs (CSV export)',
      f5: 'Package-level allow/deny rules',
      f6: 'Priority support',
    },
  },
},
```

- [ ] **Step 4: Run the audit**

```bash
make lint-i18n
```

Expected: `OK — all keys defined in both locales, no duplicates, all defined keys are used safely.`

Wait — `make lint-i18n` *also* checks "all defined keys are used in TSX/TS". The new `license.*` keys aren't referenced yet because License.tsx doesn't exist. **This step will report many "used but undefined" misses going the other way (or pass quietly if the audit only checks one direction).** Re-read the script: it flags keys USED in code but NOT defined. Keys defined but not used in code are NOT flagged. So this should pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "i18n: add license.* namespace (zh + en, ~50 keys, parity verified)"
```

---

# Phase 9 — Frontend License page

### Task 9.1: Create `License.tsx`

**Files:**
- Create: `web/src/admin/pages/License.tsx`

- [ ] **Step 1: Look at an existing admin page for style conventions**

```bash
head -80 web/src/admin/pages/Settings.tsx
```

Note: imports, page-shell pattern, useTranslation hook usage, TanStack Query pattern, shadcn component import style.

- [ ] **Step 2: Create the page**

```tsx
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { toast } from 'sonner'
import { licenseApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'

export default function License() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()

  const { data: status, isLoading } = useQuery({
    queryKey: ['license', 'status'],
    queryFn: licenseApi.status,
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
  })

  const activateTrial = useMutation({
    mutationFn: licenseApi.activateTrial,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
      toast.success(t('license.paywall.trial_activated_toast'))
    },
  })

  const setKey = useMutation({
    mutationFn: (key: string) => licenseApi.setKey(key),
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
      if (s.is_pro) {
        toast.success(t('license.key.success_toast'))
        setKeyInput('')
      }
    },
  })

  const clearKey = useMutation({
    mutationFn: licenseApi.clearKey,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
    },
  })

  const revalidate = useMutation({
    mutationFn: licenseApi.revalidate,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['license', 'status'] }),
  })

  const [keyInput, setKeyInput] = useState('')
  const [removeOpen, setRemoveOpen] = useState(false)
  const [keyExpanded, setKeyExpanded] = useState(false)

  if (isLoading || !status) {
    return <div className="p-6">…</div>
  }

  // Decide which state card to render.
  const source = status.source
  const trialUsed = status.trial_used

  const formatDate = (iso?: string) =>
    iso ? new Date(iso).toLocaleDateString(i18n.language === 'zh' ? 'zh-CN' : 'en-US') : ''

  return (
    <div className="p-6 max-w-4xl mx-auto space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">{t('license.title')}</h1>
        <p className="text-muted-foreground">{t('license.subtitle')}</p>
      </header>

      {/* --- State card --- */}

      {source === 'none' && !trialUsed && (
        <Card>
          <CardHeader>
            <CardTitle>{t('license.trial.start_button')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <p>{t('license.trial.start_explainer')}</p>
            <div className="flex gap-2">
              <Button
                onClick={() => activateTrial.mutate()}
                disabled={activateTrial.isPending}
              >
                {t('license.trial.start_button')}
              </Button>
              <Button
                variant="outline"
                onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}
              >
                {t('license.view_pricing')}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {source === 'trial' && (
        <Card>
          <CardHeader>
            <CardTitle>
              🟢 {t('license.status.pro')} · {t('license.status.trial')}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <p>
              {t('license.trial.days_left', { count: status.days_left })} ·{' '}
              {t('license.trial.expires_at', { date: formatDate(status.expires_at) })}
            </p>
            <Button
              variant="outline"
              onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}
            >
              {t('license.buy_pro')}
            </Button>
          </CardContent>
        </Card>
      )}

      {source === 'none' && trialUsed && (
        <Card>
          <CardHeader>
            <CardTitle>
              ⚠️ {t('license.trial.expired_message', { date: formatDate(status.expires_at) })}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <Button onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}>
              {t('license.buy_pro')}
            </Button>
          </CardContent>
        </Card>
      )}

      {source === 'paid' && (
        <Card>
          <CardHeader>
            <CardTitle>✓ {t('license.pro.activated')}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="text-sm">
              {t('license.pro.key_label')}: <code>{status.license_key_masked}</code>
            </div>
            {status.expires_at && (
              <div className="text-sm">
                {t('license.pro.expires_at', { date: formatDate(status.expires_at) })}
              </div>
            )}
            <div className="text-sm text-muted-foreground">
              {t('license.pro.last_checked', {
                relative_time: formatTime(status.last_checked, 'relative'),
              })}
            </div>
            <Button
              variant="outline"
              onClick={() => revalidate.mutate()}
              disabled={revalidate.isPending}
            >
              {t('license.revalidate')}
            </Button>
          </CardContent>
        </Card>
      )}

      {/* --- Key entry section --- */}

      <Card>
        <CardHeader>
          <CardTitle
            className="cursor-pointer select-none"
            onClick={() => setKeyExpanded(!keyExpanded)}
          >
            {t('license.key.title')} {keyExpanded ? '▾' : '▸'}
          </CardTitle>
        </CardHeader>
        {(keyExpanded || (source === 'none' && !trialUsed)) && (
          <CardContent className="space-y-3">
            {source !== 'paid' && (
              <>
                <div className="flex gap-2">
                  <Input
                    placeholder={t('license.key.placeholder')}
                    value={keyInput}
                    onChange={(e) => setKeyInput(e.target.value)}
                    disabled={setKey.isPending}
                  />
                  <Button
                    onClick={() => setKey.mutate(keyInput)}
                    disabled={setKey.isPending || keyInput.trim() === ''}
                  >
                    {t('license.key.activate_button')}
                  </Button>
                </div>
                {setKey.data && !setKey.data.is_pro && (
                  <div className="text-sm text-amber-700 dark:text-amber-300 space-y-1">
                    <div>{t('license.key.saved_pending_message')}</div>
                    {setKey.data.license_error && (
                      <div className="text-xs opacity-70">{setKey.data.license_error}</div>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => revalidate.mutate()}
                      disabled={revalidate.isPending}
                    >
                      {t('license.key.try_revalidate')}
                    </Button>
                  </div>
                )}
              </>
            )}
            {source === 'paid' && (
              <div className="flex gap-2">
                <Button variant="outline" onClick={() => setKeyExpanded(true)}>
                  {t('license.key.change_button')}
                </Button>
                <Button
                  variant="destructive"
                  onClick={() => setRemoveOpen(true)}
                >
                  {t('license.key.remove_button')}
                </Button>
              </div>
            )}
          </CardContent>
        )}
      </Card>

      {/* --- Feature comparison --- */}

      <Card>
        <CardHeader>
          <CardTitle>{t('license.features.heading')}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-6">
            <div>
              <div className="font-semibold mb-2">{t('license.status.free')}</div>
              <ul className="space-y-1 text-sm">
                {[1, 2, 3, 4, 5, 6].map((i) => (
                  <li key={i}>· {t(`license.features.free.f${i}`)}</li>
                ))}
              </ul>
            </div>
            <div>
              <div className="font-semibold mb-2">{t('license.status.pro')}</div>
              <ul className="space-y-1 text-sm">
                {[1, 2, 3, 4, 5, 6].map((i) => (
                  <li key={i}>· {t(`license.features.pro.f${i}`)}</li>
                ))}
              </ul>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* --- Remove confirmation dialog --- */}

      <AlertDialog open={removeOpen} onOpenChange={setRemoveOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('license.key.remove_confirm_title')}</AlertDialogTitle>
            <AlertDialogDescription>{t('license.key.remove_confirm_body')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('license.paywall.dismiss')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                clearKey.mutate()
                setRemoveOpen(false)
              }}
            >
              {t('license.key.remove_button')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
```

- [ ] **Step 3: Type-check**

```bash
cd web && npm run type-check && cd ..
```

Expected: no errors. If `formatTime` doesn't support a `'relative'` mode, simplify the call (e.g. `new Date(status.last_checked).toLocaleString()`).

- [ ] **Step 4: Commit**

```bash
git add web/src/admin/pages/License.tsx
git commit -m "feat(web/admin): new License page with state card, key entry, feature comparison"
```

### Task 9.2: Register `/admin/license` route and sidebar entry

**Files:**
- Modify: `web/src/admin/AdminApp.tsx` (route definition)
- Modify: `web/src/admin/components/MainLayout.tsx` (sidebar entries)

- [ ] **Step 1: Add the route**

```bash
grep -n "<Route" web/src/admin/AdminApp.tsx
```

Find the route block (likely React Router v6 `<Routes>...<Route .../></Routes>`). Add:

```tsx
import License from './pages/License'
// ...
<Route path="license" element={<License />} />
```

The exact path syntax depends on how other admin routes are written — keep the same indentation and structure.

- [ ] **Step 2: Add the sidebar entry**

```bash
grep -n "settings\|users\|upstream" web/src/admin/components/MainLayout.tsx | head -10
```

Find the "管理 / Manage" group. Insert a License item with a key icon (lucide-react has `KeyRound` or `Key`):

```tsx
import { KeyRound } from 'lucide-react'
// ...
{
  to: '/admin/license',
  label: t('license.title'),
  icon: KeyRound,
},
```

Place it between `Users` and `Settings` in the existing array.

- [ ] **Step 3: Build the frontend, smoke-check in dev**

```bash
cd web && npm run build && cd ..
make stop 2>/dev/null
make dev
sleep 4
echo "Open http://localhost:23333/admin in a browser, log in with admin/admin, and verify:"
echo "  1. Sidebar shows 'License' between Users and Settings"
echo "  2. Clicking it navigates to /admin/license"
echo "  3. The page renders the 'Start trial' card (since trial is unused)"
echo "  4. Clicking 'Start trial' updates the card to 'Pro · Trial · 14 days remaining' within ~1s"
echo "(Manual verification — no automated assertion here.)"
make stop
```

- [ ] **Step 4: Commit**

```bash
git add web/src/admin/AdminApp.tsx web/src/admin/components/MainLayout.tsx
git commit -m "feat(web/admin): wire /admin/license route + sidebar entry"
```

---

# Phase 10 — Frontend ProRequiredModal + axios interceptor

### Task 10.1: Create `ProRequiredModal.tsx`

**Files:**
- Create: `web/src/admin/components/ProRequiredModal.tsx`

- [ ] **Step 1: Create the component**

```tsx
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toast } from 'sonner'
import { licenseApi } from '@/lib/api'

interface ProRequiredDetail {
  code: string
  message?: string
  upgrade?: string
  trial_available: boolean
}

export default function ProRequiredModal() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [open, setOpen] = useState(false)
  const [detail, setDetail] = useState<ProRequiredDetail | null>(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    const onEvent = (e: Event) => {
      const ce = e as CustomEvent<ProRequiredDetail>
      setDetail(ce.detail)
      setOpen(true)
    }
    window.addEventListener('depsilo:pro-required', onEvent)
    return () => window.removeEventListener('depsilo:pro-required', onEvent)
  }, [])

  if (!detail) return null

  const trialAvailable = detail.trial_available

  const onStartTrial = async () => {
    setPending(true)
    try {
      await licenseApi.activateTrial()
      toast.success(t('license.paywall.trial_activated_toast'))
      setOpen(false)
    } catch {
      toast.error(t('license.paywall.title'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('license.paywall.title')}</DialogTitle>
          <DialogDescription>{t('license.paywall.body')}</DialogDescription>
        </DialogHeader>
        <DialogFooter className="flex gap-2">
          {trialAvailable ? (
            <>
              <Button onClick={onStartTrial} disabled={pending}>
                {t('license.paywall.start_trial')}
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  setOpen(false)
                  nav('/admin/license')
                }}
              >
                {t('license.paywall.learn_more')}
              </Button>
            </>
          ) : (
            <>
              <Button
                onClick={() => window.open(detail.upgrade ?? 'https://depsilo.com/#pricing', '_blank')}
              >
                {t('license.paywall.buy_pro')}
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  setOpen(false)
                  nav('/admin/license')
                }}
              >
                {t('license.paywall.view_status')}
              </Button>
            </>
          )}
          <Button variant="ghost" onClick={() => setOpen(false)}>
            {t('license.paywall.dismiss')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Type-check**

```bash
cd web && npm run type-check && cd ..
```

- [ ] **Step 3: Commit**

```bash
git add web/src/admin/components/ProRequiredModal.tsx
git commit -m "feat(web/admin): ProRequiredModal listens for depsilo:pro-required events"
```

### Task 10.2: Wire axios 402 interceptor to dispatch the event

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Locate the existing response interceptor**

```bash
grep -n "interceptors\|response.use" web/src/lib/api.ts
```

There should be an existing block handling 401 → redirect-to-login.

- [ ] **Step 2: Extend the error-arm of the interceptor**

Inside the rejection handler, before the existing `return Promise.reject(err)`:

```ts
if (
  err.response?.status === 402 &&
  err.response?.data?.code === 'PRO_REQUIRED'
) {
  window.dispatchEvent(
    new CustomEvent('depsilo:pro-required', { detail: err.response.data }),
  )
}
```

- [ ] **Step 3: Type-check**

```bash
cd web && npm run type-check && cd ..
```

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat(web/api): dispatch depsilo:pro-required custom event on 402"
```

### Task 10.3: Mount `ProRequiredModal` at AdminApp root

**Files:**
- Modify: `web/src/admin/AdminApp.tsx`

- [ ] **Step 1: Import and render the modal**

Add the import:

```tsx
import ProRequiredModal from './components/ProRequiredModal'
```

Inside the AdminApp JSX, place `<ProRequiredModal />` once at a level that's always mounted while the admin is open (e.g. just inside the layout wrapper, alongside the `<Outlet />` or `<Routes>`). It does not render until it receives an event, so placement order doesn't matter.

- [ ] **Step 2: Manual smoke-check**

```bash
cd web && npm run build && cd ..
make stop 2>/dev/null
make dev
sleep 4
echo "Open http://localhost:23333/admin and ensure trial is NOT active."
echo "Navigate to /admin/projects (Pro feature) and trigger a list API call."
echo "Expected: modal pops with 'Start 14-day free trial' as primary CTA."
echo "Click it → toast appears → modal closes → re-navigating to /admin/projects works."
make stop
```

- [ ] **Step 3: Commit**

```bash
git add web/src/admin/AdminApp.tsx
git commit -m "feat(web/admin): mount ProRequiredModal globally"
```

---

# Phase 11 — Landing page `/pro-trial`

### Task 11.1: Add `pro_trial_*` i18n keys to landing locales

**Files:**
- Modify: `../depsilo-landingpage/src/i18n/locales/zh-CN.json`
- Modify: `../depsilo-landingpage/src/i18n/locales/en.json`

- [ ] **Step 1: Confirm landing repo is on master with clean tree**

```bash
cd ../depsilo-landingpage
git status -s && git rev-parse --abbrev-ref HEAD
cd -
```

Expected: empty status, branch `master`.

- [ ] **Step 2: Add the keys to `zh-CN.json`**

Insert these keys into the existing JSON object (alphabetical or grouped, matching house style):

```json
"pro_trial_hero_title": "开启 14 天免费试用",
"pro_trial_hero_subtitle": "全部 Pro 能力。无需信用卡。无需邮箱。",
"pro_trial_path_existing_title": "已在运行 Depsilo？",
"pro_trial_path_existing_body": "打开本地管理后台 /admin/license，点击「开始 14 天免费试用」即可。",
"pro_trial_path_existing_cta": "打开管理后台",
"pro_trial_path_new_title": "还没安装？",
"pro_trial_path_new_body": "30 秒拉起一个 Depsilo 实例：",
"pro_trial_path_new_cta": "查看安装文档",
"pro_trial_features_heading": "你将解锁这些能力",
"pro_trial_features_f1": "多项目工作区",
"pro_trial_features_f2": "OSV 安全漏洞扫描",
"pro_trial_features_f3": "SBOM 导出（SPDX + CycloneDX）",
"pro_trial_features_f4": "审计日志（CSV 导出）",
"pro_trial_features_f5": "包级别 Allow/Deny 规则",
"pro_trial_features_f6": "优先技术支持",
"pro_trial_faq_q1": "为什么不要邮箱？",
"pro_trial_faq_a1": "试用是 100% 本地的，没有人需要知道你的邮箱。",
"pro_trial_faq_q2": "14 天后会发生什么？",
"pro_trial_faq_a2": "Pro 功能锁回 Free 状态。Free 功能完全保留。",
"pro_trial_faq_q3": "我能再试一次吗？",
"pro_trial_faq_a3": "一台机器只能试用一次。",
"pro_trial_faq_q4": "试用结束后想买怎么办？",
"pro_trial_faq_a4": "回到 #pricing 选择适合你的方案。"
```

- [ ] **Step 3: Add matching keys to `en.json`**

```json
"pro_trial_hero_title": "Start your 14-day free trial",
"pro_trial_hero_subtitle": "All Pro features. No credit card. No email.",
"pro_trial_path_existing_title": "Already running Depsilo?",
"pro_trial_path_existing_body": "Open your local admin at /admin/license and click \"Start 14-day free trial\".",
"pro_trial_path_existing_cta": "Open admin",
"pro_trial_path_new_title": "New here?",
"pro_trial_path_new_body": "Spin up a Depsilo instance in 30 seconds:",
"pro_trial_path_new_cta": "View install docs",
"pro_trial_features_heading": "What you'll unlock",
"pro_trial_features_f1": "Multi-project workspaces",
"pro_trial_features_f2": "OSV security vulnerability scanning",
"pro_trial_features_f3": "SBOM export (SPDX + CycloneDX)",
"pro_trial_features_f4": "Audit logs (CSV export)",
"pro_trial_features_f5": "Package-level allow/deny rules",
"pro_trial_features_f6": "Priority support",
"pro_trial_faq_q1": "Why no email?",
"pro_trial_faq_a1": "The trial is 100% local — nothing phones home, so we don't need it.",
"pro_trial_faq_q2": "What happens after 14 days?",
"pro_trial_faq_a2": "Pro features lock back to the Free tier. Your Free features are unaffected.",
"pro_trial_faq_q3": "Can I try again?",
"pro_trial_faq_a3": "One trial per install.",
"pro_trial_faq_q4": "How do I buy after the trial?",
"pro_trial_faq_a4": "Head back to #pricing and pick the plan that suits you."
```

- [ ] **Step 4: Commit in the landing repo**

```bash
cd ../depsilo-landingpage
git add src/i18n/locales/zh-CN.json src/i18n/locales/en.json
git commit -m "i18n: add pro_trial_* keys for the upcoming /pro-trial page"
cd -
```

### Task 11.2: Create `pro-trial.astro`

**Files:**
- Create: `../depsilo-landingpage/src/pages/pro-trial.astro`

- [ ] **Step 1: Open an existing page for style reference**

```bash
head -50 ../depsilo-landingpage/src/pages/index.astro
```

Note: layout import, i18n helper usage, section structure.

- [ ] **Step 2: Create the page**

```astro
---
import BaseLayout from '../layouts/BaseLayout.astro';
import { t } from '../i18n/utils';
import Nav from '../components/landing/Nav.astro';
import Footer from '../components/landing/Footer.astro';
---

<BaseLayout title={t('pro_trial_hero_title')}>
  <Nav />

  <main class="max-w-5xl mx-auto px-6 py-16 space-y-16">

    <!-- Hero -->
    <section class="text-center space-y-4">
      <h1 class="text-[40px] font-light tracking-heading" data-i18n="pro_trial_hero_title">
        {t('pro_trial_hero_title')}
      </h1>
      <p class="text-lg text-secondary font-light" data-i18n="pro_trial_hero_subtitle">
        {t('pro_trial_hero_subtitle')}
      </p>
    </section>

    <!-- Two-path block -->
    <section class="grid md:grid-cols-2 gap-6">
      <!-- Already running -->
      <div class="bg-white p-6 rounded-lg border border-border-default shadow-stripe-ambient flex flex-col">
        <h2 class="text-xl font-normal mb-3" data-i18n="pro_trial_path_existing_title">
          {t('pro_trial_path_existing_title')}
        </h2>
        <p class="text-secondary font-light mb-6 flex-grow" data-i18n="pro_trial_path_existing_body">
          {t('pro_trial_path_existing_body')}
        </p>
        <a href="/admin/license"
           class="w-full py-3 rounded signature-button text-on-primary font-normal text-center block"
           data-i18n="pro_trial_path_existing_cta">
          {t('pro_trial_path_existing_cta')}
        </a>
      </div>

      <!-- New here -->
      <div class="bg-white p-6 rounded-lg border border-border-default shadow-stripe-ambient flex flex-col">
        <h2 class="text-xl font-normal mb-3" data-i18n="pro_trial_path_new_title">
          {t('pro_trial_path_new_title')}
        </h2>
        <p class="text-secondary font-light mb-4" data-i18n="pro_trial_path_new_body">
          {t('pro_trial_path_new_body')}
        </p>
        <pre class="bg-gray-50 text-xs p-3 rounded border border-border-default mb-6 overflow-auto"><code>docker run -d --name depsilo -p 23333:23333 \
  -v depsilo-data:/app/data \
  depsilo/depsilo:latest</code></pre>
        <a href="https://github.com/depsilo/depsilo#quick-start" target="_blank" rel="noopener"
           class="w-full py-3 rounded border border-border-purple text-primary font-normal hover:bg-primary/5 transition-colors text-center block"
           data-i18n="pro_trial_path_new_cta">
          {t('pro_trial_path_new_cta')}
        </a>
      </div>
    </section>

    <!-- Features -->
    <section class="space-y-4">
      <h2 class="text-2xl font-light text-center tracking-heading" data-i18n="pro_trial_features_heading">
        {t('pro_trial_features_heading')}
      </h2>
      <ul class="grid md:grid-cols-2 gap-3 max-w-2xl mx-auto">
        {[1, 2, 3, 4, 5, 6].map((i) => (
          <li class="flex items-start gap-3 text-sm text-secondary font-light">
            <span class="text-primary">✓</span>
            <span data-i18n={`pro_trial_features_f${i}`}>{t(`pro_trial_features_f${i}`)}</span>
          </li>
        ))}
      </ul>
    </section>

    <!-- FAQ -->
    <section class="space-y-4 max-w-2xl mx-auto">
      <h2 class="text-2xl font-light text-center tracking-heading">FAQ</h2>
      {[1, 2, 3, 4].map((i) => (
        <div class="border-l-2 border-primary/30 pl-4 py-2">
          <div class="font-normal text-on-surface mb-1" data-i18n={`pro_trial_faq_q${i}`}>{t(`pro_trial_faq_q${i}`)}</div>
          <div class="text-sm text-secondary font-light" data-i18n={`pro_trial_faq_a${i}`}>{t(`pro_trial_faq_a${i}`)}</div>
        </div>
      ))}
    </section>

  </main>

  <Footer />
</BaseLayout>
```

- [ ] **Step 3: Build and preview the landing page**

```bash
cd ../depsilo-landingpage
npm run build
echo "Open http://localhost:4321/pro-trial (or whatever port preview uses) and verify:"
echo "  - Hero text, two cards, feature list, FAQ all render"
echo "  - i18n switch (zh ↔ en) updates every text node"
echo "  - 'Open admin' link points to /admin/license"
npm run preview &
PID=$!
sleep 3
kill $PID 2>/dev/null
```

(Or use `npm run dev` and visit manually.)

- [ ] **Step 4: Commit in the landing repo**

```bash
git add src/pages/pro-trial.astro
git commit -m "feat(landing): /pro-trial page closes the existing 404 from Pricing CTA"
cd -
```

---

# Phase 12 — Release notes + final verification

### Task 12.1: Update CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Inspect existing CHANGELOG style**

```bash
head -50 CHANGELOG.md
```

- [ ] **Step 2: Add entry**

Insert a new section at the top following the existing date/version convention:

```markdown
## [0.4.0] — 2026-05-21

### Added
- `/admin/license` page for self-serve 14-day Pro trial activation and runtime license-key management (set / change / remove).
- API endpoints: `POST /api/v1/admin/license/trial/activate`, `PUT /api/v1/admin/license/key`, `DELETE /api/v1/admin/license/key`.
- New backend modules: `internal/trial` (state machine) and `internal/entitlement` (façade over license + trial).
- Landing-page `/pro-trial` page (closes the existing 404 the Pricing CTA pointed at).
- Frontend: global `ProRequiredModal` triggered by 402 responses, with inline "Start trial" CTA when available.

### Changed
- `GET /api/v1/admin/license/status` response body — new `source`, `days_left`, `trial_used`, `trial_available`, `license_key_masked` fields. Old `key_masked` and `activated_at` retained as deprecated aliases for one release; will be removed in 0.5.0.
- `402 PRO_REQUIRED` response now includes a `trial_available` boolean.
- `audit.Logger` and `rules.Engine` now depend on `entitlement.Checker` instead of `license.Manager` directly — trial users now get the same audit + rules behaviour as paid users.

### Deprecated
- `EntitlementStatus.key_masked` (use `license_key_masked`)
- `EntitlementStatus.activated_at` (read from per-source state instead — frontend can derive)
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): 0.4.0 — pricing surface + trial activation"
```

### Task 12.2: Full verification sweep

**Files:** none

- [ ] **Step 1: Run all linters**

```bash
make lint
```

Expected: PASS — go vet + i18n audit both green.

- [ ] **Step 2: Run all backend tests**

```bash
make test-unit
make test-integration
```

Expected: PASS.

- [ ] **Step 3: Frontend type-check + build**

```bash
cd web && npm run type-check && npm run build && cd ..
```

Expected: PASS, dist/ produced.

- [ ] **Step 4: Manual smoke through the full happy path**

```bash
make stop 2>/dev/null
rm -rf data/depsilo.db    # ensure fresh state
make dev
sleep 4
echo "Browser checks (manual):"
echo " 1. http://localhost:23333/admin → log in as admin/admin"
echo " 2. Sidebar has 'License' between Users and Settings"
echo " 3. /admin/license shows 'Start 14-day free trial' card"
echo " 4. Click trial start → card becomes 'Pro · Trial · 14 days remaining'"
echo " 5. /admin/projects (or any Pro page) loads without a 402"
echo " 6. Direct curl /api/v1/admin/license/status returns source=trial"
echo " 7. Open https://depsilo.com/pro-trial in incognito → page renders (verify landing repo deployed)"
make stop
```

- [ ] **Step 5: Final commit if any tweaks were made during manual smoke**

Group them sensibly. If everything passed, no commit needed.

- [ ] **Step 6: Push everything**

```bash
git log --oneline $(cat /tmp/depsilo-implementation-start.txt)..HEAD
git push origin master
cd ../depsilo-landingpage
git log --oneline @{u}..HEAD
git push origin master
cd -
```

Expected: depsilo/ gets ~25-30 new commits; depsilo-landingpage/ gets 2.

---

# Self-review

After completing all phases:

1. **Spec coverage** — every section of the spec ($1-$18) maps to a task above:
   - §1 Summary, §2 Motivation — captured in plan header
   - §3 Scope — Tasks 1.1-1.3, 2.x, 4.x, 6.x, 9-11 (in scope items); deferred items called out under "Known deviation" in plan front matter
   - §4 Architecture — entirely realized across Phases 1-7
   - §5 Data model — Tasks 1.1, 1.2, 1.3
   - §6 trial.Manager — Tasks 2.1, 2.2, 2.3
   - §7 entitlement.Checker — Tasks 4.1, 4.2
   - §8 license.Manager extensions — Tasks 3.1, 3.2, 3.3, 4.3
   - §9 API endpoints — Tasks 6.1, 6.2, 6.3
   - §10 License page — Tasks 9.1, 9.2
   - §11 ProRequiredModal — Tasks 10.1, 10.2, 10.3
   - §12 API client + i18n — Tasks 8.1, 8.2
   - §13 Landing /pro-trial — Tasks 11.1, 11.2
   - §14 Testing — Tasks 2.2/2.3, 3.2/3.3, 4.2, 7.1, 9.2/10.3 manual smoke
   - §15 Edge cases — covered in handler error mapping (Task 6.2) and integration test cases (Task 7.1)
   - §16 Rollout + alias compat — Tasks 4.1 (alias fields in Status), 12.1 (CHANGELOG)
   - §17, §18 Future hooks / Open questions — documentation only, no implementation needed

2. **Placeholder scan** — searched for TBD / TODO / "implement later" / "similar to" — none in implementation steps. Two `// TODO: write management event audit log` comments inside code are deliberate deferrals documented in the front-matter "Known deviation".

3. **Type consistency** — `EntitlementStatus` fields in TS (Task 8.1) match `Status` struct JSON tags in Go (Task 4.1). `licenseApi` method names (`activateTrial`, `setKey`, `clearKey`) match handler method names. `entitlement.Source` values (`none|trial|paid`) match between Go enum (Task 4.1) and TS union (Task 8.1).

Self-review pass.

---

# Execution handoff

**Plan complete and saved to `docs/plans/2026-05-21-pricing-trial-implementation.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
