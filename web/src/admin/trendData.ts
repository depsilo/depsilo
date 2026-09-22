import type { DashboardTrendsResponse } from '@/lib/adminApi.types'
export type TrendRange = '1h' | '24h' | '7d' | '30d'
type Point = DashboardTrendsResponse['points'][number]

// UTC-aligned increments, not cumulative counters. Seven-day responses can
// fall back from half-hour buckets to hourly buckets (dashboard.go).
export function aggregateTrends(raw: Point[], range: TrendRange) {
  if (!raw.length) return []
  const points = [...raw].sort((a, b) => a.bucket - b.bucket)
  const interval = { '1h': 60, '24h': 1800, '7d': 10800, '30d': 43200 }[range]
  const sourceStep = range === '7d' && points.length === 168 && points.every(p => p.bucket % 3600 === 0)
    ? 3600 : { '1h': 10, '24h': 300, '7d': 1800, '30d': 7200 }[range]
  const first = points[0].bucket
  const last = points[points.length - 1].bucket
  const groups = new Map<number, Point[]>()
  for (const point of points) {
    const key = Math.floor(point.bucket / interval) * interval
    const group = groups.get(key) ?? []
    group.push(point)
    groups.set(key, group)
  }
  const result = []
  for (let start = Math.floor(first / interval) * interval; start <= last; start += interval) {
    const group = groups.get(start) ?? []
    const coveredStart = Math.max(first, start)
    const end = Math.min(last + sourceStep, start + interval)
    const missing = new Set(group.map(p => p.bucket)).size < (end - coveredStart) / sourceStep
    const sum = (key: keyof Point) => group.length && group.every(p => Number.isFinite(p[key]))
      ? group.reduce((total, point) => total + Number(point[key]), 0) : null
    const requests = sum('requests')
    const hits = sum('hits')
    const latency = sum('sum_latency_ms')
    result.push({
      bucket: start, bucketStart: coveredStart, bucketEnd: end,
      // The API exposes no precise end-of-query timestamp. Never claim the
      // unfinished last bucket is fully observed; show it explicitly in UI.
      partial: coveredStart !== start || end !== start + interval || start + interval > last,
      missing,
      requests, hits, misses: sum('misses'), errors: sum('errors'),
      bytes_hit: sum('bytes_hit'), bytes_miss: sum('bytes_miss'), bytes_total: sum('bytes_served'),
      hit_rate_pct: requests && hits !== null && !missing ? hits / requests * 100 : null,
      avg_latency_ms: requests && latency !== null && !missing ? latency / requests : null,
    })
  }
  return result
}
