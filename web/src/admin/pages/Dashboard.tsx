import { useState, useMemo, type ReactNode } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { adminApi } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import Metric, { type MetricChangeIntent } from '@/components/Metric'
import SectionHeader from '@/components/SectionHeader'
import TrendsCard, { type RawTrendPoint, type TrendsRange } from '@/admin/components/TrendsCard'
import NowStrip from '@/admin/components/NowStrip'
import RecentDownloads from '@/admin/components/RecentDownloads'
import EmptyState from '@/components/EmptyState'
import ButtonV2 from '@/components/Button'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import { UpstreamGroupedPanel } from '@/components/UpstreamCard'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { DashboardResponse } from '@/lib/adminApi.types'
import { getAdminRouteHref } from '@/admin/routes'
import { upstreamStatus } from '@/lib/upstreamStatus'
import {
  AreaChart, Area, XAxis, YAxis, ResponsiveContainer,
} from 'recharts'

// ── Top packages (merged, sorted by hits) ──────────────────────────

function TopPackagesList({ topPackages }: { topPackages: DashboardResponse['top_packages'] }) {
  const { t } = useTranslation()

  const merged = useMemo(() => {
    const all: Array<{ name: string; hit_count: number; ecosystem: string }> = []
    for (const [ecosystem, packages] of Object.entries(topPackages)) {
      for (const item of packages ?? []) all.push({ ...item, ecosystem })
    }
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
    <ol aria-label={t('dashboard.topPackages')}>
      {merged.map((p, i) => (
        <li
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
          {isAdminEcosystem(p.ecosystem) && <EcosystemIcon type={p.ecosystem} size={12} decorative />}
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
        </li>
      ))}
    </ol>
  )
}

function DashboardKpiSkeleton() {
  return (
    <div aria-hidden="true" className="grid grid-cols-2 gap-x-4 gap-y-8 py-2 xl:grid-cols-4 xl:gap-8">
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} className="flex flex-col items-center gap-3">
          <div className="h-3 w-20 animate-pulse rounded bg-[var(--bg-soft)]" />
          <div className="h-10 w-28 animate-pulse rounded bg-[var(--bg-soft)]" />
          <div className="h-3 w-16 animate-pulse rounded bg-[var(--bg-soft)]" />
        </div>
      ))}
    </div>
  )
}

function DashboardActionLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="stripe-focus-ring inline-flex min-h-[40px] shrink-0 items-center gap-1 rounded-[5px] px-2 text-[12px] font-[600] no-underline text-[var(--brand-text)] transition-colors duration-150 hover:bg-[var(--bg-hover)]"
    >
      {children}
      <span aria-hidden>→</span>
    </Link>
  )
}

// ── Main Dashboard ─────────────────────────────────────────────────

const TREND_REFRESH_INTERVAL: Record<TrendsRange, number> = {
  '1h': 5_000,
  '24h': 15_000,
  '7d': 30_000,
  '30d': 60_000,
}

interface TrendQueryData {
  response: Awaited<ReturnType<typeof adminApi.getDashboardTrends>>
  range: TrendsRange
}

export default function DashboardV2() {
  const { t } = useTranslation()
  const [range, setRange] = useState<TrendsRange>('1h')
  const [retainedTrendData, setRetainedTrendData] = useState<TrendQueryData>()

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'dashboard'],
    queryFn: () => adminApi.getDashboard(),
    refetchInterval: 30000,
    retry: false,
  })

  const trendsQuery = useQuery({
    queryKey: ['admin', 'dashboard', 'trends', range],
    queryFn: async (): Promise<TrendQueryData> => ({
      response: await adminApi.getDashboardTrends(range),
      range,
    }),
    placeholderData: keepPreviousData,
    refetchInterval: TREND_REFRESH_INTERVAL[range],
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  const bandwidthQuery = useQuery({
    queryKey: ['admin', 'bandwidth', '7d'],
    queryFn: () => adminApi.getBandwidthReport({ range: '7d' }),
    refetchInterval: 60000,
    retry: false,
  })

  const dashboard = data?.data

  const last24h = dashboard?.last_24h ?? { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 }
  const prev24h = dashboard?.prev_24h ?? { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 }
  const upstreams = dashboard?.upstreams || []
  const topPackages = dashboard?.top_packages || { pypi: [], apt: [] }
  const activeTrendData = trendsQuery.data ?? retainedTrendData
  const rawTrendPoints: RawTrendPoint[] = activeTrendData?.response.data.points ?? []
  const dataRange = activeTrendData?.range ?? range
  const hasTrendData = activeTrendData !== undefined
  const bandwidthData = bandwidthQuery.data
  const bandwidthSummary = bandwidthData?.data?.summary
  const bandwidthDaily = bandwidthData?.data?.daily || []
  const cacheUsagePercent = dashboard?.cache_usage_percent
  const dashboardInitialError = isError && !data
  const dashboardError = dashboardInitialError ? getApiError(error) : undefined
  const upstreamsNeedingAttention = upstreams.filter(item => upstreamStatus(item) !== 'healthy')
  const upstreamAttentionNames = upstreamsNeedingAttention
    .slice(0, 3)
    .map(item => item.name)
    .join(t('dashboard.listSeparator'))

  function formatTimeSaved(ms: number) {
    if (ms <= 0) return '0s'
    const seconds = Math.floor(ms / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    if (hours > 0) return `${hours}${t('bandwidth.hours')} ${minutes % 60}${t('bandwidth.minutes')}`
    if (minutes > 0) return `${minutes}${t('bandwidth.minutes')} ${seconds % 60}${t('bandwidth.seconds')}`
    return `${seconds}${t('bandwidth.seconds')}`
  }

  function handleTrendRangeChange(nextRange: TrendsRange) {
    if (trendsQuery.data) setRetainedTrendData(trendsQuery.data)
    setRange(nextRange)
  }

  const metrics: Array<{
    label: string
    value: string
    change: number | null
    changeIntent: MetricChangeIntent
  }> = [
    {
      label: t('dashboard.last24hRequests'),
      value: last24h.total_requests?.toLocaleString() || '0',
      change: prev24h.total_requests
        ? ((last24h.total_requests - prev24h.total_requests) / prev24h.total_requests * 100)
        : null,
      changeIntent: 'neutral',
    },
    {
      label: t('dashboard.hitRate'),
      value: last24h.hit_rate != null ? `${(last24h.hit_rate * 100).toFixed(1)}%` : '0%',
      change: prev24h.hit_rate
        ? ((last24h.hit_rate - prev24h.hit_rate) / prev24h.hit_rate * 100)
        : null,
      changeIntent: 'higher-is-better',
    },
    {
      label: t('dashboard.bytesServed'),
      value: formatBytes(last24h.bytes_served || 0),
      change: prev24h.bytes_served
        ? ((last24h.bytes_served - prev24h.bytes_served) / prev24h.bytes_served * 100)
        : null,
      changeIntent: 'neutral',
    },
    {
      label: t('dashboard.avgLatency'),
      value: `${Math.round(last24h.avg_latency_ms || 0)} ms`,
      change: prev24h.avg_latency_ms
        ? ((last24h.avg_latency_ms - prev24h.avg_latency_ms) / prev24h.avg_latency_ms * 100)
        : null,
      changeIntent: 'lower-is-better',
    },
  ]

  return (
    <div className="mx-auto max-w-[1440px] space-y-8 lg:space-y-12">
      <div className="space-y-3">
        <NowStrip />
        <RecentDownloads limit={3} />
      </div>

      <div
        data-query-key="dashboard-snapshot"
        aria-busy={isPending || undefined}
        className="space-y-4"
      >
        {dashboardInitialError && (
          <QueryErrorState
            message={dashboardError?.status === 403 ? t('common.permissionDenied') : dashboardError?.message ?? t('common.loadFailed')}
            onRetry={() => { void refetch() }}
          />
        )}

        {data && isRefetchError && (
          <InlineNotice tone="warning">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span>{t('now.staleData')}</span>
              <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void refetch() }}>
                {t('now.refresh')}
              </ButtonV2>
            </div>
          </InlineNotice>
        )}

        {dashboard && (
          <div className="space-y-2">
            {upstreamsNeedingAttention.length > 0 && (
              <InlineNotice tone={upstreamsNeedingAttention.some(item => upstreamStatus(item) === 'failed') ? 'danger' : 'warning'}>
                <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
                  <Icon name="warning" size="sm" aria-hidden />
                  <span className="min-w-0 flex-1">
                    {t('dashboard.upstreamWarning', {
                      count: upstreamsNeedingAttention.length,
                      names: upstreamAttentionNames,
                    })}
                  </span>
                  <DashboardActionLink to={getAdminRouteHref('upstreams')}>
                    {t('dashboard.viewUpstreams')}
                  </DashboardActionLink>
                </div>
              </InlineNotice>
            )}

            {cacheUsagePercent !== undefined && cacheUsagePercent > 80 && (
              <InlineNotice tone={cacheUsagePercent > 95 ? 'danger' : 'warning'}>
                <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
                  <Icon name="warning" size="sm" aria-hidden />
                  <span className="min-w-0 flex-1">
                    {t('dashboard.storageWarning', { percent: cacheUsagePercent.toFixed(1) })}
                  </span>
                  <DashboardActionLink to={getAdminRouteHref('cache')}>
                    {t('dashboard.manageCache')}
                  </DashboardActionLink>
                </div>
              </InlineNotice>
            )}
          </div>
        )}

        {!dashboardInitialError && (
          <section aria-label={t('dashboard.performanceSnapshot')}>
            <SectionHeader
              title={t('dashboard.performanceSnapshot')}
              hint={t('dashboard.comparisonHint')}
            />
            {isPending ? (
              <DashboardKpiSkeleton />
            ) : (
              <div data-dashboard-kpis className="grid grid-cols-2 gap-x-4 gap-y-8 py-2 xl:grid-cols-4 xl:gap-8">
                {metrics.map((metric) => (
                  <Metric
                    key={metric.label}
                    label={metric.label}
                    value={metric.value}
                    change={metric.change}
                    changeIntent={metric.changeIntent}
                    size="clamp(28px, 7vw, 40px)"
                  />
                ))}
              </div>
            )}
          </section>
        )}
      </div>

      {/* ── Trends — 4 tabs × 4 ranges, browser-tz X axis ───── */}
      <div data-query-key="dashboard-trends" aria-busy={trendsQuery.isFetching} className="space-y-3">
        {trendsQuery.isPending && !hasTrendData ? (
          <div aria-busy="true"><div aria-hidden="true" className="h-52 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" /></div>
        ) : trendsQuery.isError && !hasTrendData ? (
          <QueryErrorState
            message={getApiError(trendsQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(trendsQuery.error).message}
            onRetry={() => { void trendsQuery.refetch() }}
          />
        ) : (
          <>
            {hasTrendData && trendsQuery.isError && (
              <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void trendsQuery.refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>
            )}
            <TrendsCard raw={rawTrendPoints} range={range} dataRange={dataRange} onRangeChange={handleTrendRangeChange} />
          </>
        )}
      </div>

      {!dashboardInitialError && (
        <section aria-label={t('dashboard.upstreamStatus')}>
          <SectionHeader
            title={t('dashboard.upstreamStatus')}
            action={
              <DashboardActionLink to={getAdminRouteHref('upstreams')}>
                {t('dashboard.viewAll')}
              </DashboardActionLink>
            }
          />
          {isPending ? (
            <div aria-hidden="true" className="h-32 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
          ) : (
            <UpstreamGroupedPanel upstreams={upstreams} />
          )}
        </section>
      )}

      <div
        className={`grid min-w-0 gap-8 ${
          dashboardInitialError
            ? ''
            : 'xl:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)] xl:gap-10'
        }`}
      >
        {/* ── Top packages — bare list ───────────────── */}
        {!dashboardInitialError && (
          <section className="min-w-0">
            <SectionHeader title={t('dashboard.topPackages')} />
            {isPending ? (
              <div aria-hidden="true" className="h-56 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
            ) : (
              <TopPackagesList topPackages={topPackages} />
            )}
          </section>
        )}

        {/* ── Bandwidth savings (no card) ────────────── */}
        <section className="min-w-0">
          <SectionHeader
            title={t('bandwidth.bandwidthSummary')}
            action={
              <DashboardActionLink to={getAdminRouteHref('bandwidth')}>
                {t('bandwidth.viewFullReport')}
              </DashboardActionLink>
            }
          />
          {bandwidthQuery.isPending ? (
            <div aria-busy="true"><div aria-hidden="true" className="h-32 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" /></div>
          ) : bandwidthQuery.isError && !bandwidthData ? (
            <QueryErrorState
              message={getApiError(bandwidthQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(bandwidthQuery.error).message}
              onRetry={() => { void bandwidthQuery.refetch() }}
            />
          ) : (
            <div className="space-y-3">
              {bandwidthData && bandwidthQuery.isRefetchError && (
                <InlineNotice tone="warning">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{t('now.staleData')}</span>
                    <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void bandwidthQuery.refetch() }}>
                      {t('now.refresh')}
                    </ButtonV2>
                  </div>
                </InlineNotice>
              )}
              {bandwidthSummary ? (
                <>
                  <div className="mb-6 grid grid-cols-2 gap-x-4 gap-y-8 sm:gap-x-10 xl:grid-cols-4 xl:gap-y-5">
                    <Metric
                      label={t('bandwidth.totalTraffic')}
                      value={formatBytes(bandwidthSummary.total_bytes || 0)}
                      size={28}
                    />
                    <Metric
                      label={t('bandwidth.trafficSaved')}
                      value={formatBytes(bandwidthSummary.hit_bytes || 0)}
                      valueTone="ok"
                      size={28}
                    />
                    <Metric
                      label={t('bandwidth.savingsRate')}
                      value={bandwidthSummary.savings_rate != null ? `${(bandwidthSummary.savings_rate * 100).toFixed(1)}%` : '0%'}
                      valueTone={bandwidthSummary.savings_rate > 0.5 ? 'ok' : 'default'}
                      size={28}
                    />
                    <Metric
                      label={t('bandwidth.timeSaved')}
                      value={formatTimeSaved(bandwidthSummary.time_saved_ms || 0)}
                      valueTone="ok"
                      size={28}
                    />
                  </div>
                  {bandwidthDaily.length > 0 && (
                    <ResponsiveContainer width="100%" height={84}>
                      <AreaChart
                        data={bandwidthDaily}
                        margin={{ top: 4, right: 4, bottom: 0, left: 0 }}
                        desc={t('bandwidth.chartDescription')}
                      >
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
                </>
              ) : (
                <EmptyState icon="bar_chart" title={t('bandwidth.emptyTitle')} hint={t('bandwidth.emptyHint')} minHeight={160} />
              )}
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
