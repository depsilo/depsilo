/**
 * Shared upstream display component with heartbeat bars.
 * Used by: MonitorV2, DashboardV2, UpstreamsV2
 */
import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import EcosystemIcon from '@/components/EcosystemIcon'

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

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleString(document.documentElement.lang || undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
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

function useBeats(upstream: UpstreamItem, enabled = true): { beats: (number | null)[]; labels: string[] } {
  const { data } = useQuery({
    queryKey: ['upstream-heartbeat', upstream.id ?? upstream.name],
    queryFn: () => upstream.id ? adminApi.getUpstreamLatency(upstream.id, '24h') : Promise.resolve(null),
    refetchInterval: 60000,
    retry: false,
    enabled: enabled && !!upstream.id,
  })
  const realPoints: Array<{ latency_ms: number; time?: string }> = data?.data?.points || []

  return useMemo(() => {
    if (realPoints.length > 0) {
      return normalizeBeats(
        realPoints.map(p => p.latency_ms),
        realPoints.map(p => p.time ? fmtTime(p.time) : ''),
      )
    }
    return {
      beats: [] as (number | null)[],
      labels: [] as string[],
    }
  }, [realPoints, upstream.avg_latency_ms, upstream.success_rate])
}

export function HeartbeatBar({ upstream, externalBeats, labels }: { upstream: UpstreamItem; externalBeats?: (number | null)[]; labels?: string[] }) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const hook = useBeats(upstream, !externalBeats)
  const normalized = useMemo(
    () => externalBeats ? normalizeBeats(externalBeats, labels) : hook,
    [externalBeats, labels, hook],
  )
  const beats = normalized.beats
  const resolvedLabels = normalized.labels
  const emptySlots = Math.max(0, HEARTBEAT_LIMIT - beats.length)
  const displayBeats = [...Array(emptySlots).fill(null), ...beats]
  const displayLabels = [...Array(emptySlots).fill(''), ...resolvedLabels]
  const tooltipLeft = hoveredIdx === null
    ? '0%'
    : hoveredIdx * (BEAT_WIDTH + BEAT_GAP) + (BEAT_WIDTH / 2)

  return (
    <div className="relative" style={{ height: 26 }} aria-label="Upstream latency history">
      <div
        className="absolute bottom-0 left-0 flex items-stretch"
        style={{ gap: BEAT_GAP, height: 20 }}
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {displayBeats.map((lat, i) => (
          // Hover-only affordance (tooltip): default cursor — pointer
          // would promise a click these bars don't have.
          <div
            key={i}
            style={{
              width: BEAT_WIDTH,
              flex: '0 0 auto',
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
    <div>
      <div className="flex items-center justify-between mb-1">
        <span className="text-[12px] font-[400]" style={{ color: 'var(--text)' }}>{upstream.name}</span>
        <div className="flex items-center gap-2">
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
      <HeartbeatBar upstream={upstream} externalBeats={upstream.beats} labels={upstream.beatLabels} />
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

  const groups = useMemo(() => {
    const map = new Map<string, UpstreamItem[]>()
    for (const u of upstreams) {
      const key = u.adapter
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(u)
    }
    return Array.from(map.entries())
  }, [upstreams])

  if (upstreams.length === 0) {
    return <p className="text-[13px] py-4" style={{ color: 'var(--text-soft)' }}>{t('monitor.noUpstreams')}</p>
  }

  const isCards = variant === 'cards'

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: `repeat(auto-fit, minmax(${minColumnWidth}px, 1fr))`,
        gap: isCards ? 12 : '24px 40px',
      }}
    >
      {groups.map(([adapter, items]) => (
        (() => {
          const checkCount = Math.max(0, ...items.map(u => u.beats?.length ?? 0))
          return (
            <div
              key={adapter}
              className={isCards ? 'card' : undefined}
              style={isCards ? { padding: '12px 14px' } : undefined}
            >
              <div
                className="flex items-center gap-2 pb-2 mb-3"
                style={{ borderBottom: '1px solid var(--border)' }}
              >
                <EcosystemIcon type={adapter as any} size={14} useColor />
                <span className="text-[11px] font-mono font-[600] uppercase tracking-[0.1em]" style={{ color: 'var(--text)' }}>
                  {adapter}
                </span>
                <span className="text-[10px] font-mono tabular-nums ml-auto" style={{ color: 'var(--text-subtle)' }}>
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
  )
}
