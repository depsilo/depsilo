/**
 * URL state shared by the Admin list pages: a multi-value ecosystem filter
 * and a time window. Pages keep both in the query string so a filtered view
 * survives reloads and can be shared.
 */

const ECOSYSTEM_ID = /^[a-z0-9][a-z0-9_-]{0,31}$/

/**
 * Reads `pypi,npm` into ids, dropping blanks, duplicates, and anything
 * `allowed` rejects (by default, anything that is not id-shaped), so a
 * hand-edited URL canonicalises to the filters the page can show.
 */
export function parseEcosystemList(
  raw: string | null | undefined,
  allowed: (id: string) => boolean = id => ECOSYSTEM_ID.test(id),
): string[] {
  if (!raw) return []
  const seen = new Set<string>()
  for (const part of raw.split(',')) {
    const value = part.trim()
    if (value && allowed(value)) seen.add(value)
  }
  return [...seen]
}

/** The query-string form: comma-joined, or undefined for no filter. */
export function serializeEcosystemList(ids: readonly string[]): string | undefined {
  return ids.length > 0 ? ids.join(',') : undefined
}

/** Adds or removes one ecosystem, keeping the others in their order. */
export function toggleEcosystem(ids: readonly string[], id: string): string[] {
  return ids.includes(id) ? ids.filter(item => item !== id) : [...ids, id]
}

export const TIME_PRESETS = ['1h', '24h', '7d', '30d', 'all'] as const
export type TimePreset = (typeof TIME_PRESETS)[number]
export type TimeRangeValue =
  | { preset: TimePreset }
  | { preset: 'custom'; start?: string; end?: string }

const PRESET_MS: Record<Exclude<TimePreset, 'all'>, number> = {
  '1h': 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

function isValidInstant(value: string | null): value is string {
  return value !== null && value !== '' && !Number.isNaN(Date.parse(value))
}

/**
 * Reads `range`, `start`, and `end`. A custom range needs at least one valid
 * bound; anything unknown falls back to the page's default preset. `today`
 * is the audit log's former first preset and reads as 24 hours.
 */
export function parseTimeRange(params: URLSearchParams, fallback: TimePreset): TimeRangeValue {
  const range = params.get('range')
  if (range === 'custom') {
    const start = params.get('start')
    const end = params.get('end')
    const value: TimeRangeValue = { preset: 'custom' }
    if (isValidInstant(start)) value.start = new Date(start).toISOString()
    if (isValidInstant(end)) value.end = new Date(end).toISOString()
    if (value.start || value.end) return value
    return { preset: fallback }
  }
  if (range === 'today') return { preset: '24h' }
  return (TIME_PRESETS as readonly string[]).includes(range ?? '') ? { preset: range as TimePreset } : { preset: fallback }
}

/** Writes a range into params; the page default is left out of the URL. */
export function writeTimeRange(params: URLSearchParams, value: TimeRangeValue, fallback: TimePreset) {
  params.delete('range')
  params.delete('start')
  params.delete('end')
  if (value.preset === 'custom') {
    params.set('range', 'custom')
    if (value.start) params.set('start', value.start)
    if (value.end) params.set('end', value.end)
  } else if (value.preset !== fallback) {
    params.set('range', value.preset)
  }
}

/**
 * The API window for a range. Presets count back from `anchor` (the moment
 * the view was loaded or refreshed), so a page's query stays stable between
 * refreshes instead of sliding on every render. By default a preset also
 * ends at the anchor, so the list and its export are the same snapshot; a
 * page that polls for new rows passes `openEnd`.
 */
export function resolveTimeWindow(
  value: TimeRangeValue,
  anchor: number,
  { openEnd = false }: { openEnd?: boolean } = {},
): { start?: string; end?: string } {
  if (value.preset === 'custom') return { start: value.start, end: value.end }
  if (value.preset === 'all') return {}
  const start = new Date(anchor - PRESET_MS[value.preset]).toISOString()
  return openEnd ? { start } : { start, end: new Date(anchor).toISOString() }
}
