import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import type { DashboardPeriod } from '../src/lib/adminApi.types'
import {
  deriveServiceStatus,
  formatEstimatedDuration,
  hitRateValue,
  latencyComparison,
  originCoverageNote,
  periodChange,
  requestOutcome,
  sparklineGeometry,
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

describe('latencyComparison', () => {
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
  it('never reports all-clear while a signal is unavailable', () => {
    const status = deriveServiceStatus({
      nowAvailable: false,
      upstreams: [],
      policyNeedsAttention: false,
    })
    expect(status.health).toBe('unknown')
    expect(status.problems).toHaveLength(1)
  })

  it('distinguishes degraded upstreams from an outage', () => {
    const healthy = { id: 1, name: 'a', adapter: 'pypi', healthy: true, avg_latency_ms: 10, success_rate: 1 }
    const degraded = { id: 2, name: 'b', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0 }
    expect(deriveServiceStatus({ nowAvailable: true, nowStatus: 'degraded', upstreams: [healthy, degraded], policyNeedsAttention: false }).health).toBe('partial')
    expect(deriveServiceStatus({ nowAvailable: true, nowStatus: 'down', upstreams: [degraded], policyNeedsAttention: false }).health).toBe('unavailable')
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
