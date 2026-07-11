# Tamper Detection Implementation Plan

> **Status: completed and merged to `master@1259728` on 2026-07-10.** The
> unchecked boxes below are retained as the historical execution plan, not a
> live progress tracker. Final implementation deltas from the original plan:
> the immutable threshold defaults to `cache.ttl_blob` rather than a fixed 1h;
> cache misses call `Verify` so an LRU re-fetch cannot silently replace the
> baseline; a refresh mismatch extends the cache TTL to throttle repeated
> downloads/alerts; and disabling the recorder skips persistence and decisions
> but the shared `countingReader` still computes its streaming hash.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect when an immutable artifact's upstream bytes change under the same version (silent registry swap / poisoned mirror / MITM) by recording a SHA-256 on first fetch and comparing on natural re-fetch, keeping the first-seen bytes on background refresh and alerting instead of overwriting.

**Architecture:** The existing `countingReader` (already wrapping the byte stream in both the miss-store and background-refresh paths) gains a streaming SHA-256. A new `internal/tamper` package owns a `Recorder` (DB store + event + webhook hook), injected into the cache `Manager` via a setter (same package-level injection pattern as `SetSecurityScanner`). Immutability is inferred from the request TTL (`ttl >= immutableThreshold`) — no adapter changes. On background refresh of an immutable key with a recorded hash, the manager verifies **without** overwriting storage; a mismatch fires a critical webhook and a `tamper_detected` event and leaves the trusted first-seen bytes in place.

**Tech Stack:** Go 1.25.6, GORM + SQLite, Gin, zap, `crypto/sha256`, React 19 + i18next frontend.

## Global Constraints

- Go module `depsilo`; `go 1.25.6` in go.mod (CI uses `go-version-file: go.mod`).
- Errors must be handled or propagated — never `_ = err` silently (project rule; io.Copy warnings use `zap.L().Warn`).
- All IO takes `context.Context`; background goroutines honor ctx cancellation.
- Event timestamps use `now()` (never the zero value — a prior subsystem shipped year-0001 webhook stamps).
- Frontend: `zh.ts` and `en.ts` leaf-key counts MUST stay equal (`python3 scripts/i18n-audit.py` gates it in CI).
- Wedge posture: the feature ships enabled by default (`[supply_chain.tamper_detection] enabled` defaults true); a nil recorder disables persistence, comparison, and alerts.
- Full local gate before any push: `go test ./... -race`, `go test -tags integration ./tests/integration/ -count=1`, `cd web && npm run type-check && npm run build`, `python3 scripts/i18n-audit.py`.
- Verify each Go task with: `go build ./... && go test ./internal/<pkg>/...`.

---

## File Structure

- `internal/cache/manager.go` — MODIFY: `countingReader` gains a hash; `NewManager` gains `immutableThreshold`; `SetTamperRecorder`; miss-store records baseline; background-refresh verifies-not-overwrites for immutable keys.
- `internal/cache/tamper_hook.go` — CREATE: the `TamperRecorder` interface the manager consumes (kept in `cache` so the package has no import edge to `internal/tamper`).
- `internal/db/tamper.go` — CREATE: `TamperRecord` GORM model.
- `internal/db/repository.go` — MODIFY: register `TamperRecord` in `AutoMigrate`.
- `internal/tamper/recorder.go` — CREATE: `Recorder` (implements `cache.TamperRecorder`), `OnTamperFn` hook.
- `internal/tamper/recorder_test.go` — CREATE: unit tests for the three-state logic.
- `internal/quarantine/store.go` — MODIFY: add `ActionTamperDetected` constant.
- `internal/notify/event.go` — MODIFY: add `EventTamperDetected`.
- `internal/config/config.go` — MODIFY: `SupplyChainConfig` gains `TamperDetection TamperConfig`.
- `internal/server/server.go` — MODIFY: build recorder, `SetTamperRecorder`, bridge `OnTamper` to the webhook notifier, pass immutable threshold to `NewManager`.
- `config.example.toml` — MODIFY: `[supply_chain.tamper_detection]` block.
- `web/src/admin/pages/Quarantine.tsx` — MODIFY: `tamper_detected` action badge + filter option.
- `web/src/i18n/zh.ts`, `web/src/i18n/en.ts` — MODIFY: badge label.
- `tests/mock/upstream_server.go` — MODIFY: a swappable-content endpoint.
- `tests/integration/tamper_test.go` — CREATE: end-to-end mismatch → event + first-seen bytes retained.
- `CLAUDE.md`, `CHANGELOG.md` — MODIFY: document the feature.

---

## Task 1: Streaming SHA-256 in countingReader

**Files:**
- Modify: `internal/cache/manager.go:19-38`
- Test: `internal/cache/counting_reader_hash_test.go` (create)

**Interfaces:**
- Produces: `countingReader.SumHex() string` (lowercase hex sha256 of all bytes read so far); existing `NewCountingReader(io.Reader) *countingReader` and `BytesRead() int64` unchanged.

- [ ] **Step 1: Write the failing test**

Create `internal/cache/counting_reader_hash_test.go`:

```go
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestCountingReader_SumHex(t *testing.T) {
	const payload = "the quick brown fox\n"
	cr := NewCountingReader(strings.NewReader(payload))
	if _, err := io.Copy(io.Discard, cr); err != nil {
		t.Fatalf("copy: %v", err)
	}
	want := sha256.Sum256([]byte(payload))
	if got := cr.SumHex(); got != hex.EncodeToString(want[:]) {
		t.Errorf("SumHex = %s, want %s", got, hex.EncodeToString(want[:]))
	}
	if cr.BytesRead() != int64(len(payload)) {
		t.Errorf("BytesRead = %d, want %d", cr.BytesRead(), len(payload))
	}
}

func TestCountingReader_SumHex_Empty(t *testing.T) {
	cr := NewCountingReader(strings.NewReader(""))
	_, _ = io.Copy(io.Discard, cr)
	// sha256 of empty input is the well-known e3b0c442... digest.
	if got := cr.SumHex(); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("empty SumHex = %s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -run TestCountingReader_SumHex`
Expected: FAIL — `cr.SumHex undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cache/manager.go`, replace the `countingReader` struct + constructor + `Read` (lines 19-38) with:

```go
// countingReader wraps an io.Reader, counts bytes read through it, and
// computes a streaming SHA-256 of everything it reads. Used to size
// bodies when upstream omits Content-Length AND to fingerprint content
// for tamper detection — both come free in the single pass the storage
// pump already makes.
type countingReader struct {
	r io.Reader
	n int64
	h hash.Hash
}

func NewCountingReader(r io.Reader) *countingReader {
	return &countingReader{r: r, h: sha256.New()}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.n += int64(n)
		cr.h.Write(p[:n]) // hash.Hash.Write never returns an error
	}
	return n, err
}

func (cr *countingReader) BytesRead() int64 {
	return cr.n
}

// SumHex returns the lowercase-hex SHA-256 of all bytes read so far.
// Call after the reader is fully drained.
func (cr *countingReader) SumHex() string {
	return hex.EncodeToString(cr.h.Sum(nil))
}
```

Add to the import block at the top of `manager.go` (keep alphabetical within the stdlib group):

```go
	"crypto/sha256"
	"encoding/hex"
	"hash"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cache/ -run TestCountingReader`
Expected: PASS (both cases).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/manager.go internal/cache/counting_reader_hash_test.go
git commit -m "feat(cache): streaming sha256 in countingReader"
```

---

## Task 2: TamperRecord model + migration

**Files:**
- Create: `internal/db/tamper.go`
- Modify: `internal/db/repository.go` (AutoMigrate list, after the blocklist models)
- Test: `internal/db/tamper_test.go` (create)

**Interfaces:**
- Produces: `db.TamperRecord` with fields `Key string` (PK), `Ecosystem string`, `Package string`, `Version string`, `SHA256 string`, `Size int64`, `FirstSeenAt time.Time`, `LastVerifiedAt time.Time`, `VerifyCount int64`; `TableName() → "tamper_record"`.

- [ ] **Step 1: Write the failing test**

Create `internal/db/tamper_test.go`:

```go
package db

import (
	"testing"
	"time"
)

func TestTamperRecord_Migrate(t *testing.T) {
	d, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rec := TamperRecord{
		Key: "pypi/files/requests-2.32.3.tar.gz", Ecosystem: "pypi",
		Package: "requests", Version: "2.32.3", SHA256: "abc", Size: 10,
		FirstSeenAt: now, LastVerifiedAt: now,
	}
	if err := d.Create(&rec).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got TamperRecord
	if err := d.First(&got, "key = ?", rec.Key).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.SHA256 != "abc" || got.Package != "requests" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestTamperRecord_Migrate`
Expected: FAIL — `undefined: TamperRecord`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/db/tamper.go`:

```go
package db

import "time"

// TamperRecord is the first-seen content fingerprint of one immutable
// artifact (keyed by its cache key, 1:1 with the artifact). Tamper
// detection compares a re-fetched artifact's SHA-256 against SHA256
// here; a mismatch on an immutable key is a tamper alert. Domain
// helper lives in internal/tamper.
type TamperRecord struct {
	Key            string    `gorm:"size:512;primaryKey" json:"key"`
	Ecosystem      string    `gorm:"size:32;index" json:"ecosystem"`
	Package        string    `gorm:"size:256;index" json:"package"`
	Version        string    `gorm:"size:128" json:"version"`
	SHA256         string    `gorm:"size:64" json:"sha256"`
	Size           int64     `json:"size"`
	FirstSeenAt    time.Time `json:"first_seen_at"`
	LastVerifiedAt time.Time `json:"last_verified_at"`
	VerifyCount    int64     `json:"verify_count"`
}

func (TamperRecord) TableName() string { return "tamper_record" }
```

In `internal/db/repository.go`, add `&TamperRecord{},` to the `AutoMigrate` call immediately after `&BlocklistSyncState{},`:

```go
		&MaliciousPackage{},
		&MalwareOverride{},
		&BlocklistSyncState{},
		// Tamper detection (DIRECTION T1). Defined in db/tamper.go;
		// helper in internal/tamper.
		&TamperRecord{},
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestTamperRecord_Migrate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/tamper.go internal/db/repository.go internal/db/tamper_test.go
git commit -m "feat(db): TamperRecord model + migration"
```

---

## Task 3: ActionTamperDetected + EventTamperDetected constants

**Files:**
- Modify: `internal/quarantine/store.go` (action constants block)
- Modify: `internal/notify/event.go` (event type constants)

**Interfaces:**
- Produces: `quarantine.ActionTamperDetected = "tamper_detected"`; `notify.EventTamperDetected = "tamper_detected"`.

- [ ] **Step 1: Add the quarantine action constant**

In `internal/quarantine/store.go`, inside the `const (` action block, after `ActionMalwareBypassed`:

```go
	// Tamper detection (DIRECTION T1): an immutable artifact's
	// re-fetched bytes did not match the first-seen SHA-256. Written
	// by internal/tamper; shares the quarantine event stream.
	ActionTamperDetected = "tamper_detected"
```

- [ ] **Step 2: Add the notify event constant**

In `internal/notify/event.go`, inside the event `const (` block, after `EventMalwareBlocked`:

```go
	// EventTamperDetected fires when an immutable artifact's upstream
	// content changed under the same version. Severity critical — a
	// registry silently swapping bytes is a supply-chain compromise
	// signal. See docs/DIRECTION.md §T1 tamper detection.
	EventTamperDetected = "tamper_detected"
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./internal/quarantine/ ./internal/notify/`
Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
git add internal/quarantine/store.go internal/notify/event.go
git commit -m "feat: tamper_detected action + event constants"
```

---

## Task 4: cache.TamperRecorder interface

**Files:**
- Create: `internal/cache/tamper_hook.go`

**Interfaces:**
- Produces: interface `cache.TamperRecorder`:
  - `Record(ctx, key, ecosystem, pkg, version, sha256 string, size int64)` — baseline insert on first sight (idempotent; no-op if a record already exists).
  - `Verify(ctx, key, ecosystem, pkg, version, sha256 string, size int64, clientIP string) TamperResult` — compare a re-fetched hash to the baseline.
- Produces: `type TamperResult struct { KnownMismatch bool }` — `KnownMismatch=true` means a baseline existed and the hash differed (caller must NOT overwrite storage).

- [ ] **Step 1: Write the interface**

Create `internal/cache/tamper_hook.go`:

```go
package cache

import "context"

// TamperResult is what Verify returns to the manager. KnownMismatch is
// the only load-bearing bit: true means "a baseline existed and the
// re-fetched hash differs" — the manager must keep the first-seen bytes
// and NOT overwrite storage. All other cases (first sight, match) are
// KnownMismatch=false and the manager proceeds normally.
type TamperResult struct {
	KnownMismatch bool
}

// TamperRecorder is the optional contract the cache Manager consumes
// for content-integrity tracking. The concrete implementation lives in
// internal/tamper; defining the interface here keeps the cache package
// free of any import edge to it (same pattern as SecurityScanner).
//
// Record establishes the first-seen baseline for an immutable artifact
// (idempotent — a second call for a key that already has a baseline is
// a no-op). Verify compares a re-fetched artifact's hash to the
// baseline, records the audit event / fires the alert hook on mismatch,
// and returns whether the manager must protect the first-seen bytes.
type TamperRecorder interface {
	Record(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64)
	Verify(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64, clientIP string) TamperResult
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./internal/cache/`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/cache/tamper_hook.go
git commit -m "feat(cache): TamperRecorder interface"
```

---

## Task 5: tamper.Recorder (three-state logic + event/hook)

**Files:**
- Create: `internal/tamper/recorder.go`
- Test: `internal/tamper/recorder_test.go`

**Interfaces:**
- Consumes: `cache.TamperRecorder` / `cache.TamperResult` (Task 4); `db.TamperRecord` (Task 2); `db.QuarantineEvent` + `quarantine.ActionTamperDetected` (Task 3).
- Produces: `tamper.NewRecorder(*gorm.DB) *Recorder` implementing `cache.TamperRecorder`; `(*Recorder).SetOnTamper(OnTamperFn)` where `type OnTamperFn func(ev db.QuarantineEvent)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tamper/recorder_test.go`:

```go
package tamper

import (
	"context"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

func newTestRecorder(t *testing.T) *Recorder {
	t.Helper()
	d, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	return NewRecorder(d)
}

func TestRecorder_FirstSeenThenMatch(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "pypi/files/requests-2.32.3.tar.gz"

	// First sight establishes the baseline; no event.
	r.Record(ctx, key, "pypi", "requests", "2.32.3", "hashA", 100)

	var count int64
	r.db.Model(&db.QuarantineEvent{}).Count(&count)
	if count != 0 {
		t.Fatalf("baseline should write no event, got %d", count)
	}

	// A matching re-fetch verifies clean.
	res := r.Verify(ctx, key, "pypi", "requests", "2.32.3", "hashA", 100, "10.0.0.1")
	if res.KnownMismatch {
		t.Error("matching hash must not report KnownMismatch")
	}
	var rec db.TamperRecord
	r.db.First(&rec, "key = ?", key)
	if rec.VerifyCount != 1 {
		t.Errorf("VerifyCount = %d, want 1", rec.VerifyCount)
	}
}

func TestRecorder_MismatchAlertsAndProtects(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "npm/left-pad/-/left-pad-1.3.0.tgz"

	var fired *db.QuarantineEvent
	r.SetOnTamper(func(ev db.QuarantineEvent) { fired = &ev })

	r.Record(ctx, key, "npm", "left-pad", "1.3.0", "goodhash", 42)
	res := r.Verify(ctx, key, "npm", "left-pad", "1.3.0", "EVILHASH", 42, "10.0.0.2")

	if !res.KnownMismatch {
		t.Fatal("differing hash must report KnownMismatch")
	}
	if fired == nil || fired.Action != quarantine.ActionTamperDetected {
		t.Fatalf("OnTamper hook not fired with tamper_detected: %+v", fired)
	}
	// The baseline must NOT be overwritten — first-seen stays truth.
	var rec db.TamperRecord
	r.db.First(&rec, "key = ?", key)
	if rec.SHA256 != "goodhash" {
		t.Errorf("baseline hash overwritten to %s", rec.SHA256)
	}
	// The event carries a non-zero timestamp.
	if fired.CreatedAt.IsZero() {
		t.Error("event CreatedAt is the zero value")
	}
	// And it persisted.
	var evCount int64
	r.db.Model(&db.QuarantineEvent{}).Where("action = ?", quarantine.ActionTamperDetected).Count(&evCount)
	if evCount != 1 {
		t.Errorf("expected 1 persisted tamper event, got %d", evCount)
	}
}

func TestRecorder_VerifyWithoutBaselineRecords(t *testing.T) {
	r := newTestRecorder(t)
	ctx := context.Background()
	key := "cargo/serde/1.0.0/download.crate"

	// Pre-feature cached artifact: Verify with no baseline adopts the
	// current hash as the baseline and does NOT alert.
	res := r.Verify(ctx, key, "cargo", "serde", "1.0.0", "firsthash", 7, "10.0.0.3")
	if res.KnownMismatch {
		t.Error("no baseline must never report KnownMismatch")
	}
	var rec db.TamperRecord
	if err := r.db.First(&rec, "key = ?", key).Error; err != nil {
		t.Fatalf("baseline not adopted: %v", err)
	}
	if rec.SHA256 != "firsthash" {
		t.Errorf("adopted hash = %s", rec.SHA256)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tamper/`
Expected: FAIL — package/`NewRecorder` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tamper/recorder.go`:

```go
// Package tamper implements content-integrity tracking (DIRECTION T1
// tamper detection): the first-seen SHA-256 of each immutable artifact
// is the baseline, and a later re-fetch whose hash differs is a tamper
// alert. The cache Manager calls Record on first fetch and Verify on
// background refresh; this package owns the DB rows, the audit event,
// and the alert hook.
package tamper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

// OnTamperFn is the optional alert hook, fired when Verify finds a
// mismatch. Wired to the webhook notifier in server.go. Must not block
// (fire-and-forget); a panic is recovered so a misbehaving channel
// can't break the refresh path.
type OnTamperFn func(ev db.QuarantineEvent)

type Recorder struct {
	db       *gorm.DB
	now      func() time.Time
	onTamper OnTamperFn
}

func NewRecorder(database *gorm.DB) *Recorder {
	return &Recorder{db: database, now: func() time.Time { return time.Now().UTC() }}
}

func (r *Recorder) SetOnTamper(fn OnTamperFn) { r.onTamper = fn }

// Record establishes the first-seen baseline. Idempotent: if a record
// already exists for the key, this is a no-op (the existing baseline is
// the trusted truth and must never be overwritten by a later fetch).
func (r *Recorder) Record(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64) {
	var existing db.TamperRecord
	err := r.db.WithContext(ctx).First(&existing, "key = ?", key).Error
	if err == nil {
		return // baseline already set
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		zap.L().Warn("tamper: baseline lookup", zap.String("key", key), zap.Error(err))
		return
	}
	now := r.now()
	rec := db.TamperRecord{
		Key: key, Ecosystem: ecosystem, Package: pkg, Version: version,
		SHA256: sha256, Size: size, FirstSeenAt: now, LastVerifiedAt: now,
	}
	if createErr := r.db.WithContext(ctx).Create(&rec).Error; createErr != nil {
		zap.L().Warn("tamper: baseline create", zap.String("key", key), zap.Error(createErr))
	}
}

// Verify compares a re-fetched artifact's hash to the baseline.
//   - No baseline (pre-feature cache): adopt this hash as the baseline,
//     never alert. Returns KnownMismatch=false.
//   - Match: bump VerifyCount + LastVerifiedAt. Returns false.
//   - Mismatch: write a tamper_detected event, fire OnTamper, DO NOT
//     touch the baseline hash. Returns KnownMismatch=true so the
//     manager keeps the first-seen bytes.
func (r *Recorder) Verify(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64, clientIP string) cache.TamperResult {
	var rec db.TamperRecord
	err := r.db.WithContext(ctx).First(&rec, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		r.Record(ctx, key, ecosystem, pkg, version, sha256, size)
		return cache.TamperResult{}
	}
	if err != nil {
		// A DB hiccup must not turn a clean fetch into a false alarm.
		zap.L().Warn("tamper: verify lookup", zap.String("key", key), zap.Error(err))
		return cache.TamperResult{}
	}

	if rec.SHA256 == sha256 {
		r.db.WithContext(ctx).Model(&rec).Updates(map[string]interface{}{
			"last_verified_at": r.now(),
			"verify_count":     rec.VerifyCount + 1,
		})
		return cache.TamperResult{}
	}

	reason := fmt.Sprintf(
		"immutable artifact %s@%s (%s) changed upstream: first-seen sha256 %s, now %s — keeping first-seen bytes, refusing to cache the new content",
		pkg, version, ecosystem, short(rec.SHA256), short(sha256),
	)
	ev := db.QuarantineEvent{
		Ecosystem: ecosystem, Package: pkg, Version: version,
		Action: quarantine.ActionTamperDetected, Reason: reason,
		ClientIP: clientIP, CreatedAt: r.now(),
	}
	if createErr := r.db.WithContext(ctx).Create(&ev).Error; createErr != nil {
		zap.L().Warn("tamper: event create", zap.Error(createErr))
	}
	if r.onTamper != nil {
		func() {
			defer func() {
				if p := recover(); p != nil {
					zap.L().Error("tamper: OnTamper hook panicked", zap.Any("recover", p))
				}
			}()
			r.onTamper(ev)
		}()
	}
	return cache.TamperResult{KnownMismatch: true}
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tamper/`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tamper/
git commit -m "feat(tamper): Recorder — baseline/verify/alert three-state"
```

---

## Task 6: Wire the recorder into the cache manager

**Files:**
- Modify: `internal/cache/manager.go` (Manager struct + `NewManager` + `SetTamperRecorder` + `storeAndCommit` + `backgroundRefresh`)
- Test: `internal/cache/tamper_integration_test.go` (create)

**Interfaces:**
- Consumes: `TamperRecorder`, `TamperResult` (Task 4); `countingReader.SumHex()` (Task 1); `packagekey.ExtractName` (existing).
- Produces: `NewManager(storage Storage, database *gorm.DB, eventBus *EventBus, immutableThreshold time.Duration) *Manager` (signature CHANGES — one new trailing param); `(*Manager).SetTamperRecorder(TamperRecorder)`.

- [ ] **Step 1: Write the failing test**

Create `internal/cache/tamper_integration_test.go`:

```go
package cache

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"depsilo/internal/db"
)

// fakeRecorder captures manager→recorder calls and lets a test force a
// mismatch verdict.
type fakeRecorder struct {
	recorded    []string // keys passed to Record
	verified    []string // keys passed to Verify
	forceMismat bool
}

func (f *fakeRecorder) Record(_ context.Context, key, _, _, _, _ string, _ int64) {
	f.recorded = append(f.recorded, key)
}

func (f *fakeRecorder) Verify(_ context.Context, key, _, _, _, _ string, _ int64, _ string) TamperResult {
	f.verified = append(f.verified, key)
	return TamperResult{KnownMismatch: f.forceMismat}
}

func newTamperTestManager(t *testing.T) (*Manager, *fakeRecorder) {
	t.Helper()
	d, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(d); err != nil {
		t.Fatal(err)
	}
	// immutableThreshold = 1h; storage is the in-memory test double
	// already used by cache tests.
	m := NewManager(newMemStorage(), d, NewEventBus(), time.Hour)
	rec := &fakeRecorder{}
	m.SetTamperRecorder(rec)
	return m, rec
}

func TestManager_ImmutableMissRecordsBaseline(t *testing.T) {
	m, rec := newTamperTestManager(t)
	// TTL >= threshold ⇒ immutable ⇒ Record called on miss.
	_, err := m.Get(context.Background(), "pypi/files/x-1.0.0.tar.gz", "pypi", 72*time.Hour,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("payload-A")), "application/gzip", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// Give the async storeAndCommit goroutine time to run.
	waitFor(t, func() bool { return len(rec.recorded) == 1 })
	if rec.recorded[0] != "pypi/files/x-1.0.0.tar.gz" {
		t.Errorf("Record key = %s", rec.recorded[0])
	}
}

func TestManager_MutableMissSkipsRecorder(t *testing.T) {
	m, rec := newTamperTestManager(t)
	// TTL < threshold ⇒ mutable metadata ⇒ recorder untouched.
	_, err := m.Get(context.Background(), "pypi/simple/x/index.html", "pypi", 5*time.Minute,
		func(context.Context) (io.ReadCloser, string, int64, string, error) {
			return io.NopCloser(strings.NewReader("<html>")), "text/html", -1, "mock", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing should ever be recorded for a mutable key.
	time.Sleep(200 * time.Millisecond)
	if len(rec.recorded) != 0 {
		t.Errorf("mutable key recorded: %v", rec.recorded)
	}
}
```

Note: `newMemStorage()` and `waitFor()` — if the cache test package already has an in-memory storage double and a poll helper (grep `func newMemStorage`, `func waitFor` under `internal/cache/`), reuse them. If not, add this minimal pair to the new test file:

```go
type memStorage struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemStorage() *memStorage { return &memStorage{data: map[string][]byte{}} }
func (s *memStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.data[key] = b
	s.mu.Unlock()
	return nil
}
func (s *memStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[key]
	if !ok {
		return nil, 0, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}
func (s *memStorage) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}
func (s *memStorage) Delete(_ context.Context, key string) error { return nil }
func (s *memStorage) Stat(_ context.Context, key string) (*ObjectMeta, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *memStorage) List(_ context.Context, prefix string) ([]ObjectMeta, error) { return nil, nil }
func (s *memStorage) TotalSize(_ context.Context) (int64, error)                  { return 0, nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
```

Add the imports this double needs (`bytes`, `fmt`, `sync`) to the test file. First check the real `Storage` interface (`internal/cache/store.go`) and match every method exactly — if a method signature differs, fix the double to match.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -run TestManager_ -run 'Immutable|Mutable'`
Expected: FAIL — `NewManager` wants 3 args / `SetTamperRecorder` undefined.

- [ ] **Step 3: Implement — Manager field + constructor + setter**

In `internal/cache/manager.go`, add to the `Manager` struct (after `securityScanner`):

```go
	tamper             TamperRecorder
	immutableThreshold time.Duration
```

Change `NewManager` to take the threshold:

```go
// NewManager creates a new cache manager. immutableThreshold is the TTL
// at or above which an artifact is treated as immutable for tamper
// detection (metadata uses short TTLs, blobs long ones). Pass e.g. 1h.
func NewManager(storage Storage, database *gorm.DB, eventBus *EventBus, immutableThreshold time.Duration) *Manager {
	return &Manager{
		storage:            storage,
		db:                 database,
		eventBus:           eventBus,
		inflight:           make(map[string]*inflightFetch),
		immutableThreshold: immutableThreshold,
	}
}

// SetTamperRecorder attaches the optional content-integrity recorder.
// Pass nil to disable tamper persistence, comparison, and alerts. The
// hash is still computed by countingReader but never consulted.
func (m *Manager) SetTamperRecorder(r TamperRecorder) { m.tamper = r }

// isImmutable reports whether a TTL marks an artifact as immutable.
func (m *Manager) isImmutable(ttl time.Duration) bool {
	return m.immutableThreshold > 0 && ttl >= m.immutableThreshold
}
```

- [ ] **Step 4: Implement — record baseline on miss**

In `storeAndCommit` (`internal/cache/manager.go`), locate the `m.publishEvent(key, adapterType, false, size)` line. Immediately BEFORE it, insert:

```go
	// Tamper baseline: first-seen SHA-256 of immutable artifacts.
	if m.tamper != nil && m.isImmutable(ttl) {
		pkgName := packagekey.ExtractName(adapterType, key)
		m.tamper.Record(context.Background(), key, adapterType, pkgName,
			versionFromKey(adapterType, key), cr.SumHex(), size)
	}
```

`storeAndCommit` receives `ttl` (it's a parameter) and `cr` is the `countingReader` already in scope. Add a small helper at the bottom of `manager.go`:

```go
// versionFromKey is a best-effort version string for tamper event
// readability. The cache key already encodes the artifact; we only need
// something human-facing, so an empty result is acceptable.
func versionFromKey(adapterType, key string) string {
	// The filename tail usually carries the version; keep it simple —
	// the authoritative identity is the cache key itself.
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return ""
}
```

- [ ] **Step 5: Implement — verify (not overwrite) on background refresh**

In `backgroundRefresh` (`internal/cache/manager.go`), replace the body of the `m.group.Do` closure (the fetch → Put → DB-update block) with the branch below. The key change: for an immutable key with the recorder present, hash the re-fetched bytes to a discard sink and verify — never calling `storage.Put`, so the first-seen bytes stay put.

```go
	_, err, _ := m.group.Do("bg:"+key, func() (interface{}, error) {
		body, contentType, size, _, err := fetchFn(ctx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		cr := NewCountingReader(body)

		// Immutable + tamper on: verify-only. Drain to discard (do NOT
		// overwrite storage — immutable bytes are supposed to be
		// identical), then compare the hash. A mismatch keeps the
		// trusted first-seen copy and alerts; a match just extends TTL.
		if m.tamper != nil && m.isImmutable(ttl) {
			if _, copyErr := io.Copy(io.Discard, cr); copyErr != nil {
				return nil, fmt.Errorf("bg verify read: %w", copyErr)
			}
			pkgName := packagekey.ExtractName(adapterType, key)
			res := m.tamper.Verify(context.Background(), key, adapterType, pkgName,
				versionFromKey(adapterType, key), cr.SumHex(), cr.BytesRead(), "")
			if res.KnownMismatch {
				zap.L().Warn("tamper: refusing to overwrite tampered artifact", zap.String("key", key))
				// Keep first-seen bytes; do not touch storage or TTL.
				return nil, nil
			}
			now := time.Now()
			m.db.Where("key = ?", key).Updates(map[string]interface{}{
				"expires_at":    now.Add(ttl),
				"last_accessed": now,
			})
			return nil, nil
		}

		// Mutable (or tamper off): normal refresh — overwrite storage.
		if putErr := m.storage.Put(ctx, key, cr, size, contentType); putErr != nil {
			return nil, fmt.Errorf("bg refresh put: %w", putErr)
		}
		if size <= 0 {
			size = cr.BytesRead()
		}
		now := time.Now()
		m.db.Where("key = ?", key).Updates(map[string]interface{}{
			"size":          size,
			"content_type":  contentType,
			"expires_at":    now.Add(ttl),
			"last_accessed": now,
		})
		zap.L().Debug("background refresh done", zap.String("key", key))
		return nil, nil
	})
```

- [ ] **Step 6: Fix the existing NewManager call**

In `internal/server/server.go`, change line 128 from:

```go
	cacheMgr := cache.NewManager(storage, database, eventBus)
```
to:
```go
	// 1h immutable threshold: sits between the 5m index TTL and 72h
	// blob TTL defaults, so artifact blobs classify as immutable and
	// metadata does not.
	cacheMgr := cache.NewManager(storage, database, eventBus, time.Hour)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/cache/`
Expected: PASS (new tests + existing cache tests still green).

- [ ] **Step 8: Commit**

```bash
git add internal/cache/manager.go internal/cache/tamper_integration_test.go internal/server/server.go
git commit -m "feat(cache): wire tamper recorder — record on miss, verify-not-overwrite on refresh"
```

---

## Task 7: Config + server assembly + webhook bridge

**Files:**
- Modify: `internal/config/config.go` (`SupplyChainConfig`, new `TamperConfig`)
- Modify: `internal/server/server.go` (build recorder, SetTamperRecorder, OnTamper→webhook bridge)
- Modify: `config.example.toml`
- Test: manual (server boot smoke) + covered by Task 8 integration.

**Interfaces:**
- Consumes: `tamper.NewRecorder`, `(*Recorder).SetOnTamper` (Task 5); `notify.EventTamperDetected` (Task 3); `cacheMgr.SetTamperRecorder` (Task 6).
- Produces: `config.TamperConfig{ Enabled *bool }`; `SupplyChainConfig.TamperDetection TamperConfig`.

- [ ] **Step 1: Add the config struct**

In `internal/config/config.go`, add to `SupplyChainConfig` (after the `Blocklist` field):

```go
	// TamperDetection: content-integrity tracking of immutable
	// artifacts. Enabled by default.
	TamperDetection TamperConfig `mapstructure:"tamper_detection"`
```

Add the struct after `BlocklistConfig`:

```go
// TamperConfig is [supply_chain.tamper_detection] (DIRECTION T1).
// Enabled defaults to true; disabling detaches the recorder for zero
// overhead.
type TamperConfig struct {
	Enabled *bool `mapstructure:"enabled"`
}

// IsEnabled applies the default-true semantics.
func (c TamperConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }
```

- [ ] **Step 2: Assemble the recorder in server.go**

In `internal/server/server.go`, immediately AFTER the blocklist wiring block (the `} else { zap.L().Info("malicious-package blocklist disabled by config") }` that ends around line 232) and BEFORE the `// Webhook notification engine` comment, insert:

```go
	// Tamper detection (DIRECTION T1): first-seen SHA-256 of immutable
	// artifacts; a re-fetch whose hash differs keeps the trusted bytes
	// and alerts. Enabled by default; nil disables recorder decisions.
	var tamperRecorder *tamper.Recorder
	if cfg.SupplyChain.TamperDetection.IsEnabled() {
		tamperRecorder = tamper.NewRecorder(database)
		cacheMgr.SetTamperRecorder(tamperRecorder)
	} else {
		zap.L().Info("tamper detection disabled by config")
	}
```

Add the import `"depsilo/internal/tamper"` to server.go's import block (alphabetical: after `"depsilo/internal/quarantine/resolvers"` or wherever the `depsilo/internal/t*` group sits — place before `"depsilo/internal/trial"`).

- [ ] **Step 3: Bridge OnTamper to the webhook notifier**

In `internal/server/server.go`, find the `quarantineChecker.SetOnBlock(func(ev db.QuarantineEvent) {` block. Immediately AFTER that closure's closing `})`, insert:

```go
	// Tamper → webhook bridge. Same loose coupling as the quarantine
	// bridge: the tamper package never imports notify. Critical
	// severity — a registry swapping bytes under a version is a
	// compromise signal.
	if tamperRecorder != nil {
		tamperRecorder.SetOnTamper(func(ev db.QuarantineEvent) {
			webhookNotifier.Dispatch(ctx, notify.Event{
				Type:      notify.EventTamperDetected,
				Severity:  "critical",
				Title:     fmt.Sprintf("Tamper: %s %s on %s changed upstream", ev.Package, ev.Version, ev.Ecosystem),
				Message:   "An immutable artifact's upstream content changed under the same version. The first-seen bytes are being served; the new content was NOT cached.",
				Detail:    ev.Reason,
				Timestamp: ev.CreatedAt,
			})
		})
	}
```

- [ ] **Step 4: Config example**

In `config.example.toml`, after the `[supply_chain.blocklist]` block, add:

```toml

# ── Tamper detection (content integrity of immutable artifacts) ──
# Records the SHA-256 of each immutable artifact (wheel/.crate/.gem/…)
# on first fetch. When the artifact is naturally re-fetched (background
# refresh) and the upstream bytes differ, depsilo keeps the trusted
# first-seen copy, refuses to cache the new content, and fires a
# critical alert — a registry silently swapping bytes under the same
# version is a supply-chain compromise signal. Alert-only; never blocks.
[supply_chain.tamper_detection]
enabled = true
```

- [ ] **Step 5: Verify build + boot**

Run: `go build ./... && go vet ./internal/server/ ./internal/config/`
Expected: no output (success).

Then a boot smoke test:
```bash
go build -o /tmp/depsilo-tamper ./cmd/depsilo && \
  DEPSILO_CONFIG=config.example.toml timeout 4 /tmp/depsilo-tamper serve 2>&1 | grep -iE "tamper|listening|started" | head -3
```
Expected: no `tamper detection disabled` line (it's enabled by default), server starts.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/server/server.go config.example.toml
git commit -m "feat(server): assemble tamper recorder + webhook bridge + config"
```

---

## Task 8: End-to-end integration test

**Files:**
- Modify: `tests/mock/upstream_server.go` (swappable-content npm tarball)
- Create: `tests/integration/tamper_test.go`
- Modify: `tests/integration/main_test.go` (short blob TTL so refresh triggers)

**Interfaces:**
- Consumes: the running server + mock upstream (existing integration harness), `adminGet` helper (existing).

- [ ] **Step 1: Add a swappable endpoint to the mock**

In `tests/mock/upstream_server.go`, add a field to `MockUpstream` for the mutable tarball body and a setter, then a register function. Add to the struct:

```go
	tamperBody atomic.Value // string; the current bytes served at /tamper/pkg/-/pkg-1.0.0.tgz
```

Add import `"sync/atomic"` (already present if the blocklist task added it — check first). Add methods:

```go
// SetTamperBody swaps the bytes the tamper test endpoint serves, so a
// test can simulate an upstream silently changing an immutable artifact.
func (m *MockUpstream) SetTamperBody(s string) { m.tamperBody.Store(s) }

// RegisterTamper serves a fixed npm-shaped metadata doc plus a tarball
// whose bytes are controlled by SetTamperBody.
func (m *MockUpstream) RegisterTamper() {
	m.tamperBody.Store("ORIGINAL-BYTES")
	m.mux.HandleFunc("/tamperpkg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"tamperpkg","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"tamperpkg","version":"1.0.0","dist":{"tarball":"%s/tamperpkg/-/tamperpkg-1.0.0.tgz","shasum":"x","integrity":"sha512-x"}}}}`, m.URL())
	})
	m.mux.HandleFunc("/tamperpkg/-/tamperpkg-1.0.0.tgz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write([]byte(m.tamperBody.Load().(string)))
	})
}
```

Add `m.RegisterTamper()` to `RegisterAll()`.

- [ ] **Step 2: Make the immutable threshold config-overridable, and shorten the test blob TTL**

The test must trigger a background refresh within test time, which needs a short blob TTL. But a short TTL (e.g. 2s) is below the 1h immutable threshold, so it would be classified *mutable* and skip tamper detection. Resolve this by making the threshold config-overridable so the test can set it to 1s — then a 2s-TTL blob is still immutable. Three sub-steps:

Sub-step 2a — In `internal/config/config.go` `TamperConfig` (from Task 7 step 1), extend it to:
```go
	// ImmutableThresholdSeconds lets tests (and unusual deployments)
	// override the default 1h immutable-classification TTL. 0 = default.
	ImmutableThresholdSeconds int `mapstructure:"immutable_threshold_seconds"`
}

// ImmutableThreshold resolves the configured or default threshold.
func (c TamperConfig) ImmutableThreshold() time.Duration {
	if c.ImmutableThresholdSeconds > 0 {
		return time.Duration(c.ImmutableThresholdSeconds) * time.Second
	}
	return time.Hour
}
```
(add `"time"` import to config.go if not present — it is, other fields use it.)

Sub-step 2b — In `internal/server/server.go`, change the `NewManager` call to use it:
```go
	cacheMgr := cache.NewManager(storage, database, eventBus, cfg.SupplyChain.TamperDetection.ImmutableThreshold())
```

Sub-step 2c — In `tests/integration/main_test.go`, keep `ttl_blob = "2s"` AND add a supply-chain block that lowers the threshold to 1s so a 2s-TTL blob is still immutable:
```
[supply_chain.tamper_detection]
enabled = true
immutable_threshold_seconds = 1
```
(Place it alongside the existing `[supply_chain.min_release_age]` / `[supply_chain.blocklist]` blocks in the generated config.)

- [ ] **Step 3: Write the integration test**

Create `tests/integration/tamper_test.go`:

```go
//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// DIRECTION T1 acceptance: an immutable artifact whose upstream bytes
// change under the same version raises a tamper_detected event AND the
// client keeps getting the first-seen bytes (storage not overwritten).
func TestTamper_UpstreamSwapDetectedAndFirstSeenKept(t *testing.T) {
	url := depsiloURL + "/npm/tamperpkg/-/tamperpkg-1.0.0.tgz"

	// First fetch: caches ORIGINAL-BYTES and records the baseline.
	first := httpGet(t, url)
	firstBody := readBody(t, first)
	if firstBody != "ORIGINAL-BYTES" {
		t.Fatalf("first fetch body = %q", firstBody)
	}

	// Simulate an upstream silently swapping the artifact bytes.
	mockServer.SetTamperBody("EVIL-SWAPPED-BYTES")

	// Let the 2s blob TTL lapse, then hit it repeatedly: the stale hit
	// serves cache immediately and triggers a background refresh, which
	// verifies-not-overwrites and raises the event.
	time.Sleep(3 * time.Second)
	for i := 0; i < 3; i++ {
		resp := httpGet(t, url)
		body := readBody(t, resp)
		if body != "ORIGINAL-BYTES" {
			t.Fatalf("client got swapped bytes %q — first-seen not protected", body)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// A tamper_detected event must have been recorded.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/quarantine/events?action=tamper_detected")
		var payload struct {
			Items []struct {
				Package string `json:"package"`
				Action  string `json:"action"`
			} `json:"items"`
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = json.Unmarshal(raw, &payload)
		if len(payload.Items) > 0 && payload.Items[0].Action == "tamper_detected" {
			if !strings.Contains(payload.Items[0].Package, "tamperpkg") {
				t.Errorf("event package = %q", payload.Items[0].Package)
			}
			return // success
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tamper_detected event; last response: %s", raw)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
```

- [ ] **Step 4: Run the integration suite**

Run: `go test -tags integration ./tests/integration/ -run TestTamper -count=1 -v`
Expected: PASS. If it flakes on timing, the background refresh may not have fired — confirm `ttl_blob = "2s"` and `immutable_threshold_seconds = 1` are both in the generated config (print it from `writeTestConfig` if needed).

- [ ] **Step 5: Run the WHOLE integration suite (no regressions)**

Run: `go test -tags integration ./tests/integration/ -count=1`
Expected: `ok` — the shortened `ttl_blob` must not break other adapters' tests (they assert 200s / cache behavior, not TTL length).

If any pre-existing test now fails because of the 2s blob TTL, revert `ttl_blob` to `72h` and instead give ONLY the tamper test its own longer sleep with `immutable_threshold_seconds = 1` and a per-test short TTL is not possible via global config — in that case keep `ttl_blob` moderate (e.g. `"3s"`) and confirm the cache-hit tests tolerate it (they fetch twice quickly, well within 3s).

- [ ] **Step 6: Commit**

```bash
git add tests/mock/upstream_server.go tests/integration/tamper_test.go tests/integration/main_test.go internal/config/config.go internal/server/server.go
git commit -m "test(tamper): e2e upstream-swap → event + first-seen bytes kept"
```

---

## Task 9: Frontend badge + i18n

**Files:**
- Modify: `web/src/admin/pages/Quarantine.tsx` (`ACTIONS` array + `actionBadge`)
- Modify: `web/src/i18n/zh.ts`, `web/src/i18n/en.ts` (`quarantine.action.tamper_detected`)

**Interfaces:**
- Consumes: existing `actionBadge(action, t)` switch + `ACTIONS` filter list + `BadgeV2`.

- [ ] **Step 1: Add the action to the filter list + badge**

In `web/src/admin/pages/Quarantine.tsx`, add `'tamper_detected'` to the `ACTIONS` array (after `'malware_blocked'`):

```ts
const ACTIONS = ['blocked', 'malware_blocked', 'tamper_detected', 'served_eligible', 'bypassed', 'malware_bypassed', 'approved', 'approval_revoked', 'override_created', 'override_revoked']
```

In the `actionBadge` switch, add a case (after the `malware_blocked` case):

```tsx
    case 'tamper_detected':
      return <BadgeV2 variant="error">{t('quarantine.action.tamper_detected')}</BadgeV2>
```

- [ ] **Step 2: Add i18n keys (both locales, equal counts)**

In `web/src/i18n/zh.ts`, in `quarantine.action`, after `malware_blocked`:

```ts
        tamper_detected: '内容篡改',
```

In `web/src/i18n/en.ts`, in `quarantine.action`, after `malware_blocked`:

```ts
        tamper_detected: 'Tampered',
```

- [ ] **Step 3: Verify type-check + i18n audit**

Run: `cd web && npm run type-check && cd .. && python3 scripts/i18n-audit.py`
Expected: type-check clean; audit prints "all keys defined in both locales".

- [ ] **Step 4: Commit**

```bash
git add web/src/admin/pages/Quarantine.tsx web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "feat(admin): tamper_detected event badge + i18n"
```

---

## Task 10: Docs

**Files:**
- Modify: `CLAUDE.md` (§4.21 decision chain — add tamper as a post-serve integrity step)
- Modify: `CHANGELOG.md` (Unreleased → Added)

- [ ] **Step 1: CLAUDE.md**

In `CLAUDE.md`, in the §4.21 quarantine section, after the decision-chain bullet, add a new bullet:

```markdown
- **篡改检测（后台刷新时）**：不可变制品首次抓取记 SHA-256 基线（`internal/tamper/`）；
  后台刷新时重抓字节比对基线，不匹配则保留首见字节、不覆盖缓存、写 `tamper_detected`
  事件 + critical webhook（信任首见，告警不阻断）。不可变判定用 TTL 阈值（默认 1h）。
```

- [ ] **Step 2: CHANGELOG.md**

In `CHANGELOG.md`, under `## [Unreleased]` → `### Added`, prepend:

```markdown
- **Tamper detection (DIRECTION T1)**: records the SHA-256 of each
  immutable artifact on first fetch; when a natural re-fetch (background
  refresh) yields different upstream bytes under the same version,
  depsilo keeps the trusted first-seen copy, refuses to cache the new
  content, and fires a critical `tamper_detected` webhook + audit event.
  Alert-only (never blocks); the hash is computed free in the existing
  storage-pump reader; immutability inferred from TTL (no adapter
  changes). `[supply_chain.tamper_detection] enabled` (default true).
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md CHANGELOG.md
git commit -m "docs: tamper detection in CLAUDE.md + changelog"
```

---

## Final Verification (run before considering the plan done)

- [ ] `go build ./...` — clean.
- [ ] `go test ./... -race` — all green (unit).
- [ ] `go test -tags integration ./tests/integration/ -count=1` — all green (e2e).
- [ ] `cd web && npm run type-check && npm run build` — clean.
- [ ] `python3 scripts/i18n-audit.py` — locales in sync.
- [ ] `gofmt -l internal/ tests/` — no unformatted files (run `gofmt -w` on any listed).

---

## Self-Review Notes (author)

- **Spec coverage:** hashing (T1), model+migration (T2), constants (T3), interface (T4), recorder three-state (T5), manager wiring incl. verify-not-overwrite (T6), config+webhook (T7), e2e (T8), UI (T9), docs (T10). All spec sections mapped.
- **The immutable-threshold-config wrinkle (T8 step 2):** the spec hard-codes 1h; the integration test needs a lower value to trigger a refresh within test time, so the threshold became config-overridable (`immutable_threshold_seconds`, default 1h). This is a small, justified addition surfaced by making the feature testable — noted here so a reviewer sees it wasn't in the original spec.
- **Type consistency:** `TamperRecorder.Verify` returns `cache.TamperResult{KnownMismatch}` everywhere; `Record`/`Verify` signatures identical across the interface (T4), the concrete recorder (T5), the fake (T6), and the manager call sites (T6). `NewManager` new trailing param is applied at its single production call site (T6 step 6).
