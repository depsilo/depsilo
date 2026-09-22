import { describe, expect, it } from 'vitest'
import { analysisHref, bandwidthQuery, byteShare, estimatedTimeSaved, reportParams, trafficRows, validReportDates } from '../../src/admin/bandwidthAnalysis'
import { dashboardStatus } from '../../src/admin/dashboardStatus'
import type { BandwidthReportResponse, DashboardResponse, PolicyStatus } from '../../src/lib/adminApi.types'

const report: BandwidthReportResponse = {
  range: { start: '2026-09-01', end: '2026-09-07' },
  summary: { total_bytes: 1000, hit_bytes: 300, miss_bytes: 700, savings_rate: .3, total_requests: 100, hit_requests: 90, miss_requests: 10, time_saved_ms: 250, avg_hit_latency: 1, avg_miss_latency: 2 },
  daily: [], by_ecosystem: [{ ecosystem: 'pypi', hit_bytes: 300, miss_bytes: 700, hit_count: 90, miss_count: 10, avg_hit_latency_ms: 1, avg_miss_latency_ms: 2 }],
  top_packages: Array.from({ length: 7 }, (_, i) => ({ ecosystem: 'pypi', package_name: `pkg-${i}`, total_bytes: (i + 1) * 10, hit_bytes: 0, request_count: 1 })),
  by_upstream: [{ upstream: 'registry', miss_bytes: 350, request_count: 5, avg_latency_ms: 2 }],
}

describe('Overview report integration', () => {
  it('uses full byte denominators, independent of hit requests or visible Top N', () => {
    const rows = trafficRows(report, 'packages').slice(0, 5)
    expect(rows[0]).toMatchObject({ name: 'pkg-6', bytes: 70, share: .07 })
    expect(trafficRows(report, 'ecosystems')[0].share).toBe(1)
    expect(trafficRows(report, 'upstreams')[0].share).toBe(.5)
    expect(byteShare(report.summary.hit_bytes, report.summary.total_bytes)).toBe(.3)
    expect(report.summary.hit_requests / report.summary.total_requests).toBe(.9)
    expect(report.top_packages[0].package_name).toBe('pkg-0')
  })
  it('preserves real zero, unknown ratios and unestimable latency', () => {
    expect(byteShare(0, 100)).toBe(0)
    for (const [bytes, total] of [[0, 0], [1, 0], [NaN, 10], [10, Infinity], [11, 10]]) expect(byteShare(bytes, total)).toBeNull()
    expect(estimatedTimeSaved(report.summary)).toBe(250)
    expect(estimatedTimeSaved({ ...report.summary, miss_requests: 0 })).toBeNull()
  })
  it('preserves 90d/custom ranges and Overview context on both navigation directions', () => {
    const original = new URLSearchParams('analysisRange=custom&start=2026-09-01&end=2026-09-07&metric=bandwidth&trendRange=1h&group=upstreams&extra=keep')
    const href = analysisHref('bandwidth', original)
    expect(href).toContain('/admin/bandwidth?')
    const returned = analysisHref('dashboard', new URLSearchParams(href.split('?')[1]))
    expect(Object.fromEntries(new URLSearchParams(returned.split('?')[1]))).toEqual(Object.fromEntries(original))
    expect(analysisHref('dashboard', new URLSearchParams('range=90d'))).toContain('analysisRange=90d')
    expect(validReportDates('2026-02-30', '2026-03-01')).toBe(false)
    expect(validReportDates('2026-09-07', '2026-09-01')).toBe(false)
    expect(validReportDates('2026-09-01', '2026-09-01')).toBe(true)
  })
  it('shares range-specific query keys and avoids live-rate report polling', () => {
    expect(bandwidthQuery(reportParams('7d', '', '')).queryKey).toEqual(['admin', 'bandwidth', { range: '7d' }])
    expect(bandwidthQuery(reportParams('custom', '', '')).enabled).toBe(false)
    expect(bandwidthQuery(reportParams('90d', '', '')).refetchInterval).toBe(60_000)
  })
})

it('derives one category list from upstream, cache and policy facts, without hiding known failures', () => {
  const window = { total_requests: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 }
  const snapshot: DashboardResponse = { last_24h: window, prev_24h: window, daily_stats: [], top_packages: {}, cache_usage_percent: 90, upstreams: [
    { id: 1, name: 'slow', adapter: 'pypi', healthy: true, avg_latency_ms: 200, success_rate: 1 },
    { id: 2, name: 'failed', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0 },
  ] }
  const policy: PolicyStatus = { status: 'unknown', using_stale_snapshot: false, snapshot_age_seconds: 0, refresh_failures: 0, on_load_error: '' }
  const result = dashboardStatus(snapshot, policy, { snapshotError: true, policyError: false, proxyResponding: true })
  expect(result.issueCategories).toEqual(['upstream', 'cache', 'policy'])
  expect(result.upstreamHealth).toEqual({ total: 2, healthy: 0, reachable: 1, failed: 1, slow: 1 })
  expect(result.overall).toBe('attention')
  expect(dashboardStatus(undefined, undefined, { snapshotError: false, policyError: false, proxyResponding: false }).issueCategories).toEqual([])
})
