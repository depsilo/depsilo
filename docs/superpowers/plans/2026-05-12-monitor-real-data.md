# Monitor Page Real Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all fake/seeded sparkline data on the Monitor page with real time-series from the backend, and replace upstream sparklines with Statuspage-style colored status bars.

**Architecture:** Extend the public `GET /api/v1/stats` endpoint to include two new fields: `series` (12 points of 5-minute AccessLog aggregates for KPI sparklines) and `latency_series` per upstream (12 points of 5-minute UpstreamLatencyLog ping aggregates). Frontend replaces `KPI_SERIES`/`genSeries` with API data, and replaces upstream Sparkline with a new StatusBar component.

**Tech Stack:** Go (Gin + GORM), React 18 + TypeScript, TanStack Query

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/api/public/stats.go` | Add `series` and per-upstream `latency_series` to stats response |
| Create | `web/src/components/StatusBar.tsx` | 12-bar Statuspage-style colored status bar with hover tooltip |
| Modify | `web/src/portal/pages/Monitor.tsx` | Replace fake data imports with real API series; replace upstream Sparkline with StatusBar |
| Modify | `web/src/components/HeroSparkline.tsx` | Update time labels from hardcoded `-60m` to dynamic based on 12 points (every 5 min) |

Files **not** modified (but referenced):
- `web/src/components/Sparkline.tsx` — still used by StatStrip KPI cards (data source changes, component stays)
- `web/src/lib/ecosystemData.ts` — `KPI_SERIES` and `genSeries` stay (used by other pages), just no longer imported by Monitor
- `web/src/lib/api.ts` — no change needed (`statsApi.getStats()` already fetches `/stats`)

---

### Task 1: Backend — Add KPI time series to /api/v1/stats

**Files:**
- Modify: `internal/api/public/stats.go:56-265`

- [ ] **Step 1: Add the series aggregation query after the existing today stats block**

Add this code after line 76 (after the `hitRate` calculation), before the cache stats block:

```go
// ── 5-minute KPI series (last 1 hour) ──
type seriesPoint struct {
	Bucket       int64   `json:"time_unix"`
	Requests     int64   `json:"requests"`
	Hits         int64   `json:"hits"`
	HitRate      float64 `json:"hit_rate"`
	BytesSaved   int64   `json:"bytes_saved"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
}

oneHourAgo := now.Add(-1 * time.Hour)
const bucketSec = 300 // 5 minutes

type rawBucket struct {
	Bucket     int64
	Requests   int64
	Hits       int64
	SavedBytes int64
	SumLatency int64
	MissCount  int64
}
var rawBuckets []rawBucket

h.db.Model(&db.AccessLog{}).
	Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
		COUNT(*) AS requests,
		SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END) AS hits,
		COALESCE(SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END), 0) AS saved_bytes,
		COALESCE(SUM(CASE WHEN hit = 0 THEN latency_ms ELSE 0 END), 0) AS sum_latency,
		SUM(CASE WHEN hit = 0 THEN 1 ELSE 0 END) AS miss_count`,
		bucketSec, bucketSec).
	Where("created_at >= ?", oneHourAgo).
	Group("bucket").
	Order("bucket ASC").
	Find(&rawBuckets)

// Build 12 time-aligned buckets
seriesPoints := make([]seriesPoint, 0, 12)
bucketMap := make(map[int64]*rawBucket, len(rawBuckets))
for i := range rawBuckets {
	bucketMap[rawBuckets[i].Bucket] = &rawBuckets[i]
}

startBucket := (oneHourAgo.Unix() / int64(bucketSec)) * int64(bucketSec)
for i := 0; i < 12; i++ {
	bk := startBucket + int64(i)*int64(bucketSec)
	pt := seriesPoint{
		Bucket: bk,
	}
	if rb, ok := bucketMap[bk]; ok {
		pt.Requests = rb.Requests
		pt.Hits = rb.Hits
		if rb.Requests > 0 {
			pt.HitRate = float64(rb.Hits) / float64(rb.Requests)
		}
		pt.BytesSaved = rb.SavedBytes
		if rb.MissCount > 0 {
			pt.AvgLatencyMs = rb.SumLatency / rb.MissCount
		}
	}
	seriesPoints = append(seriesPoints, pt)
}
```

- [ ] **Step 2: Add the series field to the JSON response**

Change the `c.JSON(http.StatusOK, gin.H{...})` block at the end of GetStats. Add `"series"` after `"extra_indexes"`:

```go
"series": gin.H{
	"interval_minutes": 5,
	"points":           seriesPoints,
},
```

- [ ] **Step 3: Verify it compiles and returns data**

Run: `go build ./cmd/depsilo && curl -s http://localhost:23333/api/v1/stats | jq '.series'`

Expected: JSON object with `interval_minutes: 5` and `points` array of 12 objects.

- [ ] **Step 4: Commit**

```bash
git add internal/api/public/stats.go
git commit -m "feat(stats): add 5-minute KPI time series to /api/v1/stats"
```

---

### Task 2: Backend — Add per-upstream latency series to /api/v1/stats

**Files:**
- Modify: `internal/api/public/stats.go`

- [ ] **Step 1: Add the upstream latency series query**

Add a helper function above `GetStats`:

```go
type upstreamLatencyPoint struct {
	Time      time.Time `json:"time"`
	LatencyMs int64     `json:"latency_ms"`
	Healthy   bool      `json:"healthy"`
	Requests  int64     `json:"requests"`
}

func (h *StatsHandler) upstreamLatencySeries(name string, since time.Time) []upstreamLatencyPoint {
	const bucketSec = 300

	type rawLP struct {
		Bucket      int64
		AvgLatency  int64
		HealthyPct  float64
		CheckCount  int64
	}
	var raw []rawLP

	h.db.Model(&db.UpstreamLatencyLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			AVG(latency_ms) AS avg_latency,
			AVG(CASE WHEN healthy = 1 THEN 1.0 ELSE 0.0 END) AS healthy_pct,
			COUNT(*) AS check_count`,
			bucketSec, bucketSec).
		Where("name = ? AND created_at >= ?", name, since).
		Group("bucket").
		Order("bucket ASC").
		Find(&raw)

	lpMap := make(map[int64]*rawLP, len(raw))
	for i := range raw {
		lpMap[raw[i].Bucket] = &raw[i]
	}

	startBucket := (since.Unix() / int64(bucketSec)) * int64(bucketSec)
	points := make([]upstreamLatencyPoint, 0, 12)
	for i := 0; i < 12; i++ {
		bk := startBucket + int64(i)*int64(bucketSec)
		pt := upstreamLatencyPoint{
			Time: time.Unix(bk, 0).UTC(),
		}
		if r, ok := lpMap[bk]; ok {
			pt.LatencyMs = r.AvgLatency
			pt.Healthy = r.HealthyPct > 0.5
			pt.Requests = r.CheckCount
		}
		points = append(points, pt)
	}
	return points
}
```

- [ ] **Step 2: Add `latency_series` to each upstream entry in the response**

In `GetStats`, extract `oneHourAgo` to be computed once (it's already there from Task 1). Then modify the upstream append pattern. Replace each `upstreams = append(upstreams, gin.H{...})` block to include the new field. The pattern for each pool (example for pypi):

```go
for _, u := range h.pypiPool.Upstreams() {
	upstreams = append(upstreams, gin.H{
		"name":            u.Name,
		"adapter":         "pypi",
		"url":             u.URL,
		"healthy":         u.Healthy,
		"avg_latency_ms":  u.AvgLatency().Milliseconds(),
		"success_rate":    u.SuccessRate(),
		"latency_series":  h.upstreamLatencySeries(u.Name, oneHourAgo),
	})
}
```

Apply the same `"latency_series": h.upstreamLatencySeries(u.Name, oneHourAgo)` line to all 12 pool loops (apt, npm, go, cargo, maven, rubygems, composer, nuget, conda, cran, helm).

- [ ] **Step 3: Verify**

Run: `go build ./cmd/depsilo && curl -s http://localhost:23333/api/v1/stats | jq '.upstreams[0].latency_series'`

Expected: Array of 12 objects with `time`, `latency_ms`, `healthy`, `requests`.

- [ ] **Step 4: Commit**

```bash
git add internal/api/public/stats.go
git commit -m "feat(stats): add per-upstream ping latency series to /api/v1/stats"
```

---

### Task 3: Frontend — Create StatusBar component

**Files:**
- Create: `web/src/components/StatusBar.tsx`

- [ ] **Step 1: Create the StatusBar component**

```tsx
// web/src/components/StatusBar.tsx
import { useState } from 'react'

interface LatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
  requests: number
}

interface Props {
  points: LatencyPoint[]
}

function barColor(pt: LatencyPoint): string {
  if (pt.requests === 0) return 'var(--border)'           // grey — no data
  if (!pt.healthy) return 'var(--danger)'                   // red — failed
  if (pt.latency_ms > 500) return 'var(--danger)'           // red — very slow
  if (pt.latency_ms > 100) return 'var(--warn)'             // yellow — degraded
  return 'var(--ok)'                                        // green — healthy
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export default function StatusBar({ points }: Props) {
  const [hover, setHover] = useState<number | null>(null)

  if (!points || points.length === 0) return null

  return (
    <div style={{ position: 'relative', display: 'flex', gap: 1.5, alignItems: 'center', height: 22 }}>
      {points.map((pt, i) => (
        <div
          key={i}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(null)}
          style={{
            flex: 1,
            height: '100%',
            borderRadius: 2,
            background: barColor(pt),
            opacity: hover !== null && hover !== i ? 0.5 : 1,
            transition: 'opacity 120ms',
            cursor: 'pointer',
          }}
        />
      ))}
      {hover !== null && points[hover] && (
        <div
          style={{
            position: 'absolute',
            bottom: '100%',
            left: `${(hover / points.length) * 100}%`,
            transform: 'translateX(-50%)',
            marginBottom: 6,
            padding: '5px 8px',
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border)',
            borderRadius: 6,
            fontSize: 10,
            whiteSpace: 'nowrap',
            zIndex: 10,
            boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
            display: 'flex',
            flexDirection: 'column',
            gap: 2,
          }}
        >
          <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
            {formatTime(points[hover].time)}
          </span>
          <span style={{ color: 'var(--text)' }}>
            {points[hover].requests === 0
              ? 'No data'
              : `${points[hover].latency_ms}ms · ${points[hover].healthy ? 'healthy' : 'unhealthy'}`}
          </span>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd web && npx tsc --noEmit`

Expected: No errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/StatusBar.tsx
git commit -m "feat(ui): add StatusBar component for upstream latency visualization"
```

---

### Task 4: Frontend — Update HeroSparkline time labels

**Files:**
- Modify: `web/src/components/HeroSparkline.tsx:34`

- [ ] **Step 1: Update time labels for 12-point / 5-minute intervals**

Replace line 34:

```tsx
// old
const timeLabels = ['−60m', '−45m', '−30m', '−15m', 'now']
```

with:

```tsx
const timeLabels = ['−60m', '−45m', '−30m', '−15m', 'now']
```

Actually the labels are still correct for a 60-minute window — 5 evenly-spaced labels for the 12-point range. No change needed to labels, but the component must handle 12 points gracefully. It already does (it maps any `values.length >= 2`). **Skip this step — no modification needed.**

---

### Task 5: Frontend — Wire Monitor page to real API data

**Files:**
- Modify: `web/src/portal/pages/Monitor.tsx`

- [ ] **Step 1: Update TypeScript interfaces to match new API response**

Replace the existing `UpstreamInfo` and `StatsData` interfaces at the top of the file:

```tsx
interface LatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
  requests: number
}

interface UpstreamInfo {
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  latency_series?: LatencyPoint[]
}

interface SeriesPoint {
  time_unix: number
  requests: number
  hits: number
  hit_rate: number
  bytes_saved: number
  avg_latency_ms: number
}

interface StatsData {
  service: { status: string }
  today: {
    total_requests: number
    hit_count: number
    miss_count: number
    hit_rate: number
    bytes_served: number
    bytes_saved: number
  }
  cache: { total_files: number; total_size_bytes: number }
  upstreams: UpstreamInfo[]
  series?: {
    interval_minutes: number
    points: SeriesPoint[]
  }
}
```

- [ ] **Step 2: Update imports — remove fake data, add StatusBar**

Replace the import block:

```tsx
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import StatusDot from '@/components/StatusDot'
import Sparkline from '@/components/Sparkline'
import HeroSparkline from '@/components/HeroSparkline'
import StatusBar from '@/components/StatusBar'
import type { MirrorStatus } from '@/lib/ecosystemData'
```

Note: `KPI_SERIES` and `genSeries` are **removed** from imports.

- [ ] **Step 3: Update HitRateHero to accept and use real series data**

Replace the `HitRateHero` component:

```tsx
function HitRateHero({ hitRate, series }: { hitRate: number; series: SeriesPoint[] }) {
  const { t } = useTranslation()
  const displayRate = (hitRate * 100).toFixed(1)

  // Extract hit_rate values for the sparkline (fallback to empty)
  const sparkValues = series.length > 0 ? series.map(p => p.hit_rate) : [0, 0]

  return (
    <div
      className="card aurora-rim"
      style={{
        padding: '28px 32px',
        display: 'grid',
        gridTemplateColumns: '320px 1fr',
        alignItems: 'stretch',
        gap: 32,
        minHeight: 168,
        position: 'relative',
        overflow: 'hidden',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <div>
          <div className="eyebrow" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <StatusDot status="healthy" live />
            <span>{t('monitor.hitRateToday')}</span>
          </div>
          <div
            className="aurora-glow"
            style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 14 }}
          >
            <span
              className="grad-text-aurora"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 76,
                fontWeight: 600,
                letterSpacing: '-0.06em',
                lineHeight: 1,
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              {displayRate}
            </span>
            <span style={{ fontSize: 22, color: 'var(--text-soft)' }}>%</span>
          </div>
        </div>
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          position: 'relative',
          borderLeft: '0.5px solid var(--border)',
          paddingLeft: 32,
        }}
      >
        <HeroSparkline values={sparkValues} />
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Update StatStrip to use real series data**

Replace the `StatStrip` component:

```tsx
function StatStrip({ data, upstreams, series }: { data: StatsData['today']; upstreams: UpstreamInfo[]; series: SeriesPoint[] }) {
  const { t } = useTranslation()
  const reqFmt   = formatRequests(data.total_requests)
  const savedFmt = formatBytes(data.bytes_saved)

  const healthyLatencies = upstreams.filter(u => u.healthy && u.avg_latency_ms > 0).map(u => u.avg_latency_ms)
  const p50Ms = healthyLatencies.length > 0
    ? Math.round(healthyLatencies.reduce((a, b) => a + b, 0) / healthyLatencies.length)
    : null

  // Extract sparkline data from real series (fallback to [0,0] for minimum 2 points)
  const reqSeries     = series.length > 0 ? series.map(p => p.requests) : [0, 0]
  const savedSeries   = series.length > 0 ? series.map(p => p.bytes_saved) : [0, 0]
  const latencySeries = series.length > 0 ? series.map(p => p.avg_latency_ms) : [0, 0]

  const items = [
    { label: t('monitor.requests'),       value: reqFmt.value,                          unit: reqFmt.unit,                          tone: 'brand' as const, series: reqSeries },
    { label: t('monitor.bandwidthSaved'), value: savedFmt.value,                        unit: savedFmt.unit,                        tone: 'brand' as const, series: savedSeries },
    { label: t('monitor.avgLatency'),     value: p50Ms !== null ? String(p50Ms) : '—',  unit: p50Ms !== null ? 'ms' : '',            tone: 'neutral' as const, series: latencySeries },
  ]

  return (
    <div className="card" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 0, padding: 0 }}>
      {items.map((it, i) => (
        <div
          key={it.label}
          style={{
            padding: '16px 20px',
            borderRight: i < items.length - 1 ? '0.5px solid var(--border)' : 'none',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{it.label}</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8 }}>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
              <span style={{ fontFamily: 'var(--font-mono)', fontSize: 32, fontWeight: 600, letterSpacing: '-0.04em', fontVariantNumeric: 'tabular-nums' }}>
                {it.value}
              </span>
              <span style={{ fontSize: 13, color: 'var(--text-soft)' }}>{it.unit}</span>
            </div>
            <Sparkline data={it.series} width={120} height={26} tone={it.tone} />
          </div>
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 5: Update MirrorTile to use StatusBar**

Replace the `MirrorTile` component and remove the `upstreamSeries` function:

```tsx
function MirrorTile({ upstream }: { upstream: UpstreamInfo }) {
  const status = mirrorStatus(upstream)
  const isFailed = status === 'failed'

  return (
    <div
      className="row-hover"
      style={{
        display: 'grid',
        gridTemplateColumns: '50px 1fr 120px',
        alignItems: 'center',
        gap: 12,
        padding: '10px 14px',
        borderBottom: '0.5px solid var(--border)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot status={status} />
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text)' }}>
          {upstream.adapter}
        </span>
      </div>
      <div style={{ minWidth: 0 }}>
        <div
          className="mono"
          style={{
            fontSize: 12,
            color: isFailed ? 'var(--text-subtle)' : 'var(--text)',
            textDecoration: isFailed ? 'line-through' : 'none',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {(upstream.url || upstream.name).replace(/^https?:\/\//, '')}
        </div>
        <div style={{ display: 'flex', gap: 14, marginTop: 2, fontSize: 11 }}>
          <span style={{ color: 'var(--text-subtle)' }}>
            P50{' '}
            <span
              className="num"
              style={{
                color: isFailed ? 'var(--text-subtle)' : upstream.avg_latency_ms > 100 ? 'var(--warn-text)' : 'var(--text-muted)',
              }}
            >
              {isFailed ? '—' : `${upstream.avg_latency_ms}ms`}
            </span>
          </span>
          <span style={{ color: 'var(--text-subtle)' }}>
            hit{' '}
            <span className="num" style={{ color: isFailed ? 'var(--text-subtle)' : 'var(--text-muted)' }}>
              {isFailed ? '—' : `${(upstream.success_rate * 100).toFixed(1)}%`}
            </span>
          </span>
        </div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <StatusBar points={upstream.latency_series ?? []} />
      </div>
    </div>
  )
}
```

- [ ] **Step 6: Update MonitorPage to pass series data to components**

Replace the `MonitorPage` component's return to pass `series`:

```tsx
export default function MonitorPage() {
  const { t } = useTranslation()
  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const upstreams = data?.upstreams ?? []
  const hitRate   = data?.today.hit_rate ?? 0
  const series    = data?.series?.points ?? []
  const today     = data?.today ?? {
    total_requests: 0, hit_count: 0, miss_count: 0,
    hit_rate: 0, bytes_served: 0, bytes_saved: 0,
  }

  const healthyCounts = upstreams.reduce(
    (acc, u) => {
      const s = mirrorStatus(u)
      acc[s] = (acc[s] ?? 0) + 1
      return acc
    },
    {} as Record<MirrorStatus, number>
  )

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
        <div>
          <h1
            className="grad-text"
            style={{ margin: 0, fontSize: 44, fontWeight: 700, letterSpacing: '-0.04em', lineHeight: 1.02 }}
          >
            {t('monitor.title')}
          </h1>
          <p style={{ margin: '14px 0 0 0', fontSize: 17, lineHeight: 1.45, color: 'var(--text)', maxWidth: 620, fontWeight: 400, letterSpacing: '-0.005em' }}>
            {t('monitor.subtitle')}
          </p>
        </div>
      </div>

      <HitRateHero hitRate={hitRate} series={series} />
      <StatStrip data={today} upstreams={upstreams} series={series} />

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'flex', alignItems: 'flex-end', justifyContent: 'space-between' }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 26, fontWeight: 700, letterSpacing: '-0.03em', lineHeight: 1.1 }}>
              {t('monitor.upstreams')}{' '}
              <span style={{ fontFamily: 'var(--font-mono)', fontSize: 14, fontWeight: 500, letterSpacing: '-0.02em', color: 'var(--text-subtle)', marginLeft: 6 }}>
                {upstreams.length}
              </span>
            </h2>
            <p style={{ margin: '2px 0 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="healthy" />
                <span className="num">{healthyCounts.healthy ?? 0}</span> {t('monitor.healthy')}
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="degraded" />
                <span className="num">{healthyCounts.degraded ?? 0}</span> {t('monitor.degraded')}
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="failed" />
                <span className="num">{healthyCounts.failed ?? 0}</span> {t('monitor.failed')}
              </span>
            </p>
          </div>
        </div>
        {upstreams.length > 0 && <MirrorMatrix upstreams={upstreams} />}
      </div>
    </div>
  )
}
```

- [ ] **Step 7: Verify the full build**

Run: `cd web && npx tsc --noEmit && npm run build`

Expected: No errors, build succeeds.

- [ ] **Step 8: Commit**

```bash
git add web/src/portal/pages/Monitor.tsx
git commit -m "feat(monitor): replace fake sparklines with real API data and StatusBar"
```

---

### Task 6: Build, deploy and verify end-to-end

**Files:** None (integration verification)

- [ ] **Step 1: Full build and restart**

```bash
make dev
```

Expected: Server starts successfully on :23333.

- [ ] **Step 2: Verify API returns series data**

```bash
curl -s http://localhost:23333/api/v1/stats | jq '{series: .series, first_upstream_latency: .upstreams[0].latency_series}'
```

Expected: `series.points` has 12 entries, `first_upstream_latency` has 12 entries.

- [ ] **Step 3: Visual verification**

Open `http://localhost:23333/` → navigate to Monitor page. Verify:
- HitRateHero shows a real sparkline (may be flat 0 if no recent traffic)
- StatStrip sparklines show real data
- Upstream mirrors show colored status bars instead of sparklines
- Hover on status bars shows tooltip with time + latency

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "chore(monitor): verify real data integration"
```
