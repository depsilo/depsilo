import { describe, expect, it } from 'vitest'
import { upstreamStatus } from '../src/lib/upstreamStatus'

describe('upstream health observations', () => {
  it('does not mistake default routing eligibility for measured health', () => {
    expect(upstreamStatus({ healthy: true, avg_latency_ms: 0, has_observations: false })).toBe('unknown')
    expect(upstreamStatus({ healthy: false, avg_latency_ms: 0, has_observations: false })).toBe('failed')
  })

  it.each([
    [0, 'healthy'],
    [1, 'healthy'],
    [999, 'healthy'],
    [1000, 'degraded'],
  ] as const)('classifies observed latency %i ms without dropping zero samples', (latency, status) => {
    expect(upstreamStatus({ healthy: true, avg_latency_ms: latency, has_observations: true })).toBe(status)
  })

  it('preserves the public API status when observation metadata is absent', () => {
    expect(upstreamStatus({ healthy: true, avg_latency_ms: 0 })).toBe('healthy')
    expect(upstreamStatus({ healthy: true, avg_latency_ms: 1000 })).toBe('degraded')
    expect(upstreamStatus({ healthy: false, avg_latency_ms: 20 })).toBe('failed')
  })
})
