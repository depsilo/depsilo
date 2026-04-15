# Bandwidth Savings Report

## Overview

Add a dedicated bandwidth savings report page to the admin panel, plus a summary section on the existing Dashboard. All data is aggregated from the existing `AccessLog` table — no new tables needed.

## Backend

### New API Endpoint

```
GET /api/v1/admin/bandwidth?range=7d|30d|90d|custom&start=YYYY-MM-DD&end=YYYY-MM-DD
```

Requires JWT auth (admin). Returns a single aggregated response:

```json
{
  "range": { "start": "2026-04-06", "end": "2026-04-13" },

  "summary": {
    "total_bytes": 98000000000,
    "hit_bytes": 87000000000,
    "miss_bytes": 11000000000,
    "savings_rate": 0.888,
    "total_requests": 42000,
    "hit_requests": 37800,
    "time_saved_ms": 1890000
  },

  "daily": [
    {
      "date": "2026-04-06",
      "hit_bytes": 12000000000,
      "miss_bytes": 1500000000,
      "hit_count": 5400,
      "miss_count": 600,
      "time_saved_ms": 270000
    }
  ],

  "by_ecosystem": [
    {
      "ecosystem": "pypi",
      "hit_bytes": 45000000000,
      "miss_bytes": 5000000000,
      "hit_count": 18000,
      "miss_count": 2000,
      "avg_hit_latency_ms": 3,
      "avg_miss_latency_ms": 120
    }
  ],

  "top_packages": [
    {
      "package_name": "torch",
      "ecosystem": "pypi",
      "total_bytes": 18000000000,
      "hit_bytes": 17000000000,
      "request_count": 340
    }
  ],

  "by_upstream": [
    {
      "upstream": "tuna-pypi",
      "miss_bytes": 3200000000,
      "request_count": 1200,
      "avg_latency_ms": 95
    }
  ]
}
```

### Implementation Details

**File**: `internal/api/admin/bandwidth.go`

All queries aggregate from `access_logs` table:

1. **summary**: Two queries — `SUM(bytes_sent)` grouped by `hit` flag, plus `SUM/AVG(latency_ms)` grouped by `hit` flag. Time saved = `SUM((avg_miss_latency_per_ecosystem - actual_hit_latency) for each hit request)`. Simplified to: `hit_count * (avg_miss_latency - avg_hit_latency)` per ecosystem, then summed.

2. **daily**: `GROUP BY DATE(created_at), hit` within the date range.

3. **by_ecosystem**: `GROUP BY adapter_type, hit` within the date range.

4. **top_packages**: `GROUP BY package_name, adapter_type` within the date range, `ORDER BY SUM(bytes_sent) DESC LIMIT 10`.

5. **by_upstream**: `WHERE hit = false GROUP BY upstream` within the date range (only miss requests hit upstreams).

### Time Saved Calculation

```
For each ecosystem:
  avg_miss_latency = AVG(latency_ms WHERE hit=false)
  avg_hit_latency  = AVG(latency_ms WHERE hit=true)
  hit_count        = COUNT(*) WHERE hit=true
  time_saved       = hit_count * (avg_miss_latency - avg_hit_latency)

Total time_saved = SUM across all ecosystems
```

If an ecosystem has zero misses in the range, use the overall average miss latency as fallback.

### Route Registration

Add to `internal/api/router.go` under the admin group:

```go
adminGroup.GET("/bandwidth", bandwidthHandler.GetReport)
```

## Frontend

### 1. Dashboard Summary Section

**File**: `web/src/admin/pages/Dashboard.tsx`

Add a new section at the bottom of the existing Dashboard page:

- 4 metric cards in a row:
  - Total Traffic (bytes_served, formatted)
  - Traffic Saved (hit_bytes, formatted, green)
  - Savings Rate (percentage, green if >50%)
  - Time Saved (formatted as "X hours Y min")
- Below cards: a mini area chart showing daily hit_bytes vs miss_bytes for the last 7 days
- "View Full Report" link to `/admin/bandwidth`

Data source: call `/api/v1/admin/bandwidth?range=7d`

### 2. Bandwidth Report Page

**File**: `web/src/admin/pages/BandwidthReport.tsx`

**Route**: `/admin/bandwidth`

**Layout**:

```
+--------------------------------------------------+
| [7d] [30d] [90d] [Custom: start - end]          |
+--------------------------------------------------+
| [Total Traffic] [Saved] [Savings Rate] [Time Saved] |
+--------------------------------------------------+
|                                                  |
|  Stacked Area Chart (daily hit vs miss bytes)    |
|  X: dates, Y: bytes (auto MB/GB/TB)             |
|                                                  |
+--------------------------------------------------+
| Ecosystem Breakdown | Top 10 Packages | Upstream |
| (donut + list)      | (horizontal bar)| (bar)   |
|                     |                 |          |
+--------------------------------------------------+
| Latency Comparison                               |
| Grouped bar chart: hit avg vs miss avg per eco   |
| + "Total time saved: X hours Y minutes"          |
+--------------------------------------------------+
```

### 3. Sidebar Navigation

**File**: `web/src/admin/AdminApp.tsx`

Add "Bandwidth Report" under the monitoring section, between Dashboard and Access Logs:

```
[Monitoring]
- Dashboard
- Bandwidth Report    <-- new
- Access Logs
```

Icon: `BarChart3` from lucide-react.

### 4. API Client

**File**: `web/src/lib/api.ts`

```typescript
adminApi.getBandwidthReport(params: {
  range?: '7d' | '30d' | '90d' | 'custom';
  start?: string;
  end?: string;
}) => GET /api/v1/admin/bandwidth
```

### 5. i18n Keys

Add to both `en.ts` and `zh.ts`:

```
bandwidth.title: "Bandwidth Report" / "带宽报告"
bandwidth.totalTraffic: "Total Traffic" / "总流量"
bandwidth.trafficSaved: "Traffic Saved" / "节省流量"
bandwidth.savingsRate: "Savings Rate" / "节省率"
bandwidth.timeSaved: "Time Saved" / "节省时间"
bandwidth.daily: "Daily Trend" / "每日趋势"
bandwidth.byEcosystem: "By Ecosystem" / "按生态"
bandwidth.topPackages: "Top Packages" / "热门包"
bandwidth.byUpstream: "By Upstream" / "按上游"
bandwidth.hitBytes: "Cache Hit" / "缓存命中"
bandwidth.missBytes: "Cache Miss" / "缓存未命中"
bandwidth.latencyComparison: "Latency Comparison" / "延迟对比"
bandwidth.avgHitLatency: "Avg Hit Latency" / "命中平均延迟"
bandwidth.avgMissLatency: "Avg Miss Latency" / "未命中平均延迟"
bandwidth.viewFullReport: "View Full Report" / "查看完整报告"
bandwidth.last7d: "Last 7 Days" / "近 7 天"
bandwidth.last30d: "Last 30 Days" / "近 30 天"
bandwidth.last90d: "Last 90 Days" / "近 90 天"
bandwidth.custom: "Custom Range" / "自定义范围"
bandwidth.hours: "hours" / "小时"
bandwidth.minutes: "min" / "分钟"
```

### Charts Library

Use recharts (already installed) for all charts:
- `AreaChart` for daily trend (stacked)
- `PieChart` for ecosystem donut
- `BarChart` (horizontal) for top packages
- `BarChart` (vertical) for upstream breakdown
- `BarChart` (grouped) for latency comparison

## Scope Boundaries

- No new database tables
- No PDF/CSV export (not requested)
- No cost/money estimation
- No alerting on bandwidth thresholds
- No changes to how AccessLog is recorded
