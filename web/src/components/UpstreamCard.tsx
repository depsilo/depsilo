/**
 * Shared upstream display component with heartbeat bars.
 * Used by: MonitorV2, DashboardV2, UpstreamsV2
 */
import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import EcosystemIcon from '@/components/EcosystemIcon'
import { isEcosystemType } from '@/lib/ecosystemTypes'

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
}

// ── Heartbeat bar ──────────────────────────────────────────────────

const HEARTBEAT_LIMIT = 44
const BEAT_WIDTH = 6
const BEAT_GAP = 2
const HEARTBEAT_WIDTH = HEARTBEAT_LIMIT * BEAT_WIDTH + (HEARTBEAT_LIMIT - 1) * BEAT_GAP

// Red is reserved for DOWN (-1): a slow-but-alive upstream must never
// paint the panel in danger color — a wall of red on a page whose
// header says "0 failed" reads as a contradiction. Slow is a single
// amber tier, and the 150ms threshold matches mirrorStatus() on the
// Monitor page so ticks and status dots always agree.
function beatColor(latency: number | null): string {
  if (latency === null) return 'color-mix(in oklab, var(--border-strong) 58%, var(--bg-card))'
  if (latency < 0) return 'color-mix(in oklab, var(--danger) 78%, var(--bg-card))'
  if (latency < 150) return 'color-mix(in oklab, var(--ok) 82%, var(--bg-card))'
  return 'color-mix(in oklab, var(--warn) 76%, var(--bg-card))'
}

function beatLabel(latency: number | null): string {
  if (latency === null) return 'No data'
  if (latency < 0) return 'Down'
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
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const normalized = useMemo(
    () => normalizeBeats(upstream.beats ?? [], upstream.beatLabels),
    [upstream.beats, upstream.beatLabels],
  )
  const beats = normalized.beats
  const resolvedLabels = normalized.labels
  const emptySlots = Math.max(0, HEARTBEAT_LIMIT - beats.length)
  const displayBeats = [...Array(emptySlots).fill(null), ...beats]
  const displayLabels = [...Array(emptySlots).fill(''), ...resolvedLabels]
  const tooltipLeft = hoveredIdx === null
    ? '0%'
    : `${((hoveredIdx + 0.5) / displayBeats.length) * 100}%`

  return (
    <div
      data-upstream-heartbeat
      className="relative w-full min-w-0"
      style={{ height: 26, maxWidth: HEARTBEAT_WIDTH }}
      aria-label="Upstream latency history"
    >
      <div
        className="absolute inset-x-0 bottom-0 grid items-stretch"
        style={{
          gridTemplateColumns: `repeat(${HEARTBEAT_LIMIT}, minmax(0, ${BEAT_WIDTH}px))`,
          columnGap: BEAT_GAP,
          height: 20,
        }}
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {displayBeats.map((lat, i) => (
          // Hover-only affordance (tooltip): default cursor — pointer
          // would promise a click these bars don't have.
          <div
            key={i}
            style={{
              minWidth: 0,
              borderRadius: 4,
              background: beatColor(lat),
              opacity: beats.length === 0 || lat === null ? 0.14 : (hoveredIdx !== null && hoveredIdx !== i ? 0.42 : 1),
              boxShadow: hoveredIdx === i
                ? '0 0 0 1px color-mix(in oklab, var(--text) 22%, transparent) inset'
                : 'none',
              transition: 'opacity 90ms ease, box-shadow 90ms ease',
            }}
            onMouseEnter={() => setHoveredIdx(i)}
          />
        ))}
      </div>
      {hoveredIdx !== null && (
        <div
          className="absolute bottom-full mb-1 px-2 py-0.5 rounded-[3px] text-[10px] font-mono whitespace-nowrap pointer-events-none z-10"
          style={{ background: 'var(--text)', color: 'var(--bg-page)', left: tooltipLeft, transform: 'translateX(-50%)' }}
        >
          {displayBeats[hoveredIdx] === null
            ? beatLabel(null)
            : <>{displayLabels?.[hoveredIdx] ? `${displayLabels[hoveredIdx]} · ` : ''}{beatLabel(displayBeats[hoveredIdx])}</>}
        </div>
      )}
    </div>
  )
}

// ── Upstream row (name + latency + dot + heartbeat) ────────────────

export function UpstreamRow({ upstream, actions }: { upstream: UpstreamItem; actions?: React.ReactNode }) {
  return (
    <div data-upstream-row className="min-w-0">
      <div className="mb-1 flex min-w-0 items-center justify-between gap-2">
        <span
          className="min-w-0 flex-1 truncate text-[12px] font-[400]"
          title={upstream.name}
          style={{ color: 'var(--text)' }}
        >
          {upstream.name}
        </span>
        <div className="flex shrink-0 items-center gap-2">
          <span
            className="font-mono text-[11px] tabular-nums"
            style={{
              color: (upstream.avg_latency_ms || 0) <= 1 ? 'var(--text-soft)'
                : upstream.avg_latency_ms < 100 ? 'var(--ok)'
                : upstream.avg_latency_ms < 500 ? 'var(--text-soft)' : 'var(--danger)',
            }}
          >
            {(upstream.avg_latency_ms || 0) <= 1 ? '--' : `${upstream.avg_latency_ms}ms`}
          </span>
          <span className="text-[10px]" style={{ color: upstream.healthy ? 'var(--ok)' : 'var(--danger)' }}>●</span>
          {actions}
        </div>
      </div>
      <HeartbeatBar upstream={upstream} />
    </div>
  )
}

// ── Grouped upstream panel (2-col grid) ────────────────────────────

interface UpstreamGroupedPanelProps {
  upstreams: UpstreamItem[]
  renderActions?: (upstream: UpstreamItem) => React.ReactNode
  variant?: 'plain' | 'cards'
  minColumnWidth?: number
}

export function UpstreamGroupedPanel({ upstreams, renderActions, variant = 'plain', minColumnWidth = 360 }: UpstreamGroupedPanelProps) {
  const { t } = useTranslation()

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
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(auto-fit, minmax(min(100%, ${minColumnWidth}px), 1fr))`,
          gap: isCards ? 12 : '24px 40px',
          minWidth: 0,
          width: '100%',
        }}
      >
        {groups.map(([adapter, items]) => (
          (() => {
            const checkCount = Math.max(0, ...items.map(u => u.beats?.length ?? 0))
            return (
              <div
                key={adapter}
                data-upstream-group={adapter}
                className={isCards ? 'card min-w-0' : 'min-w-0'}
                style={isCards ? { padding: '12px 14px' } : undefined}
              >
                <div
                  className="mb-3 flex min-w-0 items-center gap-2 pb-2"
                  style={{ borderBottom: '1px solid var(--border)' }}
                >
                  {isEcosystemType(adapter) && <EcosystemIcon type={adapter} size={14} useColor />}
                  <span className="min-w-0 truncate text-[11px] font-mono font-[600] uppercase tracking-[0.1em]" style={{ color: 'var(--text)' }}>
                    {adapter}
                  </span>
                  <span className="ml-auto shrink-0 text-[10px] font-mono tabular-nums" style={{ color: 'var(--text-subtle)' }}>
                    {t('monitor.historySummary', { count: checkCount })} · {items.filter(u => u.healthy).length}/{items.length}
                  </span>
                </div>
                <div className="space-y-3">
                  {items.map((u) => (
                    <UpstreamRow
                      key={u.name}
                      upstream={u}
                      actions={renderActions?.(u)}
                    />
                  ))}
                </div>
              </div>
            )
          })()
        ))}
      </div>
    </>
  )
}
