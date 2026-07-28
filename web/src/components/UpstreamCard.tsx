/**
 * Shared upstream display component with heartbeat bars.
 * Used by: MonitorV2, DashboardV2, UpstreamsV2
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
  url?: string
  proxy?: string
  priority?: number
  beats?: (number | null)[]
  beatLabels?: string[]
  probeMode?: 'active' | 'passive'
  probeInterval?: string
  lastCheckedAt?: string | null
  workerRunning?: boolean
}

// ── Heartbeat bar ──────────────────────────────────────────────────

const HEARTBEAT_LIMIT = 44
const BEAT_WIDTH = 6
const BEAT_GAP = 2
const HEARTBEAT_WIDTH = HEARTBEAT_LIMIT * BEAT_WIDTH + (HEARTBEAT_LIMIT - 1) * BEAT_GAP

// Red is reserved for DOWN (-1): a slow-but-alive upstream must never
// paint the panel in danger color — a wall of red on a page whose
// header says "0 failed" reads as a contradiction. Slow is a single
// amber tier, and the shared 150ms threshold keeps ticks and status
// labels aligned across Portal and Admin.
function beatColor(latency: number | null): string {
  if (latency === null) return 'color-mix(in oklab, var(--border-strong) 58%, var(--bg-card))'
  if (latency < 0) return 'color-mix(in oklab, var(--danger) 78%, var(--bg-card))'
  if (latency < 150) return 'color-mix(in oklab, var(--ok) 82%, var(--bg-card))'
  return 'color-mix(in oklab, var(--warn) 76%, var(--bg-card))'
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

export function HeartbeatBar({ upstream }: { upstream: UpstreamItem }) {
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
      className="stripe-focus-ring relative w-full min-w-0 rounded-[5px]"
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
      <div
        className="absolute inset-x-0 bottom-1 grid items-stretch"
        style={{
          gridTemplateColumns: `repeat(${HEARTBEAT_LIMIT}, minmax(0, ${BEAT_WIDTH}px))`,
          columnGap: BEAT_GAP,
          height: 24,
        }}
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {displayBeats.map((lat, i) => (
          <div
            key={i}
            data-heartbeat-beat
            data-heartbeat-index={i}
            aria-hidden="true"
            style={{
              minWidth: 0,
              borderRadius: 4,
              background: beatColor(lat),
              opacity: beats.length === 0 || lat === null ? 0.14 : (activeIdx !== null && activeIdx !== i ? 0.42 : 1),
              boxShadow: activeIdx === i
                ? '0 0 0 1px color-mix(in oklab, var(--text) 22%, transparent) inset'
                : 'none',
              transition: 'opacity 90ms ease, box-shadow 90ms ease',
            }}
            onMouseEnter={() => setHoveredIdx(i)}
          />
        ))}
      </div>
      {activeIdx !== null && (
        <div
          id={detailId}
          data-heartbeat-detail
          role="status"
          aria-live="polite"
          aria-atomic="true"
          className="absolute bottom-full mb-1 px-2 py-0.5 rounded-[3px] text-[11px] font-mono whitespace-nowrap pointer-events-none z-10"
          style={{ background: 'var(--text)', color: 'var(--bg-page)', left: tooltipLeft, transform: tooltipTransform }}
        >
          {activeDetail}
        </div>
      )}
    </div>
  )
}

// ── Upstream row (name + latency + dot + heartbeat) ────────────────

export function UpstreamRow({
  upstream,
  actions,
  metadata,
}: {
  upstream: UpstreamItem
  actions?: React.ReactNode
  metadata?: React.ReactNode
}) {
  const { t } = useTranslation()
  const status = upstreamStatus(upstream)
  const hasActions = actions !== undefined && actions !== null && actions !== false
  const latency = (upstream.avg_latency_ms || 0) <= 1 ? '--' : `${upstream.avg_latency_ms}ms`
  const statusIndicator = (
    <span
      className="inline-flex shrink-0 items-center gap-1 text-[11px] font-[550]"
      style={{ color: 'var(--text-muted)' }}
    >
      <StatusDot status={status} />
      {t(`monitor.${status}`)}
    </span>
  )
  const latencyIndicator = (
    <span
      className="shrink-0 font-mono text-[11px] tabular-nums"
      style={{ color: 'var(--text-muted)' }}
    >
      {latency}
    </span>
  )

  return (
    <div data-upstream-row data-upstream-status={status} className="min-w-0">
      {hasActions ? (
        <>
          <div className="flex min-w-0 items-center justify-between gap-2">
            <span
              className="min-w-0 flex-1 truncate text-[12px] font-[400]"
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
              <div data-upstream-metadata className="min-w-0 text-[11px]" style={{ color: 'var(--text-muted)' }}>
                {metadata}
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          <div className="mb-1 flex min-w-0 items-center justify-between gap-2">
            <span
              className="min-w-0 flex-1 truncate text-[12px] font-[400]"
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
              className="mb-1 min-w-0 text-[11px]"
              style={{ color: 'var(--text-muted)' }}
            >
              {metadata}
            </div>
          )}
        </>
      )}
      <HeartbeatBar upstream={upstream} />
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
    refetch: refetchLatencies,
  } = useQuery({
    queryKey: ['admin', 'upstreams', 'latencies', '24h'],
    queryFn: () => adminApi.getUpstreamLatencies('24h'),
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

  return (
    <>
      {showLatencyError && (
        <div
          data-upstream-history-error
          role="status"
          className="mb-3 flex min-h-10 items-center justify-between gap-3 rounded-[6px] border px-3 py-2 text-[12px]"
          style={{ borderColor: 'var(--warn-border)', color: 'var(--warn-text)', background: 'var(--warn-fill)' }}
        >
          <span>{t('monitor.historyUnavailable')}</span>
          <button
            type="button"
            className="shrink-0 font-[600] underline underline-offset-2"
            style={{ color: 'var(--text)' }}
            onClick={() => { void refetchLatencies() }}
          >
            {t('common.retry')}
          </button>
        </div>
      )}
      <div
        data-upstream-grouped-panel
        data-upstream-panel-layout={layout}
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(auto-fit, minmax(min(100%, ${minColumnWidth}px), 1fr))`,
          gap: isCards ? 12 : '24px 40px',
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
                    className="flex min-w-0 items-center gap-2 text-[12px] font-mono font-[600] uppercase tracking-[0.1em]"
                    style={{ color: 'var(--text)' }}
                  >
                    {isEcosystemType(adapter) && <EcosystemIcon type={adapter} size={14} useColor decorative />}
                    <span className="min-w-0 truncate">{adapter}</span>
                  </h2>
                  <span className="ml-auto shrink-0 text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-muted)' }}>
                    {t('monitor.historySummary', { count: checkCount })} · {t('monitor.healthySummary', { healthy: healthyCount, total: items.length })}
                  </span>
                </div>
                <div
                  data-upstream-item-grid={usesAdaptiveItemGrid ? 'auto-fill' : 'stack'}
                  className={usesAdaptiveItemGrid ? undefined : 'space-y-3'}
                  style={usesAdaptiveItemGrid ? {
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 350px), 1fr))',
                    gap: '12px 24px',
                    minWidth: 0,
                  } : undefined}
                >
                  {items.map((u) => (
                    <UpstreamRow
                      key={u.id ?? `${u.adapter}:${u.name}:${u.url ?? ''}`}
                      upstream={u}
                      actions={renderActions?.(u)}
                      metadata={renderMetadata?.(u)}
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
