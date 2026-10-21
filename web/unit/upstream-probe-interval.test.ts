import { describe, expect, it } from 'vitest'
import { normalizeProbeInterval } from '../src/admin/upstreams/probeInterval'

describe('probe interval round trip', () => {
  it.each([
    ['30m0s', '30m'], ['1h0m0s', '1h'], ['1m0s', '1m'], ['1800s', '30m'],
    ['15s', '15s'], ['45m30s', '45m30s'], ['1.5s', '1.5s'], ['invalid', 'invalid'],
  ])('represents %s without changing its duration', (value, expected) => {
    expect(normalizeProbeInterval(value)).toBe(expected)
  })
})
