import { describe, expect, it } from 'vitest'

import {
  parseEcosystemList,
  parseTimeRange,
  resolveTimeWindow,
  serializeEcosystemList,
  toggleEcosystem,
  writeTimeRange,
} from '../src/admin/listFilters'

describe('ecosystem list filter', () => {
  it('reads comma-separated ids and drops junk and duplicates', () => {
    expect(parseEcosystemList('pypi, npm,,pypi,<script>,Cargo')).toEqual(['pypi', 'npm'])
    expect(parseEcosystemList(null)).toEqual([])
    expect(parseEcosystemList('pypi,invalid-ecosystem', id => id === 'pypi')).toEqual(['pypi'])
  })

  it('toggles one id and serializes back to the query form', () => {
    expect(toggleEcosystem(['pypi'], 'npm')).toEqual(['pypi', 'npm'])
    expect(toggleEcosystem(['pypi', 'npm'], 'pypi')).toEqual(['npm'])
    expect(serializeEcosystemList(['pypi', 'npm'])).toBe('pypi,npm')
    expect(serializeEcosystemList([])).toBeUndefined()
  })
})

describe('time range filter', () => {
  const anchor = Date.parse('2026-11-16T12:00:00Z')

  it('resolves presets back from the anchor and "all" to no bounds', () => {
    // A preset is a snapshot ending at the anchor, so the list and its
    // export agree; a polling page leaves the end open.
    expect(resolveTimeWindow({ preset: '1h' }, anchor)).toEqual({ start: '2026-11-16T11:00:00.000Z', end: '2026-11-16T12:00:00.000Z' })
    expect(resolveTimeWindow({ preset: '7d' }, anchor, { openEnd: true })).toEqual({ start: '2026-11-09T12:00:00.000Z' })
    expect(resolveTimeWindow({ preset: 'all' }, anchor)).toEqual({})
  })

  it('round-trips a custom range and keeps the default out of the URL', () => {
    const params = new URLSearchParams()
    writeTimeRange(params, { preset: 'custom', start: '2026-11-10T00:00:00.000Z', end: '2026-11-11T00:00:00.000Z' }, '24h')
    expect(parseTimeRange(params, '24h')).toEqual({ preset: 'custom', start: '2026-11-10T00:00:00.000Z', end: '2026-11-11T00:00:00.000Z' })
    writeTimeRange(params, { preset: '24h' }, '24h')
    expect(params.toString()).toBe('')
  })

  it('falls back on unknown or empty ranges and reads the old "today" as 24 hours', () => {
    expect(parseTimeRange(new URLSearchParams('range=decade'), 'all')).toEqual({ preset: 'all' })
    expect(parseTimeRange(new URLSearchParams('range=custom&start=nope'), 'all')).toEqual({ preset: 'all' })
    expect(parseTimeRange(new URLSearchParams('range=today'), 'all')).toEqual({ preset: '24h' })
  })
})
