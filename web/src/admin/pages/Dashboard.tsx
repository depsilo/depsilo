import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import AdminPage from '@/admin/components/AdminPage'
import DashboardAttention from '@/admin/components/DashboardAttention'
import NowStrip from '@/admin/components/NowStrip'
import RecentDownloads from '@/admin/components/RecentDownloads'
import TrendsCard, { type RawTrendPoint, type TrendsRange } from '@/admin/components/TrendsCard'
import Metric, { type MetricChangeIntent } from '@/components/Metric'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { formatBytes } from '@/lib/utils'
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

function DashboardKpiSkeleton() {
  return (
    <div aria-hidden="true" className="admin-kpi-grid grid grid-cols-2 lg:grid-cols-4">
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} className="flex flex-col items-start gap-2">
          <div className="h-3 w-20 animate-pulse rounded bg-[var(--bg-soft)]" />
          <div className="h-8 w-28 animate-pulse rounded bg-[var(--bg-soft)]" />
          <div className="h-3 w-16 animate-pulse rounded bg-[var(--bg-soft)]" />
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
  const last24h = dashboard?.last_24h ?? {
    total_requests: 0,
    hit_count: 0,
    hit_rate: 0,
    bytes_served: 0,
    avg_latency_ms: 0,
  }
  const prev24h = dashboard?.prev_24h ?? {
    total_requests: 0,
    hit_count: 0,
    hit_rate: 0,
    bytes_served: 0,
    avg_latency_ms: 0,
  }
  const upstreams = dashboard?.upstreams ?? []
  const upstreamsNeedingAttention = upstreams.filter(item => upstreamStatus(item) !== 'healthy')
  const activeTrendData = trendsQuery.data ?? retainedTrendData
  const rawTrendPoints: RawTrendPoint[] = activeTrendData?.response.data.points ?? []
  const dataRange = activeTrendData?.range ?? range
  const hasTrendData = activeTrendData !== undefined
  const dashboardInitialError = dashboardQuery.isError && !dashboardQuery.data
  const dashboardError = dashboardInitialError ? getApiError(dashboardQuery.error) : undefined

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
    <AdminPage>
      <div className="w-full space-y-7 lg:space-y-8">
        <div
          data-query-key="dashboard-snapshot"
          aria-busy={dashboardQuery.isPending || undefined}
          className="space-y-7"
        >
          <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)] 2xl:grid-cols-[minmax(0,1fr)_380px]">
            <NowStrip
              cacheHitRate={dashboard?.last_24h?.hit_rate}
              cacheDataPending={dashboardQuery.isPending}
            />
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
          </div>

          {!dashboardInitialError && (
            <section
              className="admin-kpi-section"
              aria-label={`${t('dashboard.performanceSnapshot')}. ${t('dashboard.comparisonHint')}`}
            >
              <SectionHeader
                title={t('dashboard.performanceSnapshot')}
                divider={false}
                action={(
                  <span aria-hidden="true" className="hidden text-[11px] text-[var(--text-subtle)] sm:inline">
                    {t('dashboard.comparisonShort')}
                  </span>
                )}
              />
              {dashboardQuery.isPending ? (
                <DashboardKpiSkeleton />
              ) : (
                <div
                  data-dashboard-kpis
                  className="admin-kpi-grid grid grid-cols-2 lg:grid-cols-4"
                >
                  {metrics.map(metric => (
                    <Metric
                      key={metric.label}
                      label={metric.label}
                      value={metric.value}
                      change={metric.change}
                      changeIntent={metric.changeIntent}
                      align="start"
                      size="clamp(28px, 4vw, 32px)"
                    />
                  ))}
                </div>
              )}
            </section>
          )}
        </div>

        <div className="grid min-w-0 items-start gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)] 2xl:grid-cols-[minmax(0,1fr)_380px]">
          <div
            data-query-key="dashboard-trends"
            aria-busy={trendsQuery.isFetching || undefined}
            className="min-w-0"
          >
            {trendsQuery.isPending && !hasTrendData ? (
              <div
                aria-busy="true"
                className="admin-primary-panel p-4"
              >
                <div aria-hidden="true" className="h-56 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
              </div>
            ) : trendsQuery.isError && !hasTrendData ? (
              <div className="admin-primary-panel p-4">
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
