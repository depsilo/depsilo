# Bandwidth Savings Report — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a bandwidth savings report page to the admin panel and a summary section on the Dashboard, showing traffic saved, time saved, breakdown by ecosystem/package/upstream.

**Architecture:** New backend handler `bandwidth.go` with a single aggregation endpoint querying `access_logs`. New frontend page `BandwidthReport.tsx` with recharts visualizations. Dashboard gets a new summary section at the bottom. All data comes from existing `AccessLog` table — no schema changes.

**Tech Stack:** Go/Gin/GORM (backend), React/TypeScript/recharts/TanStack Query (frontend), i18next (i18n)

---

### Task 1: Backend — Bandwidth Report API

**Files:**
- Create: `internal/api/admin/bandwidth.go`
- Modify: `internal/api/router.go:85-86`

- [ ] **Step 1: Create `internal/api/admin/bandwidth.go`**

```go
package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type BandwidthHandler struct {
	db *gorm.DB
}

func NewBandwidthHandler(database *gorm.DB) *BandwidthHandler {
	return &BandwidthHandler{db: database}
}

func (h *BandwidthHandler) GetReport(c *gin.Context) {
	rangeParam := c.DefaultQuery("range", "7d")
	startParam := c.Query("start")
	endParam := c.Query("end")

	var start, end time.Time
	now := time.Now()

	switch rangeParam {
	case "custom":
		var err error
		start, err = time.Parse("2006-01-02", startParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DATE", "message": "invalid start date"})
			return
		}
		end, err = time.Parse("2006-01-02", endParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DATE", "message": "invalid end date"})
			return
		}
		end = end.Add(24*time.Hour - time.Nanosecond) // end of day
	case "30d":
		start = now.AddDate(0, 0, -30).Truncate(24 * time.Hour)
		end = now
	case "90d":
		start = now.AddDate(0, 0, -90).Truncate(24 * time.Hour)
		end = now
	default: // 7d
		start = now.AddDate(0, 0, -7).Truncate(24 * time.Hour)
		end = now
	}

	// Summary: total/hit/miss bytes and counts
	type summaryRow struct {
		Hit        bool
		TotalBytes int64
		Count      int64
		AvgLatency float64
	}
	var summaryRows []summaryRow
	h.db.Model(&db.AccessLog{}).
		Select("hit, COALESCE(SUM(bytes_sent), 0) as total_bytes, COUNT(*) as count, COALESCE(AVG(latency_ms), 0) as avg_latency").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("hit").Scan(&summaryRows)

	var totalBytes, hitBytes, missBytes, totalRequests, hitRequests, missRequests int64
	var avgHitLatency, avgMissLatency float64
	for _, r := range summaryRows {
		totalBytes += r.TotalBytes
		totalRequests += r.Count
		if r.Hit {
			hitBytes = r.TotalBytes
			hitRequests = r.Count
			avgHitLatency = r.AvgLatency
		} else {
			missBytes = r.TotalBytes
			missRequests = r.Count
			avgMissLatency = r.AvgLatency
		}
	}

	var savingsRate float64
	if totalBytes > 0 {
		savingsRate = float64(hitBytes) / float64(totalBytes)
	}

	// Time saved: hit_count * (avg_miss_latency - avg_hit_latency) per ecosystem
	type ecoLatency struct {
		AdapterType    string
		Hit            bool
		AvgLatencyMs   float64
		Count          int64
	}
	var ecoLatencies []ecoLatency
	h.db.Model(&db.AccessLog{}).
		Select("adapter_type, hit, AVG(latency_ms) as avg_latency_ms, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("adapter_type, hit").Scan(&ecoLatencies)

	// Build per-ecosystem latency maps
	type ecoStats struct {
		hitCount       int64
		avgHitLatency  float64
		avgMissLatency float64
		missCount      int64
	}
	ecoMap := make(map[string]*ecoStats)
	for _, el := range ecoLatencies {
		if ecoMap[el.AdapterType] == nil {
			ecoMap[el.AdapterType] = &ecoStats{}
		}
		if el.Hit {
			ecoMap[el.AdapterType].hitCount = el.Count
			ecoMap[el.AdapterType].avgHitLatency = el.AvgLatencyMs
		} else {
			ecoMap[el.AdapterType].missCount = el.Count
			ecoMap[el.AdapterType].avgMissLatency = el.AvgLatencyMs
		}
	}

	var timeSavedMs int64
	for _, es := range ecoMap {
		missLat := es.avgMissLatency
		if missLat == 0 && avgMissLatency > 0 {
			missLat = avgMissLatency // fallback to global avg
		}
		diff := missLat - es.avgHitLatency
		if diff > 0 {
			timeSavedMs += int64(diff * float64(es.hitCount))
		}
	}

	// Daily breakdown
	type dailyRow struct {
		Date       string
		Hit        bool
		Bytes      int64
		Count      int64
		AvgLatency float64
	}
	var dailyRows []dailyRow
	h.db.Model(&db.AccessLog{}).
		Select("DATE(created_at) as date, hit, COALESCE(SUM(bytes_sent), 0) as bytes, COUNT(*) as count, COALESCE(AVG(latency_ms), 0) as avg_latency").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("date, hit").Order("date").Scan(&dailyRows)

	type dailyPoint struct {
		Date        string `json:"date"`
		HitBytes    int64  `json:"hit_bytes"`
		MissBytes   int64  `json:"miss_bytes"`
		HitCount    int64  `json:"hit_count"`
		MissCount   int64  `json:"miss_count"`
		TimeSavedMs int64  `json:"time_saved_ms"`
	}
	dailyMap := make(map[string]*dailyPoint)
	var dailyOrder []string
	for _, r := range dailyRows {
		if dailyMap[r.Date] == nil {
			dailyMap[r.Date] = &dailyPoint{Date: r.Date}
			dailyOrder = append(dailyOrder, r.Date)
		}
		dp := dailyMap[r.Date]
		if r.Hit {
			dp.HitBytes = r.Bytes
			dp.HitCount = r.Count
		} else {
			dp.MissBytes = r.Bytes
			dp.MissCount = r.Count
		}
	}
	daily := make([]dailyPoint, 0, len(dailyOrder))
	for _, d := range dailyOrder {
		daily = append(daily, *dailyMap[d])
	}

	// By ecosystem
	type ecoRow struct {
		AdapterType string
		Hit         bool
		Bytes       int64
		Count       int64
		AvgLatency  float64
	}
	var ecoRows []ecoRow
	h.db.Model(&db.AccessLog{}).
		Select("adapter_type, hit, COALESCE(SUM(bytes_sent), 0) as bytes, COUNT(*) as count, COALESCE(AVG(latency_ms), 0) as avg_latency").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("adapter_type, hit").Scan(&ecoRows)

	type ecoPoint struct {
		Ecosystem       string  `json:"ecosystem"`
		HitBytes        int64   `json:"hit_bytes"`
		MissBytes       int64   `json:"miss_bytes"`
		HitCount        int64   `json:"hit_count"`
		MissCount       int64   `json:"miss_count"`
		AvgHitLatencyMs float64 `json:"avg_hit_latency_ms"`
		AvgMissLatencyMs float64 `json:"avg_miss_latency_ms"`
	}
	ecoPointMap := make(map[string]*ecoPoint)
	for _, r := range ecoRows {
		if ecoPointMap[r.AdapterType] == nil {
			ecoPointMap[r.AdapterType] = &ecoPoint{Ecosystem: r.AdapterType}
		}
		ep := ecoPointMap[r.AdapterType]
		if r.Hit {
			ep.HitBytes = r.Bytes
			ep.HitCount = r.Count
			ep.AvgHitLatencyMs = r.AvgLatency
		} else {
			ep.MissBytes = r.Bytes
			ep.MissCount = r.Count
			ep.AvgMissLatencyMs = r.AvgLatency
		}
	}
	byEcosystem := make([]ecoPoint, 0, len(ecoPointMap))
	for _, ep := range ecoPointMap {
		byEcosystem = append(byEcosystem, *ep)
	}

	// Top packages by total bytes
	type pkgRow struct {
		PackageName string `json:"package_name"`
		Ecosystem   string `json:"ecosystem"`
		TotalBytes  int64  `json:"total_bytes"`
		HitBytes    int64  `json:"hit_bytes"`
		Count       int64  `json:"request_count"`
	}
	var topPackages []pkgRow
	h.db.Model(&db.AccessLog{}).
		Select("package_name, adapter_type as ecosystem, SUM(bytes_sent) as total_bytes, SUM(CASE WHEN hit = 1 THEN bytes_sent ELSE 0 END) as hit_bytes, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ? AND package_name != ''", start, end).
		Group("package_name, adapter_type").
		Order("total_bytes DESC").Limit(10).Scan(&topPackages)

	// By upstream (miss only)
	type upstreamRow struct {
		Upstream      string  `json:"upstream"`
		MissBytes     int64   `json:"miss_bytes"`
		RequestCount  int64   `json:"request_count"`
		AvgLatencyMs  float64 `json:"avg_latency_ms"`
	}
	var byUpstream []upstreamRow
	h.db.Model(&db.AccessLog{}).
		Select("upstream, COALESCE(SUM(bytes_sent), 0) as miss_bytes, COUNT(*) as request_count, COALESCE(AVG(latency_ms), 0) as avg_latency_ms").
		Where("created_at >= ? AND created_at <= ? AND hit = ? AND upstream != ''", start, end, false).
		Group("upstream").Order("miss_bytes DESC").Scan(&byUpstream)

	c.JSON(http.StatusOK, gin.H{
		"range": gin.H{
			"start": start.Format("2006-01-02"),
			"end":   end.Format("2006-01-02"),
		},
		"summary": gin.H{
			"total_bytes":      totalBytes,
			"hit_bytes":        hitBytes,
			"miss_bytes":       missBytes,
			"savings_rate":     savingsRate,
			"total_requests":   totalRequests,
			"hit_requests":     hitRequests,
			"miss_requests":    missRequests,
			"time_saved_ms":    timeSavedMs,
			"avg_hit_latency":  avgHitLatency,
			"avg_miss_latency": avgMissLatency,
		},
		"daily":        daily,
		"by_ecosystem": byEcosystem,
		"top_packages": topPackages,
		"by_upstream":  byUpstream,
	})
}
```

- [ ] **Step 2: Register the route in `internal/api/router.go`**

Add after line 86 (`adminGroup.GET("/dashboard/trends", dashHandler.GetTrends)`):

```go
	// Bandwidth report
	bandwidthHandler := admin.NewBandwidthHandler(deps.DB)
	adminGroup.GET("/bandwidth", bandwidthHandler.GetReport)
```

- [ ] **Step 3: Verify backend compiles**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./cmd/server`
Expected: clean build, no errors

- [ ] **Step 4: Commit**

```bash
git add internal/api/admin/bandwidth.go internal/api/router.go
git commit -m "feat: add bandwidth savings report API endpoint"
```

---

### Task 2: Frontend — API Client & i18n

**Files:**
- Modify: `web/src/lib/api.ts:96` (before closing brace of adminApi)
- Modify: `web/src/i18n/en.ts:75` (after dashboard section)
- Modify: `web/src/i18n/zh.ts:75` (after dashboard section)

- [ ] **Step 1: Add API method to `web/src/lib/api.ts`**

Add inside the `adminApi` object, after the `testRule` line (line 96):

```typescript
  // Bandwidth report
  getBandwidthReport: (params: { range?: string; start?: string; end?: string }) =>
    api.get('/admin/bandwidth', { params }),
```

- [ ] **Step 2: Add English i18n keys to `web/src/i18n/en.ts`**

Add a new `bandwidth` section after the `dashboard` section (after line 75):

```typescript
    // Bandwidth Report
    bandwidth: {
      title: 'Bandwidth Report',
      totalTraffic: 'Total Traffic',
      trafficSaved: 'Traffic Saved',
      savingsRate: 'Savings Rate',
      timeSaved: 'Time Saved',
      dailyTrend: 'Daily Trend',
      byEcosystem: 'By Ecosystem',
      topPackages: 'Top Packages',
      byUpstream: 'By Upstream',
      hitBytes: 'Cache Hit',
      missBytes: 'Cache Miss',
      latencyComparison: 'Latency Comparison',
      avgHitLatency: 'Hit Avg',
      avgMissLatency: 'Miss Avg',
      viewFullReport: 'View Full Report →',
      last7d: '7 Days',
      last30d: '30 Days',
      last90d: '90 Days',
      custom: 'Custom',
      bandwidthSummary: 'Bandwidth Savings',
      hours: 'h',
      minutes: 'min',
      seconds: 's',
      totalBandwidth: 'Total',
      savedBandwidth: 'Saved',
    },
```

- [ ] **Step 3: Add Chinese i18n keys to `web/src/i18n/zh.ts`**

Add matching `bandwidth` section after the `dashboard` section (after line 75):

```typescript
    // Bandwidth Report
    bandwidth: {
      title: '带宽报告',
      totalTraffic: '总流量',
      trafficSaved: '节省流量',
      savingsRate: '节省率',
      timeSaved: '节省时间',
      dailyTrend: '每日趋势',
      byEcosystem: '按生态',
      topPackages: '热门包',
      byUpstream: '按上游',
      hitBytes: '缓存命中',
      missBytes: '缓存未命中',
      latencyComparison: '延迟对比',
      avgHitLatency: '命中平均',
      avgMissLatency: '未命中平均',
      viewFullReport: '查看完整报告 →',
      last7d: '7 天',
      last30d: '30 天',
      last90d: '90 天',
      custom: '自定义',
      bandwidthSummary: '带宽节省',
      hours: '时',
      minutes: '分',
      seconds: '秒',
      totalBandwidth: '总量',
      savedBandwidth: '已节省',
    },
```

- [ ] **Step 4: Commit**

```bash
cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo
git add web/src/lib/api.ts web/src/i18n/en.ts web/src/i18n/zh.ts
git commit -m "feat: add bandwidth report API client and i18n keys"
```

---

### Task 3: Frontend — BandwidthReport Page

**Files:**
- Create: `web/src/admin/pages/BandwidthReport.tsx`

- [ ] **Step 1: Create `web/src/admin/pages/BandwidthReport.tsx`**

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import MetricCardV2 from '@/components/MetricCard'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import {
  AreaChart, Area, BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function formatTimeSaved(ms: number, t: (key: string) => string): string {
  if (ms <= 0) return '0s'
  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  if (hours > 0) return `${hours}${t('bandwidth.hours')} ${minutes % 60}${t('bandwidth.minutes')}`
  if (minutes > 0) return `${minutes}${t('bandwidth.minutes')} ${seconds % 60}${t('bandwidth.seconds')}`
  return `${seconds}${t('bandwidth.seconds')}`
}

const ECO_COLORS = [
  'var(--stripe-purple)', '#3b82f6', '#10b981', '#f59e0b',
  '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4',
  '#84cc16', '#f97316', '#6366f1', '#14b8a6',
]

function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--heading)' }}>{label}</p>
      {payload.map((entry: any) => (
        <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {formatBytes(entry.value)}
        </p>
      ))}
    </div>
  )
}

function LatencyTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--heading)' }}>{label}</p>
      {payload.map((entry: any) => (
        <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {Math.round(entry.value)} ms
        </p>
      ))}
    </div>
  )
}

export default function BandwidthReport() {
  const { t } = useTranslation()
  const [range, setRange] = useState('7d')
  const [customStart, setCustomStart] = useState('')
  const [customEnd, setCustomEnd] = useState('')

  const params = range === 'custom'
    ? { range: 'custom', start: customStart, end: customEnd }
    : { range }

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'bandwidth', params],
    queryFn: () => adminApi.getBandwidthReport(params),
    enabled: range !== 'custom' || (!!customStart && !!customEnd),
    refetchInterval: 60000,
  })

  const report = data?.data
  const summary = report?.summary || {}
  const daily = report?.daily || []
  const byEcosystem = report?.by_ecosystem || []
  const topPackages = report?.top_packages || []
  const byUpstream = report?.by_upstream || []

  const ranges = [
    { value: '7d', label: t('bandwidth.last7d') },
    { value: '30d', label: t('bandwidth.last30d') },
    { value: '90d', label: t('bandwidth.last90d') },
    { value: 'custom', label: t('bandwidth.custom') },
  ]

  // Ecosystem donut data
  const ecoDonutData = byEcosystem
    .map((e: any) => ({ name: e.ecosystem, value: e.hit_bytes + e.miss_bytes }))
    .filter((e: any) => e.value > 0)
    .sort((a: any, b: any) => b.value - a.value)

  // Latency comparison data
  const latencyData = byEcosystem
    .filter((e: any) => e.avg_miss_latency_ms > 0)
    .map((e: any) => ({
      ecosystem: e.ecosystem,
      hit: Math.round(e.avg_hit_latency_ms),
      miss: Math.round(e.avg_miss_latency_ms),
    }))
    .sort((a: any, b: any) => b.miss - a.miss)

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid gap-4 grid-cols-4">
          {[...Array(4)].map((_, i) => <div key={i} className="h-24 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />)}
        </div>
        <div className="h-80 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Range selector */}
      <div className="flex items-center gap-2 flex-wrap">
        {ranges.map(r => (
          <button
            key={r.value}
            onClick={() => setRange(r.value)}
            className="px-3 py-1 text-[12px] font-[400] rounded-[4px] cursor-pointer transition-colors duration-150"
            style={{
              background: range === r.value ? 'var(--stripe-purple)' : 'transparent',
              color: range === r.value ? 'var(--on-primary)' : 'var(--body)',
              border: range === r.value ? 'none' : '1px solid var(--border)',
            }}
          >
            {r.label}
          </button>
        ))}
        {range === 'custom' && (
          <div className="flex items-center gap-2 ml-2">
            <input
              type="date"
              value={customStart}
              onChange={e => setCustomStart(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--heading)' }}
            />
            <span className="text-[12px]" style={{ color: 'var(--body)' }}>—</span>
            <input
              type="date"
              value={customEnd}
              onChange={e => setCustomEnd(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--heading)' }}
            />
          </div>
        )}
      </div>

      {/* Summary metrics */}
      <div className="grid gap-4 grid-cols-4">
        <MetricCardV2
          label={t('bandwidth.totalTraffic')}
          value={formatBytes(summary.total_bytes || 0)}
          icon={<Icon name="cloud_download" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.trafficSaved')}
          value={formatBytes(summary.hit_bytes || 0)}
          icon={<Icon name="savings" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.savingsRate')}
          value={summary.savings_rate != null ? `${(summary.savings_rate * 100).toFixed(1)}%` : '0%'}
          icon={<Icon name="trending_up" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.timeSaved')}
          value={formatTimeSaved(summary.time_saved_ms || 0, t)}
          icon={<Icon name="schedule" size="sm" />}
        />
      </div>

      {/* Daily trend — stacked area chart */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
          {t('bandwidth.dailyTrend')}
        </h3>
        <ResponsiveContainer width="100%" height={240}>
          <AreaChart data={daily}>
            <defs>
              <linearGradient id="gradHitBytes" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#10b981" stopOpacity={0.3} />
                <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
              </linearGradient>
              <linearGradient id="gradMissBytes" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--error)" stopOpacity={0.2} />
                <stop offset="100%" stopColor="var(--error)" stopOpacity={0.02} />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={50} tickFormatter={(v: number) => formatBytes(v)} />
            <Tooltip content={<ChartTooltip />} />
            <Legend wrapperStyle={{ fontSize: 11 }} />
            <Area type="monotone" dataKey="hit_bytes" stackId="1" stroke="#10b981" strokeWidth={1.5} fill="url(#gradHitBytes)" name={t('bandwidth.hitBytes')} />
            <Area type="monotone" dataKey="miss_bytes" stackId="1" stroke="var(--error)" strokeWidth={1.5} fill="url(#gradMissBytes)" name={t('bandwidth.missBytes')} />
          </AreaChart>
        </ResponsiveContainer>
      </CardV2>

      {/* Three columns: ecosystem donut, top packages bar, upstream bar */}
      <div className="grid gap-4 grid-cols-3">
        {/* Ecosystem donut */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.byEcosystem')}
          </h3>
          {ecoDonutData.length > 0 ? (
            <>
              <ResponsiveContainer width="100%" height={160}>
                <PieChart>
                  <Pie data={ecoDonutData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={40} outerRadius={65} paddingAngle={2}>
                    {ecoDonutData.map((_: any, i: number) => (
                      <Cell key={i} fill={ECO_COLORS[i % ECO_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip formatter={(value: number) => formatBytes(value)} />
                </PieChart>
              </ResponsiveContainer>
              <div className="space-y-1.5 mt-2">
                {ecoDonutData.slice(0, 6).map((e: any, i: number) => (
                  <div key={e.name} className="flex items-center gap-2 text-[11px]">
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ background: ECO_COLORS[i % ECO_COLORS.length] }} />
                    <EcosystemIcon type={e.name} size={12} />
                    <span className="font-mono" style={{ color: 'var(--heading)' }}>{e.name}</span>
                    <span className="ml-auto font-mono tabular-nums" style={{ color: 'var(--body)' }}>{formatBytes(e.value)}</span>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>

        {/* Top packages horizontal bar */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.topPackages')}
          </h3>
          {topPackages.length > 0 ? (
            <div className="space-y-2">
              {topPackages.map((p: any, i: number) => {
                const max = topPackages[0]?.total_bytes || 1
                return (
                  <div key={`${p.ecosystem}-${p.package_name}`} className="flex items-center gap-2">
                    <span className="text-[11px] font-mono tabular-nums w-4 shrink-0 text-right" style={{ color: 'var(--body)' }}>{i + 1}</span>
                    <EcosystemIcon type={p.ecosystem} size={12} />
                    <span className="font-mono text-[11px] truncate flex-1" style={{ color: 'var(--heading)' }}>{p.package_name}</span>
                    <span className="font-mono text-[10px] tabular-nums shrink-0" style={{ color: 'var(--body)' }}>{formatBytes(p.total_bytes)}</span>
                    <div className="w-16 h-1 rounded-full shrink-0" style={{ background: 'var(--surface-container)' }}>
                      <div className="h-full rounded-full" style={{ width: `${(p.total_bytes / max) * 100}%`, background: 'var(--stripe-purple)' }} />
                    </div>
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>

        {/* Upstream bar */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.byUpstream')}
          </h3>
          {byUpstream.length > 0 ? (
            <ResponsiveContainer width="100%" height={Math.max(160, byUpstream.length * 32)}>
              <BarChart data={byUpstream} layout="vertical" margin={{ left: 0, right: 10 }}>
                <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => formatBytes(v)} />
                <YAxis type="category" dataKey="upstream" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={80} />
                <Tooltip formatter={(value: number) => formatBytes(value)} />
                <Bar dataKey="miss_bytes" fill="var(--stripe-purple)" radius={[0, 3, 3, 0]} barSize={16} name={t('bandwidth.totalBandwidth')} />
              </BarChart>
            </ResponsiveContainer>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>
      </div>

      {/* Latency comparison */}
      <CardV2>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
            {t('bandwidth.latencyComparison')}
          </h3>
          <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--success)' }}>
            {t('bandwidth.timeSaved')}: {formatTimeSaved(summary.time_saved_ms || 0, t)}
          </span>
        </div>
        {latencyData.length > 0 ? (
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={latencyData}>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
              <XAxis dataKey="ecosystem" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={40} tickFormatter={(v: number) => `${v}ms`} />
              <Tooltip content={<LatencyTooltip />} />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Bar dataKey="hit" fill="#10b981" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgHitLatency')} />
              <Bar dataKey="miss" fill="var(--error)" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgMissLatency')} />
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
        )}
      </CardV2>
    </div>
  )
}
```

- [ ] **Step 2: Verify frontend compiles**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo
git add web/src/admin/pages/BandwidthReport.tsx
git commit -m "feat: add BandwidthReport page component"
```

---

### Task 4: Frontend — Route & Sidebar Registration

**Files:**
- Modify: `web/src/admin/AdminApp.tsx:1-11` (imports), `web/src/admin/AdminApp.tsx:39-42` (routes)
- Modify: `web/src/admin/components/MainLayout.tsx:43-47` (monitorItems), `web/src/admin/components/MainLayout.tsx:57-66` (pageTitles)

- [ ] **Step 1: Add route to `web/src/admin/AdminApp.tsx`**

Add import after line 4 (`import DashboardV2 from './pages/Dashboard'`):

```typescript
import BandwidthReportV2 from './pages/BandwidthReport'
```

Add route after line 35 (`<Route index element={<DashboardV2 />} />`):

```tsx
        <Route path="bandwidth" element={<BandwidthReportV2 />} />
```

- [ ] **Step 2: Add sidebar nav item to `web/src/admin/components/MainLayout.tsx`**

In the `monitorItems` array (line 43-47), add after the Dashboard entry:

```typescript
    { label: t('bandwidth.title'), to: '/admin/bandwidth', icon: 'bar_chart' },
```

In the `pageTitles` object (line 57-66), add:

```typescript
    '/admin/bandwidth': t('bandwidth.title'),
```

- [ ] **Step 3: Add nav translation keys**

These were already included in the `bandwidth` section added in Task 2 (`bandwidth.title`). No extra work needed.

- [ ] **Step 4: Verify frontend compiles**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo
git add web/src/admin/AdminApp.tsx web/src/admin/components/MainLayout.tsx
git commit -m "feat: register bandwidth report route and sidebar nav item"
```

---

### Task 5: Frontend — Dashboard Bandwidth Summary Section

**Files:**
- Modify: `web/src/admin/pages/Dashboard.tsx:215-224` (before upstreams section)

- [ ] **Step 1: Add bandwidth summary to Dashboard**

Add imports at the top of `Dashboard.tsx` (after the existing recharts imports on line 12):

```typescript
import { Link } from 'react-router-dom'
```

Add a new query after the `trendsData` query (after line 99):

```typescript
  const { data: bwData } = useQuery({
    queryKey: ['admin', 'bandwidth', '7d'],
    queryFn: () => adminApi.getBandwidthReport({ range: '7d' }),
    refetchInterval: 60000,
  })
```

Add a new section before the `{/* Upstreams — full width */}` comment (before line 217). Insert between the chart+packages grid and the upstreams section:

```tsx
      {/* Bandwidth savings summary */}
      {bwData?.data?.summary && (() => {
        const bw = bwData.data.summary
        const bwDaily = bwData.data.daily || []
        const formatTimeSaved = (ms: number) => {
          if (ms <= 0) return '0s'
          const s = Math.floor(ms / 1000); const m = Math.floor(s / 60); const h = Math.floor(m / 60)
          if (h > 0) return `${h}${t('bandwidth.hours')} ${m % 60}${t('bandwidth.minutes')}`
          if (m > 0) return `${m}${t('bandwidth.minutes')} ${s % 60}${t('bandwidth.seconds')}`
          return `${s}${t('bandwidth.seconds')}`
        }
        return (
          <CardV2>
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
                {t('bandwidth.bandwidthSummary')}
              </h3>
              <Link
                to="/admin/bandwidth"
                className="text-[11px] font-[400] no-underline transition-colors duration-150"
                style={{ color: 'var(--stripe-purple)' }}
              >
                {t('bandwidth.viewFullReport')}
              </Link>
            </div>
            <div className="grid grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.totalTraffic')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: 'var(--heading)' }}>{formatBytes(bw.total_bytes || 0)}</p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.trafficSaved')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: '#10b981' }}>{formatBytes(bw.hit_bytes || 0)}</p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.savingsRate')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: bw.savings_rate > 0.5 ? '#10b981' : 'var(--heading)' }}>
                  {bw.savings_rate != null ? `${(bw.savings_rate * 100).toFixed(1)}%` : '0%'}
                </p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.timeSaved')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: '#10b981' }}>{formatTimeSaved(bw.time_saved_ms || 0)}</p>
              </div>
            </div>
            {bwDaily.length > 0 && (
              <ResponsiveContainer width="100%" height={100}>
                <AreaChart data={bwDaily}>
                  <defs>
                    <linearGradient id="gradBwHit" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#10b981" stopOpacity={0.3} />
                      <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 9 }} axisLine={false} tickLine={false} />
                  <YAxis hide />
                  <Area type="monotone" dataKey="hit_bytes" stroke="#10b981" strokeWidth={1.5} fill="url(#gradBwHit)" />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </CardV2>
        )
      })()}
```

- [ ] **Step 2: Add `getBandwidthReport` import if not already available**

The `adminApi` import on line 4 already includes `getBandwidthReport` (added in Task 2). No extra import needed.

- [ ] **Step 3: Verify frontend compiles**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo/web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo
git add web/src/admin/pages/Dashboard.tsx
git commit -m "feat: add bandwidth savings summary section to Dashboard"
```

---

### Task 6: Build Verification

**Files:** none (verification only)

- [ ] **Step 1: Build backend**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go build ./cmd/server`
Expected: clean build

- [ ] **Step 2: Build frontend**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo/web && npm run build`
Expected: clean build, output in `web/dist/`

- [ ] **Step 3: Run backend tests**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && go test ./...`
Expected: all existing tests pass
