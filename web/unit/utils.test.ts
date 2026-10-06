import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatLatency, formatTime } from '../src/lib/utils'

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date(2026, 7, 10, 12, 0, 0))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('formatTime locale contract', () => {
  const threeDaysAgo = new Date(2026, 7, 7, 12, 0, 0).toISOString()

  it('formats relative days in Chinese', () => {
    expect(formatTime(threeDaysAgo, 'relative', 'zh-CN')).toBe('3天前')
  })

  it('formats relative days in English', () => {
    expect(formatTime(threeDaysAgo, 'relative', 'en-US')).toBe('3 days ago')
  })

  it('follows the language applied to the document by i18n', () => {
    vi.stubGlobal('document', { documentElement: { lang: 'en' } })
    expect(formatTime(threeDaysAgo, 'relative')).toBe('3 days ago')
  })

  it('renders invalid timestamps as unavailable', () => {
    expect(formatTime('not-a-timestamp', 'relative', 'en-US')).toBe('-')
  })
})

describe('formatLatency unit contract', () => {
  it('keeps sub-second samples in milliseconds', () => {
    expect(formatLatency(0)).toBe('0 ms')
    expect(formatLatency(264.4)).toBe('264 ms')
    expect(formatLatency(999)).toBe('999 ms')
  })

  it('switches to seconds so a stalled upstream stays readable', () => {
    expect(formatLatency(1_000)).toBe('1.00 s')
    expect(formatLatency(30_120)).toBe('30.1 s')
    expect(formatLatency(59_999)).toBe('60.0 s')
  })

  it('switches to minutes beyond a minute', () => {
    expect(formatLatency(60_000)).toBe('1.0 min')
    expect(formatLatency(15 * 60_000)).toBe('15 min')
  })

  it('renders unknown values as unavailable instead of zero', () => {
    expect(formatLatency(Number.NaN)).toBe('—')
    expect(formatLatency(-1)).toBe('—')
  })
})
