import { expect, it } from 'vitest'
import { aggregateTrends } from '../../src/admin/trendData'
import type { DashboardTrendsResponse } from '../../src/lib/adminApi.types'
const zero = (bucket: number): DashboardTrendsResponse['points'][number] => ({ bucket, date: '', requests: 0, hits: 0, misses: 0, hit_rate: 0, bytes_served: 0, bytes_hit: 0, bytes_miss: 0, sum_latency_ms: 0, avg_latency_ms: 0, errors: 0 })

it('preserves all increments and computes request-weighted latency and hit rate', () => {
  const raw = Array.from({ length: 6 }, (_, i) => zero(1800000000 + i * 300))
  Object.assign(raw[0], { requests: 1, hits: 1, sum_latency_ms: 100, avg_latency_ms: 100, hit_rate: 1, bytes_hit: 100, bytes_served: 100 })
  Object.assign(raw[1], { requests: 9, misses: 9, sum_latency_ms: 90, avg_latency_ms: 10, bytes_miss: 900, bytes_served: 900, errors: 2 })
  const [result] = aggregateTrends(raw, '24h')
  expect(result).toMatchObject({ requests: 10, hits: 1, misses: 9, bytes_total: 1000, bytes_hit: 100, bytes_miss: 900, errors: 2, avg_latency_ms: 19, hit_rate_pct: 10, missing: false })
})

it('keeps one active bucket in sparse seven-day data, including hourly fallback', () => {
  for (const step of [1800, 3600]) {
    const raw = Array.from({ length: 604800 / step }, (_, i) => zero(1800010800 + i * step))
    Object.assign(raw[100], { requests: 99, hits: 99, bytes_hit: 1024, bytes_served: 1024 })
    const result = aggregateTrends(raw, '7d')
    expect(result.filter(point => point.requests)).toHaveLength(1)
    expect(result.reduce((sum, p) => sum + (p.requests ?? 0), 0)).toBe(99)
    expect(result.reduce((sum, p) => sum + (p.bytes_total ?? 0), 0)).toBe(1024)
    expect(result.every(p => !p.missing)).toBe(true)
    expect(result.length).toBeLessThan(60)
  }
})

it('distinguishes successful zeros from gaps without inventing a latency sample', () => {
  const raw = Array.from({ length: 18 }, (_, i) => zero(1800000000 + i * 300))
  const result = aggregateTrends(raw.filter((_, index) => index < 6 || index >= 12), '24h')
  expect(result[0]).toMatchObject({ requests: 0, avg_latency_ms: null, hit_rate_pct: null, missing: false })
  expect(result[1]).toMatchObject({ requests: null, bytes_total: null, avg_latency_ms: null, missing: true })
  expect(result[2].requests).toBe(0)
})

it('keeps edge coverage and missing latency measurements explicit', () => {
  const raw = [zero(1800000300), zero(1800000600)]
  Object.assign(raw[0], { requests: 5, hits: 5, sum_latency_ms: null })
  const [result] = aggregateTrends(raw, '24h')
  expect(result.bucketStart).toBe(raw[0].bucket)
  expect(result.bucketEnd).toBe(raw[1].bucket + 300)
  expect(result.partial).toBe(true)
  expect(result.avg_latency_ms).toBeNull()
  expect(result.requests).toBe(5)
})
