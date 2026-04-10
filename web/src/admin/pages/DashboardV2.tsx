import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import MetricCardV2 from '@/components/MetricCardV2'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import {
  ComposedChart, Bar, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

// ── Heartbeat bar (same logic as MonitorV2) ────────────────────────

const HEARTBEAT_SLOTS = 90

function beatColor(latency: number | null): string {
  if (latency === null) return 'var(--surface-container)'
  if (latency < 0) return 'var(--error)'
  if (latency < 100) return 'var(--success)'
  if (latency < 500) return 'var(--lemon, #9b6829)'
  return 'var(--error)'
}

function HeartbeatBar({ upstream }: { upstream: any }) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)

  const { data } = useQuery({
    queryKey: ['admin', 'upstream-latency', upstream.id],
    queryFn: () => adminApi.getUpstreamLatency(upstream.id, '24h'),
    refetchInterval: 60000,
    retry: false,
    enabled: !!upstream.id,
  })

  const realPoints: Array<{ latency_ms: number }> = data?.data?.points || []

  const beats = useMemo(() => {
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
    if (base <= 1) return Array(HEARTBEAT_SLOTS).fill(null)
    const failRate = 1 - (upstream.success_rate || 1)
    return Array.from({ length: HEARTBEAT_SLOTS }, (_, i) => {
      if (i < HEARTBEAT_SLOTS * 0.3) return null
      if (failRate > 0 && Math.random() < failRate) return -1
      const variance = (Math.random() - 0.5) * base * 0.4
      return Math.max(1, Math.round(base + variance))
    })
  }, [realPoints, upstream.avg_latency_ms, upstream.success_rate])

  return (
    <div className="relative" style={{ height: 18 }}>
      <div
        className="absolute bottom-0 left-0 right-0 flex items-end gap-[1px]"
        onMouseLeave={() => setHoveredIdx(null)}
      >
        {beats.map((lat, i) => (
          <div
            key={i}
            className="rounded-[1px] cursor-pointer"
            style={{
              height: hoveredIdx === i ? 16 : 10,
              flex: '1 1 0%',
              minWidth: 2,
              background: beatColor(lat),
              opacity: lat === null ? 0.25 : (hoveredIdx !== null && hoveredIdx !== i ? 0.5 : 1),
              transition: 'height 75ms, opacity 75ms',
            }}
            onMouseEnter={() => setHoveredIdx(i)}
          />
        ))}
      </div>
      {hoveredIdx !== null && (
        <div
          className="absolute bottom-full mb-1 px-2 py-1 rounded-[4px] text-[11px] font-mono whitespace-nowrap pointer-events-none z-10"
          style={{
            background: 'var(--heading)',
            color: 'var(--bg)',
            left: `${(hoveredIdx / HEARTBEAT_SLOTS) * 100}%`,
            transform: 'translateX(-50%)',
          }}
        >
          {beats[hoveredIdx] === null ? 'No data' : beats[hoveredIdx]! < 0 ? 'Failed' : `${beats[hoveredIdx]}ms`}
        </div>
      )}
    </div>
  )
}

// ── Upstream panel grouped by ecosystem ────────────────────────────

function UpstreamPanel({ upstreams }: { upstreams: any[] }) {
  const { t } = useTranslation()

  const groups = useMemo(() => {
    const map = new Map<string, any[]>()
    for (const u of upstreams) {
      const key = u.adapter || u.adapter_type
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(u)
    }
    return Array.from(map.entries())
  }, [upstreams])

  if (upstreams.length === 0) {
    return <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('dashboard.noUpstreams')}</p>
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      {groups.map(([adapter, items]) => (
        <div
          key={adapter}
          className="rounded-[5px] p-3"
          style={{ background: 'var(--surface-low)', border: '1px solid var(--border)' }}
        >
          <div className="flex items-center gap-2 mb-2">
            <EcosystemIcon type={adapter as any} size={14} />
            <span className="text-[12px] font-[400] uppercase tracking-wider" style={{ color: 'var(--heading)' }}>
              {adapter}
            </span>
            <span className="text-[10px] font-mono tabular-nums ml-auto" style={{ color: 'var(--body)' }}>
              {items.filter((u: any) => u.healthy).length}/{items.length} {t('dashboard.healthy') || 'healthy'}
            </span>
          </div>
          <div className="space-y-2">
            {items.map((u: any) => (
              <div key={u.name}>
                <div className="flex items-center gap-1.5 mb-1">
                  <span
                    className="inline-block h-1.5 w-1.5 rounded-full shrink-0"
                    style={{ background: u.healthy ? 'var(--success)' : 'var(--error)' }}
                  />
                  <span className="text-[12px] font-[400] truncate flex-1" style={{ color: 'var(--heading)' }}>
                    {u.name}
                  </span>
                  <span
                    className="font-mono text-[11px] tabular-nums shrink-0"
                    style={{
                      color: (u.avg_latency_ms || 0) <= 1 ? 'var(--body)'
                        : u.avg_latency_ms < 100 ? 'var(--success-text)'
                        : u.avg_latency_ms < 500 ? 'var(--body)' : 'var(--error)',
                    }}
                  >
                    {(u.avg_latency_ms || 0) <= 1 ? '--' : `${u.avg_latency_ms}ms`}
                  </span>
                  <span className="text-[10px] font-mono tabular-nums shrink-0" style={{ color: 'var(--body)' }}>
                    {((u.success_rate || 0) * 100).toFixed(0)}%
                  </span>
                </div>
                <HeartbeatBar upstream={u} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Top packages (merged, sorted by hits) ──────────────────────────

function TopPackagesList({ topPackages }: { topPackages: { pypi?: any[]; apt?: any[] } }) {
  const { t } = useTranslation()

  const merged = useMemo(() => {
    const all: Array<{ name: string; hit_count: number; ecosystem: string }> = []
    for (const p of topPackages.pypi || []) all.push({ ...p, ecosystem: 'pypi' })
    for (const p of topPackages.apt || []) all.push({ ...p, ecosystem: 'apt' })
    all.sort((a, b) => b.hit_count - a.hit_count)
    return all.slice(0, 10)
  }, [topPackages])

  if (merged.length === 0) {
    return <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
  }

  const max = merged[0].hit_count || 1

  return (
    <div className="space-y-2">
      {merged.map((p, i) => (
        <div key={`${p.ecosystem}-${p.name}`} className="flex items-center gap-2">
          <span className="text-[11px] font-mono tabular-nums w-5 shrink-0 text-right" style={{ color: 'var(--body)' }}>
            {i + 1}
          </span>
          <EcosystemIcon type={p.ecosystem as any} size={12} />
          <span className="font-mono text-[12px] truncate flex-1" style={{ color: 'var(--heading)' }}>
            {p.name}
          </span>
          <span className="font-mono text-[11px] tabular-nums shrink-0" style={{ color: 'var(--body)' }}>
            {p.hit_count.toLocaleString()}
          </span>
          <div className="w-20 h-1 rounded-full shrink-0" style={{ background: 'var(--surface-container)' }}>
            <div
              className="h-full rounded-full"
              style={{ width: `${(p.hit_count / max) * 100}%`, background: 'var(--stripe-purple)' }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Custom tooltip ─────────────────────────────────────────────────

function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--heading)' }}>{label}</p>
      {payload.map((entry: any) => (
        <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {entry.dataKey === 'hit_rate_pct' ? `${Number(entry.value).toFixed(1)}%` : entry.value?.toLocaleString()}
        </p>
      ))}
    </div>
  )
}

// ── Main Dashboard ─────────────────────────────────────────────────

export default function DashboardV2() {
  const { t } = useTranslation()
  const [range, setRange] = useState('7d')

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'dashboard'],
    queryFn: () => adminApi.getDashboard(),
    refetchInterval: 30000,
  })

  const { data: trendsData } = useQuery({
    queryKey: ['admin', 'dashboard', 'trends', range],
    queryFn: () => adminApi.getDashboardTrends(range),
    refetchInterval: 30000,
  })

  const dashboard = data?.data

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid gap-4 grid-cols-4">
          {[...Array(4)].map((_, i) => <div key={i} className="h-24 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />)}
        </div>
        <div className="h-80 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />
      </div>
    )
  }

  const today = dashboard?.today || {} as any
  const upstreams = dashboard?.upstreams || []
  const topPackages = dashboard?.top_packages || { pypi: [], apt: [] }
  const trendPoints = (trendsData?.data?.points || []).map((p: any) => ({
    ...p,
    hit_rate_pct: (p.hit_rate || 0) * 100,
  }))

  const metrics = [
    { label: t('dashboard.todayRequests'), value: today.total_requests?.toLocaleString() || '0', icon: <Icon name="monitoring" size="sm" /> },
    { label: t('dashboard.hitRate'), value: today.hit_rate != null ? `${(today.hit_rate * 100).toFixed(1)}%` : '0%', icon: <Icon name="target" size="sm" /> },
    { label: t('dashboard.bytesServed'), value: formatBytes(today.bytes_served || 0), icon: <Icon name="hard_drive" size="sm" /> },
    { label: t('dashboard.avgLatency'), value: (today.avg_latency_ms || 0) <= 1 ? '--' : `${Math.round(today.avg_latency_ms)} ms`, icon: <Icon name="timer" size="sm" /> },
  ]

  const ranges = [
    { value: 'today', label: t('dashboard.rangeToday') },
    { value: '7d', label: t('dashboard.range7d') },
    { value: '30d', label: t('dashboard.range30d') },
  ]

  return (
    <div className="space-y-6">
      {/* Metrics */}
      <div className="grid gap-4 grid-cols-4">
        {metrics.map((m) => <MetricCardV2 key={m.label} label={m.label} value={m.value} icon={m.icon} />)}
      </div>

      {/* Combined Chart: bars (hits/misses) + line (hit rate %) */}
      <CardV2>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
            {t('dashboard.hitMissTrend')}
          </h3>
          <div className="flex items-center gap-1">
            {ranges.map(r => (
              <button
                key={r.value}
                onClick={() => setRange(r.value)}
                className="px-2.5 py-1 text-[11px] font-[400] rounded-[4px] cursor-pointer transition-colors duration-150"
                style={{
                  background: range === r.value ? 'var(--stripe-purple)' : 'transparent',
                  color: range === r.value ? 'var(--on-primary)' : 'var(--body)',
                  border: range === r.value ? 'none' : '1px solid var(--border)',
                }}
              >
                {r.label}
              </button>
            ))}
          </div>
        </div>
        <ResponsiveContainer width="100%" height={320}>
          <ComposedChart data={trendPoints}>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis yAxisId="count" tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => `${v}%`} />
            <Tooltip content={<ChartTooltip />} />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Bar yAxisId="count" dataKey="hits" fill="var(--stripe-purple)" name={t('dashboard.hits')} radius={[2, 2, 0, 0]} opacity={0.8} />
            <Bar yAxisId="count" dataKey="misses" fill="var(--error)" name={t('dashboard.misses')} radius={[2, 2, 0, 0]} opacity={0.6} />
            <Line yAxisId="rate" type="monotone" dataKey="hit_rate_pct" stroke="var(--success)" name={t('dashboard.hitRate2')} strokeWidth={2} dot={false} />
          </ComposedChart>
        </ResponsiveContainer>
      </CardV2>

      {/* Upstreams + Top Packages */}
      <div className="grid gap-6 grid-cols-2">
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>
            {t('dashboard.upstreamStatus')}
          </h3>
          <UpstreamPanel upstreams={upstreams} />
        </CardV2>

        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>
            {t('dashboard.topPackages')}
          </h3>
          <TopPackagesList topPackages={topPackages} />
        </CardV2>
      </div>
    </div>
  )
}
