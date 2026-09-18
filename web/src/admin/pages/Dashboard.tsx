import { Link2 } from 'lucide-react'
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import DashboardAttention from '@/admin/components/DashboardAttention'
import NowStrip from '@/admin/components/NowStrip'
import RecentDownloads from '@/admin/components/RecentDownloads'
import TrendsCard, { type RawTrendPoint, type TrendsRange } from '@/admin/components/TrendsCard'
import Metric, { type MetricChangeIntent } from '@/components/app/metric'
import QueryErrorState from '@/components/app/error-state'
import { cn } from '@/lib/utils'
import SectionHeader from '@/components/app/section-header'
import { adminApi, statsApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import type { NowResponse } from '@/lib/adminApi.types'
import { getAdminRouteHref } from '@/admin/routes'
import { upstreamStatus } from '@/lib/upstreamStatus'

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

/**
 * One internally divided data rail rather than four loose fragments: a 2x2
 * cross below `lg`, a single row of divided cells from `lg` up. The mobile
 * rules are `max-lg:` so they cannot fight the desktop ones.
 */
const KPI_CELL =
  'px-3.5 pt-4 pb-[18px] max-lg:even:border-l max-lg:even:border-border ' +
  'max-lg:nth-[n+3]:border-t max-lg:nth-[n+3]:border-border ' +
  'lg:px-6 lg:pt-[18px] lg:pb-5 lg:nth-[n+2]:border-l lg:nth-[n+2]:border-border'

/**
 * The supporting rail's width, decided once: a 320px minimum from `xl`, a
 * fixed 380px from `2xl`. Spelled out per row, the same value appeared in four
 * different grid templates.
 */
const RAIL_WIDTH = '[--rail-w:320px] 2xl:[--rail-w:380px]'

/** Rail first, main instrument second. Both stretch, so the request path fills the row. */
const SPLIT_RAIL_FIRST =
  `grid min-w-0 gap-5 ${RAIL_WIDTH} ` +
  'xl:grid-cols-[minmax(var(--rail-w),1fr)_minmax(0,2fr)] ' +
  '2xl:grid-cols-[var(--rail-w)_minmax(0,1fr)]'

/**
 * Main column first, supporting rail second. `items-start` deliberately stops
 * the rail stretching to the main column's height, which is what the previous
 * row wanted and this one does not.
 */
const SPLIT_RAIL_LAST =
  `grid min-w-0 items-start gap-5 ${RAIL_WIDTH} ` +
  'xl:grid-cols-[minmax(0,2fr)_minmax(var(--rail-w),1fr)] ' +
  '2xl:grid-cols-[minmax(0,1fr)_var(--rail-w)]'

function DashboardKpiSkeleton() {
  return (
    <div aria-hidden="true" className="grid grid-cols-2 lg:grid-cols-4">
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} className={cn('flex flex-col items-start gap-2', KPI_CELL)}>
          <div className="h-3 w-20 animate-pulse rounded bg-muted" />
          <div className="h-8 w-28 animate-pulse rounded bg-muted" />
          <div className="h-3 w-16 animate-pulse rounded bg-muted" />
        </div>
      ))}
    </div>
  )
}

function StatusMetric({
  label,
  value,
  detail,
  tone = 'default',
  className,
}: {
  label: string
  value: string
  detail: string
  tone?: 'default' | 'ok' | 'warning' | 'danger'
  className?: string
}) {
  const color = tone === 'ok'
    ? 'var(--success)'
    : tone === 'warning'
      ? 'var(--warning)'
      : tone === 'danger'
        ? 'var(--destructive)'
        : 'var(--foreground)'

  return (
    <div className={cn('flex min-w-0 flex-col items-start text-left', className)} data-dashboard-status-metric>
      <span className="text-meta font-semibold text-muted-foreground">{label}</span>
      <span
        data-metric-value
        className="mt-2 min-w-0 font-semibold leading-[1.15]"
        style={{ color, fontFamily: 'var(--font-sans)', fontSize: 'clamp(20px, 3vw, 28px)' }}
      >
        {value}
      </span>
      <span className="mt-1.5 text-meta leading-[1.45] text-muted-foreground">{detail}</span>
    </div>
  )
}

export default function DashboardV2() {
  const { t } = useTranslation()
  const [range, setRange] = useState<TrendsRange>('1h')
  const [retainedTrendData, setRetainedTrendData] = useState<TrendQueryData>()

  const nowQuery = useQuery<NowResponse>({
    queryKey: ['admin', 'now'],
    queryFn: async ({ signal }) => (await statsApi.getNow({ signal })).data,
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    staleTime: 4_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  const dashboardQuery = useQuery({
    queryKey: ['admin', 'dashboard'],
    queryFn: ({ signal }) => adminApi.getDashboard({ signal }),
    refetchInterval: 30_000,
    retry: false,
  })

  const trendsQuery = useQuery({
    queryKey: ['admin', 'dashboard', 'trends', range],
    queryFn: async ({ signal }): Promise<TrendQueryData> => ({
      response: await adminApi.getDashboardTrends(range, { signal }),
      range,
    }),
    placeholderData: keepPreviousData,
    refetchInterval: TREND_REFRESH_INTERVAL[range],
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  const dashboard = dashboardQuery.data?.data
  const last24h = dashboard?.last_24h
  const prev24h = dashboard?.prev_24h
  const upstreams = dashboard?.upstreams ?? []
  const upstreamsNeedingAttention = upstreams.filter(item => upstreamStatus(item) !== 'healthy')
  const activeTrendData = trendsQuery.data ?? retainedTrendData
  const rawTrendPoints: RawTrendPoint[] = activeTrendData?.response.data.points ?? []
  const dataRange = activeTrendData?.range ?? range
  const hasTrendData = activeTrendData !== undefined
  const dashboardInitialError = dashboardQuery.isError && !dashboardQuery.data
  const dashboardError = dashboardInitialError ? getApiError(dashboardQuery.error) : undefined
  const nowData = nowQuery.data
  const nowInitialError = nowQuery.isError && !nowData
  const nowStale = nowQuery.isRefetchError && Boolean(nowData)
  const nowStatus = nowInitialError
    ? t('now.statusUnavailable')
    : nowQuery.isPending && !nowData
      ? t('loading')
      : nowStale
        ? t('now.staleData')
        : nowData?.status === 'healthy'
          ? t(nowData.last_activity || nowData.rate.requests_per_min > 0 ? 'now.statusHealthy' : 'now.statusReady')
          : nowData?.status === 'degraded'
            ? t('now.statusDegraded')
            : t('now.statusDown')
  const nowTone = nowInitialError || nowStale
    ? 'warning'
    : nowData?.status === 'healthy'
      ? 'ok'
      : nowData?.status === 'degraded'
        ? 'warning'
        : nowData
          ? 'danger'
          : 'default'
  const requestCount = last24h?.total_requests
  const hitRate = last24h && last24h.total_requests > 0 ? last24h.hit_rate : null
  const upstreamValue = nowData ? `${nowData.upstreams.healthy} / ${nowData.upstreams.total}` : '—'
  const upstreamTone = nowData && nowData.upstreams.total > 0
    ? nowData.upstreams.healthy < nowData.upstreams.total ? 'warning' : 'ok'
    : 'default'

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
      label: t('dashboard.hitRate'),
      value: hitRate === null ? '—' : `${(hitRate * 100).toFixed(1)}%`,
      change: hitRate !== null && prev24h?.hit_rate
        ? ((hitRate - prev24h.hit_rate) / prev24h.hit_rate * 100)
        : null,
      changeIntent: 'higher-is-better',
    },
    {
      label: t('dashboard.last24hRequests'),
      value: requestCount === undefined ? '—' : requestCount.toLocaleString(),
      change: requestCount !== undefined && prev24h?.total_requests
        ? ((requestCount - prev24h.total_requests) / prev24h.total_requests * 100)
        : null,
      changeIntent: 'neutral',
    },
  ]

  return (
    <AdminPage
      actions={(
        <Link
          to={getAdminRouteHref('connect')}
          className="inline-flex min-h-9 items-center justify-center gap-1.5 rounded-[5px] px-3 py-1.5 text-body font-medium no-underline pointer-coarse:min-h-10"
          style={{ color: 'var(--primary-foreground)', background: 'var(--primary)' }}
        >
          <Link2 className="icon icon-sm" aria-hidden="true" />
          {t('dashboard.connectClient')}
        </Link>
      )}
    >
      <div className="w-full space-y-7 lg:space-y-8">
        <section
          data-query-key="dashboard-snapshot"
          data-dashboard-health
          aria-busy={dashboardQuery.isPending || nowQuery.isPending || undefined}
          aria-label={`${t('dashboard.healthOverview')}. ${t('dashboard.snapshotRange')}`}
          className="[&>header]:mb-0"
        >
          <SectionHeader
            title={t('dashboard.healthOverview')}
            divider={false}
            action={(
              <span className="text-meta text-muted-foreground">
                {t('dashboard.snapshotRange')}
              </span>
            )}
          />
          {(dashboardQuery.isPending || nowQuery.isPending) && !dashboard && !nowData ? (
            <DashboardKpiSkeleton />
          ) : (
            <div data-dashboard-kpis className="grid grid-cols-2 lg:grid-cols-4">
              <StatusMetric
                className={KPI_CELL}
                label={t('dashboard.serviceStatus')}
                value={nowStatus}
                tone={nowTone}
                detail={nowInitialError ? t('dashboard.statusUnavailableHint') : nowStale ? t('now.staleData') : t('dashboard.liveRefresh')}
              />
              <Metric
                className={KPI_CELL}
                label={metrics[0].label}
                value={metrics[0].value}
                change={metrics[0].change}
                changeIntent={metrics[0].changeIntent}
                align="start"
                size="clamp(28px, 4vw, 32px)"
              />
              <StatusMetric
                className={KPI_CELL}
                label={t('dashboard.currentHealthyUpstreams')}
                value={upstreamValue}
                tone={upstreamTone}
                detail={nowData
                  ? nowData.upstreams.total > 0 ? t('dashboard.healthyUpstreams') : t('dashboard.noUpstreams')
                  : t('dashboard.statusUnavailableHint')}
              />
              <Metric
                className={KPI_CELL}
                label={metrics[1].label}
                value={metrics[1].value}
                change={metrics[1].change}
                changeIntent={metrics[1].changeIntent}
                align="start"
                size="clamp(28px, 4vw, 32px)"
              />
            </div>
          )}
        </section>

        <div className={SPLIT_RAIL_FIRST}>
          <DashboardAttention
            isPending={dashboardQuery.isPending}
            isFetching={dashboardQuery.isFetching}
            initialErrorMessage={dashboardError?.status === 403
              ? t('common.permissionDenied')
              : dashboardError?.message}
            isStale={Boolean(dashboardQuery.data && dashboardQuery.isRefetchError)}
            upstreams={upstreamsNeedingAttention}
            cacheUsagePercent={dashboard?.cache_usage_percent}
            onRetry={() => { void dashboardQuery.refetch() }}
          />
          <NowStrip
            cacheHitRate={last24h?.hit_rate}
            cacheDataPending={dashboardQuery.isPending}
          />
        </div>

        <div className={SPLIT_RAIL_LAST}>
          <div
            data-query-key="dashboard-trends"
            aria-busy={trendsQuery.isFetching || undefined}
            className="min-w-0"
          >
            {trendsQuery.isPending && !hasTrendData ? (
              <div
                aria-busy="true"
                className="border-b border-border bg-card p-4"
              >
                <div aria-hidden="true" className="h-56 animate-pulse rounded-[6px] bg-muted" />
              </div>
            ) : trendsQuery.isError && !hasTrendData ? (
              <div className="border-b border-border bg-card p-4">
                <QueryErrorState
                  message={getApiError(trendsQuery.error).status === 403
                    ? t('common.permissionDenied')
                    : getApiError(trendsQuery.error).message}
                  onRetry={() => { void trendsQuery.refetch() }}
                />
              </div>
            ) : (
              <TrendsCard
                raw={rawTrendPoints}
                range={range}
                dataRange={dataRange}
                isStale={Boolean(hasTrendData && trendsQuery.isError)}
                onRetry={() => { void trendsQuery.refetch() }}
                onRangeChange={handleTrendRangeChange}
              />
            )}
          </div>

          <RecentDownloads limit={3} variant="rail" />
        </div>
      </div>
    </AdminPage>
  )
}
