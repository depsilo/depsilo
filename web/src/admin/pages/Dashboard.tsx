import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import DashboardAttention from '@/admin/components/DashboardAttention'
import NowStrip from '@/admin/components/NowStrip'
import RecentDownloads from '@/admin/components/RecentDownloads'
import TrendsCard, { type RawTrendPoint, type TrendsRange } from '@/admin/components/TrendsCard'
import Metric, { type MetricChangeIntent } from '@/components/Metric'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
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

function StatusMetric({
  label,
  value,
  detail,
  tone = 'default',
}: {
  label: string
  value: string
  detail: string
  tone?: 'default' | 'ok' | 'warning' | 'danger'
}) {
  const color = tone === 'ok'
    ? 'var(--ok-text)'
    : tone === 'warning'
      ? 'var(--warn-text)'
      : tone === 'danger'
        ? 'var(--danger-text)'
        : 'var(--text)'

  return (
    <div className="flex min-w-0 flex-col items-start text-left" data-dashboard-status-metric>
      <span className="text-[11px] font-[600]" style={{ color: 'var(--text-subtle)' }}>{label}</span>
      <span
        data-metric-value
        className="mt-2 min-w-0 font-[650] leading-[1.15]"
        style={{ color, fontFamily: 'var(--font-display)', fontSize: 'clamp(20px, 3vw, 28px)' }}
      >
        {value}
      </span>
      <span className="mt-1.5 text-[11px] leading-[1.45]" style={{ color: 'var(--text-soft)' }}>{detail}</span>
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
          className="app-button inline-flex min-h-9 items-center justify-center gap-1.5 rounded-[5px] px-3 py-1.5 text-[13px] font-[500] no-underline stripe-focus-ring"
          style={{ color: 'var(--btn-fg)', background: 'var(--btn)' }}
        >
          <Icon name="link" size="sm" />
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
          className="admin-kpi-section"
        >
          <SectionHeader
            title={t('dashboard.healthOverview')}
            divider={false}
            action={(
              <span className="text-[11px] text-[var(--text-subtle)]">
                {t('dashboard.snapshotRange')}
              </span>
            )}
          />
          {(dashboardQuery.isPending || nowQuery.isPending) && !dashboard && !nowData ? (
            <DashboardKpiSkeleton />
          ) : (
            <div data-dashboard-kpis className="admin-kpi-grid grid grid-cols-2 lg:grid-cols-4">
              <StatusMetric
                label={t('dashboard.serviceStatus')}
                value={nowStatus}
                tone={nowTone}
                detail={nowInitialError ? t('dashboard.statusUnavailableHint') : nowStale ? t('now.staleData') : t('dashboard.liveRefresh')}
              />
              <Metric
                label={metrics[0].label}
                value={metrics[0].value}
                change={metrics[0].change}
                changeIntent={metrics[0].changeIntent}
                align="start"
                size="clamp(28px, 4vw, 32px)"
              />
              <StatusMetric
                label={t('dashboard.currentHealthyUpstreams')}
                value={upstreamValue}
                tone={upstreamTone}
                detail={nowData
                  ? nowData.upstreams.total > 0 ? t('dashboard.healthyUpstreams') : t('dashboard.noUpstreams')
                  : t('dashboard.statusUnavailableHint')}
              />
              <Metric
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

        <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(320px,1fr)_minmax(0,2fr)] 2xl:grid-cols-[380px_minmax(0,1fr)]">
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
