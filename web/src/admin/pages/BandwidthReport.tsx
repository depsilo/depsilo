import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import Metric from '@/components/Metric'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import ButtonV2 from '@/components/Button'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import EcosystemIcon from '@/components/EcosystemIcon'
import { getApiError } from '@/lib/apiError'
import { getEcosystemColor } from '@/lib/ecosystemColors'
import {
  AreaChart, Area, BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'
import type { TooltipContentProps, TooltipValueType } from 'recharts'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { BandwidthSummary } from '@/lib/adminApi.types'

const EMPTY_SUMMARY: BandwidthSummary = {
  total_bytes: 0,
  hit_bytes: 0,
  miss_bytes: 0,
  savings_rate: 0,
  total_requests: 0,
  hit_requests: 0,
  miss_requests: 0,
  time_saved_ms: 0,
  avg_hit_latency: 0,
  avg_miss_latency: 0,
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

function ChartTooltip({ active, payload, label }: TooltipContentProps<TooltipValueType, string | number>) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--text)' }}>{label}</p>
      {payload.map((entry) => (
        <p key={String(entry.dataKey)} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {formatBytes(Number(entry.value))}
        </p>
      ))}
    </div>
  )
}

function LatencyTooltip({ active, payload, label }: TooltipContentProps<TooltipValueType, string | number>) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[4px] px-3 py-2 text-[12px]" style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}>
      <p className="font-[400] mb-1" style={{ color: 'var(--text)' }}>{label}</p>
      {payload.map((entry) => (
        <p key={String(entry.dataKey)} className="font-mono tabular-nums" style={{ color: entry.color }}>
          {entry.name}: {Math.round(Number(entry.value))} ms
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
  const queryEnabled = range !== 'custom' || (!!customStart && !!customEnd)

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'bandwidth', params],
    queryFn: () => adminApi.getBandwidthReport(params),
    enabled: queryEnabled,
    refetchInterval: 60000,
    retry: false,
  })

  const report = data?.data
  const summary = report?.summary ?? EMPTY_SUMMARY
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

  const ecoDonutData = byEcosystem
    .map((e) => ({ name: e.ecosystem, value: e.hit_bytes + e.miss_bytes }))
    .filter((e) => e.value > 0)
    .sort((a, b) => b.value - a.value)

  const latencyData = byEcosystem
    .filter((e) => e.avg_miss_latency_ms > 0)
    .map((e) => ({
      ecosystem: e.ecosystem,
      hit: Math.round(e.avg_hit_latency_ms),
      miss: Math.round(e.avg_miss_latency_ms),
    }))
    .sort((a, b) => b.miss - a.miss)

  if (queryEnabled && isPending) {
    return (
      <div aria-busy="true" className="space-y-12">
        <div aria-hidden="true">
        <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-4 lg:gap-8">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="flex flex-col items-center gap-3">
              <div className="h-3 w-20 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
              <div className="h-11 w-32 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
            </div>
          ))}
        </div>
        <div className="h-80 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
        </div>
      </div>
    )
  }

  if (queryEnabled && isError && !data) {
    const normalized = getApiError(error)
    return <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void refetch() }} />
  }

  return (
    <div className="space-y-12">
      {data && isRefetchError && (
        <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>
      )}
      {/* ── Range chips ──────────────────────────────── */}
      <div className="flex items-center gap-2 flex-wrap">
        {ranges.map(r => {
          const active = range === r.value
          return (
            <button
              key={r.value}
              onClick={() => setRange(r.value)}
              className="whitespace-nowrap rounded-[4px] px-3 py-1 text-[12px] font-[500] transition-colors duration-150 cursor-pointer"
              style={{
                background: active ? 'var(--btn-primary-bg)' : 'transparent',
                color: active ? 'white' : 'var(--text-soft)',
                border: active ? 'none' : '1px solid var(--border)',
              }}
            >
              {r.label}
            </button>
          )
        })}
        {range === 'custom' && (
          <div className="flex items-center gap-2 ml-2">
            <input
              type="date"
              value={customStart}
              onChange={e => setCustomStart(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)' }}
            />
            <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>-</span>
            <input
              type="date"
              value={customEnd}
              onChange={e => setCustomEnd(e.target.value)}
              className="px-2 py-1 text-[12px] rounded-[4px] font-mono"
              style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)' }}
            />
          </div>
        )}
      </div>

      {/* ── Summary metrics ──────────────────────────── */}
      <div className="grid grid-cols-2 gap-6 py-2 lg:grid-cols-4 lg:gap-8">
        <Metric label={t('bandwidth.totalTraffic')} value={formatBytes(summary.total_bytes || 0)} />
        <Metric label={t('bandwidth.trafficSaved')} value={formatBytes(summary.hit_bytes || 0)} valueTone="ok" />
        <Metric
          label={t('bandwidth.savingsRate')}
          value={summary.savings_rate != null ? `${(summary.savings_rate * 100).toFixed(1)}%` : '0%'}
          valueTone={summary.savings_rate > 0.5 ? 'ok' : 'default'}
        />
        <Metric
          label={t('bandwidth.timeSaved')}
          value={formatTimeSaved(summary.time_saved_ms || 0, t)}
          valueTone="ok"
        />
      </div>

      {/* ── Daily trend (full width) ─────────────────── */}
      <section>
        <SectionHeader title={t('bandwidth.dailyTrend')} />
        {daily.length > 0 ? (
          <ResponsiveContainer width="100%" height={240}>
            <AreaChart data={daily} margin={{ top: 4, right: 12, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="gradHitBytes" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--ok)" stopOpacity={0.3} />
                  <stop offset="100%" stopColor="var(--ok)" stopOpacity={0.02} />
                </linearGradient>
                <linearGradient id="gradMissBytes" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--danger)" stopOpacity={0.2} />
                  <stop offset="100%" stopColor="var(--danger)" stopOpacity={0.02} />
                </linearGradient>
              </defs>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
              <XAxis dataKey="date" tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} width={50} tickFormatter={(v: number) => formatBytes(v)} />
              <Tooltip content={ChartTooltip} />
              <Legend wrapperStyle={{ fontSize: 11, paddingTop: 4 }} />
              <Area type="monotone" dataKey="hit_bytes" stackId="1" stroke="var(--ok)" strokeWidth={1.5} fill="url(#gradHitBytes)" name={t('bandwidth.hitBytes')} />
              <Area type="monotone" dataKey="miss_bytes" stackId="1" stroke="var(--danger)" strokeWidth={1.5} fill="url(#gradMissBytes)" name={t('bandwidth.missBytes')} />
            </AreaChart>
          </ResponsiveContainer>
        ) : (
          <EmptyState
            icon="show_chart"
            title={t('bandwidth.emptyTitle')}
            hint={t('bandwidth.emptyHint')}
            minHeight={240}
          />
        )}
      </section>

      {/* ── Three side-by-side panels: ecosystem / top packages / upstream ── */}
      <div className="grid grid-cols-1 gap-y-12 xl:grid-cols-3 xl:gap-x-10">
        {/* Ecosystem donut */}
        <section>
          <SectionHeader title={t('bandwidth.byEcosystem')} />
          {ecoDonutData.length > 0 ? (
            <>
              <ResponsiveContainer width="100%" height={160}>
                <PieChart>
                  <Pie data={ecoDonutData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={40} outerRadius={65} paddingAngle={2}>
                    {ecoDonutData.map((d: { name: string }, i: number) => (
                      <Cell key={d.name} fill={getEcosystemColor(d.name, i)} />
                    ))}
                  </Pie>
                  <Tooltip formatter={(value) => formatBytes(Number(value))} />
                </PieChart>
              </ResponsiveContainer>
              <div className="space-y-1.5 mt-3">
                {ecoDonutData.slice(0, 6).map((e, i) => (
                  <div key={e.name} className="flex items-center gap-2 text-[11px]">
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ background: getEcosystemColor(e.name, i) }} />
                    {isAdminEcosystem(e.name) && <EcosystemIcon type={e.name} size={12} />}
                    <span className="font-mono" style={{ color: 'var(--text)' }}>{e.name}</span>
                    <span className="ml-auto font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatBytes(e.value)}</span>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <EmptyState icon="donut_large" title={t('noData')} minHeight={180} />
          )}
        </section>

        {/* Top packages */}
        <section>
          <SectionHeader title={t('bandwidth.topPackages')} />
          {topPackages.length > 0 ? (
            <div>
              {topPackages.map((p, i) => {
                const max = topPackages[0]?.total_bytes || 1
                return (
                  <div
                    key={`${p.ecosystem}-${p.package_name}`}
                    className="flex items-center gap-3 py-1.5"
                    style={{ borderBottom: i < topPackages.length - 1 ? '1px solid var(--border-soft, var(--border))' : 'none' }}
                  >
                    <span className="text-[11px] font-mono tabular-nums w-4 shrink-0 text-right" style={{ color: 'var(--text-subtle)' }}>{i + 1}</span>
                    {isAdminEcosystem(p.ecosystem) && <EcosystemIcon type={p.ecosystem} size={12} />}
                    <span className="font-mono text-[11px] truncate flex-1" style={{ color: 'var(--text)' }}>{p.package_name}</span>
                    <span className="font-mono text-[10px] tabular-nums shrink-0" style={{ color: 'var(--text-soft)' }}>{formatBytes(p.total_bytes)}</span>
                    <div className="w-14 h-[3px] rounded-full shrink-0" style={{ background: 'var(--bg-soft)' }}>
                      <div className="h-full rounded-full" style={{ width: `${(p.total_bytes / max) * 100}%`, background: 'var(--brand)' }} />
                    </div>
                  </div>
                )
              })}
            </div>
          ) : (
            <EmptyState icon="inventory_2" title={t('noData')} minHeight={180} />
          )}
        </section>

        {/* Upstream bar */}
        <section>
          <SectionHeader title={t('bandwidth.byUpstream')} />
          {byUpstream.length > 0 ? (
            <ResponsiveContainer width="100%" height={Math.max(160, byUpstream.length * 32)}>
              <BarChart data={byUpstream} layout="vertical" margin={{ left: 0, right: 10 }}>
                <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" horizontal={false} />
                <XAxis type="number" tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} tickFormatter={(v: number) => formatBytes(v)} />
                <YAxis type="category" dataKey="upstream" tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} width={80} />
                <Tooltip formatter={(value) => formatBytes(Number(value))} />
                <Bar dataKey="miss_bytes" fill="var(--brand)" radius={[0, 3, 3, 0]} barSize={16} name={t('bandwidth.totalBandwidth')} />
              </BarChart>
            </ResponsiveContainer>
          ) : (
            <EmptyState icon="dns" title={t('noData')} minHeight={180} />
          )}
        </section>
      </div>

      {/* ── Latency comparison ──────────────────────── */}
      <section>
        <SectionHeader
          title={t('bandwidth.latencyComparison')}
          action={
            <span className="text-[11px] font-mono tabular-nums" style={{ color: 'var(--ok-text)' }}>
              {t('bandwidth.timeSaved')}: {formatTimeSaved(summary.time_saved_ms || 0, t)}
            </span>
          }
        />
        {latencyData.length > 0 ? (
          <ResponsiveContainer width="100%" height={200}>
            <BarChart data={latencyData} margin={{ top: 4, right: 12, bottom: 0, left: 0 }}>
              <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
              <XAxis dataKey="ecosystem" tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: 'var(--text-soft)', fontSize: 10 }} axisLine={false} tickLine={false} width={40} tickFormatter={(v: number) => `${v}ms`} />
              <Tooltip content={LatencyTooltip} />
              <Legend wrapperStyle={{ fontSize: 11, paddingTop: 4 }} />
              <Bar dataKey="hit" fill="var(--ok)" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgHitLatency')} />
              <Bar dataKey="miss" fill="var(--danger)" radius={[3, 3, 0, 0]} barSize={20} name={t('bandwidth.avgMissLatency')} />
            </BarChart>
          </ResponsiveContainer>
        ) : (
          <EmptyState icon="speed" title={t('noData')} minHeight={200} />
        )}
      </section>
    </div>
  )
}
