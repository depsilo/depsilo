import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatTime } from '../src/lib/utils'

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
