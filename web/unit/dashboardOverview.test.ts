import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import type { DashboardPeriod, DashboardUpstream, NowResponse, ReadinessResponse } from '../src/lib/adminApi.types'
import {
  buildRequestFlow,
  buildUpstreamHealth,
  deriveServiceStatus,
  deriveUsageState,
  formatEstimatedDuration,
  hitRateValue,
  latencyComparison,
  originCoverageNote,
  periodChange,
  requestOutcome,
  sparklineGeometry,
  sparklineHasVariance,
  timeSavedMs,
} from '../src/lib/dashboardOverview'

function period(overrides: Partial<DashboardPeriod> = {}): DashboardPeriod {
  return {
    total_requests: 100,
    hit_count: 80,
    miss_count: 20,
    hit_requests: 80,
    miss_requests: 20,
    hit_rate: 0.8,
    bytes_served: 1000,
    hit_bytes: 800,
    miss_bytes: 200,
    avg_latency_ms: 50,
    avg_hit_latency_ms: 20,
    avg_miss_latency_ms: 200,
    time_saved_ms: 80 * 180,
    upstream_requests: 20,
    upstream_bytes: 180,
    errors: 0,
    ...overrides,
  }
}

describe('hitRateValue', () => {
  it('renders a missing sample as null rather than 0%', () => {
    expect(hitRateValue(period({ total_requests: 0, hit_rate: 0 }))).toBeNull()
    expect(hitRateValue(undefined)).toBeNull()
  })

  it('keeps a genuine zero-percent rate', () => {
    expect(hitRateValue(period({ total_requests: 10, hit_count: 0, hit_rate: 0 }))).toBe(0)
  })
})

describe('deriveUsageState', () => {
  it('never invents a first-use state from an empty request window', () => {
    expect(deriveUsageState({ period: undefined, hasHistoricalRequests: false })).toBe('unknown')
    expect(deriveUsageState({ period: period({ total_requests: 0 }), hasHistoricalRequests: false })).toBe('no-recent-evidence')
    expect(deriveUsageState({ period: period({ total_requests: 0 }), hasHistoricalRequests: true })).toBe('empty-period')
    expect(deriveUsageState({ period: period(), hasHistoricalRequests: false })).toBe('active')
  })

  it.each([
    ['never-used', 'waiting-first-request'],
    ['used', 'empty-period'],
    ['unknown', 'no-recent-evidence'],
  ] as const)('uses durable %s evidence independently of retained request history', (state, expected) => {
    expect(deriveUsageState({
      period: period({ total_requests: 0 }),
      hasHistoricalRequests: false,
      usage: { state, first_successful_request_at: null },
    })).toBe(expected)
  })

  it('does not mistake generic request activity for a successful artifact download', () => {
    expect(deriveUsageState({
      period: period(),
      hasHistoricalRequests: true,
      usage: { state: 'never-used', first_successful_request_at: null },
    })).toBe('waiting-first-request')
  })

  it('keeps confirmed successful usage ahead of an older never-used response', () => {
    expect(deriveUsageState({
      period: period({ total_requests: 0 }),
      hasHistoricalRequests: true,
      hasSuccessfulRequest: true,
      usage: { state: 'never-used', first_successful_request_at: null },
    })).toBe('empty-period')
    expect(deriveUsageState({
      period: period(),
      hasHistoricalRequests: false,
      hasSuccessfulRequest: true,
      usage: { state: 'never-used', first_successful_request_at: null },
    })).toBe('active')
  })

  it.each([undefined, period({ total_requests: Number.NaN }), period({ total_requests: -1 })])('does not turn missing or invalid statistics into first use', missing => {
    expect(deriveUsageState({
      period: missing,
      hasHistoricalRequests: false,
      usage: { state: 'never-used', first_successful_request_at: null },
    })).toBe('unknown')
  })
})

describe('latencyComparison', () => {
  it('keeps measured zero milliseconds and withholds averages without samples', () => {
    const zero = latencyComparison(period({ avg_hit_latency_ms: 0 }))
    expect(zero.hitMs).toBe(0)
    expect(zero.reductionPct).toBe(100)
    const missing = latencyComparison(period({ hit_requests: 0, miss_requests: 0 }))
    expect(missing.hitMs).toBeNull()
    expect(missing.missMs).toBeNull()
  })

  it('withholds a reduction when either side lacks samples', () => {
    const comparison = latencyComparison(period({ hit_requests: 3, miss_requests: 2 }))
    expect(comparison.sufficient).toBe(false)
    expect(comparison.reductionPct).toBeNull()
  })

  it('computes a reduction only with enough samples on both sides', () => {
    const comparison = latencyComparison(period({ hit_requests: 80, miss_requests: 20 }))
    expect(comparison.sufficient).toBe(true)
    expect(comparison.reductionPct).toBeCloseTo(90, 5)
  })
})

describe('timeSavedMs', () => {
  it('withholds the estimate when either side lacks comparable samples', () => {
    expect(timeSavedMs(undefined)).toBeNull()
    expect(timeSavedMs(period({ hit_requests: 3, miss_requests: 2 }))).toBeNull()
  })

  it('withholds a zero estimate: no comparable pair is not a measured zero', () => {
    expect(timeSavedMs(period({ time_saved_ms: 0 }))).toBeNull()
  })

  it('returns the period estimate once both sides are comparable', () => {
    expect(timeSavedMs(period())).toBe(14_400)
  })
})

describe('formatEstimatedDuration', () => {
  const stubT = ((key: string, opts: { value: string }) => `${key}|${opts.value}`) as unknown as TFunction

  it('rounds to the largest readable unit', () => {
    expect(formatEstimatedDuration(1_600, stubT)).toBe('overview.durationSeconds|1.6')
    expect(formatEstimatedDuration(90_000, stubT)).toBe('overview.durationMinutes|1.5')
    expect(formatEstimatedDuration(3.4 * 3_600_000, stubT)).toBe('overview.durationHours|3.4')
    expect(formatEstimatedDuration(36 * 3_600_000, stubT)).toBe('overview.durationDays|1.5')
  })
})

describe('requestOutcome', () => {
  it('separates policy blocks from ordinary failures', () => {
    expect(requestOutcome({ cache_result: 'unknown', status_code: 403 })).toBe('blocked')
    expect(requestOutcome({ cache_result: 'unknown', status_code: 502 })).toBe('failed')
    expect(requestOutcome({ cache_result: 'hit', status_code: 200 })).toBe('hit')
    expect(requestOutcome({ cache_result: 'miss', status_code: 200 })).toBe('miss')
    expect(requestOutcome({ status_code: 200 })).toBe('unknown')
  })
})

describe('originCoverageNote', () => {
  it('marks a window that starts before metering began as partial', () => {
    const note = originCoverageNote(
      { measured: true, since: '2026-09-15T00:00:00Z', window_complete: false },
      '2026-09-03T00:00:00Z',
    )
    expect(note.partial).toBe(true)
  })

  it('reports unmeasured coverage explicitly', () => {
    expect(originCoverageNote(undefined, undefined)).toEqual({ partial: true, since: null, measured: false })
  })
})

describe('deriveServiceStatus', () => {
  const readiness: ReadinessResponse = { status: 'ready', version: 'dev', checks: { database: 'ready', storage: 'ready' } }

  it('never reports all-clear while a signal is unavailable', () => {
    const status = deriveServiceStatus({
      nowAvailable: false,
      upstreams: [],
      policyNeedsAttention: false,
    })
    expect(status.health).toBe('unknown')
    expect(status.problems.map(problem => problem.code)).toEqual(['readiness-unavailable', 'status-unavailable'])
    expect(deriveServiceStatus({ nowAvailable: true, upstreams: [], policyNeedsAttention: false }).health).toBe('unknown')
    expect(deriveServiceStatus({ nowAvailable: true, readiness, upstreams: [], policyNeedsAttention: false }).health).toBe('healthy')
  })

  it('does not treat unavailable upstreams as a cache-serving outage', () => {
    const healthy = { id: 1, name: 'a', adapter: 'pypi', healthy: true, avg_latency_ms: 10, success_rate: 1 }
    const degraded = { id: 2, name: 'b', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0 }
    expect(deriveServiceStatus({ nowAvailable: true, nowStatus: 'degraded', readiness, upstreams: [healthy, degraded], policyNeedsAttention: false }).health).toBe('partial')
    expect(deriveServiceStatus({ nowAvailable: true, nowStatus: 'down', readiness, upstreams: [degraded], policyNeedsAttention: false }).health).toBe('partial')
  })

  it('reports reachable slow upstreams as a warning', () => {
    const slow = { id: 1, name: 'slow', adapter: 'npm', healthy: true, avg_latency_ms: 1100, success_rate: 1 }
    const status = deriveServiceStatus({ nowAvailable: true, readiness, upstreams: [slow], policyNeedsAttention: false })
    expect(status.health).toBe('partial')
    expect(status.problems).toEqual([{ code: 'upstreams-slow', severity: 'warning', count: 1, names: 'slow (npm #1)' }])
  })

  it.each(['database', 'storage'] as const)('retains a known %s failure when status probes fail', dependency => {
    const status = deriveServiceStatus({
      nowAvailable: false,
      readiness: { ...readiness, status: 'not_ready', checks: { ...readiness.checks, [dependency]: 'unavailable' } },
      readinessAvailable: false,
      readinessStale: true,
      upstreams: [],
      policyNeedsAttention: true,
    })
    expect(status.health).toBe('unavailable')
    expect(status.problems).toEqual(expect.arrayContaining([
      { code: dependency, severity: 'danger' },
      { code: 'readiness-stale', severity: 'warning' },
      { code: 'status-unavailable', severity: 'warning' },
      { code: 'policy', severity: 'warning' },
    ]))
  })

  it('does not reuse a stale healthy readiness sample as an all-clear', () => {
    expect(deriveServiceStatus({ nowAvailable: true, readiness, readinessStale: true, upstreams: [], policyNeedsAttention: false }).health).toBe('unknown')
  })

  it('keeps same-name upstreams as distinct objects identified by ID', () => {
    const upstream = (id: number, name: string) =>
      ({ id, name, adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0 })
    const status = deriveServiceStatus({
      nowAvailable: true,
      readiness,
      upstreams: [upstream(1, 'official'), upstream(2, 'official'), upstream(3, 'official'), upstream(4, 'tuna')],
      policyNeedsAttention: false,
    })
    expect(status.problems[0]).toMatchObject({
      code: 'upstreams', count: 4, names: 'official (npm #1), official (npm #2), official (npm #3)',
    })
    expect(status.upstreamIssues.map(item => item.id)).toEqual([1, 2, 3, 4])
    expect(status.affectedUpstreamCount).toBe(4)
    expect(status.upstreamCheckCount).toBe(4)
  })

  it('retains unavailable and slow checks for one object without double-counting the object', () => {
    const item = { id: 12, name: 'official', adapter: 'npm', healthy: false, avg_latency_ms: 1300, success_rate: 0.2 }
    const status = deriveServiceStatus({
      nowAvailable: true, readiness, upstreams: [item, item], policyNeedsAttention: false,
    })
    expect(status.health).toBe('partial')
    expect(status.upstreamIssues).toEqual([{
      ...item, key: 'upstream:12', state: 'unavailable', checks: ['unavailable', 'slow'],
    }])
    expect(status.problems.map(problem => problem.code)).toEqual(['upstreams', 'upstreams-slow'])
    expect(status.affectedUpstreamCount).toBe(1)
    expect(status.upstreamCheckCount).toBe(2)
    expect(status.checkCount).toBe(2)
  })

  it.each([
    { flags: { upstreamsAvailable: false }, code: 'upstream-status-unavailable' },
    { flags: { upstreamsStale: true }, code: 'upstream-status-stale' },
    { flags: { policyAvailable: false }, code: 'policy-status-unavailable' },
    { flags: { policyStale: true }, code: 'policy-status-stale' },
  ])('keeps $code distinct from a confirmed failure', ({ flags, code }) => {
    const status = deriveServiceStatus({
      nowAvailable: true, readiness, upstreams: [], policyNeedsAttention: false, ...flags,
    })
    expect(status.health).toBe('unknown')
    expect(status.problems).toEqual([{ code, severity: 'warning' }])
    expect(status.upstreamCheckCount).toBe(0)
    expect(status.checkCount).toBe(1)
  })

  it('retains confirmed upstream and policy failures while another signal is loading or stale', () => {
    const status = deriveServiceStatus({
      nowAvailable: false,
      upstreams: [{ id: 9, name: 'mirror', adapter: 'pypi', healthy: false, avg_latency_ms: 1200, success_rate: 0.1 }],
      upstreamsAvailable: true,
      upstreamsStale: true,
      policyNeedsAttention: true,
      policyStale: true,
    })
    expect(status.health).toBe('partial')
    expect(status.upstreamIssues[0].checks).toEqual(['unavailable', 'slow'])
    expect(status.problems.map(problem => problem.code)).toEqual([
      'readiness-unavailable', 'status-unavailable', 'upstream-status-stale', 'policy-status-stale',
      'upstreams', 'upstreams-slow', 'policy',
    ])
    expect(status.affectedUpstreamCount).toBe(1)
    expect(status.upstreamCheckCount).toBe(2)
    expect(status.checkCount).toBe(7)
  })

  it('retains all healthy objects for the always-available status details', () => {
    const upstream = { id: 3, name: 'official', adapter: 'npm', healthy: true, avg_latency_ms: 40, success_rate: 1 }
    const status = deriveServiceStatus({
      nowAvailable: true, readiness, upstreams: [upstream], policyNeedsAttention: false,
      upstreamsAvailable: true, policyAvailable: true,
    })
    expect(status.health).toBe('healthy')
    expect(status.upstreams).toEqual([{ ...upstream, key: 'upstream:3', state: 'healthy', checks: [] }])
    expect(status.upstreamIssues).toEqual([])
    expect(status.affectedUpstreamCount).toBe(0)
    expect(status.checkCount).toBe(0)
  })
})

describe('buildUpstreamHealth', () => {
  function upstream(overrides: Partial<DashboardUpstream> = {}): DashboardUpstream {
    return { id: 1, name: 'official', adapter: 'npm', healthy: true, avg_latency_ms: 10, success_rate: 1, ...overrides }
  }

  it('preserves unidentified legacy rows instead of merging them by zero ID or name', () => {
    const objects = buildUpstreamHealth([
      upstream({ id: 0, healthy: false }),
      upstream({ id: 0, adapter: 'pypi', healthy: false }),
    ])
    expect(objects.map(item => item.key)).toEqual(['unidentified:0', 'unidentified:1'])
    expect(objects).toHaveLength(2)
  })

  it('orders affected objects before healthy ones, preserving stable identities and raw metrics', () => {
    const healthy = upstream({ id: 8 })
    const slow = upstream({ id: 9, avg_latency_ms: 1000, success_rate: 0.8 })
    const failed = upstream({ id: 10, healthy: false, avg_latency_ms: 300, success_rate: 0.2 })
    const objects = buildUpstreamHealth([healthy, slow, failed])
    expect(objects.map(item => item.id)).toEqual([10, 9, 8])
    expect(objects[1]).toEqual({ ...slow, key: 'upstream:9', state: 'slow', checks: ['slow'] })
  })

  it('keeps the one-second threshold independent from availability', () => {
    const objects = buildUpstreamHealth([
      upstream({ id: 1, avg_latency_ms: 999 }),
      upstream({ id: 2, healthy: false, avg_latency_ms: 1000 }),
    ])
    expect(objects.find(item => item.id === 1)?.checks).toEqual([])
    expect(objects.find(item => item.id === 2)?.checks).toEqual(['unavailable', 'slow'])
  })
})

describe('sparklineHasVariance', () => {
  it('hides flat or single-sample series and keeps a real range', () => {
    expect(sparklineHasVariance([])).toBe(false)
    expect(sparklineHasVariance([5])).toBe(false)
    expect(sparklineHasVariance([5, 5, 5, 5])).toBe(false)
    expect(sparklineHasVariance([5, 5.4, 5.1])).toBe(true)
  })
})

describe('periodChange', () => {
  it('withholds a delta without a comparable baseline', () => {
    expect(periodChange(2, null)).toBeNull()
    expect(periodChange(2, 0)).toBeNull()
    expect(periodChange(2, 1)).toBe(100)
  })
})

describe('sparklineGeometry', () => {
  it('returns nothing below two samples', () => {
    expect(sparklineGeometry([])).toBeNull()
    expect(sparklineGeometry([4])).toBeNull()
  })

  it('draws a smooth monotone curve that stays inside the box', () => {
    const geometry = sparklineGeometry([1, 3, 2, 6])
    expect(geometry).not.toBeNull()
    expect(geometry!.line.startsWith('M ')).toBe(true)
    expect(geometry!.line).toContain('C ')
    expect(geometry!.area.endsWith('Z')).toBe(true)
    expect(geometry!.last.x).toBeCloseTo(93, 5)
    expect(geometry!.last.y).toBeGreaterThanOrEqual(3)
    expect(geometry!.last.y).toBeLessThanOrEqual(27)
  })

  it('keeps a flat series flat instead of overshooting', () => {
    const geometry = sparklineGeometry([5, 5, 5, 5])
    const ys = [...geometry!.line.matchAll(/(?:M|C)\s*[-\d.]+ ([-\d.]+)|,\s*[-\d.]+ ([-\d.]+)/g)]
      .map(match => Number(match[1] ?? match[2]))
    expect(new Set(ys).size).toBe(1)
  })
})

describe('buildRequestFlow', () => {
  const measuredNow: NowResponse = {
    status: 'healthy',
    uptime_seconds: 600,
    now_unix: 1786032000,
    version: 'dev',
    rate: {
      requests_per_min: 12,
      ingress_bps: 10,
      egress_bps: 20,
      has_data: true,
      measured: true,
      window_seconds: 60,
      service_requests_per_sec: 1,
      service_bytes_per_sec: 1024,
      origin_requests_per_sec: 0.1,
      origin_bytes_per_sec: 2048,
    },
    upstreams: { healthy: 1, total: 1 },
    sparkline: [],
  }
  const measuredCoverage = { measured: true, since: null, window_complete: true }

  it('withholds live rates until the 60s meter reports a sample', () => {
    const model = buildRequestFlow({
      now: { ...measuredNow, rate: { ...measuredNow.rate, measured: false } },
      period: period(),
      coverage: measuredCoverage,
      rangeStart: undefined,
    })
    expect(model.liveServiceBytesPerSec).toBeNull()
    expect(model.liveOriginBytesPerSec).toBeNull()
    expect(model.hitRate).toBe(0.8)
  })

  it('renders a period without a hit/miss sample as unknown, not 0%', () => {
    const model = buildRequestFlow({
      now: measuredNow,
      period: period({ total_requests: 0, hit_requests: 0, miss_requests: 0, hit_rate: 0, bytes_served: 0 }),
      coverage: measuredCoverage,
      rangeStart: undefined,
    })
    expect(model.hitRate).toBeNull()
    expect(model.hitRequests).toBe(0)
    expect(model.missRequests).toBe(0)
    expect(model.servedBytes).toBe(0)
    expect(model.servedRequests).toBe(0)
  })

  it('treats an unmetered origin window as not collected, not zero', () => {
    const model = buildRequestFlow({
      now: measuredNow,
      period: period(),
      coverage: { measured: false, since: null, window_complete: false },
      rangeStart: undefined,
    })
    expect(model.originMeasured).toBe(false)
    expect(model.originBytes).toBeNull()
    expect(model.originRequests).toBeNull()
    // Client-facing totals stay measurable even when origin metering never ran.
    expect(model.servedBytes).toBe(1000)
    expect(model.hitRequests).toBe(80)
  })

  it('passes measured zeros through with the live rails intact', () => {
    const model = buildRequestFlow({
      now: measuredNow,
      period: period({ bytes_served: 0, upstream_bytes: 0, upstream_requests: 0 }),
      coverage: measuredCoverage,
      rangeStart: undefined,
    })
    expect(model.servedBytes).toBe(0)
    expect(model.originBytes).toBe(0)
    expect(model.originRequests).toBe(0)
    expect(model.liveServiceBytesPerSec).toBe(1024)
    expect(model.liveOriginBytesPerSec).toBe(2048)
  })
})
