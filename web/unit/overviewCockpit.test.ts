import { describe, expect, it } from 'vitest'

import type { AdminUpstream, CapabilityFact, DashboardTrendPoint, DashboardUpstream } from '../src/lib/adminApi.types'
import { buildUpstreamHealth, deriveServiceStatus } from '../src/lib/dashboardOverview'
import {
  activitySpan,
  buildAttentionItems,
  findActivityBursts,
  groupUpstreamFleet,
  isActivityTick,
  niceAxisMax,
  rebucketActivity,
  summarizeProtections,
} from '../src/lib/overviewCockpit'

function point(bucket: number, overrides: Partial<DashboardTrendPoint> = {}): DashboardTrendPoint {
  return {
    bucket,
    date: '',
    requests: 0,
    hits: 0,
    misses: 0,
    hit_rate: 0,
    bytes_served: 0,
    bytes_hit: 0,
    bytes_miss: 0,
    upstream_requests: 0,
    upstream_bytes: 0,
    sum_latency_ms: 0,
    avg_latency_ms: 0,
    errors: 0,
    ...overrides,
  }
}

/** A series of `count` buckets `step` seconds apart, starting at a UTC midnight. */
function series(count: number, step: number, active: Record<number, Partial<DashboardTrendPoint>> = {}): DashboardTrendPoint[] {
  const start = Date.UTC(2026, 10, 5) / 1000
  return Array.from({ length: count }, (_, index) => point(start + index * step, active[index]))
}

function upstream(id: number, adapter: string, overrides: Partial<DashboardUpstream> = {}): DashboardUpstream {
  return { id, name: `u${id}`, adapter, healthy: true, avg_latency_ms: 100, success_rate: 1, ...overrides }
}

function listed(id: number, adapter: string, priority: number, overrides: Partial<AdminUpstream> = {}): AdminUpstream {
  return { id, adapter_type: adapter, priority, name: `u${id}`, healthy: true, ...overrides } as AdminUpstream
}

function status(upstreams: DashboardUpstream[], extra: Partial<Parameters<typeof deriveServiceStatus>[0]> = {}) {
  return deriveServiceStatus({
    nowAvailable: true,
    nowStatus: 'healthy',
    readiness: { status: 'ready', version: 'test', checks: { database: 'ready', storage: 'ready' } },
    upstreams,
    upstreamsAvailable: true,
    policyNeedsAttention: false,
    policyAvailable: true,
    ...extra,
  })
}

describe('activitySpan', () => {
  it('keeps the server bucket when the series already fits', () => {
    expect(activitySpan(series(60, 60), 72)).toBe(60)
  })

  it('widens to a clock span that divides a day and is a multiple of the step', () => {
    // 7d at 30 min: 336 buckets into at most 72 bars needs 2.5 h; the next clock span is 3 h.
    expect(activitySpan(series(336, 1800), 72)).toBe(3 * 3600)
    // 30d at 2 h: 360 buckets into at most 72 bars needs 10 h; the next clock span is 12 h.
    expect(activitySpan(series(360, 7200), 72)).toBe(12 * 3600)
  })

  it('never returns a span that splits a server bucket', () => {
    // 1h at 10 s: 360 buckets / 72 = 50 s, so the first clock span that holds whole 10 s buckets is 60 s.
    const span = activitySpan(series(360, 10), 72)
    expect(span).toBe(60)
    expect(span % 10).toBe(0)
  })
})

describe('rebucketActivity', () => {
  it('sums every field exactly into clock-aligned buckets', () => {
    const points = series(6, 1800, {
      0: { requests: 3, hits: 2, misses: 1, bytes_hit: 20, bytes_miss: 10, upstream_bytes: 9, errors: 1 },
      1: { requests: 5, hits: 5, bytes_hit: 50 },
      5: { requests: 1, misses: 1, bytes_miss: 7, upstream_bytes: 7 },
    })
    const buckets = rebucketActivity(points, 3600, 0)
    expect(buckets).toHaveLength(3)
    expect(buckets[0]).toMatchObject({ requests: 8, hits: 7, misses: 1, bytesHit: 70, bytesMiss: 10, bytesUpstream: 9, errors: 1 })
    expect(buckets[2]).toMatchObject({ requests: 1, misses: 1, bytesMiss: 7, bytesUpstream: 7 })
    expect(buckets.reduce((sum, bucket) => sum + bucket.requests, 0)).toBe(9)
    expect(buckets[0].end - buckets[0].start).toBe(3600)
  })

  it('never splits a server bucket when the offset is not a multiple of the step', () => {
    // +5:30 against 2-hour buckets: the grid shifts to the nearest whole step.
    const offset = -(5 * 3600 + 1800)
    const points = series(24, 7200, Object.fromEntries(Array.from({ length: 24 }, (_, i) => [i, { requests: 1 }])))
    const bars = rebucketActivity(points, 12 * 3600, offset)
    for (const bar of bars) {
      expect(Math.abs((bar.start - points[0].bucket) % 7200)).toBe(0)
      expect(bar.requests).toBe(points.filter(p => p.bucket >= bar.start && p.bucket < bar.end).length)
    }
    expect(bars.reduce((sum, bar) => sum + bar.requests, 0)).toBe(24)
  })

  it('takes the offset per bucket, so bars follow the local clock across a daylight-saving change', () => {
    const change = Date.UTC(2026, 10, 5, 12) / 1000
    const offsetAt = (at: number) => (at < change ? 4 : 5) * 3600 // e.g. EDT → EST
    const points = series(48, 3600)
    const bars = rebucketActivity(points, 86400, offsetAt)
    for (const bar of bars) {
      expect(Math.abs((bar.start - offsetAt(bar.start)) % 86400)).toBe(0)
    }
  })

  it('marks where data begins in a leading bar the window cuts short', () => {
    const points = series(6, 1800).map(point => ({ ...point, bucket: point.bucket + 1800 }))
    const bars = rebucketActivity(points, 7200, 0)
    expect(bars[0].start).toBe(points[0].bucket - 1800)
    expect(bars[0].dataStart).toBe(points[0].bucket)
    expect(bars[1].dataStart).toBe(bars[1].start)
  })

  it('aligns days to the viewer offset, not UTC', () => {
    // UTC+8: local midnight is 16:00 UTC the day before.
    const offset = -8 * 3600
    const points = series(48, 3600, { 15: { requests: 1 }, 16: { requests: 2 } })
    const buckets = rebucketActivity(points, 86400, offset)
    const withRequests = buckets.filter(bucket => bucket.requests > 0)
    expect(withRequests.map(bucket => bucket.requests)).toEqual([1, 2])
    expect((withRequests[1].start - offset) % 86400).toBe(0)
  })
})

describe('isActivityTick', () => {
  it('ticks the first bar of each local day even when no bar starts at midnight', () => {
    const bars = rebucketActivity(series(14 * 24, 3600), 3 * 3600, -(5 * 3600 + 1800))
    const ticks = bars.filter((bar, index) => isActivityTick(bar, '7d', bars[index - 1]))
    const days = new Set(bars.map(bar => new Date(bar.start * 1000).toDateString()))
    expect(ticks.length).toBe(days.size - 1)
  })
})

describe('findActivityBursts', () => {
  it('returns runs newest first and tolerates one quiet bucket inside a run', () => {
    const points = series(12, 300, {
      1: { requests: 2, hits: 1, misses: 1 },
      2: { requests: 3, hits: 3 },
      // index 3 is a single quiet bucket: still the same burst
      4: { requests: 1, misses: 1 },
      // indexes 5 and 6 are quiet: the burst ends
      7: { requests: 4, hits: 4, errors: 1 },
    })
    const bursts = findActivityBursts(points, 10)
    expect(bursts).toHaveLength(2)
    expect(bursts[0]).toMatchObject({ requests: 4, hits: 4, errors: 1 })
    expect(bursts[1]).toMatchObject({ requests: 6, hits: 4, misses: 2 })
    expect(bursts[1].end - bursts[1].start).toBe(4 * 300)
  })

  it('honours the limit and handles an idle series', () => {
    const points = series(20, 60, { 1: { requests: 1 }, 5: { requests: 1 }, 9: { requests: 1 } })
    expect(findActivityBursts(points, 2)).toHaveLength(2)
    expect(findActivityBursts(series(20, 60), 4)).toEqual([])
  })
})

describe('niceAxisMax', () => {
  it('rounds up to a readable maximum', () => {
    expect(niceAxisMax(0)).toBe(1)
    expect(niceAxisMax(7)).toBe(8)
    expect(niceAxisMax(2002)).toBe(3000)
    expect(niceAxisMax(4743)).toBe(5000)
    expect(niceAxisMax(10000)).toBe(10000)
  })
})

describe('buildAttentionItems', () => {
  it('says whether a down upstream leaves its ecosystem without a fallback', () => {
    const health = [
      upstream(1, 'rubygems', { name: 'ruby-china', healthy: false }),
      upstream(2, 'rubygems', { name: 'tuna' }),
      upstream(3, 'npm', { name: 'only', healthy: false }),
    ]
    const items = buildAttentionItems({
      status: status(health),
      upstreams: [listed(1, 'rubygems', 1, { last_failure: { code: 'tls' } }), listed(2, 'rubygems', 2), listed(3, 'npm', 1)],
    })
    expect(items.map(item => item.kind === 'upstream' ? [item.upstream.name, item.tone, item.usableSiblings] : item.key)).toEqual([
      ['only', 'danger', 0],
      ['ruby-china', 'warning', 1],
    ])
    const ruby = items.find(item => item.kind === 'upstream' && item.upstream.name === 'ruby-china')
    expect(ruby?.kind === 'upstream' && ruby.failure).toEqual({ code: 'tls' })
  })

  it('treats a slow upstream as a warning and never attaches a failure to it', () => {
    const items = buildAttentionItems({ status: status([upstream(1, 'go', { avg_latency_ms: 1500 })]) })
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({ kind: 'upstream', tone: 'warning', down: false, failure: null })
  })

  it('lists open security suggestions last, as information, without changing health', () => {
    const health = status([upstream(1, 'pypi')])
    const items = buildAttentionItems({
      status: health,
      suggestionsTotal: 12,
      security: { total_vulnerabilities: 30, affected_packages: 4, by_severity: { critical: 2 }, auto_blocked_count: 0, last_scan_at: null, scan_in_progress: false },
    })
    expect(health.health).toBe('healthy')
    expect(items).toEqual([{ kind: 'security', key: 'security', tone: 'info', suggestions: 12, affectedPackages: 4, critical: 2 }])
  })

  it('puts readiness failures first and drops the generic degraded signal when upstreams explain it', () => {
    const health = status([upstream(1, 'apt', { healthy: false }), upstream(2, 'apt')], {
      nowStatus: 'degraded',
      readiness: { status: 'not_ready', version: 'test', checks: { database: 'unavailable', storage: 'ready' } },
      cacheUsagePercent: 91,
    })
    const items = buildAttentionItems({ status: health, suggestionsTotal: 0 })
    expect(items.map(item => item.key)).toEqual(['database', 'cache', 'upstream:1'])
    expect(items[0].tone).toBe('danger')
    expect(items.find(item => item.key === 'cache')).toMatchObject({ kind: 'problem', percent: 91 })
  })

  it('keeps the degraded signal when no upstream accounts for it', () => {
    const items = buildAttentionItems({ status: status([upstream(1, 'apt')], { nowStatus: 'degraded' }) })
    expect(items.map(item => item.key)).toEqual(['degraded'])
  })
})

describe('summarizeProtections', () => {
  const fact = (name: string, ecosystem: string, overrides: Partial<CapabilityFact> = {}): CapabilityFact => ({
    name, ecosystem, support: 'supported', mode: 'block', data_status: 'fresh', ...overrides,
  })

  it('counts armed ecosystems, shares or mixes the mode, and surfaces data problems', () => {
    const rows = summarizeProtections([
      fact('malicious_blocklist', 'npm'),
      fact('malicious_blocklist', 'pypi'),
      fact('malicious_blocklist', 'apt', { support: 'unsupported', mode: 'off' }),
      fact('vulnerability_scanning', 'npm', { mode: 'alert_only', data_status: 'never_synced' }),
      fact('vulnerability_scanning', 'pypi', { mode: 'alert_only', data_status: 'stale' }),
      fact('manual_rules', 'npm', { mode: 'block' }),
      fact('manual_rules', 'pypi', { mode: 'warn', data_status: 'error' }),
      fact('minimum_release_age', 'npm', { mode: 'off' }),
      fact('policy_snapshot', ''),
    ])
    const byKey = Object.fromEntries(rows.map(row => [row.key, row]))
    expect(rows.map(row => row.key)).toEqual(['malicious_blocklist', 'manual_rules', 'vulnerability_scanning', 'minimum_release_age', 'tamper_alerts'])
    expect(byKey.malicious_blocklist).toMatchObject({ armed: 2, mode: 'block', dataIssue: null, neverSynced: 0 })
    expect(byKey.manual_rules).toMatchObject({ armed: 2, mode: 'mixed', dataIssue: 'error' })
    expect(byKey.vulnerability_scanning).toMatchObject({ armed: 2, mode: 'alert_only', dataIssue: 'stale', neverSynced: 1 })
    expect(byKey.minimum_release_age).toMatchObject({ armed: 0, mode: null })
    expect(byKey.tamper_alerts).toMatchObject({ armed: 0, mode: null })
  })

  it('returns nothing before capabilities load', () => {
    expect(summarizeProtections(undefined)).toEqual([])
  })
})

describe('groupUpstreamFleet', () => {
  it('groups by ecosystem in product order and orders each group by selection priority', () => {
    const health = buildUpstreamHealth([
      upstream(5, 'npm', { name: 'official', avg_latency_ms: 210 }),
      upstream(4, 'npm', { name: 'mirror', healthy: false, avg_latency_ms: 2000 }),
      upstream(1, 'pypi', { name: 'tuna', avg_latency_ms: 300 }),
      upstream(9, 'custom', { name: 'x' }),
    ])
    const fleet = groupUpstreamFleet(health, ['pypi', 'npm'], [listed(4, 'npm', 1), listed(5, 'npm', 2)])
    expect(fleet.map(group => group.ecosystem)).toEqual(['pypi', 'npm', 'custom'])
    const npm = fleet[1]
    expect(npm.upstreams.map(item => [item.name, item.state])).toEqual([['mirror', 'unavailable'], ['official', 'healthy']])
    // The preferred mirror is down, so the latency shown is the first usable one's.
    expect(npm.primaryLatencyMs).toBe(210)
  })

  it('names no preferred upstream before the selection order is known', () => {
    const health = buildUpstreamHealth([upstream(2, 'npm', { avg_latency_ms: 80 }), upstream(1, 'npm', { avg_latency_ms: 300 })])
    expect(groupUpstreamFleet(health, ['npm'])[0].primaryLatencyMs).toBeNull()
    expect(groupUpstreamFleet(health, ['npm'], [listed(2, 'npm', 1), listed(1, 'npm', 2)])[0].primaryLatencyMs).toBe(80)
  })

  it('reports no latency when an ecosystem has no usable upstream', () => {
    const health = buildUpstreamHealth([upstream(1, 'go', { healthy: false })])
    expect(groupUpstreamFleet(health, ['go'], [listed(1, 'go', 1)])[0].primaryLatencyMs).toBeNull()
  })
})
