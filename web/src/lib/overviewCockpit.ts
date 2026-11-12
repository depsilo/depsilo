/**
 * Shaping for the Overview cockpit: activity buckets, bursts, attention items,
 * protection coverage, and the Upstream fleet. Like dashboardOverview.ts, this
 * module holds no UI copy; it returns facts that components translate.
 */
import type {
  AdminUpstream,
  CapabilityFact,
  DashboardRange,
  DashboardTrendPoint,
  SecurityDashboard,
  UpstreamFailure,
} from './adminApi.types'
import type { ServiceProblemCode, ServiceStatusModel, UpstreamHealthStatus } from './dashboardOverview'

// ---------------------------------------------------------------------------
// Activity

/** A span of the activity chart: exact sums of the trend buckets inside it. */
export interface ActivityBucket {
  /** Unix seconds, inclusive: the bar's clock-aligned slot. */
  start: number
  /** Where data inside the slot begins; later than `start` only for a leading bar the window cuts short. */
  dataStart: number
  /** Unix seconds, exclusive. */
  end: number
  requests: number
  hits: number
  misses: number
  bytesHit: number
  bytesMiss: number
  /** Measured Depsilo→upstream bytes; zero where origin metering had not started. */
  bytesUpstream: number
  errors: number
}

const HOUR = 3600
/** Spans that divide a day, so bars line up with clock and calendar ticks. */
const CLOCK_SPANS = [
  60, 120, 300, 600, 900, 1800,
  HOUR, 2 * HOUR, 3 * HOUR, 4 * HOUR, 6 * HOUR, 8 * HOUR, 12 * HOUR, 24 * HOUR, 48 * HOUR, 72 * HOUR, 168 * HOUR,
]

function trendStep(points: DashboardTrendPoint[]): number {
  return points.length > 1 ? Math.max(1, points[1].bucket - points[0].bucket) : 60
}

/** Seconds to add to local wall-clock time to get UTC, for the viewer's zone at a given instant. */
export function localUtcOffsetSeconds(at: Date = new Date()): number {
  return at.getTimezoneOffset() * 60
}

/** The viewer's offset at each instant, so bars on either side of a daylight-saving change both follow the local clock. */
const viewerOffset = (unixSeconds: number) => localUtcOffsetSeconds(new Date(unixSeconds * 1000))

/**
 * The widest span that still draws at most `maxBars` bars. It is a multiple of
 * the trend series' own step, so a bar never splits a server bucket.
 */
export function activitySpan(points: DashboardTrendPoint[], maxBars: number): number {
  const step = trendStep(points)
  const wanted = step * Math.max(1, Math.ceil(points.length / Math.max(1, maxBars)))
  return CLOCK_SPANS.find(span => span >= wanted && span % step === 0) ?? wanted
}

function emptyBucket(start: number, end: number): ActivityBucket {
  return { start, dataStart: start, end, requests: 0, hits: 0, misses: 0, bytesHit: 0, bytesMiss: 0, bytesUpstream: 0, errors: 0 }
}

function addPoint(bucket: ActivityBucket, point: DashboardTrendPoint): void {
  bucket.requests += point.requests
  bucket.hits += point.hits
  bucket.misses += point.misses
  bucket.bytesHit += point.bytes_hit
  bucket.bytesMiss += point.bytes_miss
  bucket.bytesUpstream += point.upstream_bytes ?? 0
  bucket.errors += point.errors
}

/**
 * Sums trend buckets into `span`-second bars aligned to local clock boundaries.
 * A bar always holds whole server buckets: where the viewer's offset is not a
 * multiple of the server step (e.g. +5:30 against 2-hour buckets), the grid
 * shifts to the nearest step boundary, and each bar's start and end say
 * exactly which time its sum covers. The offset is taken per bucket, so a
 * daylight-saving change moves the grid with the local clock.
 */
export function rebucketActivity(
  points: DashboardTrendPoint[],
  span: number,
  utcOffsetSeconds: number | ((unixSeconds: number) => number) = viewerOffset,
): ActivityBucket[] {
  const step = trendStep(points)
  const offsetAt = typeof utcOffsetSeconds === 'function' ? utcOffsetSeconds : () => utcOffsetSeconds
  const groups = new Map<number, ActivityBucket>()
  let firstData = Infinity
  for (const point of points) {
    firstData = Math.min(firstData, point.bucket)
    const offset = Math.round(offsetAt(point.bucket) / step) * step
    const key = Math.floor((point.bucket - offset) / span)
    let group = groups.get(key)
    if (!group) {
      const start = key * span + offset
      group = emptyBucket(start, start + span)
      groups.set(key, group)
    }
    addPoint(group, point)
  }
  const bars = [...groups.values()].sort((a, b) => a.start - b.start)
  // The window rarely starts on a bar boundary; the leading bar says where its data begins.
  if (bars.length > 0 && bars[0].start < firstData) bars[0].dataStart = firstData
  return bars
}

/**
 * Contiguous runs of requests, newest first. One quiet bucket does not end a
 * run, so a build that pauses briefly still reads as one burst.
 */
export function findActivityBursts(points: DashboardTrendPoint[], limit: number): ActivityBucket[] {
  const step = trendStep(points)
  const bursts: ActivityBucket[] = []
  let current: ActivityBucket | null = null
  let quiet = 0
  for (const point of [...points].sort((a, b) => a.bucket - b.bucket)) {
    if (point.requests > 0) {
      if (!current) {
        current = emptyBucket(point.bucket, point.bucket + step)
        bursts.push(current)
      }
      current.end = point.bucket + step
      addPoint(current, point)
      quiet = 0
    } else if (current) {
      quiet += 1
      if (quiet > 1) current = null
    }
  }
  return bursts.reverse().slice(0, Math.max(0, limit))
}

/**
 * Whether a bar's start is a labelled tick on the time axis for this range.
 * Day ticks mark the first bar of each local day rather than an exact
 * midnight, because a shifted grid (see rebucketActivity) may never start at 00:00.
 */
export function isActivityTick(bucket: ActivityBucket, range: DashboardRange, previous?: ActivityBucket): boolean {
  const date = new Date(bucket.start * 1000)
  if (range === '1h') return date.getMinutes() % 10 === 0 && date.getSeconds() === 0
  if (range === '24h') return date.getMinutes() === 0 && date.getHours() % 4 === 0
  if (!previous) return false
  const newDay = new Date(previous.start * 1000).getDate() !== date.getDate()
  if (range === '7d') return newDay
  return newDay && date.getDate() % 5 === 0
}

/** A round axis maximum at or above `value`. */
export function niceAxisMax(value: number): number {
  if (!(value > 0)) return 1
  const magnitude = Math.pow(10, Math.floor(Math.log10(value)))
  const fraction = value / magnitude
  const nice = [1, 1.5, 2, 3, 4, 5, 6, 8, 10].find(step => fraction <= step) ?? 10
  return nice * magnitude
}

// ---------------------------------------------------------------------------
// Attention

export type AttentionTone = 'danger' | 'warning' | 'info'

export type AttentionItem =
  | { kind: 'problem'; key: string; tone: AttentionTone; code: ServiceProblemCode; percent?: number }
  | {
    kind: 'upstream'
    key: string
    tone: AttentionTone
    upstream: UpstreamHealthStatus
    down: boolean
    /** Explanation of the newest failed check, when the Upstream list has one. */
    failure: UpstreamFailure | null
    /** Other Upstreams of the same ecosystem that still pass their check. */
    usableSiblings: number
  }
  | { kind: 'security'; key: 'security'; tone: 'info'; suggestions: number; affectedPackages: number | null; critical: number }

const TONE_ORDER: Record<AttentionTone, number> = { danger: 0, warning: 1, info: 2 }

/**
 * What an operator should look at, most urgent first. Upstream problems are
 * listed one by one, because whether clients are affected depends on what else
 * the same ecosystem can fall back to. Open security suggestions are work to
 * do, not a fault, so they never change the service status.
 */
export function buildAttentionItems(args: {
  status: ServiceStatusModel
  upstreams?: AdminUpstream[]
  suggestionsTotal?: number
  security?: SecurityDashboard
}): AttentionItem[] {
  const { status, upstreams, suggestionsTotal, security } = args
  const items: AttentionItem[] = []

  for (const problem of status.problems) {
    if (problem.code === 'upstreams' || problem.code === 'upstreams-slow') continue
    // The health snapshot only knows that something is degraded. When the
    // affected Upstreams are listed individually below, that says it better.
    if (problem.code === 'degraded' && status.upstreamIssues.length > 0) continue
    items.push({
      kind: 'problem',
      key: problem.code,
      tone: problem.severity === 'danger' ? 'danger' : 'warning',
      code: problem.code,
      percent: problem.percent,
    })
  }

  for (const issue of status.upstreamIssues) {
    const down = issue.checks.includes('unavailable')
    const usableSiblings = status.upstreams.filter(other => (
      other.adapter === issue.adapter && other.key !== issue.key && other.state !== 'unavailable'
    )).length
    const listed = issue.id > 0 ? upstreams?.find(upstream => upstream.id === issue.id) : undefined
    items.push({
      kind: 'upstream',
      key: issue.key,
      tone: down && usableSiblings === 0 ? 'danger' : 'warning',
      upstream: issue,
      down,
      failure: down ? listed?.last_failure ?? null : null,
      usableSiblings,
    })
  }

  if (suggestionsTotal !== undefined && suggestionsTotal > 0) {
    items.push({
      kind: 'security',
      key: 'security',
      tone: 'info',
      suggestions: suggestionsTotal,
      affectedPackages: security ? security.affected_packages : null,
      critical: security?.by_severity.critical ?? 0,
    })
  }

  return items
    .map((item, index) => ({ item, index }))
    .sort((a, b) => TONE_ORDER[a.item.tone] - TONE_ORDER[b.item.tone] || a.index - b.index)
    .map(entry => entry.item)
}

// ---------------------------------------------------------------------------
// Protection coverage

export const PROTECTION_KEYS = [
  'malicious_blocklist',
  'manual_rules',
  'vulnerability_scanning',
  'minimum_release_age',
  'tamper_alerts',
] as const
export type ProtectionKey = (typeof PROTECTION_KEYS)[number]

export interface ProtectionSummary {
  key: ProtectionKey
  /** Ecosystems where the capability is supported and switched on. */
  armed: number
  /** The shared mode of those ecosystems, `mixed` when they differ, null when none is armed. */
  mode: string | null
  /** The worst data state among armed ecosystems that needs an operator. */
  dataIssue: 'error' | 'stale' | null
  /** Armed ecosystems whose data source has never synced. */
  neverSynced: number
}

/** One row per protection capability, summarised across its ecosystems. */
export function summarizeProtections(facts: CapabilityFact[] | undefined): ProtectionSummary[] {
  if (!facts) return []
  return PROTECTION_KEYS.map(key => {
    const armed = facts.filter(fact => (
      fact.name === key && Boolean(fact.ecosystem) && fact.support === 'supported' && fact.mode !== 'off'
    ))
    const modes = new Set(armed.map(fact => fact.mode))
    return {
      key,
      armed: armed.length,
      mode: armed.length === 0 ? null : modes.size === 1 ? armed[0].mode : 'mixed',
      dataIssue: armed.some(fact => fact.data_status === 'error') ? 'error'
        : armed.some(fact => fact.data_status === 'stale') ? 'stale' : null,
      neverSynced: armed.filter(fact => fact.data_status === 'never_synced').length,
    }
  })
}

// ---------------------------------------------------------------------------
// Upstream fleet

export interface FleetUpstream {
  key: string
  id: number
  name: string
  state: UpstreamHealthStatus['state']
  latencyMs: number
}

export interface FleetGroup {
  ecosystem: string
  upstreams: FleetUpstream[]
  /** Moving-average latency of the first usable Upstream in selection order; null when that order is unknown. */
  primaryLatencyMs: number | null
}

/**
 * Upstream health grouped by ecosystem. State comes from the health snapshot,
 * the same source as the service status, so the two never disagree. The
 * Upstream list only contributes selection order (priority).
 */
export function groupUpstreamFleet(
  health: UpstreamHealthStatus[],
  ecosystemOrder: readonly string[],
  list?: AdminUpstream[],
): FleetGroup[] {
  const priority = new Map<number, number>()
  for (const upstream of list ?? []) priority.set(upstream.id, upstream.priority)
  const groups = new Map<string, FleetUpstream[]>()
  for (const upstream of health) {
    const group = groups.get(upstream.adapter) ?? []
    group.push({
      key: upstream.key,
      id: upstream.id,
      name: upstream.name,
      state: upstream.state,
      latencyMs: upstream.avg_latency_ms,
    })
    groups.set(upstream.adapter, group)
  }
  const rank = (ecosystem: string) => {
    const index = ecosystemOrder.indexOf(ecosystem)
    return index === -1 ? ecosystemOrder.length : index
  }
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([ecosystem, upstreams]) => {
      const ordered = [...upstreams].sort((a, b) => (
        (priority.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (priority.get(b.id) ?? Number.MAX_SAFE_INTEGER) || a.id - b.id
      ))
      const primary = ordered.find(upstream => upstream.state !== 'unavailable')
      return {
        ecosystem,
        upstreams: ordered,
        // Without the Upstream list the selection order is unknown, so no Upstream is named preferred.
        primaryLatencyMs: list && primary && Number.isFinite(primary.latencyMs) && primary.latencyMs > 0 ? primary.latencyMs : null,
      }
    })
}
