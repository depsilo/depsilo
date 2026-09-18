import { Link2 } from 'lucide-react'
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import AdminPage from '@/admin/components/AdminPage'
import DashboardAttention from '@/admin/components/DashboardAttention'
import NowStrip from '@/admin/components/NowStrip'
import RecentDownloads from '@/admin/components/RecentDownloads'
import TrendsCard, { type RawTrendPoint, type TrendsRange } from '@/admin/components/TrendsCard'
import Metric, { type MetricChangeIntent } from '@/components/app/metric'
import QueryErrorState from '@/components/app/error-state'
import { LinkButton } from '@/components/app/button'
import { cn, formatBytes } from '@/lib/utils'
import SectionHeader from '@/components/app/section-header'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
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

/**
 * One operational canvas: the live request path and trend stay together as
 * the primary instrument, while attention and activity form a compact rail.
 * Keeping this order in the DOM also makes the first mobile viewport answer
 * the most important question before it asks the operator to investigate.
 */
const DASHBOARD_LAYOUT =
  `grid min-w-0 items-start gap-5 ${RAIL_WIDTH} ` +
  'xl:grid-cols-[minmax(0,1fr)_minmax(var(--rail-w),0.38fr)] ' +
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

export default function DashboardV2() {
  const { t } = useTranslation()
  const [range, setRange] = useState<TrendsRange>('1h')
  const [retainedTrendData, setRetainedTrendData] = useState<TrendQueryData>()

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
  const requestCount = last24h?.total_requests
  const hitRate = last24h && last24h.total_requests > 0 ? last24h.hit_rate : null

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
    {
      label: t('dashboard.bytesServed'),
      value: last24h ? formatBytes(last24h.bytes_served) : '—',
      change: null,
      changeIntent: 'neutral',
    },
    {
      label: t('dashboard.avgLatency'),
      value: last24h ? `${Math.round(last24h.avg_latency_ms)} ${t('dashboard.msUnit')}` : '—',
      change: null,
      changeIntent: 'neutral',
    },
  ]

  return (
    <AdminPage
      actions={(
        <LinkButton to={getAdminRouteHref('connect')}>
          <Link2 className="icon icon-sm" aria-hidden="true" />
          {t('dashboard.connectClient')}
        </LinkButton>
      )}
    >
      <div className="w-full space-y-6">
        <section
          data-query-key="dashboard-snapshot"
          data-dashboard-health
          aria-busy={dashboardQuery.isPending || undefined}
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
          {dashboardQuery.isPending && !dashboard ? (
            <DashboardKpiSkeleton />
          ) : (
            <div data-dashboard-kpis className="grid grid-cols-2 lg:grid-cols-4">
              {metrics.map(metric => (
                <Metric
                  key={metric.label}
                  className={KPI_CELL}
                  label={metric.label}
                  value={metric.value}
                  change={metric.change}
                  changeIntent={metric.changeIntent}
                  size="sm"
                  align="start"
                />
              ))}
            </div>
          )}
        </section>

        <div className={DASHBOARD_LAYOUT}>
          <div className="grid min-w-0 gap-5">
            <NowStrip
              cacheHitRate={last24h?.hit_rate}
              cacheDataPending={dashboardQuery.isPending}
            />
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
                  <div aria-hidden="true" className="h-56 animate-pulse rounded-sm bg-muted" />
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
          </div>
          <aside className="grid min-w-0 gap-5">
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
            <RecentDownloads limit={3} variant="rail" />
          </aside>
        </div>
      </div>
    </AdminPage>
  )
}
