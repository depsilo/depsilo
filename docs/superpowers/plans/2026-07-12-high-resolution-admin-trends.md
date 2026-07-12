# High-Resolution Admin Trends Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Admin trends at 10-second, 5-minute, 30-minute, and 2-hour resolution with bounded payloads and range-aware near-real-time refresh.

**Architecture:** Add an eight-day five-minute access-log rollup maintained by the existing non-blocking recorder and transactionally backfilled from recent raw logs. The trends handler selects raw, five-minute, or hourly data by range and emits at most 360 UTC-aligned points; React renders those points directly with responsive tick sampling and range-specific polling.

**Tech Stack:** Go 1.24, Gin, GORM, SQLite, React 19, TypeScript, TanStack Query, Recharts 3, Playwright.

## Global Constraints

- Keep `GET /api/v1/admin/dashboard/trends?range=...` and its JSON point shape backward compatible.
- Return at most 360 points for every supported range.
- Use UTC for storage and bucket alignment; format time in the browser timezone.
- Keep recorder ingestion non-blocking and keep raw access-log writes enabled.
- Retain five-minute rollups for exactly eight days independently of long-term rollup retention.
- Do not add a frontend or backend dependency.
- Preserve the last successful chart and stale-data warning when background refresh fails.
- Follow strict red-green-refactor: every behavioral edit starts with a failing focused test.

---

### Task 1: Persist Five-Minute Rollups From The Batched Recorder

**Files:**
- Modify: `internal/db/models.go`
- Modify: `internal/db/repository.go`
- Modify: `internal/accesslog/event.go`
- Modify: `internal/accesslog/upsert.go`
- Modify: `internal/accesslog/recorder.go`
- Test: `internal/accesslog/event_test.go`
- Test: `internal/accesslog/recorder_test.go`

**Interfaces:**
- Produces: `db.AccessLogFiveMinutely`, table `access_log_five_minutely`.
- Produces: `Event.FiveMinuteKey() fiveMinuteKey` with UTC-aligned `BucketStart int64`.
- Produces: `upsertFiveMinutely(context.Context, *gorm.DB, map[fiveMinuteKey]*counters) error`.
- Preserves: `Recorder.Record`, `Flush`, and `Close` signatures.

- [ ] **Step 1: Write the failing key and recorder tests**

Add to `event_test.go`:

```go
func TestEvent_FiveMinuteKey_AlignsInUTC(t *testing.T) {
	e := Event{AdapterType: "pypi", Hit: true, Upstream: "tuna", At: mustParse(t, "2026-07-12T18:27:49+08:00")}
	got := e.FiveMinuteKey()
	want := mustParse(t, "2026-07-12T10:25:00Z").Unix()
	if got.BucketStart != want || got.AdapterType != "pypi" || !got.Hit || got.Upstream != "tuna" {
		t.Fatalf("FiveMinuteKey() = %+v, want bucket=%d pypi hit tuna", got, want)
	}
}

func TestEvent_FiveMinuteKey_SeparatesBoundary(t *testing.T) {
	a := Event{At: mustParse(t, "2026-07-12T10:29:59Z")}.FiveMinuteKey()
	b := Event{At: mustParse(t, "2026-07-12T10:30:00Z")}.FiveMinuteKey()
	if b.BucketStart-a.BucketStart != 300 { t.Fatalf("bucket delta = %d", b.BucketStart-a.BucketStart) }
}
```

Migrate `db.AccessLogFiveMinutely` in `newTestDB`. Extend `TestRecorder_BatchedFlush_WritesRawAndRollup` to expect three five-minute dimension rows. Add an accumulation test that records three events, flushes, records four events in the same five-minute bucket, flushes, and expects one row with `RequestCount=7`, `TotalBytes=700`, and bucket `2026-07-12T10:00:00Z`.

- [ ] **Step 2: Verify RED**

Run `go test ./internal/accesslog -run 'FiveMinute|BatchedFlush' -count=1`.

Expected: compilation fails because the model and key do not exist.

- [ ] **Step 3: Add the model and migration**

Add to `models.go` and register it in `db.AutoMigrate` after `AccessLog`:

```go
type AccessLogFiveMinutely struct {
	BucketStart  int64     `gorm:"primaryKey" json:"bucket_start"`
	AdapterType  string    `gorm:"size:16;primaryKey" json:"adapter_type"`
	Hit          bool      `gorm:"primaryKey" json:"hit"`
	Upstream     string    `gorm:"size:128;primaryKey;default:''" json:"upstream"`
	RequestCount int64     `json:"request_count"`
	TotalBytes   int64     `json:"total_bytes"`
	SumLatencyMs int64     `json:"sum_latency_ms"`
	ErrorCount   int64     `json:"error_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AccessLogFiveMinutely) TableName() string { return "access_log_five_minutely" }
```

- [ ] **Step 4: Add key derivation and idempotent accumulation**

Add to `event.go`:

```go
type fiveMinuteKey struct { BucketStart int64; AdapterType string; Hit bool; Upstream string }

func (e Event) FiveMinuteKey() fiveMinuteKey {
	unix := e.At.UTC().Unix()
	return fiveMinuteKey{BucketStart: (unix / 300) * 300, AdapterType: e.AdapterType, Hit: e.Hit, Upstream: e.Upstream}
}
```

Implement `upsertFiveMinutely` with the same additive conflict semantics as the hourly table:

```go
func upsertFiveMinutely(ctx context.Context, gdb *gorm.DB, m map[fiveMinuteKey]*counters) error {
	if len(m) == 0 { return nil }
	rows := make([]db.AccessLogFiveMinutely, 0, len(m))
	now := time.Now().UTC()
	for key, count := range m {
		rows = append(rows, db.AccessLogFiveMinutely{
			BucketStart: key.BucketStart, AdapterType: key.AdapterType, Hit: key.Hit, Upstream: key.Upstream,
			RequestCount: count.RequestCount, TotalBytes: count.TotalBytes,
			SumLatencyMs: count.SumLatencyMs, ErrorCount: count.ErrorCount, UpdatedAt: now,
		})
	}
	return gdb.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "bucket_start"}, {Name: "adapter_type"}, {Name: "hit"}, {Name: "upstream"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"request_count": gorm.Expr("access_log_five_minutely.request_count + excluded.request_count"),
			"total_bytes": gorm.Expr("access_log_five_minutely.total_bytes + excluded.total_bytes"),
			"sum_latency_ms": gorm.Expr("access_log_five_minutely.sum_latency_ms + excluded.sum_latency_ms"),
			"error_count": gorm.Expr("access_log_five_minutely.error_count + excluded.error_count"),
			"updated_at": now,
		}),
	}).CreateInBatches(rows, 200).Error
}
```

- [ ] **Step 5: Wire the recorder**

Add `aggFiveMinutely map[fiveMinuteKey]*counters` to `batchedRecorder`, initialize it, accumulate it in `ingest`, and swap it during `flushAll`:

```go
key := e.FiveMinuteKey()
if r.aggFiveMinutely[key] == nil { r.aggFiveMinutely[key] = &counters{} }
r.aggFiveMinutely[key].add(e)
```

Call `upsertFiveMinutely` before `upsertHourly`. Log `failed to upsert five-minute rollup` on failure and continue other writes.

- [ ] **Step 6: Verify GREEN and commit**

Run:

```bash
gofmt -w internal/db/models.go internal/db/repository.go internal/accesslog/event.go internal/accesslog/upsert.go internal/accesslog/recorder.go internal/accesslog/event_test.go internal/accesslog/recorder_test.go
go test ./internal/accesslog ./internal/db -count=1
```

Expected: PASS.

Commit: `git commit -m "feat(accesslog): record five-minute rollups"` with the files above.

---

### Task 2: Backfill And Retain Fine-Grained History

**Files:**
- Modify: `internal/accesslog/backfill.go`
- Test: `internal/accesslog/backfill_test.go`
- Modify: `internal/accesslog/retention.go`
- Test: `internal/accesslog/retention_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `db.AccessLogFiveMinutely` from Task 1.
- Produces: `BackfillFiveMinutely(context.Context, *gorm.DB, time.Time) error`.
- Produces: `RetentionConfig.FiveMinuteDays int`, set to `8` by the server.
- Uses marker key `access_log_five_minutely_v1` in `db.ControlPlaneState`.

- [ ] **Step 1: Write failing backfill and retention tests**

Migrate `db.ControlPlaneState` in `newTestDB`. Test that `BackfillFiveMinutely` includes a one-hour-old raw row, excludes an eight-day-old row, writes the marker, and becomes a no-op after the marker exists. Add a SQLite trigger that rejects marker insertion and assert the transaction leaves no five-minute rows.

Add this retention assertion:

```go
func TestRetention_PrunesFiveMinuteRowsAfterEightDays(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	d.Create(&db.AccessLogFiveMinutely{BucketStart: now.Add(-9*24*time.Hour).Unix(), AdapterType: "pypi"})
	d.Create(&db.AccessLogFiveMinutely{BucketStart: now.Add(-7*24*time.Hour).Unix(), AdapterType: "npm"})
	RunRetention(context.Background(), d, RetentionConfig{FiveMinuteDays: 8})
	var rows []db.AccessLogFiveMinutely
	d.Find(&rows)
	if len(rows) != 1 || rows[0].AdapterType != "npm" { t.Fatalf("rows = %+v", rows) }
}
```

- [ ] **Step 2: Verify RED**

Run `go test ./internal/accesslog -run 'BackfillFiveMinutely|PrunesFiveMinute' -count=1`.

Expected: compilation fails because the function and retention field do not exist.

- [ ] **Step 3: Implement transactional backfill**

Add `fiveMinuteBackfillMarker`. `BackfillFiveMinutely` must first read the marker and return only when it exists. Otherwise execute one GORM transaction that deletes rows in the trailing seven-day window, runs this grouped insert, and saves the marker:

```sql
INSERT INTO access_log_five_minutely
  (bucket_start, adapter_type, hit, upstream, request_count, total_bytes, sum_latency_ms, error_count, updated_at)
SELECT
  (CAST(strftime('%s', created_at) AS INTEGER) / 300) * 300,
  adapter_type, hit, COALESCE(upstream, ''), COUNT(*),
  COALESCE(SUM(bytes_sent), 0), COALESCE(SUM(latency_ms), 0),
  COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0), datetime('now')
FROM access_logs
WHERE created_at >= ?
GROUP BY 1, adapter_type, hit, upstream
```

Use `now.UTC().Add(-7*24*time.Hour)` as the cutoff. Any error must roll back rows and marker together.

- [ ] **Step 4: Wire retention and startup**

Delete fine rows older than `time.Now().UTC().AddDate(0, 0, -FiveMinuteDays).Unix()` when `FiveMinuteDays > 0`. Set `FiveMinuteDays: 8` in `server.go`.

When `BackfillOnStart` is true, call the new backfill after `BackfillIfEmpty` and before `NewRecorder`. Log success/failure and duration; failure must not abort startup.

- [ ] **Step 5: Verify GREEN and commit**

Run:

```bash
gofmt -w internal/accesslog/backfill.go internal/accesslog/backfill_test.go internal/accesslog/retention.go internal/accesslog/retention_test.go internal/server/server.go
go test ./internal/accesslog ./internal/server -count=1
```

Expected: PASS.

Commit: `git commit -m "feat(accesslog): backfill fine-grained trend history"` with the Task 2 files.

---

### Task 3: Serve Bounded Range-Aware Trend Buckets

**Files:**
- Modify: `internal/api/admin/dashboard.go`
- Test: `internal/api/admin/dashboard_test.go`

**Interfaces:**
- Consumes: `access_log_five_minutely` and existing `access_log_hourly`.
- Produces: unchanged `trendPoint` JSON schema.
- Adds private specs: `1h=360x10s`, `24h=288x5m`, `7d=336x30m`, `30d=360x2h`.
- Adds private `DashboardHandler.now func() time.Time`, initialized to `time.Now`.

- [ ] **Step 1: Write failing range and aggregation tests**

Add a test helper that migrates `AccessLog`, `AccessLogFiveMinutely`, and `AccessLogHourly`, creates a rollup-enabled handler, and fixes `handler.now` at `2026-07-12T12:34:56Z`.

Add this table test:

```go
func TestDashboardTrends_ReturnsExpectedResolutionForEveryRange(t *testing.T) {
	tests := []struct { rangeParam string; wantPoints int; wantStep int64 }{
		{"1h", 360, 10}, {"24h", 288, 300}, {"7d", 336, 1800}, {"30d", 360, 7200},
	}
	for _, tt := range tests {
		t.Run(tt.rangeParam, func(t *testing.T) {
			_, router := newTrendsTestHandler(t)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/trends?range="+tt.rangeParam, nil))
			var body struct { Points []trendPoint `json:"points"` }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil { t.Fatal(err) }
			if len(body.Points) != tt.wantPoints { t.Fatalf("points=%d want=%d", len(body.Points), tt.wantPoints) }
			for i := 1; i < len(body.Points); i++ {
				if body.Points[i].Bucket-body.Points[i-1].Bucket != tt.wantStep { t.Fatalf("step at %d", i) }
			}
		})
	}
}
```

Add tests that insert hit and miss dimensions into the current partial bucket and assert request totals, bytes, errors, hit rate, and weighted latency. Add fallback cases: empty five-minute data makes `24h` use raw five-minute grouping and `7d` return 168 hourly points.

- [ ] **Step 2: Verify RED**

Run `go test ./internal/api/admin -run 'DashboardTrends' -count=1`.

Expected: FAIL because existing ranges return coarse points and the clock is not injectable.

- [ ] **Step 3: Add deterministic range specs and zero filling**

Add:

```go
type trendSpec struct { buckets int; interval time.Duration }

var trendSpecs = map[string]trendSpec{
	"1h":  {buckets: 360, interval: 10 * time.Second},
	"24h": {buckets: 288, interval: 5 * time.Minute},
	"7d":  {buckets: 336, interval: 30 * time.Minute},
	"30d": {buckets: 360, interval: 2 * time.Hour},
}
```

For each spec, compute `end := h.now().UTC().Truncate(spec.interval)` and `start := end.Add(-time.Duration(spec.buckets-1)*spec.interval)`. A shared builder iterates exactly `spec.buckets` times, looks up aggregate rows by Unix bucket start, and emits zero-valued points for gaps. Keep invalid range fallback as `7d`.

- [ ] **Step 4: Implement source-specific queries**

Use this routing:

```go
switch rangeParam {
case "1h":
	points = h.trendsRaw(spec)
case "24h":
	points = h.trendsFiveMinutely(spec)
case "30d":
	points = h.trendsHourlyGrouped(spec)
default:
	points = h.trendsSevenDays(spec)
}
```

Raw grouping uses `(strftime('%s', created_at) / intervalSec) * intervalSec`. Five-minute grouping uses `(bucket_start / intervalSec) * intervalSec`. Hourly grouping reconstructs Unix time from `date` and `hour`, then groups pairs into two-hour buckets. All paths sum the same counters and use `trendHourBucket.toPoint` for rates and weighted averages.

Before `24h`/`7d`, check whether the five-minute table has a row in the requested window. If absent, use indexed raw grouping for `24h` and 168 hourly buckets for `7d`. With `useRollup=false`, preserve raw grouping for all ranges.

- [ ] **Step 5: Verify GREEN and commit**

Run:

```bash
gofmt -w internal/api/admin/dashboard.go internal/api/admin/dashboard_test.go
go test ./internal/api/admin ./internal/api ./internal/accesslog ./internal/server -count=1
```

Expected: PASS.

Commit: `git commit -m "feat(admin): serve high-resolution trend buckets"` with the Task 3 files.

---

### Task 4: Render Full-Resolution Responsive Trends

**Files:**
- Modify: `web/src/admin/components/TrendsCard.tsx`
- Modify: `web/src/admin/pages/Dashboard.tsx`
- Test: `web/e2e/admin-query-states.spec.ts`
- Test: `web/e2e/admin-contrast.spec.ts`
- Test: `web/e2e/admin-visual-matrix.spec.ts`

**Interfaces:**
- Consumes: unchanged `DashboardTrendsResponse.points`.
- Produces: range refresh values `5000`, `15000`, `30000`, and `60000` milliseconds.
- Preserves: `TrendsCard({ raw, range, onRangeChange })`.

- [ ] **Step 1: Write failing polling and rendering tests**

Use Playwright's paused clock in `admin-query-states.spec.ts`. Count trend requests and assert `1h` refetches at 5,000 ms; after selecting `24h`, assert no second call at 14,999 ms and a second call at 15,000 ms. Preserve the existing stale-chart test.

Supply non-empty points in `admin-contrast.spec.ts` and assert series paths contain no cubic `C` command:

```ts
const paths = page.locator('[data-query-key="dashboard-trends"] .recharts-area-curve, [data-query-key="dashboard-trends"] .recharts-line-curve')
for (let i = 0; i < await paths.count(); i += 1) {
  await expect(paths.nth(i)).not.toHaveAttribute('d', /C/)
}
```

Add 1280x800 and 390x844 visual-matrix cases with 360 points. Assert chart height is at least 220 px, X-axis visible tick count is between 3 and 8, and `document.documentElement.scrollWidth <= window.innerWidth`.

- [ ] **Step 2: Verify RED**

Run:

```bash
cd web
npm run test:e2e -- --grep 'trend|Trend'
```

Expected: polling, linear path, or height assertions fail.

- [ ] **Step 3: Remove browser-side daily rebucketing**

Delete `dayKey`, `dayStartUnix`, and `rebucketDays`. Keep all API points:

```ts
const points = useMemo<ChartPoint[]>(() => {
  const granularity = range === '1h' ? 'minute' : range === '30d' ? 'day' : 'hour'
  return raw.map(point => toChartPoint(point, granularity))
}, [raw, range])
```

Import `useMediaQuery` from `@/hooks/useMediaQuery` and call `useMediaQuery('(max-width: 640px)')`. Use a numeric X axis with `dataKey="bucket"`, `type="number"`, `domain={['dataMin', 'dataMax']}`, a `fmtTime` tick formatter, and `minTickGap`. Set `tickCount` to 4 on mobile and 8 on desktop. Format tooltip labels from numeric bucket values in local time.

- [ ] **Step 4: Improve fidelity and polling**

Set chart height to 240 px. Change every trend `Area` and `Line` from `type="monotone"` to `type="linear"`; keep line dots disabled and add `isAnimationActive={false}` to every series.

In `Dashboard.tsx`, add:

```ts
const TREND_REFRESH_INTERVAL: Record<TrendsRange, number> = {
  '1h': 5_000,
  '24h': 15_000,
  '7d': 30_000,
  '30d': 60_000,
}
```

Use `refetchInterval: TREND_REFRESH_INTERVAL[range]`. Preserve the range query key, focus refetch, retry setting, and stale-data UI.

- [ ] **Step 5: Verify GREEN and commit**

Run:

```bash
cd web
npm run type-check
npm run type-check:e2e
npx eslint src/admin/components/TrendsCard.tsx src/admin/pages/Dashboard.tsx e2e/admin-query-states.spec.ts e2e/admin-contrast.spec.ts e2e/admin-visual-matrix.spec.ts
npm run test:e2e -- --grep 'trend|Trend'
```

Expected: PASS.

Commit: `git commit -m "feat(admin): render fine-grained live trends"` with the Task 4 files.

---

### Task 5: Full Verification And Operations Documentation

**Files:**
- Modify: `docs/self-test-checklist.md`
- Modify only if existing text requires reconciliation: `README.md`

**Interfaces:**
- Validates preceding tasks; introduces no runtime interface.

- [ ] **Step 1: Add the operator check**

Add under Admin dashboard checks:

```markdown
- [ ] 趋势图粒度：1h/24h/7d/30d 分别返回约 360/288/336/360 个点；持续请求时 1h 图在 5 秒内更新，移动端无横向溢出。
```

Only update `README.md` if it already states trend granularity; replace that statement rather than adding a marketing section.

- [ ] **Step 2: Run complete backend verification**

Run `go test ./... -count=1`.

Expected: PASS.

- [ ] **Step 3: Run complete frontend verification**

Run:

```bash
cd web
npm run type-check
npm run type-check:e2e
npm run build
npm run test:e2e
npm run lint
```

Expected: types, build, and Playwright pass. Full lint either passes or reports only documented unrelated baseline errors; every touched file must pass Task 4's focused lint.

- [ ] **Step 4: Verify live data and screenshots**

Run backend at `127.0.0.1:23333` and Vite at `127.0.0.1:4173`. Generate requests through a local adapter, authenticate, then request:

```bash
curl -fsS 'http://127.0.0.1:23333/api/v1/admin/dashboard/trends?range=1h' -H "Authorization: Bearer $DEPSILO_TOKEN"
```

Expected: exactly 360 entries, 10-second bucket steps, and the newest request appears after the next recorder flush.

Capture and inspect Playwright screenshots at 1280x800, 390x844, and 320x720. Confirm the chart is nonblank, labels and tooltips do not overlap, timestamps are local, and there is no horizontal overflow.

- [ ] **Step 5: Review and commit documentation**

Run `git diff --check`, `git status --short`, and `git diff --stat`. Commit intended documentation as `docs: document high-resolution trend checks`; omit `README.md` when unchanged.
