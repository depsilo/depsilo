import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import MetricCardV2 from '@/components/MetricCard'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import {
  AreaChart, Area, BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function formatTimeSaved(ms: number, t: (key: string) => string): string {
  if (ms <= 0) return '0s'
  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  if (hours > 0) return `${hours}${t('bandwidth.hours')} ${minutes % 60}${t('bandwidth.minutes')}`
  if (minutes > 0) return `${minutes}${t('bandwidth.minutes')} ${seconds % 60}${t('bandwidth.seconds')}`
  return `${seconds}${t('bandwidth.seconds')}`
}

const ECO_COLORS = [
  'var(--stripe-purple)', '#3b82f6', '#10b981', '#f59e0b',
  '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4',
  '#84cc16', '#f97316', '#6366f1', '#14b8a6',
]

function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--heading)' }}>{label}</p>
      {payload.map((entry: any) => (
        <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {formatBytes(entry.value)}
        </p>
      ))}
    </div>
  )
}

function LatencyTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--heading)' }}>{label}</p>
      {payload.map((entry: any) => (
        <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {Math.round(entry.value)} ms
        </p>
      ))}
    </div>
  )
}

export default function BandwidthReport() {
  const { t } = useTranslation()
  const [range, setRange] = useState('7d')
  const [customStart, setCustomStart] = useState('')
  const [customEnd, setCustomEnd] = useState('')

  const params = range === 'custom'
    ? { range: 'custom', start: customStart, end: customEnd }
    : { range }

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'bandwidth', params],
    queryFn: () => adminApi.getBandwidthReport(params),
    enabled: range !== 'custom' || (!!customStart && !!customEnd),
    refetchInterval: 60000,
  })

  const report = data?.data
  const summary = report?.summary || {}
  const daily = report?.daily || []
  const byEcosystem = report?.by_ecosystem || []
  const topPackages = report?.top_packages || []
  const byUpstream = report?.by_upstream || []

  const ranges = [
    { value: '7d', label: t('bandwidth.last7d') },
    { value: '30d', label: t('bandwidth.last30d') },
    { value: '90d', label: t('bandwidth.last90d') },
    { value: 'custom', label: t('bandwidth.custom') },
  ]

  // Ecosystem donut data
  const ecoDonutData = byEcosystem
    .map((e: any) => ({ name: e.ecosystem, value: e.hit_bytes + e.miss_bytes }))
    .filter((e: any) => e.value > 0)
    .sort((a: any, b: any) => b.value - a.value)

  // Latency comparison data
  const latencyData = byEcosystem
    .filter((e: any) => e.avg_miss_latency_ms > 0)
    .map((e: any) => ({
      ecosystem: e.ecosystem,
      hit: Math.round(e.avg_hit_latency_ms),
      miss: Math.round(e.avg_miss_latency_ms),
    }))
    .sort((a: any, b: any) => b.miss - a.miss)

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

  return (
    <div className="space-y-6">
      {/* Range selector */}
      <div className="flex items-center gap-2 flex-wrap">
        {ranges.map(r => (
          <button
            key={r.value}
            onClick={() => setRange(r.value)}
            className="px-3 py-1 text-[12px] font-[400] rounded-[4px] cursor-pointer transition-colors duration-150"
            style={{
              background: range === r.value ? 'var(--stripe-purple)' : 'transparent',
              color: range === r.value ? 'var(--on-primary)' : 'var(--body)',
              border: range === r.value ? 'none' : '1px solid var(--border)',
            }}
          >
            {r.label}
          </button>
        ))}
        {range === 'custom' && (
          <div className="flex items-center gap-2 ml-2">
            <input
              type="date"
              value={customStart}
              onChange={e => setCustomStart(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--heading)' }}
            />
            <span className="text-[12px]" style={{ color: 'var(--body)' }}>—</span>
            <input
              type="date"
              value={customEnd}
              onChange={e => setCustomEnd(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--heading)' }}
            />
          </div>
        )}
      </div>

      {/* Summary metrics */}
      <div className="grid gap-4 grid-cols-4">
        <MetricCardV2
          label={t('bandwidth.totalTraffic')}
          value={formatBytes(summary.total_bytes || 0)}
          icon={<Icon name="cloud_download" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.trafficSaved')}
          value={formatBytes(summary.hit_bytes || 0)}
          icon={<Icon name="savings" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.savingsRate')}
          value={summary.savings_rate != null ? `${(summary.savings_rate * 100).toFixed(1)}%` : '0%'}
          icon={<Icon name="trending_up" size="sm" />}
        />
        <MetricCardV2
          label={t('bandwidth.timeSaved')}
          value={formatTimeSaved(summary.time_saved_ms || 0, t)}
          icon={<Icon name="schedule" size="sm" />}
        />
      </div>

      {/* Daily trend — stacked area chart */}
      <CardV2>
        <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
          {t('bandwidth.dailyTrend')}
        </h3>
        <ResponsiveContainer width="100%" height={240}>
          <AreaChart data={daily}>
            <defs>
              <linearGradient id="gradHitBytes" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#10b981" stopOpacity={0.3} />
                <stop offset="100%" stopColor="#10b981" stopOpacity={0.02} />
              </linearGradient>
              <linearGradient id="gradMissBytes" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--error)" stopOpacity={0.2} />
                <stop offset="100%" stopColor="var(--error)" stopOpacity={0.02} />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
            <XAxis dataKey="date" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={50} tickFormatter={(v: number) => formatBytes(v)} />
            <Tooltip content={<ChartTooltip />} />
            <Legend wrapperStyle={{ fontSize: 11 }} />
            <Area type="monotone" dataKey="hit_bytes" stackId="1" stroke="#10b981" strokeWidth={1.5} fill="url(#gradHitBytes)" name={t('bandwidth.hitBytes')} />
            <Area type="monotone" dataKey="miss_bytes" stackId="1" stroke="var(--error)" strokeWidth={1.5} fill="url(#gradMissBytes)" name={t('bandwidth.missBytes')} />
          </AreaChart>
        </ResponsiveContainer>
      </CardV2>

      {/* Three columns: ecosystem donut, top packages, upstream bar */}
      <div className="grid gap-4 grid-cols-3">
        {/* Ecosystem donut */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.byEcosystem')}
          </h3>
          {ecoDonutData.length > 0 ? (
            <>
              <ResponsiveContainer width="100%" height={160}>
                <PieChart>
                  <Pie data={ecoDonutData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={40} outerRadius={65} paddingAngle={2}>
                    {ecoDonutData.map((_: any, i: number) => (
                      <Cell key={i} fill={ECO_COLORS[i % ECO_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip formatter={(value: number) => formatBytes(value)} />
                </PieChart>
              </ResponsiveContainer>
              <div className="space-y-1.5 mt-2">
                {ecoDonutData.slice(0, 6).map((e: any, i: number) => (
                  <div key={e.name} className="flex items-center gap-2 text-[11px]">
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ background: ECO_COLORS[i % ECO_COLORS.length] }} />
                    <EcosystemIcon type={e.name} size={12} />
                    <span className="font-mono" style={{ color: 'var(--heading)' }}>{e.name}</span>
                    <span className="ml-auto font-mono tabular-nums" style={{ color: 'var(--body)' }}>{formatBytes(e.value)}</span>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>

        {/* Top packages */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.topPackages')}
          </h3>
          {topPackages.length > 0 ? (
            <div className="space-y-2">
              {topPackages.map((p: any, i: number) => {
                const max = topPackages[0]?.total_bytes || 1
                return (
                  <div key={`${p.ecosystem}-${p.package_name}`} className="flex items-center gap-2">
                    <span className="text-[11px] font-mono tabular-nums w-4 shrink-0 text-right" style={{ color: 'var(--body)' }}>{i + 1}</span>
                    <EcosystemIcon type={p.ecosystem} size={12} />
                    <span className="font-mono text-[11px] truncate flex-1" style={{ color: 'var(--heading)' }}>{p.package_name}</span>
                    <span className="font-mono text-[10px] tabular-nums shrink-0" style={{ color: 'var(--body)' }}>{formatBytes(p.total_bytes)}</span>
                    <div className="w-16 h-1 rounded-full shrink-0" style={{ background: 'var(--surface-container)' }}>
                      <div className="h-full rounded-full" style={{ width: `${(p.total_bytes / max) * 100}%`, background: 'var(--stripe-purple)' }} />
                    </div>
                  </div>
                )
              })}
            </div>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>

        {/* Upstream bar */}
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-3" style={{ color: 'var(--body)' }}>
            {t('bandwidth.byUpstream')}
          </h3>
          {byUpstream.length > 0 ? (
            <ResponsiveContainer width="100%" height={Math.max(160, byUpstream.length * 32)}>
              <BarChart data={byUpstream} layout="vertical" margin={{ left: 0, right: 10 }}>
                <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => formatBytes(v)} />
                <YAxis type="category" dataKey="upstream" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={80} />
                <Tooltip formatter={(value: number) => formatBytes(value)} />
                <Bar dataKey="miss_bytes" fill="var(--stripe-purple)" radius={[0, 3, 3, 0]} barSize={16} name={t('bandwidth.totalBandwidth')} />
              </BarChart>
            </ResponsiveContainer>
          ) : (
            <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
          )}
        </CardV2>
      </div>

      {/* Latency comparison */}
      <CardV2>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
            {t('bandwidth.latencyComparison')}
          </h3>
          <span className="text-[12px] font-mono tabular-nums" style={{ color: 'var(--success)' }}>
            {t('bandwidth.timeSaved')}: {formatTimeSaved(summary.time_saved_ms || 0, t)}
          </span>
        </div>
        {latencyData.length > 0 ? (
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={latencyData}>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" />
              <XAxis dataKey="ecosystem" tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: 'var(--body)', fontSize: 10 }} axisLine={false} tickLine={false} width={40} tickFormatter={(v: number) => `${v}ms`} />
              <Tooltip content={<LatencyTooltip />} />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Bar dataKey="hit" fill="#10b981" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgHitLatency')} />
              <Bar dataKey="miss" fill="var(--error)" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgMissLatency')} />
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('noData')}</p>
        )}
      </CardV2>
    </div>
  )
}
