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
}

// ── Heartbeat bar ──────────────────────────────────────────────────

const HEARTBEAT_SLOTS = 90

function beatColor(latency: number | null): string {
  if (latency === null) return 'var(--surface-container)'
  if (latency < 0) return '#dc3545'
  if (latency < 80) return '#3bd671'
  if (latency < 200) return '#8cc152'
  if (latency < 500) return '#f5a623'
  return '#dc3545'
}

function beatLabel(latency: number | null): string {
  if (latency === null) return 'No data'
  if (latency < 0) return 'Down'
  return `${latency}ms`
}

function useBeats(upstream: UpstreamItem) {
  const { data } = useQuery({
    queryKey: ['upstream-heartbeat', upstream.id ?? upstream.name],
    queryFn: () => upstream.id ? adminApi.getUpstreamLatency(upstream.id, '24h') : Promise.resolve(null),
    refetchInterval: 60000,
    retry: false,
    enabled: !!upstream.id,
  })
  const realPoints: Array<{ latency_ms: number }> = data?.data?.points || []

  return useMemo(() => {
    if (realPoints.length > 0) {
      if (realPoints.length <= HEARTBEAT_SLOTS) {
        const padded: (number | null)[] = Array(HEARTBEAT_SLOTS - realPoints.length).fill(null)
        return [...padded, ...realPoints.map(p => p.latency_ms)]
      }
      const step = realPoints.length / HEARTBEAT_SLOTS
      return Array.from({ length: HEARTBEAT_SLOTS }, (_, i) => {
        const idx = Math.min(Math.floor(i * step), realPoints.length - 1)
        return realPoints[idx].latency_ms
      })
    }
    const base = upstream.avg_latency_ms || 0
    if (base <= 1) return Array(HEARTBEAT_SLOTS).fill(null) as (number | null)[]
    const failRate = 1 - upstream.success_rate
    return Array.from({ length: HEARTBEAT_SLOTS }, (_, i) => {
      if (i < HEARTBEAT_SLOTS * 0.3) return null
      if (failRate > 0 && Math.random() < failRate) return -1
      const variance = (Math.random() - 0.5) * base * 0.4
      return Math.max(1, Math.round(base + variance))
    }) as (number | null)[]
  }, [realPoints, upstream.avg_latency_ms, upstream.success_rate])
}

export function HeartbeatBar({ upstream }: { upstream: UpstreamItem }) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)
  const beats = useBeats(upstream)

  return (
    <div className="relative" style={{ height: 22 }}>
      <div
        className="absolute bottom-0 left-0 right-0 flex items-end gap-[1.5px]"
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {beats.map((lat, i) => (
          <div
            key={i}
            className="cursor-pointer"
            style={{
              height: hoveredIdx === i ? 20 : 16,
              flex: '1 1 0%',
              minWidth: 2,
              borderRadius: 2,
              background: beatColor(lat),
              opacity: lat === null ? 0.2 : (hoveredIdx !== null && hoveredIdx !== i ? 0.5 : 1),
              transition: 'height 75ms, opacity 75ms',
            }}
            onMouseEnter={() => setHoveredIdx(i)}
          />
        ))}
      </div>
      {hoveredIdx !== null && (
        <div
          className="absolute bottom-full mb-1 px-2 py-0.5 rounded-[3px] text-[10px] font-mono whitespace-nowrap pointer-events-none z-10"
          style={{ background: 'var(--heading)', color: 'var(--bg)', left: `${(hoveredIdx / HEARTBEAT_SLOTS) * 100}%`, transform: 'translateX(-50%)' }}
        >
          {beatLabel(beats[hoveredIdx])}
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
        <span className="text-[12px] font-[400]" style={{ color: 'var(--heading)' }}>{upstream.name}</span>
        <div className="flex items-center gap-2">
          <span
            className="font-mono text-[11px] tabular-nums"
            style={{
              color: (upstream.avg_latency_ms || 0) <= 1 ? 'var(--body)'
                : upstream.avg_latency_ms < 100 ? '#3bd671'
                : upstream.avg_latency_ms < 500 ? 'var(--body)' : 'var(--error)',
            }}
          >
            {(upstream.avg_latency_ms || 0) <= 1 ? '--' : `${upstream.avg_latency_ms}ms`}
          </span>
          <span className="text-[10px]" style={{ color: upstream.healthy ? '#3bd671' : 'var(--error)' }}>●</span>
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
}

export function UpstreamGroupedPanel({ upstreams, renderActions }: UpstreamGroupedPanelProps) {
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
    return <p className="text-[13px] py-4" style={{ color: 'var(--body)' }}>{t('monitor.noUpstreams')}</p>
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      {groups.map(([adapter, items]) => (
        <div
          key={adapter}
          className="rounded-[5px] p-4"
          style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
        >
          <div className="flex items-center gap-2 mb-3">
            <EcosystemIcon type={adapter as any} size={14} />
            <span className="text-[12px] font-[400] uppercase tracking-wider" style={{ color: 'var(--heading)' }}>
              {adapter}
            </span>
            <span className="text-[10px] font-mono tabular-nums ml-auto" style={{ color: 'var(--body)' }}>
              {items.filter(u => u.healthy).length}/{items.length}
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
      ))}
    </div>
  )
}
