import type {
  DashboardPeriod,
  DashboardRange,
  DashboardUpstream,
  NowResponse,
  OriginCoverage,
} from './adminApi.types'
import type { TFunction } from 'i18next'
import { readLocalStorage, writeLocalStorage } from './storage'
import { upstreamStatus } from './upstreamStatus'

export const DASHBOARD_RANGES: readonly DashboardRange[] = ['1h', '24h', '7d', '30d']

/** First-run default. The operator's choice is remembered after that. */
const DEFAULT_DASHBOARD_RANGE: DashboardRange = '30d'

const RANGE_STORAGE_KEY = 'depsilo-dashboard-range'

function isDashboardRange(value: unknown): value is DashboardRange {
  return typeof value === 'string' && (DASHBOARD_RANGES as readonly string[]).includes(value)
}

export function readDashboardRange(): DashboardRange {
  const stored = readLocalStorage(RANGE_STORAGE_KEY)
  return isDashboardRange(stored) ? stored : DEFAULT_DASHBOARD_RANGE
}

export function writeDashboardRange(range: DashboardRange): void {
  writeLocalStorage(RANGE_STORAGE_KEY, range)
}

/**
 * Hit rate for display. Returns null when the period has no hit/miss sample so
 * the UI renders "—" instead of a misleading 0% or 100%.
 */
export function hitRateValue(period: DashboardPeriod | undefined): number | null {
  if (!period || period.total_requests <= 0) return null
  return period.hit_rate
}

/** Minimum samples before a latency comparison is presented as a claim. */
const MIN_LATENCY_SAMPLES = 20

export interface LatencyComparison {
  hitMs: number | null
  missMs: number | null
  hitSamples: number
  missSamples: number
  /** Estimated reduction of hit latency vs miss latency, when credible. */
  reductionPct: number | null
  sufficient: boolean
}

/**
 * Compares hit vs miss response time using the period's own sample counts.
 * Averages are the period-weighted averages the server already computed, so
 * this never averages pre-averaged buckets.
 */
export function latencyComparison(period: DashboardPeriod | undefined): LatencyComparison {
  const hitSamples = period ? Math.max(0, period.hit_requests) : 0
  const missSamples = period ? Math.max(0, period.miss_requests) : 0
  const hitMs = period && period.avg_hit_latency_ms > 0 ? period.avg_hit_latency_ms : null
  const missMs = period && period.avg_miss_latency_ms > 0 ? period.avg_miss_latency_ms : null
  const sufficient = hitSamples >= MIN_LATENCY_SAMPLES
    && missSamples >= MIN_LATENCY_SAMPLES
    && hitMs !== null
    && missMs !== null
    && missMs > 0
  const reductionPct = sufficient && missMs > hitMs
    ? ((missMs - hitMs) / missMs) * 100
    : null
  return { hitMs, missMs, hitSamples, missSamples, reductionPct, sufficient }
}

export interface OriginCoverageNote {
  partial: boolean
  since: string | null
  measured: boolean
}

/**
 * Reports whether the selected range's origin totals cover the whole window.
 * A window that starts before origin metering began must be labelled partial.
 */
export function originCoverageNote(
  coverage: OriginCoverage | undefined,
  rangeStart: string | undefined,
): OriginCoverageNote {
  if (!coverage || !coverage.measured) {
    return { partial: true, since: null, measured: false }
  }
  if (!rangeStart) {
    return { partial: !coverage.window_complete, since: coverage.since, measured: true }
  }
  const start = Date.parse(rangeStart)
  const since = coverage.since ? Date.parse(coverage.since) : NaN
  const partial = !coverage.window_complete
    || (Number.isFinite(start) && Number.isFinite(since) && start < since)
  return { partial, since: coverage.since, measured: true }
}

/** Percentage with one decimal; null renders as an em dash. */
export function formatPercentRatio(ratio: number | null, fractionDigits = 1): string {
  if (ratio === null || !Number.isFinite(ratio)) return '—'
  return `${(ratio * 100).toFixed(fractionDigits)}%`
}

export function formatSignedPercent(percent: number | null, fractionDigits = 1): string {
  if (percent === null || !Number.isFinite(percent)) return '—'
  return `${percent >= 0 ? '+' : ''}${percent.toFixed(fractionDigits)}%`
}

/**
 * Relative change between the selected period and the same-length preceding
 * period. Returns null when the comparison is not complete (no baseline).
 */
export function periodChange(current: number | null, previous: number | null): number | null {
  if (current === null || previous === null || previous === 0) return null
  return ((current - previous) / previous) * 100
}

/**
 * Estimated waiting avoided by cache hits in the period. Withheld unless the
 * hit and miss latency comparison has enough comparable samples on both sides,
 * because the server reports 0 for "no comparable pair" as well as for a real
 * zero. Always presented as an estimate: it is the period's latency difference
 * summed over hits, not a measured reduction of any downstream build.
 */
export function timeSavedMs(period: DashboardPeriod | undefined): number | null {
  if (!period) return null
  const comparison = latencyComparison(period)
  if (!comparison.sufficient || comparison.reductionPct === null) return null
  return period.time_saved_ms > 0 ? period.time_saved_ms : null
}

/**
 * Overview metric dictionary — the only place that decides what each Overview
 * region means. UI copy stays out of this file; the gating lives here so the
 * request-flow visual, the tiles and the tests all read from one definition.
 *
 * - Status strip health      deriveServiceStatus(...) + now.status/upstreams/policy
 * - Status strip activity    now.rate.service_requests_per_sec, gated by rate.measured
 * - Flow rail service        now.rate.service_bytes_per_sec (last 60s, Depsilo → clients)
 * - Flow rail origin         now.rate.origin_bytes_per_sec  (last 60s, upstreams → Depsilo)
 * - Flow node clients        window.bytes_served / total_requests
 * - Flow node Depsilo        window.hit_rate + hit/miss requests (no sample → —)
 * - Flow node upstream       window.upstream_bytes / upstream_requests, gated by
 *                            origin coverage: an unmetered window is "not collected",
 *                            never a measured zero.
 * - Flow outcome BLOCK       Countless by design. Blocked rows are excluded from the
 *                            period aggregate (internal/api/admin/dashboard.go), so
 *                            they cannot share the hit-rate denominator. Security owns
 *                            the blocked/quarantine record; the chip links there.
 * - Cache benefits           hit_rate, hit_bytes (estimated savings), latencyComparison
 * - Runtime resources        runtime.process / runtime.cache / now.rate
 *
 * A zero is rendered only when the API measured a zero. "Not collected", "no
 * sample" and "unsupported" keep their own honest states throughout.
 */

export interface RequestFlowModel {
  /** Live 60s byte rates; null until the meter has produced a sample. */
  liveServiceBytesPerSec: number | null
  liveOriginBytesPerSec: number | null
  /** Period hit rate; null without a hit/miss sample (renders —, not 0%). */
  hitRate: number | null
  hitRequests: number | null
  missRequests: number | null
  /** Period totals; 0 stays a measured zero once the payload exists. */
  servedBytes: number | null
  servedRequests: number | null
  /** Upstream period totals; null when origin metering never ran. */
  originBytes: number | null
  originRequests: number | null
  originMeasured: boolean
}

export function buildRequestFlow(args: {
  now?: NowResponse
  period?: DashboardPeriod
  coverage?: OriginCoverage
  rangeStart?: string
}): RequestFlowModel {
  const { now, period, coverage, rangeStart } = args
  const measured = now?.rate.measured === true
  const hitRate = hitRateValue(period)
  const originMeasured = originCoverageNote(coverage, rangeStart).measured
  return {
    liveServiceBytesPerSec: measured ? (now?.rate.service_bytes_per_sec ?? 0) : null,
    liveOriginBytesPerSec: measured ? (now?.rate.origin_bytes_per_sec ?? 0) : null,
    hitRate,
    hitRequests: hitRate === null ? null : (period?.hit_requests ?? 0),
    missRequests: hitRate === null ? null : (period?.miss_requests ?? 0),
    servedBytes: period ? period.bytes_served : null,
    servedRequests: period ? period.total_requests : null,
    originBytes: period && originMeasured ? period.upstream_bytes : null,
    originRequests: period && originMeasured ? period.upstream_requests : null,
    originMeasured,
  }
}

/**
 * Coarse duration for the estimated time saved, rounded to one unit so the
 * figure stays readable ("3.4 小时", not "3 小时 24 分 12 秒").
 */
export function formatEstimatedDuration(ms: number, t: TFunction): string {
  const seconds = ms / 1000
  if (seconds < 60) return t('overview.durationSeconds', { value: trimTo(seconds) })
  const minutes = seconds / 60
  if (minutes < 60) return t('overview.durationMinutes', { value: trimTo(minutes) })
  const hours = minutes / 60
  if (hours < 24) return t('overview.durationHours', { value: trimTo(hours) })
  return t('overview.durationDays', { value: trimTo(hours / 24) })
}

function trimTo(value: number): string {
  return value < 10 ? value.toFixed(1) : value.toFixed(0)
}

export interface SparklineGeometry {
  /** Smooth path for the line. */
  line: string
  /** Closed path for the soft area fill beneath the line. */
  area: string
  /** Last sample position, for the end dot. */
  last: { x: number; y: number }
}

function fmt(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2)
}

/**
 * Builds a monotone-cubic sparkline that never overshoots the sampled data
 * (Fritsch–Carlson tangents). Straight polylines look coarse at small sizes;
 * a plain Catmull-Rom would invent peaks between samples, so monotone is the
 * honest smoothing choice. Returns null with fewer than two samples.
 */
export function sparklineGeometry(
  series: number[],
  width = 96,
  height = 30,
  pad = 3,
): SparklineGeometry | null {
  if (series.length < 2) return null
  const min = Math.min(...series)
  const max = Math.max(...series)
  const span = max - min || 1
  const x = (index: number) => pad + (index * (width - 2 * pad)) / (series.length - 1)
  const y = (value: number) => height - pad - ((value - min) / span) * (height - 2 * pad)
  const points: Array<[number, number]> = series.map((value, index) => [x(index), y(value)])

  const n = points.length
  const dx: number[] = []
  const slope: number[] = []
  for (let i = 0; i < n - 1; i += 1) {
    dx[i] = points[i + 1][0] - points[i][0]
    slope[i] = dx[i] === 0 ? 0 : (points[i + 1][1] - points[i][1]) / dx[i]
  }
  const tangent: number[] = new Array(n)
  tangent[0] = slope[0] ?? 0
  tangent[n - 1] = slope[n - 2] ?? 0
  for (let i = 1; i < n - 1; i += 1) {
    if (slope[i - 1] * slope[i] <= 0) {
      tangent[i] = 0
    } else {
      const w1 = 2 * dx[i] + dx[i - 1]
      const w2 = dx[i] + 2 * dx[i - 1]
      tangent[i] = (w1 + w2) / (w1 / slope[i - 1] + w2 / slope[i])
    }
  }

  let line = `M ${fmt(points[0][0])} ${fmt(points[0][1])}`
  for (let i = 0; i < n - 1; i += 1) {
    const c1x = points[i][0] + dx[i] / 3
    const c1y = points[i][1] + (tangent[i] * dx[i]) / 3
    const c2x = points[i + 1][0] - dx[i] / 3
    const c2y = points[i + 1][1] - (tangent[i + 1] * dx[i]) / 3
    line += ` C ${fmt(c1x)} ${fmt(c1y)}, ${fmt(c2x)} ${fmt(c2y)}, ${fmt(points[i + 1][0])} ${fmt(points[i + 1][1])}`
  }

  const area = `${line} L ${fmt(points[n - 1][0])} ${height} L ${fmt(points[0][0])} ${height} Z`
  return { line, area, last: { x: points[n - 1][0], y: points[n - 1][1] } }
}

export type ServiceHealth = 'healthy' | 'partial' | 'unavailable' | 'unknown'

export type ServiceProblemCode = 'status-unavailable' | 'upstreams' | 'policy' | 'cache' | 'degraded'

export interface ServiceProblem {
  code: ServiceProblemCode
  severity: 'warning' | 'danger'
  count?: number
  names?: string
  percent?: number
}

export interface ServiceStatusModel {
  health: ServiceHealth
  problems: ServiceProblem[]
}

/**
 * Combines availability, upstream health, and policy-snapshot state into one
 * status. Availability, upstreams, policy, and data collection are distinct:
 * a missing sample is "unknown", not a healthy service, and idle is not failure.
 */
export function deriveServiceStatus(args: {
  nowAvailable: boolean
  nowStatus?: NowResponse['status']
  upstreams: DashboardUpstream[]
  policyNeedsAttention: boolean
  cacheUsagePercent?: number
}): ServiceStatusModel {
  if (!args.nowAvailable) {
    return { health: 'unknown', problems: [{ code: 'status-unavailable', severity: 'warning' }] }
  }

  const problems: ServiceProblem[] = []
  const unhealthy = args.upstreams.filter(item => upstreamStatus(item) !== 'healthy')
  if (unhealthy.length > 0) {
    const allDown = unhealthy.length === args.upstreams.length
    problems.push({
      code: 'upstreams',
      severity: allDown ? 'danger' : 'warning',
      count: unhealthy.length,
      names: unhealthy.slice(0, 3).map(item => item.name).join(', '),
    })
  } else if (args.nowStatus === 'down' || args.nowStatus === 'degraded') {
    problems.push({ code: 'degraded', severity: args.nowStatus === 'down' ? 'danger' : 'warning' })
  }
  if (args.policyNeedsAttention) {
    problems.push({ code: 'policy', severity: 'warning' })
  }
  if (args.cacheUsagePercent !== undefined && args.cacheUsagePercent > 80) {
    problems.push({ code: 'cache', severity: 'warning', percent: args.cacheUsagePercent })
  }

  if (problems.some(problem => problem.severity === 'danger')) {
    return { health: 'unavailable', problems }
  }
  if (problems.length > 0) {
    return { health: 'partial', problems }
  }
  return { health: 'healthy', problems: [] }
}

export type RequestOutcome = 'hit' | 'miss' | 'blocked' | 'failed' | 'unknown'

/**
 * Normalizes one client request row into the outcome shown in the list and
 * detail dialog. Blocks are distinguished from ordinary failures by status;
 * the plain cache_result value alone cannot tell "refused by policy" apart.
 */
export function requestOutcome(log: {
  cache_result?: string
  hit?: boolean
  status_code?: number
}): RequestOutcome {
  const status = log.status_code ?? 0
  if (status === 403 || status === 451) return 'blocked'
  if (status >= 400) return 'failed'
  if (log.cache_result === 'hit') return 'hit'
  if (log.cache_result === 'miss') return 'miss'
  if (log.hit === true) return 'hit'
  return 'unknown'
}

export const REQUEST_OUTCOME_META: Record<RequestOutcome, { key: string; variant: 'success' | 'warning' | 'error' | 'neutral' }> = {
  hit: { key: 'overview.outcomeHit', variant: 'success' },
  miss: { key: 'overview.outcomeMiss', variant: 'warning' },
  blocked: { key: 'overview.outcomeBlocked', variant: 'error' },
  failed: { key: 'overview.outcomeFailed', variant: 'error' },
  unknown: { key: 'overview.outcomeUnknown', variant: 'neutral' },
}

export function rangeLabelKey(range: DashboardRange): string {
  switch (range) {
    case '1h': return 'overview.range1h'
    case '7d': return 'overview.range7d'
    case '30d': return 'overview.range30d'
    default: return 'overview.range24h'
  }
}

/** Whether the selected range's origin bytes cover the whole window. */
export function coverageDetail(
  coverage: OriginCoverage | undefined,
  rangeStart: string | undefined,
  t: TFunction,
  locale?: string,
): string {
  const note = originCoverageNote(coverage, rangeStart)
  if (!note.measured) return t('overview.originNotCollected')
  if (!note.partial) return ''
  return note.since
    ? t('overview.originPartialSince', {
      time: new Date(note.since).toLocaleString(locale, {
        year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
      }),
    })
    : t('overview.originPartial')
}

export function formatRelativeSeconds(seconds: number, t: TFunction): string {
  if (seconds < 5) return t('now.justNow')
  if (seconds < 60) return t('now.secondsAgo', { count: seconds })
  if (seconds < 3600) return t('now.minutesAgo', { count: Math.floor(seconds / 60) })
  if (seconds < 86400) return t('now.hoursAgo', { count: Math.floor(seconds / 3600) })
  return t('now.daysAgo', { count: Math.floor(seconds / 86400) })
}

export function formatUptimeSeconds(seconds: number, t: TFunction): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  if (days > 0) return t('now.uptimeDH', { days, hours })
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours > 0) return t('now.uptimeHM', { hours, minutes })
  return t('now.uptimeMin', { minutes })
}
