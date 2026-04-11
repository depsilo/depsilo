import { useState, useEffect, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi, adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import BadgeV2 from '@/components/BadgeV2'
import EcosystemIcon from '@/components/EcosystemIcon'

interface CacheEvent {
  id: string
  timestamp: string
  hit: boolean
  package_name: string
  file_name: string
  upstream?: string
  adapter_type: string
}

interface StatsData {
  service: { status: string }
  today: {
    total_requests: number; hit_count: number; miss_count: number
    hit_rate: number; bytes_served: number; bytes_saved: number
  }
  cache: { total_files: number; total_size_bytes: number }
  upstreams: Array<{ name: string; adapter: string; healthy: boolean; avg_latency_ms: number; success_rate: number }>
  top_packages: {
    pypi: Array<{ name: string; hit_count: number }>
    apt: Array<{ name: string; hit_count: number }>
  }
}

function formatTime(dateStr: string): string {
  const d = new Date(dateStr)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}


// ── Uptime Kuma–style heartbeat bar ────────────────────────────────

const HEARTBEAT_SLOTS = 90

function beatColor(latency: number | null): string {
  if (latency === null) return 'var(--surface-container)'
  if (latency < 0) return '#dc3545'   // red — down
  if (latency < 80) return '#3bd671'   // bright green — fast
  if (latency < 200) return '#8cc152'  // olive green — ok
  if (latency < 500) return '#f5a623'  // orange — slow
  return '#dc3545'                     // red — very slow
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
    const base = upstream.avg_latency_ms
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

function HeartbeatBar({ upstream }: { upstream: UpstreamItem }) {
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

// ── Upstream panel — grid layout, grouped by ecosystem ─────────────

interface UpstreamItem {
  id?: number
  name: string
  adapter: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
}

function UpstreamStatusPanel({ upstreams }: { upstreams: UpstreamItem[] }) {
  const { t } = useTranslation()

  const groups = useMemo(() => {
    const map = new Map<string, UpstreamItem[]>()
    for (const u of upstreams) {
      if (!map.has(u.adapter)) map.set(u.adapter, [])
      map.get(u.adapter)!.push(u)
    }
    return Array.from(map.entries())
  }, [upstreams])

  if (upstreams.length === 0) {
    return (
      <CardV2>
        <h3 className="text-[12px] font-[400] uppercase tracking-wider mb-3" style={{ color: 'var(--body)' }}>
          {t('monitor.upstreams')}
        </h3>
        <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('monitor.noUpstreams')}</p>
      </CardV2>
    )
  }

  return (
    <div>
      <h3 className="text-[12px] font-[400] uppercase tracking-wider mb-3" style={{ color: 'var(--body)' }}>
        {t('monitor.upstreams')}
      </h3>

      {/* Grouped upstream cards — 2 columns */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {groups.map(([adapter, items]) => (
          <div
            key={adapter}
            className="rounded-[5px] p-4"
            style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
          >
            {/* Group header */}
            <div className="flex items-center gap-2 mb-3">
              <EcosystemIcon type={adapter as any} size={14} />
              <span className="text-[12px] font-[400] uppercase tracking-wider" style={{ color: 'var(--heading)' }}>
                {adapter}
              </span>
              <span className="text-[10px] font-mono tabular-nums ml-auto" style={{ color: 'var(--body)' }}>
                {items.filter(u => u.healthy).length}/{items.length}
              </span>
            </div>

            {/* Upstream rows inside one card */}
            <div className="space-y-3">
              {items.map((u) => (
                <div key={u.name}>
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-[12px] font-[400]" style={{ color: 'var(--heading)' }}>{u.name}</span>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-[11px] tabular-nums" style={{ color: (u.avg_latency_ms || 0) <= 1 ? 'var(--body)' : u.avg_latency_ms < 100 ? '#3bd671' : u.avg_latency_ms < 500 ? 'var(--body)' : 'var(--error)' }}>
                        {(u.avg_latency_ms || 0) <= 1 ? '--' : `${u.avg_latency_ms}ms`}
                      </span>
                      <span className="text-[10px]" style={{ color: u.healthy ? '#3bd671' : 'var(--error)' }}>
                        {u.healthy ? '●' : '○'}
                      </span>
                    </div>
                  </div>
                  <HeartbeatBar upstream={u} />
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

// ── Main page ──────────────────────────────────────────────────────

export default function MonitorV2() {
  const { t } = useTranslation()
  const [events, setEvents] = useState<CacheEvent[]>([])
  const [extraHits, setExtraHits] = useState(0)
  const [extraMisses, setExtraMisses] = useState(0)
  const [connected, setConnected] = useState(false)

  // Stats from API
  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor'],
    queryFn: async () => { const res = await statsApi.getStats(); return res.data },
    refetchInterval: 30000,
  })

  // SSE
  const handleEvent = useCallback((e: MessageEvent) => {
    try {
      const event: CacheEvent = JSON.parse(e.data)
      if (!event.id) event.id = `${event.timestamp}-${Math.random().toString(36).slice(2, 8)}`
      setEvents((prev) => [event, ...prev].slice(0, 100))
      if (event.hit) setExtraHits((h) => h + 1)
      else setExtraMisses((m) => m + 1)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const es = new EventSource('/api/v1/events/stream')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = handleEvent
    return () => es.close()
  }, [handleEvent])

  const totalHits = (data?.today.hit_count ?? 0) + extraHits
  const totalMisses = (data?.today.miss_count ?? 0) + extraMisses
  const totalRequests = totalHits + totalMisses
  const hitRate = totalRequests > 0 ? ((totalHits / totalRequests) * 100).toFixed(1) : '0.0'

  const upstreams = data?.upstreams ?? []
  const topPypi = data?.top_packages?.pypi ?? []
  const topApt = data?.top_packages?.apt ?? []
  const maxPypi = Math.max(...topPypi.map(p => p.hit_count), 1)
  const maxApt = Math.max(...topApt.map(p => p.hit_count), 1)

  return (
    <div className="space-y-6">
      {/* Header */}
      <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--heading)' }}>
        {t('monitor.title')}
      </h1>

      {/* Metrics row — 4 cols in one bar */}
      <div
        className="grid grid-cols-4 rounded-[6px] overflow-hidden"
        style={{ border: '1px solid var(--border)', background: 'var(--surface)' }}
      >
        {[
          { label: t('monitor.hitRate'), value: `${hitRate}%`, accent: true },
          { label: t('monitor.requests'), value: totalRequests.toLocaleString() },
          { label: t('monitor.hits'), value: totalHits.toLocaleString() },
          { label: t('monitor.misses'), value: totalMisses.toLocaleString() },
        ].map((m, i) => (
          <div
            key={m.label}
            className="px-5 py-4"
            style={{ borderLeft: i > 0 ? '1px solid var(--border)' : 'none' }}
          >
            <p className="text-[11px] font-[400] uppercase tracking-wider mb-1" style={{ color: 'var(--body)' }}>{m.label}</p>
            <p
              className="text-[22px] font-[300] font-mono tabular-nums tracking-tight"
              style={{ color: m.accent ? 'var(--stripe-purple)' : 'var(--heading)' }}
            >
              {m.value}
            </p>
          </div>
        ))}
      </div>

      {/* Event stream */}
      <div>
        <div className="flex items-center gap-2 mb-2">
          <span className="relative flex h-2 w-2">
            {connected && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--success)' }} />}
            <span className="relative inline-flex rounded-full h-2 w-2" style={{ background: connected ? 'var(--success)' : 'var(--error)' }} />
          </span>
          <span className="text-[12px]" style={{ color: connected ? 'var(--success-text)' : 'var(--error)' }}>
            {connected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
        <div className="rounded-[5px] overflow-hidden" style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}>
          {events.length === 0 ? (
            <p className="py-12 text-center text-[13px]" style={{ color: 'var(--body)' }}>
              {t('monitor.noEvents')}
            </p>
          ) : (
            <div>
              {events.map((event, i) => (
                <div
                  key={event.id}
                  className="flex items-center gap-3 px-4 py-2 animate-fade-in"
                  style={{
                    borderBottom: i < events.length - 1 ? '1px solid var(--border)' : 'none',
                    borderLeft: `3px solid ${event.hit ? 'var(--success)' : 'var(--lemon, #9b6829)'}`,
                  }}
                >
                  <span className="text-[11px] font-mono tabular-nums shrink-0 w-14" style={{ color: 'var(--body)' }}>
                    {formatTime(event.timestamp)}
                  </span>
                  <EcosystemIcon type={event.adapter_type as any} size={14} />
                  <span className="font-mono text-[12px] truncate min-w-0 flex-1" style={{ color: 'var(--heading)' }}>
                    {event.package_name}
                  </span>
                  <BadgeV2 variant={event.hit ? 'success' : 'warning'} className="shrink-0">
                    {event.hit ? 'HIT' : 'MISS'}
                  </BadgeV2>
                  {!event.hit && event.upstream && (
                    <span className="text-[11px] shrink-0" style={{ color: 'var(--body)' }}>→ {event.upstream}</span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Upstreams — grouped by ecosystem, with heartbeat bars */}
      <UpstreamStatusPanel upstreams={upstreams} />

      {/* Top packages */}
      <div className="grid gap-4 md:grid-cols-2">
        {topPypi.length > 0 && (
          <CardV2>
            <div className="flex items-center gap-1.5 mb-3">
              <EcosystemIcon type="pypi" size={14} />
              <span className="text-[12px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>
                {t('monitor.topPackages')} — PyPI
              </span>
            </div>
            <div className="space-y-1.5">
              {topPypi.slice(0, 8).map(pkg => (
                <div key={pkg.name} className="flex items-center gap-2">
                  <span className="font-mono text-[12px] truncate flex-1" style={{ color: 'var(--heading)' }}>{pkg.name}</span>
                  <span className="font-mono text-[11px] tabular-nums shrink-0" style={{ color: 'var(--body)' }}>{pkg.hit_count.toLocaleString()}</span>
                  <div className="w-16 h-1 rounded-full shrink-0" style={{ background: 'var(--surface-container)' }}>
                    <div className="h-full rounded-full" style={{ width: `${(pkg.hit_count / maxPypi) * 100}%`, background: 'var(--stripe-purple)' }} />
                  </div>
                </div>
              ))}
            </div>
          </CardV2>
        )}
        {topApt.length > 0 && (
          <CardV2>
            <div className="flex items-center gap-1.5 mb-3">
              <EcosystemIcon type="apt" size={14} />
              <span className="text-[12px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>
                {t('monitor.topPackages')} — APT
              </span>
            </div>
            <div className="space-y-1.5">
              {topApt.slice(0, 8).map(pkg => (
                <div key={pkg.name} className="flex items-center gap-2">
                  <span className="font-mono text-[12px] truncate flex-1" style={{ color: 'var(--heading)' }}>{pkg.name}</span>
                  <span className="font-mono text-[11px] tabular-nums shrink-0" style={{ color: 'var(--body)' }}>{pkg.hit_count.toLocaleString()}</span>
                  <div className="w-16 h-1 rounded-full shrink-0" style={{ background: 'var(--surface-container)' }}>
                    <div className="h-full rounded-full" style={{ width: `${(pkg.hit_count / maxApt) * 100}%`, background: 'var(--success)' }} />
                  </div>
                </div>
              ))}
            </div>
          </CardV2>
        )}
        {topPypi.length === 0 && topApt.length === 0 && (
          <CardV2><p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p></CardV2>
        )}
      </div>
    </div>
  )
}
