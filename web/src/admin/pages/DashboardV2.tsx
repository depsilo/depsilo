import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import MetricCardV2 from '@/components/MetricCardV2'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function UpstreamCard({ upstream }: { upstream: any }) {
  const { t } = useTranslation()
  const { data: latencyData } = useQuery({
    queryKey: ['admin', 'upstream-latency', upstream.id],
    queryFn: () => adminApi.getUpstreamLatency(upstream.id, '24h'),
    refetchInterval: 60000,
  })
  const points = latencyData?.data?.points || []

  return (
    <div
      className="flex items-center justify-between rounded-[5px] px-4 py-3"
      style={{ background: 'var(--surface-low)', border: '1px solid var(--border)' }}
    >
      <div className="flex items-center gap-3">
        <span className="relative flex h-2 w-2 shrink-0">
          {upstream.healthy && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--success)' }} />}
          <span className="relative inline-flex rounded-full h-2 w-2" style={{ background: upstream.healthy ? 'var(--success)' : 'var(--error)' }} />
        </span>
        <EcosystemIcon type={upstream.adapter} size={16} />
        <div>
          <p className="text-[14px] font-[400]" style={{ color: 'var(--heading)' }}>{upstream.name}</p>
          <p className="text-[10px] uppercase tracking-wider" style={{ color: 'var(--body)' }}>{upstream.adapter}</p>
        </div>
      </div>
      {points.length > 1 && (
        <div className="w-[120px] h-[40px]">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={points}>
              <Line type="monotone" dataKey="latency_ms" stroke="var(--stripe-purple)" strokeWidth={1.5} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
      <div className="text-right">
        <p className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{upstream.avg_latency_ms} ms</p>
        <p className="text-[10px]" style={{ color: 'var(--body)' }}>
          {t('dashboard.availability')} {((upstream.success_rate || 0) * 100).toFixed(1)}%
        </p>
      </div>
    </div>
  )
}

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
  const trendPoints = trendsData?.data?.points || []

  const metrics = [
    { label: t('dashboard.todayRequests'), value: today.total_requests?.toLocaleString() || '0', icon: <Icon name="monitoring" size="sm" /> },
    { label: t('dashboard.hitRate'), value: today.hit_rate != null ? `${(today.hit_rate * 100).toFixed(1)}%` : '0%', icon: <Icon name="target" size="sm" /> },
    { label: t('dashboard.bytesServed'), value: formatBytes(today.bytes_served || 0), icon: <Icon name="hard_drive" size="sm" /> },
    { label: t('dashboard.avgLatency'), value: `${Math.round(today.avg_latency_ms || 0)} ms`, icon: <Icon name="timer" size="sm" /> },
  ]

  return (
    <div className="space-y-6">
      {/* Metrics */}
      <div className="grid gap-4 grid-cols-4">
        {metrics.map((m) => <MetricCardV2 key={m.label} label={m.label} value={m.value} icon={m.icon} />)}
      </div>

      {/* Range selector */}
      <div className="flex items-center gap-1">
        {[
          { value: 'today', label: t('dashboard.rangeToday') },
          { value: '7d', label: t('dashboard.range7d') },
          { value: '30d', label: t('dashboard.range30d') },
        ].map(r => (
          <button
            key={r.value}
            onClick={() => setRange(r.value)}
            className="px-3 py-1.5 text-[13px] font-[400] rounded-[4px] transition-colors duration-150 cursor-pointer"
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

      {/* Trend Chart */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>
          {t('dashboard.hitMissTrend')}
        </h3>
        <ResponsiveContainer width="100%" height={300}>
          <LineChart data={trendPoints}>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <Tooltip contentStyle={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '4px', fontSize: 12 }} />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Line type="monotone" dataKey="hits" stroke="var(--stripe-purple)" name={t('dashboard.hits')} strokeWidth={2} dot={false} />
            <Line type="monotone" dataKey="misses" stroke="var(--error)" name={t('dashboard.misses')} strokeWidth={2} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      </CardV2>

      {/* Hit Rate Trend */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>
          {t('dashboard.hitRateTrend')}
        </h3>
        <ResponsiveContainer width="100%" height={250}>
          <LineChart data={trendPoints.map((p: any) => ({ ...p, hit_rate_pct: (p.hit_rate || 0) * 100 }))}>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis domain={[0, 100]} tick={{ fill: 'var(--body)', fontSize: 11 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => `${v}%`} />
            <Tooltip contentStyle={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '4px', fontSize: 12 }} formatter={(value) => [`${Number(value).toFixed(1)}%`, t('dashboard.hitRate2')]} />
            <Line type="monotone" dataKey="hit_rate_pct" stroke="var(--stripe-purple)" name={t('dashboard.hitRate2')} strokeWidth={2} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      </CardV2>

      <div className="grid gap-6 grid-cols-2">
        {/* Upstreams */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('dashboard.upstreamStatus')}</h3>
          <div className="space-y-3">
            {upstreams.length === 0 && <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('dashboard.noUpstreams')}</p>}
            {upstreams.map((u: any) => <UpstreamCard key={u.name} upstream={u} />)}
          </div>
        </CardV2>

        {/* Top Packages */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('dashboard.topPackages')}</h3>
          <div className="grid gap-6 grid-cols-2">
            <div>
              <div className="flex items-center gap-2 mb-3">
                <EcosystemIcon type="pypi" size={14} />
                <p className="text-[11px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>PyPI</p>
              </div>
              <div className="space-y-2">
                {(topPackages.pypi || []).slice(0, 10).map((p: any, i: number) => {
                  const max = topPackages.pypi?.[0]?.hit_count || 1
                  return (
                    <div key={p.name} className="space-y-1">
                      <div className="flex items-center justify-between text-[12px]">
                        <span className="truncate font-mono" style={{ color: 'var(--heading)' }}>{i + 1}. {p.name}</span>
                        <span className="font-mono tabular-nums ml-2" style={{ color: 'var(--body)' }}>{p.hit_count}</span>
                      </div>
                      <div className="h-1 w-full rounded-full" style={{ background: 'var(--surface-container)' }}>
                        <div className="h-1 rounded-full transition-all" style={{ width: `${(p.hit_count / max) * 100}%`, background: 'var(--stripe-purple)' }} />
                      </div>
                    </div>
                  )
                })}
                {(!topPackages.pypi || topPackages.pypi.length === 0) && <p className="text-[12px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>}
              </div>
            </div>
            <div>
              <div className="flex items-center gap-2 mb-3">
                <EcosystemIcon type="apt" size={14} />
                <p className="text-[11px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>APT</p>
              </div>
              <div className="space-y-2">
                {(topPackages.apt || []).slice(0, 10).map((p: any, i: number) => {
                  const max = topPackages.apt?.[0]?.hit_count || 1
                  return (
                    <div key={p.name} className="space-y-1">
                      <div className="flex items-center justify-between text-[12px]">
                        <span className="truncate font-mono" style={{ color: 'var(--heading)' }}>{i + 1}. {p.name}</span>
                        <span className="font-mono tabular-nums ml-2" style={{ color: 'var(--body)' }}>{p.hit_count}</span>
                      </div>
                      <div className="h-1 w-full rounded-full" style={{ background: 'var(--surface-container)' }}>
                        <div className="h-1 rounded-full transition-all" style={{ width: `${(p.hit_count / max) * 100}%`, background: 'var(--success)' }} />
                      </div>
                    </div>
                  )
                })}
                {(!topPackages.apt || topPackages.apt.length === 0) && <p className="text-[12px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>}
              </div>
            </div>
          </div>
        </CardV2>
      </div>
    </div>
  )
}
