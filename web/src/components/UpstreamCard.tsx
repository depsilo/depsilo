import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Info } from 'lucide-react'
/**
 * Shared upstream display component with heartbeat bars.
 * Used by: Monitor, Dashboard, Upstreams
 */
import { useId, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import EcosystemIcon from '@/components/EcosystemIcon'
import StatusDot from '@/components/StatusDot'
import { isEcosystemType } from '@/lib/ecosystemTypes'
import { upstreamStatus } from '@/lib/upstreamStatus'

// ── Types ──────────────────────────────────────────────────────────

export interface UpstreamItem {
  id?: number
  name: string
  adapter: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  has_observations?: boolean
  url?: string
  proxy?: string
  via?: string
  priority?: number
  beats?: (number | null)[]
  beatLabels?: string[]
  probeMode?: 'active' | 'passive'
  probeInterval?: string
  lastCheckedAt?: string | null
  workerRunning?: boolean
}

// ── Heartbeat bar ──────────────────────────────────────────────────
//
// Height carries latency, colour carries the latency tier, and the two
// channels are kept independent so a failed probe can never read as a fast
// one. A probe that failed to answer has no latency to draw, so it is the one
// column allowed to reach the top of the 24px slot (healthy probes are capped
// at 18px); "down" is therefore a difference in shape as well as in colour.
// A slot with no sample paints nothing at all, so "never measured" can never
// read as "measured fast"; the baseline keeps the empty window legible.
//
// Red is reserved for DOWN (-1): a slow-but-alive upstream must never
// paint the panel in danger color — a wall of red on a page whose
// header says "0 failed" reads as a contradiction. Slow is a single
// amber tier. This floor is the per-probe tier for a single round trip,
// which is a narrower question than the rolling-average bar that decides
// an upstream's status label (1 s — see lib/upstreamStatus.ts): a mirror
// can be labelled healthy while individual probes still run amber.
const HEARTBEAT_LIMIT = 44
const BEAT_WIDTH = 6
const BEAT_GAP = 2
const HEARTBEAT_WIDTH = HEARTBEAT_LIMIT * BEAT_WIDTH + (HEARTBEAT_LIMIT - 1) * BEAT_GAP
const BEAT_AREA = 24
const BEAT_MIN = 4
const BEAT_MAX = 18
const BEAT_FLOOR_MS = 150

function beatHeight(latency: number, scale: number): number {
  if (latency < 0) return BEAT_AREA
  return BEAT_MIN + Math.round((Math.min(latency, scale) / scale) * (BEAT_MAX - BEAT_MIN))
}

function beatColor(latency: number): string {
  if (latency < 0) return 'color-mix(in oklab, var(--danger) 78%, var(--bg-card))'
  if (latency < BEAT_FLOOR_MS) return 'color-mix(in oklab, var(--ok) 82%, var(--bg-card))'
  return 'color-mix(in oklab, var(--warn) 76%, var(--bg-card))'
}

/** Exposed on each beat so the encoding can be asserted without reading styles. */
function beatState(latency: number | null): 'none' | 'ok' | 'slow' | 'down' {
  if (latency === null) return 'none'
  if (latency < 0) return 'down'
  return latency < BEAT_FLOOR_MS ? 'ok' : 'slow'
}

function beatLabel(latency: number | null, noDataLabel: string, failedLabel: string): string {
  if (latency === null) return noDataLabel
  if (latency < 0) return failedLabel
  return `${latency}ms`
}

const timeFormatters = new Map<string, Intl.DateTimeFormat>()

function fmtTime(iso: string): string {
  const locale = document.documentElement.lang || 'default'
  let formatter = timeFormatters.get(locale)
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale === 'default' ? undefined : locale, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
    timeFormatters.set(locale, formatter)
  }
  return formatter.format(new Date(iso))
}

function normalizeBeats(
  beats: (number | null)[],
  labels: string[] = [],
  limit = HEARTBEAT_LIMIT,
): { beats: (number | null)[]; labels: string[] } {
  if (beats.length <= limit) {
    return { beats, labels }
  }

  return {
    beats: beats.slice(-limit),
    labels: labels.slice(-limit),
  }
}

function HeartbeatBar({ upstream, showEmptyLabel = false }: { upstream: UpstreamItem; showEmptyLabel?: boolean }) {
  const { t } = useTranslation()
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const [selectedIdx, setSelectedIdx] = useState<number | null>(null)
  const detailId = useId()
  const instructionsId = useId()
  const normalized = useMemo(
    () => normalizeBeats(upstream.beats ?? [], upstream.beatLabels),
    [upstream.beats, upstream.beatLabels],
  )
  const beats = normalized.beats
  const resolvedLabels = normalized.labels
  // One scale per row: the strip answers "how has this upstream behaved",
  // while the average printed beside it answers "how does it compare".
  // Anchoring the denominator at the 150ms tier boundary keeps an all-fast
  // row from being stretched into a dramatic-looking chart.
  const scale = useMemo(() => {
    const measured = (upstream.beats ?? [])
      .filter((beat): beat is number => beat !== null && beat >= 0)
    return Math.max(BEAT_FLOOR_MS, ...measured)
  }, [upstream.beats])
  const emptySlots = Math.max(0, HEARTBEAT_LIMIT - beats.length)
  const displayBeats = [...Array(emptySlots).fill(null), ...beats]
  const displayLabels = [...Array(emptySlots).fill(''), ...resolvedLabels]
  const activeIdx = hoveredIdx ?? selectedIdx
  const latestIdx = displayBeats.length - 1
  const firstDataIdx = beats.length > 0 ? emptySlots : latestIdx
  const tooltipLeft = activeIdx === null
    ? '0%'
    : `${((activeIdx + 0.5) / displayBeats.length) * 100}%`
  const tooltipTransform = activeIdx === null || activeIdx < 6
    ? 'translateX(0)'
    : activeIdx > displayBeats.length - 7
      ? 'translateX(-100%)'
      : 'translateX(-50%)'
  const activeDetail = activeIdx === null
    ? ''
    : `${displayLabels[activeIdx] ? `${displayLabels[activeIdx]} · ` : ''}${beatLabel(displayBeats[activeIdx], t('monitor.noLatencyData'), t('monitor.failed'))}`

  function moveSelection(delta: number) {
    setSelectedIdx(current =>
      Math.max(firstDataIdx, Math.min(latestIdx, (current ?? latestIdx) + delta)),
    )
  }

  return (
    <div
      data-upstream-heartbeat
      className="stripe-focus-ring relative w-full min-w-0 rounded-sm"
      style={{ height: 40, maxWidth: HEARTBEAT_WIDTH }}
      tabIndex={0}
      aria-label={t('monitor.latencyHistoryNamed', { name: upstream.name })}
      aria-describedby={`${instructionsId}${activeIdx === null ? '' : ` ${detailId}`}`}
      aria-keyshortcuts="ArrowLeft ArrowRight Home End Enter Space Escape"
      onFocus={() => { setSelectedIdx(current => current ?? latestIdx) }}
      onBlur={() => {
        setHoveredIdx(null)
        setSelectedIdx(null)
      }}
      onPointerDown={event => {
        event.currentTarget.focus({ preventScroll: true })
        const bounds = event.currentTarget.getBoundingClientRect()
        const ratio = bounds.width > 0
          ? (event.clientX - bounds.left) / bounds.width
          : 1
        const pointerIdx = Math.floor(ratio * displayBeats.length)
        setSelectedIdx(
          Math.max(firstDataIdx, Math.min(latestIdx, pointerIdx)),
        )
      }}
      onKeyDown={event => {
        if (event.key === 'ArrowLeft') {
          event.preventDefault()
          moveSelection(-1)
        } else if (event.key === 'ArrowRight') {
          event.preventDefault()
          moveSelection(1)
        } else if (event.key === 'Home') {
          event.preventDefault()
          setSelectedIdx(firstDataIdx)
        } else if (event.key === 'End' || event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          setSelectedIdx(latestIdx)
        } else if (event.key === 'Escape') {
          event.preventDefault()
          setHoveredIdx(null)
          setSelectedIdx(null)
        }
      }}
    >
      <span id={instructionsId} className="sr-only">
        {t('monitor.latencyHistoryInstructions')}
      </span>
      {showEmptyLabel && beats.length === 0 && (
        <span className="absolute inset-x-0 top-1 text-[12px] text-muted-foreground">
          {t('monitor.noObservations')}
        </span>
      )}
      <div
        className="absolute inset-x-0 bottom-1 grid items-end"
        style={{
          gridTemplateColumns: `repeat(${HEARTBEAT_LIMIT}, minmax(0, ${BEAT_WIDTH}px))`,
          columnGap: BEAT_GAP,
          height: BEAT_AREA,
          // Baseline: short bars still read as columns rather than as dots.
          borderBottom: '1px solid var(--border)',
        }}
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {displayBeats.map((lat, i) => {
          // Every column keeps the full slot height so each probe stays
          // hoverable; the painted bar inside it is what carries the latency.
          const barHeight = lat === null ? 0 : beatHeight(lat, scale)
          const barColor = lat === null ? 'transparent' : beatColor(lat)
          return (
            <div
              key={i}
              data-heartbeat-beat
              data-heartbeat-index={i}
              data-heartbeat-state={beatState(lat)}
              data-heartbeat-height={barHeight}
              aria-hidden="true"
              style={{
                minWidth: 0,
                height: BEAT_AREA,
                background: `linear-gradient(to top, ${barColor} 0, ${barColor} ${barHeight}px, transparent ${barHeight}px)`,
                opacity: beats.length === 0 ? 0.4 : (activeIdx !== null && activeIdx !== i ? 0.42 : 1),
                boxShadow: activeIdx === i
                  ? '0 0 0 1px color-mix(in oklab, var(--text) 22%, transparent) inset'
                  : 'none',
                transition: 'opacity 90ms ease, box-shadow 90ms ease',
              }}
              onMouseEnter={() => setHoveredIdx(i)}
            />
          )
        })}
      </div>
      {activeIdx !== null && (
        <div
          id={detailId}
          data-heartbeat-detail
          role="status"
          aria-live="polite"
          aria-atomic="true"
          className="absolute bottom-full mb-1 px-2 py-1 rounded-sm text-[13px] font-mono whitespace-nowrap pointer-events-none z-10"
          style={{ background: 'var(--inverse)', color: 'var(--on-inverse)', left: tooltipLeft, transform: tooltipTransform }}
        >
          {activeDetail}
        </div>
      )}
    </div>
  )
}

// ── Upstream row (name + latency + dot + heartbeat) ────────────────

function UpstreamRow({
  upstream,
  actions,
  metadata,
  operational = false,
  historyReady = true,
}: {
  upstream: UpstreamItem
  actions?: React.ReactNode
  metadata?: React.ReactNode
  operational?: boolean
  historyReady?: boolean
}) {
  const { t } = useTranslation()
  const status = upstreamStatus(upstream)
  const hasActions = actions !== undefined && actions !== null && actions !== false
  const latency = upstream.has_observations === false || (upstream.has_observations === undefined && upstream.avg_latency_ms <= 1)
    ? '—'
    : `${upstream.avg_latency_ms} ms`
  const statusIndicator = (
    <span
      className="inline-flex shrink-0 items-center gap-1.5 text-[13px] font-medium"
      style={{ color: 'var(--text-muted)' }}
    >
      <StatusDot status={status} />
      {t(`monitor.${status}`)}
    </span>
  )
  const latencyIndicator = (
    <span
      className="shrink-0 text-[13px] tabular-nums"
      style={{ color: 'var(--text-muted)' }}
    >
      {operational ? t('monitor.averageLatency', { value: latency }) : latency}
    </span>
  )

  return (
    <div data-upstream-row data-upstream-status={status} className="min-w-0">
      {operational ? (
        <>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <span className="min-w-0 flex-1 truncate text-[14px] font-semibold" title={upstream.name}>
              {upstream.name}
            </span>
            {hasActions && <div className="flex shrink-0 items-center">{actions}</div>}
          </div>
          <div data-upstream-operational-line className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
            {statusIndicator}
            {latencyIndicator}
          </div>
          {metadata !== undefined && metadata !== null && metadata !== false && (
            <div data-upstream-metadata className="mb-1 min-w-0 text-[13px] leading-relaxed text-muted-foreground">
              {metadata}
            </div>
          )}
        </>
      ) : hasActions ? (
        <>
          <div className="flex min-w-0 items-center justify-between gap-2">
            <span
              className="min-w-0 flex-1 truncate text-[13px] font-[400]"
              title={upstream.name}
              style={{ color: 'var(--text)' }}
            >
              {upstream.name}
            </span>
            <div className="flex shrink-0 items-center">
              {actions}
            </div>
          </div>
          <div
            data-upstream-operational-line
            className="mb-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1"
          >
            {statusIndicator}
            {latencyIndicator}
            {metadata !== undefined && metadata !== null && metadata !== false && (
              <div data-upstream-metadata className="min-w-0 text-[12px]" style={{ color: 'var(--text-muted)' }}>
                {metadata}
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          <div className="mb-1 flex min-w-0 items-center justify-between gap-2">
            <span
              className="min-w-0 flex-1 truncate text-[13px] font-[400]"
              title={upstream.name}
              style={{ color: 'var(--text)' }}
            >
              {upstream.name}
            </span>
            <div className="flex shrink-0 items-center gap-2">
              {latencyIndicator}
              {statusIndicator}
            </div>
          </div>
          {metadata !== undefined && metadata !== null && metadata !== false && (
            <div
              data-upstream-metadata
              className="mb-1 min-w-0 text-[12px]"
              style={{ color: 'var(--text-muted)' }}
            >
              {metadata}
            </div>
          )}
        </>
      )}
      <HeartbeatBar upstream={upstream} showEmptyLabel={operational && historyReady} />
    </div>
  )
}

// ── Grouped upstream panel (2-col grid) ────────────────────────────

interface UpstreamGroupedPanelProps {
  upstreams: UpstreamItem[]
  renderActions?: (upstream: UpstreamItem) => React.ReactNode
  renderMetadata?: (upstream: UpstreamItem) => React.ReactNode
  variant?: 'plain' | 'cards'
  minColumnWidth?: number
  layout?: 'tiles' | 'adaptive'
}

export function UpstreamGroupedPanel({
  upstreams,
  renderActions,
  renderMetadata,
  variant = 'plain',
  minColumnWidth = 360,
  layout = 'tiles',
}: UpstreamGroupedPanelProps) {
  const { t } = useTranslation()
  const groupHeadingBaseId = useId()

  const heartbeatUpstreamIDs = useMemo(() => Array.from(new Set(
    upstreams.flatMap(upstream => upstream.id !== undefined && upstream.beats === undefined ? [upstream.id] : []),
  )).sort((left, right) => left - right), [upstreams])

  const {
    data: latencyResponse,
    isError: isLatencyError,
    isRefetchError: isLatencyRefetchError,
    isSuccess: isLatencySuccess,
    refetch: refetchLatencies,
  } = useQuery({
    queryKey: ['admin', 'upstreams', 'latencies', '24h'],
    queryFn: ({ signal }) => adminApi.getUpstreamLatencies('24h', { signal }),
    staleTime: 55_000,
    refetchInterval: 60000,
    retry: false,
    enabled: heartbeatUpstreamIDs.length > 0,
  })

  const seriesByUpstreamID = useMemo(() => new Map(
    (latencyResponse?.data.series ?? []).map(series => [series.upstream_id, series.points] as const),
  ), [latencyResponse])

  const resolvedUpstreams = useMemo(() => upstreams.map(upstream => {
    if (upstream.id === undefined || upstream.beats !== undefined) return upstream
    const points = seriesByUpstreamID.get(upstream.id) ?? []
    return {
      ...upstream,
      beats: points.map(point => point.healthy ? point.latency_ms : -1),
      beatLabels: points.map(point => fmtTime(point.time)),
    }
  }), [seriesByUpstreamID, upstreams])

  const groups = useMemo(() => {
    const map = new Map<string, UpstreamItem[]>()
    for (const u of resolvedUpstreams) {
      const key = u.adapter
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(u)
    }
    return Array.from(map.entries())
  }, [resolvedUpstreams])

  if (upstreams.length === 0) {
    return <p className="text-[13px] py-4" style={{ color: 'var(--text-soft)' }}>{t('monitor.noUpstreams')}</p>
  }

  const isCards = variant === 'cards'
  const showLatencyError = heartbeatUpstreamIDs.length > 0 && (isLatencyError || isLatencyRefetchError)
  const isOperational = layout === 'adaptive'
  // An adaptive operator panel stops at two columns, even on large monitors.
  // Portal tiles retain their existing compact responsive layout.
  const adaptiveColumns = 'repeat(auto-fit, minmax(min(100%, max(360px, calc((100% - 32px) / 2))), 1fr))'

  return (
    <>
      {isOperational && (
        <div className="mb-3 flex min-w-0 items-center gap-1 text-[13px] text-muted-foreground">
          <span>{t('monitor.historyWindow', { count: HEARTBEAT_LIMIT })}</span>
          <Tooltip>
            <TooltipTrigger render={<Button variant="ghost" size="icon" className="size-10 shrink-0" aria-label={t('monitor.historyWindow', { count: HEARTBEAT_LIMIT })} />}>
              <Info className="size-4" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[min(360px,calc(100vw-32px))]">{t('monitor.historyLegend')}</TooltipContent>
          </Tooltip>
        </div>
      )}
      {showLatencyError && (
        <div
          data-upstream-history-error
          role="status"
          className="mb-3 flex min-h-10 items-center justify-between gap-3 rounded-md border px-3 py-2 text-[12.5px]"
          style={{ borderColor: 'var(--warn-border)', color: 'var(--warn-text)', background: 'var(--warn-fill)' }}
        >
          <span>{t('monitor.historyUnavailable')}</span>
          <Button variant="link"
            type="button"
            className="min-h-[40px] px-0 font-semibold"
            style={{ color: 'var(--text)' }}
            onClick={() => { void refetchLatencies() }}
          >
            {t('common.retry')}
          </Button>
        </div>
      )}
      <div
        data-upstream-grouped-panel
        data-upstream-panel-layout={layout}
        style={{
          display: 'grid',
          gridTemplateColumns: isOperational ? adaptiveColumns : `repeat(auto-fit, minmax(min(100%, ${minColumnWidth}px), 1fr))`,
          gap: isCards ? 12 : '24px 32px',
          minWidth: 0,
          width: '100%',
        }}
      >
        {groups.map(([adapter, items], groupIndex) => (
          (() => {
            const checkCount = Math.max(0, ...items.map(u => u.beats?.length ?? 0))
            const healthyCount = items.filter(u => upstreamStatus(u) === 'healthy').length
            const usesAdaptiveItemGrid = layout === 'adaptive' && (groups.length === 1 || items.length >= 3)
            const headingId = `${groupHeadingBaseId}-${groupIndex}`
            return (
              <section
                key={adapter}
                data-upstream-group={adapter}
                data-upstream-group-layout={usesAdaptiveItemGrid ? 'full' : 'tile'}
                aria-labelledby={headingId}
                className={isCards ? 'card min-w-0' : 'min-w-0'}
                style={{
                  ...(isCards ? { padding: '12px 14px' } : undefined),
                  ...(usesAdaptiveItemGrid ? { gridColumn: '1 / -1' } : undefined),
                }}
              >
                <div
                  className="mb-3 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 pb-2"
                  style={{ borderBottom: '1px solid var(--border)' }}
                >
                  <h2
                    id={headingId}
                    className="flex min-w-0 items-center gap-2 text-[13px] font-semibold uppercase tracking-wide"
                    style={{ color: 'var(--text)' }}
                  >
                    {isEcosystemType(adapter) && <EcosystemIcon type={adapter} size={14} useColor decorative />}
                    <span className="min-w-0 truncate">{adapter}</span>
                  </h2>
                  <span className="ml-auto shrink-0 text-[12.5px] font-mono tabular-nums" style={{ color: 'var(--text-muted)' }}>
                    {!isOperational && <>{t('monitor.historySummary', { count: checkCount })} · </>}{t('monitor.healthySummary', { healthy: healthyCount, total: items.length })}
                  </span>
                </div>
                <div
                  data-upstream-item-grid={usesAdaptiveItemGrid ? 'auto-fill' : 'stack'}
                  className={usesAdaptiveItemGrid ? undefined : 'space-y-5'}
                  style={usesAdaptiveItemGrid ? {
                    display: 'grid',
                    gridTemplateColumns: adaptiveColumns,
                    gap: '20px 32px',
                    minWidth: 0,
                  } : undefined}
                >
                  {items.map((u) => (
                    <UpstreamRow
                      key={u.id ?? `${u.adapter}:${u.name}:${u.url ?? ''}`}
                      upstream={u}
                      actions={renderActions?.(u)}
                      metadata={renderMetadata?.(u)}
                      operational={isOperational}
                      historyReady={u.id === undefined || upstreams.find(item => item.id === u.id)?.beats !== undefined || isLatencySuccess}
                    />
                  ))}
                </div>
              </section>
            )
          })()
        ))}
      </div>
    </>
  )
}
