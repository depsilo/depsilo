import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { adminApi } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import Metric from '@/components/Metric'
import SectionHeader from '@/components/SectionHeader'
import TrendsCard, { type TrendsRange } from '@/admin/components/TrendsCard'
import EmptyState from '@/components/EmptyState'
import { UpstreamGroupedPanel } from '@/components/UpstreamCard'
import {
  AreaChart, Area, XAxis, YAxis, ResponsiveContainer,
} from 'recharts'

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
    return (
      <EmptyState
        icon="inventory_2"
        title={t('dashboard.emptyTopPackagesTitle')}
        hint={t('dashboard.emptyTopPackagesHint')}
      />
    )
  }

  const max = merged[0].hit_count || 1

  return (
    <div>
      {merged.map((p, i) => (
        <div
          key={`${p.ecosystem}-${p.name}`}
          className="flex items-center gap-3 py-1.5"
          style={{ borderBottom: i < merged.length - 1 ? '1px solid var(--border-soft, var(--border))' : 'none' }}
        >
          <span
            className="text-[11px] font-mono tabular-nums w-4 shrink-0 text-right"
            style={{ color: 'var(--text-subtle)' }}
          >
            {i + 1}
          </span>
          <EcosystemIcon type={p.ecosystem as any} size={12} />
          <span className="font-mono text-[12px] truncate flex-1" style={{ color: 'var(--text)' }}>
            {p.name}
          </span>
          <span className="font-mono text-[11px] tabular-nums shrink-0" style={{ color: 'var(--text-soft)' }}>
            {p.hit_count.toLocaleString()}
          </span>
          <div className="w-16 h-[3px] rounded-full shrink-0" style={{ background: 'var(--bg-soft)' }}>
            <div
              className="h-full rounded-full"
              style={{ width: `${(p.hit_count / max) * 100}%`, background: 'var(--brand)' }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Main Dashboard ─────────────────────────────────────────────────

export default function DashboardV2() {
  const { t } = useTranslation()
  const [range, setRange] = useState<TrendsRange>('1h')

  const { data, isLoading, isError, refetch } = useQuery({
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
      <div className="space-y-12">
        <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-4 py-2">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="flex flex-col items-center gap-3">
              <div className="h-3 w-20 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
              <div className="h-11 w-32 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
            </div>
          ))}
        </div>
        <div className="h-72 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
      </div>
    )
  }

  if (isError) {
    return (
      <EmptyState
        icon="wifi_off"
        title={t('common.loadFailed', 'Could not load dashboard')}
        hint={t('dashboard.loadFailedHint', 'Check whether the Depsilo server is reachable, then retry.')}
        tone="danger"
        minHeight={360}
        action={(
          <button
            type="button"
            onClick={() => refetch()}
            className="inline-flex min-h-10 items-center justify-center rounded-[6px] px-3 text-[12px] font-[600] transition-[background,color,transform] duration-150 active:scale-[0.96]"
            style={{ background: 'var(--brand)', color: 'white' }}
          >
            {t('common.retry', 'Retry')}
          </button>
        )}
      />
    )
  }

  const last24h = dashboard?.last_24h || {} as any
  const prev24h = dashboard?.prev_24h || {} as any
  const upstreams = dashboard?.upstreams || []
  const topPackages = dashboard?.top_packages || { pypi: [], apt: [] }
  const rawTrendPoints = (trendsData?.data?.points || []) as any[]

  const metrics = [
    {
      label: t('dashboard.last24hRequests'),
      value: last24h.total_requests?.toLocaleString() || '0',
      change: prev24h.total_requests
        ? ((last24h.total_requests - prev24h.total_requests) / prev24h.total_requests * 100)
        : null,
    },
    {
      label: t('dashboard.hitRate'),
      value: last24h.hit_rate != null ? `${(last24h.hit_rate * 100).toFixed(1)}%` : '0%',
      change: prev24h.hit_rate
        ? ((last24h.hit_rate - prev24h.hit_rate) / prev24h.hit_rate * 100)
        : null,
    },
    {
      label: t('dashboard.bytesServed'),
      value: formatBytes(last24h.bytes_served || 0),
      change: prev24h.bytes_served
        ? ((last24h.bytes_served - prev24h.bytes_served) / prev24h.bytes_served * 100)
        : null,
    },
    {
      label: t('dashboard.avgLatency'),
      value: `${Math.round(last24h.avg_latency_ms || 0)} ms`,
      change: prev24h.avg_latency_ms
        ? ((last24h.avg_latency_ms - prev24h.avg_latency_ms) / prev24h.avg_latency_ms * 100)
        : null,
    },
  ]

  return (
    <div className="space-y-12">
      {/* ── 24h metrics row ─────────────────────────── */}
      <section>
        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 xl:grid-cols-4 xl:gap-8 py-2">
          {metrics.map((m) => (
            <Metric key={m.label} label={m.label} value={m.value} change={m.change} />
          ))}
        </div>
      </section>

      {/* ── Storage alert (kept colored for emphasis) ── */}
      {dashboard?.cache_usage_percent > 80 && (
        <div
          className="flex items-center gap-2 rounded-[5px] px-4 py-2.5 text-[13px]"
          style={{
            background: dashboard.cache_usage_percent > 95 ? 'var(--danger-fill)' : 'var(--warn-fill)',
            color: dashboard.cache_usage_percent > 95 ? 'var(--danger-text)' : 'var(--warn-text)',
            border: `0.5px solid ${dashboard.cache_usage_percent > 95 ? 'var(--danger-border)' : 'var(--warn-border)'}`,
          }}
        >
          <Icon name="warning" size="sm" />
          {t('dashboard.storageWarning', { percent: dashboard.cache_usage_percent?.toFixed(1) })}
        </div>
      )}

      {/* ── Trends — 4 tabs × 4 ranges, browser-tz X axis ───── */}
      <TrendsCard raw={rawTrendPoints} range={range} onRangeChange={setRange} />


      {/* ── Top packages — bare list ─────────────────── */}
      <section>
        <SectionHeader title={t('dashboard.topPackages')} />
        <TopPackagesList topPackages={topPackages} />
      </section>

      {/* ── Bandwidth savings (no card) ──────────────── */}
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
          <section>
            <SectionHeader
              title={t('bandwidth.bandwidthSummary')}
              action={
                <Link
                  to="/admin/bandwidth"
                  className="text-[11px] font-[500] no-underline inline-flex items-center gap-1 transition-colors duration-150"
                  style={{ color: 'var(--brand-text)' }}
                >
                  {t('bandwidth.viewFullReport')}
                  <span aria-hidden>→</span>
                </Link>
              }
            />
            <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-x-10 gap-y-5 mb-6">
              <Metric label={t('bandwidth.totalTraffic')} value={formatBytes(bw.total_bytes || 0)} />
              <Metric
                label={t('bandwidth.trafficSaved')}
                value={formatBytes(bw.hit_bytes || 0)}
                valueTone="ok"
              />
              <Metric
                label={t('bandwidth.savingsRate')}
                value={bw.savings_rate != null ? `${(bw.savings_rate * 100).toFixed(1)}%` : '0%'}
                valueTone={bw.savings_rate > 0.5 ? 'ok' : 'default'}
              />
              <Metric
                label={t('bandwidth.timeSaved')}
                value={fmtTime(bw.time_saved_ms || 0)}
                valueTone="ok"
              />
            </div>
            {bwDaily.length > 0 && (
              <ResponsiveContainer width="100%" height={84}>
                <AreaChart data={bwDaily} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
                  <defs>
                    <linearGradient id="gradBwHit" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="var(--ok)" stopOpacity={0.3} />
                      <stop offset="100%" stopColor="var(--ok)" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="date" tick={{ fill: 'var(--text-soft)', fontSize: 9 }} axisLine={false} tickLine={false} />
                  <YAxis hide />
                  <Area type="monotone" dataKey="hit_bytes" stroke="var(--ok)" strokeWidth={1.5} fill="url(#gradBwHit)" />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </section>
        )
      })()}

      {/* ── Upstream status (component still uses internal cards) ── */}
      <section>
        <SectionHeader title={t('dashboard.upstreamStatus')} />
        <UpstreamGroupedPanel upstreams={upstreams} />
      </section>
    </div>
  )
}
