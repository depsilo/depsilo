import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import MetricCardV2 from '@/components/MetricCard'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import { UpstreamGroupedPanel } from '@/components/UpstreamCard'
import {
  ComposedChart, AreaChart, Area, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  Legend, ResponsiveContainer,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
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

  const { data: bwData } = useQuery({
    queryKey: ['admin', 'bandwidth', '7d'],
    queryFn: () => adminApi.getBandwidthReport({ range: '7d' }),
    refetchInterval: 60000,
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
  const yesterday = dashboard?.yesterday || {} as any
  const upstreams = dashboard?.upstreams || []
  const topPackages = dashboard?.top_packages || { pypi: [], apt: [] }
  const trendPoints = (trendsData?.data?.points || []).map((p: any) => ({
    ...p,
    hit_rate_pct: (p.hit_rate || 0) * 100,
  }))

  const metrics = [
    { label: t('dashboard.todayRequests'), value: today.total_requests?.toLocaleString() || '0', icon: <Icon name="monitoring" size="sm" />, change: yesterday.total_requests ? ((today.total_requests - yesterday.total_requests) / yesterday.total_requests * 100) : null },
    { label: t('dashboard.hitRate'), value: today.hit_rate != null ? `${(today.hit_rate * 100).toFixed(1)}%` : '0%', icon: <Icon name="target" size="sm" />, change: yesterday.hit_rate ? ((today.hit_rate - yesterday.hit_rate) / yesterday.hit_rate * 100) : null },
    { label: t('dashboard.bytesServed'), value: formatBytes(today.bytes_served || 0), icon: <Icon name="hard_drive" size="sm" />, change: yesterday.bytes_served ? ((today.bytes_served - yesterday.bytes_served) / yesterday.bytes_served * 100) : null },
    { label: t('dashboard.avgLatency'), value: (today.avg_latency_ms || 0) <= 1 ? '--' : `${Math.round(today.avg_latency_ms)} ms`, icon: <Icon name="timer" size="sm" />, change: yesterday.avg_latency_ms ? ((today.avg_latency_ms - yesterday.avg_latency_ms) / yesterday.avg_latency_ms * 100) : null },
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
        {metrics.map((m) => <MetricCardV2 key={m.label} label={m.label} value={m.value} icon={m.icon} change={m.change} />)}
      </div>

      {/* Storage alert */}
      {dashboard?.cache_usage_percent > 80 && (
        <div
          className="flex items-center gap-2 rounded-[5px] px-4 py-2.5 text-[13px]"
          style={{
            background: dashboard.cache_usage_percent > 95 ? 'var(--error-container)' : 'rgba(245,166,35,0.1)',
            color: dashboard.cache_usage_percent > 95 ? 'var(--error)' : 'var(--lemon, #9b6829)',
            border: `1px solid ${dashboard.cache_usage_percent > 95 ? 'var(--error)' : 'rgba(245,166,35,0.3)'}`,
          }}
        >
          <Icon name="warning" size="sm" />
          {t('dashboard.storageWarning', { percent: dashboard.cache_usage_percent?.toFixed(1) })}
        </div>
      )}

      {/* Chart + Top Packages side by side */}
      <div className="grid gap-4 grid-cols-3">
        {/* Chart — 2/3 width */}
        <CardV2 className="col-span-2">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
              {t('dashboard.hitMissTrend')}
            </h3>
            <div className="flex items-center gap-1">
              {ranges.map(r => (
                <button
                  key={r.value}
                  onClick={() => setRange(r.value)}
                  className="px-2 py-0.5 text-[11px] font-[400] rounded-[4px] cursor-pointer transition-colors duration-150"
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
          <ResponsiveContainer width="100%" height={180}>
            <ComposedChart data={trendPoints}>
              <defs>
                <linearGradient id="gradHits" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--stripe-purple)" stopOpacity={0.3} />
                  <stop offset="100%" stopColor="var(--stripe-purple)" stopOpacity={0.02} />
                </linearGradient>
                <linearGradient id="gradMisses" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--error)" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="var(--error)" stopOpacity={0.02} />
                </linearGradient>
              </defs>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
              <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis yAxisId="count" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={30} />
              <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => `${v}%`} width={35} />
              <Tooltip content={<ChartTooltip />} />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Area yAxisId="count" type="monotone" dataKey="hits" stroke="var(--stripe-purple)" strokeWidth={1.5} fill="url(#gradHits)" name={t('dashboard.hits')} />
              <Area yAxisId="count" type="monotone" dataKey="misses" stroke="var(--error)" strokeWidth={1.5} fill="url(#gradMisses)" name={t('dashboard.misses')} />
              <Line yAxisId="rate" type="monotone" dataKey="hit_rate_pct" stroke="var(--success)" name={t('dashboard.hitRate2')} strokeWidth={2} dot={false} />
            </ComposedChart>
          </ResponsiveContainer>
        </CardV2>

        {/* Top Packages — 1/3 width */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('dashboard.topPackages')}
          </h3>
          <TopPackagesList topPackages={topPackages} />
        </CardV2>
      </div>

      {/* Bandwidth savings summary */}
      {bwData?.data?.summary && (() => {
        const bw = bwData.data.summary
        const bwDaily = bwData.data.daily || []
        const fmtTime = (ms: number) => {
          if (ms <= 0) return '0s'
          const s = Math.floor(ms / 1000); const m = Math.floor(s / 60); const h = Math.floor(m / 60)
          if (h > 0) return `${h}${t('bandwidth.hours')} ${m % 60}${t('bandwidth.minutes')}`
          if (m > 0) return `${m}${t('bandwidth.minutes')} ${s % 60}${t('bandwidth.seconds')}`
          return `${s}${t('bandwidth.seconds')}`
        }
        return (
          <CardV2>
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
                {t('bandwidth.bandwidthSummary')}
              </h3>
              <Link
                to="/admin/bandwidth"
                className="text-[11px] font-[400] no-underline transition-colors duration-150"
                style={{ color: 'var(--stripe-purple)' }}
              >
                {t('bandwidth.viewFullReport')}
              </Link>
            </div>
            <div className="grid grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.totalTraffic')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: 'var(--heading)' }}>{formatBytes(bw.total_bytes || 0)}</p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.trafficSaved')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: '#10b981' }}>{formatBytes(bw.hit_bytes || 0)}</p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.savingsRate')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: bw.savings_rate > 0.5 ? '#10b981' : 'var(--heading)' }}>
                  {bw.savings_rate != null ? `${(bw.savings_rate * 100).toFixed(1)}%` : '0%'}
                </p>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{t('bandwidth.timeSaved')}</p>
                <p className="text-[20px] font-[300] font-mono tabular-nums mt-1" style={{ color: '#10b981' }}>{fmtTime(bw.time_saved_ms || 0)}</p>
              </div>
            </div>
            {bwDaily.length > 0 && (
              <ResponsiveContainer width="100%" height={100}>
                <AreaChart data={bwDaily}>
                  <defs>
                    <linearGradient id="gradBwHit" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#10b981" stopOpacity={0.3} />
                      <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 9 }} axisLine={false} tickLine={false} />
                  <YAxis hide />
                  <Area type="monotone" dataKey="hit_bytes" stroke="#10b981" strokeWidth={1.5} fill="url(#gradBwHit)" />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </CardV2>
        )
      })()}

      {/* Upstreams — full width */}
      <div>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
          {t('dashboard.upstreamStatus')}
        </h3>
        <UpstreamGroupedPanel upstreams={upstreams} />
      </div>
    </div>
  )
}
