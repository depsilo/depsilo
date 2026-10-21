import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import ActivityTrends from '@/admin/components/dashboard/ActivityTrends'
import ConnectionGuide from '@/admin/components/dashboard/ConnectionGuide'
import CacheBenefits from '@/admin/components/dashboard/CacheBenefits'
import DashboardServiceStatus from '@/admin/components/dashboard/DashboardServiceStatus'
import RecentRequests from '@/admin/components/dashboard/RecentRequests'
import RequestDetailsDialog from '@/admin/components/dashboard/RequestDetailsDialog'
import RuntimeResources, { CacheStorageOverview } from '@/admin/components/dashboard/RuntimeResources'
import TrafficOverview from '@/admin/components/dashboard/TrafficOverview'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import { getAdminRouteHref } from '@/admin/routes'
import { usePolicyStatus } from '@/admin/usePolicyStatus'
import type { DashboardRange, DashboardResponse, DashboardTrendPoint, NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { adminApi, statsApi } from '@/lib/api'
import { getReadiness } from '@/lib/readiness'
import { getApiError } from '@/lib/apiError'
import {
  DASHBOARD_RANGES,
  deriveServiceStatus,
  deriveUsageState,
  readDashboardRange,
  writeDashboardRange,
} from '@/lib/dashboardOverview'
import { formatTime } from '@/lib/utils'
import './Dashboard.css'

const TREND_REFRESH_INTERVAL: Record<DashboardRange, number> = {
  '1h': 5_000,
  '24h': 15_000,
  '7d': 30_000,
  '30d': 60_000,
}

const RANGE_KEY: Record<DashboardRange, string> = {
  '1h': 'dashboard.range1h',
  '24h': 'dashboard.range24h',
  '7d': 'dashboard.range7d',
  '30d': 'dashboard.range30d',
}

interface TrendQueryData {
  response: Awaited<ReturnType<typeof adminApi.getDashboardTrends>>
  range: DashboardRange
}

export default function Dashboard() {
  const { t } = useTranslation()
  const [range, setRange] = useState<DashboardRange>(() => readDashboardRange())
  const [showProblems, setShowProblems] = useState(false)
  const [mobileResourcesOpen, setMobileResourcesOpen] = useState(false)
  const [detailsLogId, setDetailsLogId] = useState<number | null>(null)
  const policy = usePolicyStatus()

  const readinessQuery = useQuery({
    queryKey: ['admin', 'readiness'],
    queryFn: async ({ signal }) => (await getReadiness({ signal })).data,
    refetchInterval: 30_000,
    staleTime: 25_000,
    retry: false,
  })

  const recentQuery = useQuery({
    queryKey: ['admin', 'dashboard', 'recent-requests', 5],
    queryFn: ({ signal }) => adminApi.listLogs({ page: 1, page_size: 5 }, { signal }),
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    staleTime: 4_000,
    retry: false,
  })

  const nowQuery = useQuery<NowResponse>({
    queryKey: ['admin', 'now'],
    queryFn: async ({ signal }) => (await statsApi.getNow({ signal })).data,
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    staleTime: 4_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  const overviewQuery = useQuery({
    queryKey: ['admin', 'overview', range],
    queryFn: ({ signal }) => adminApi.getOverview(range, { signal }),
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
    retry: false,
  })

  const runtimeQuery = useQuery<RuntimeResponse>({
    queryKey: ['admin', 'runtime'],
    queryFn: async ({ signal }) => (await adminApi.getRuntime({ signal })).data,
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    staleTime: 4_000,
    retry: false,
  })

  const trendsQuery = useQuery({
    queryKey: ['admin', 'trends', range],
    queryFn: async ({ signal }): Promise<TrendQueryData> => ({
      response: await adminApi.getDashboardTrends(range, { signal }),
      range,
    }),
    placeholderData: keepPreviousData,
    refetchInterval: TREND_REFRESH_INTERVAL[range],
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  // Retain the last successful overview/trends payload. React Query drops the
  // placeholder once a refetch errors, but the Overview must keep showing the
  // previous period (with its own range label) and mark it stale instead of
  // blanking the region. Storing the previous value during render is React's
  // documented "adjust state from previous render" pattern.
  const [retainedOverview, setRetainedOverview] = useState<DashboardResponse | undefined>(undefined)
  if (overviewQuery.data?.data && overviewQuery.data.data !== retainedOverview) {
    setRetainedOverview(overviewQuery.data.data)
  }
  const overview = overviewQuery.data?.data ?? retainedOverview
  const overviewRange = overview?.range?.key ?? range
  const swapped = overviewRange !== range
  // Health is independent of the selected statistics range. An older cached
  // range response must never replace a more recent upstream observation.
  const [healthSnapshot, setHealthSnapshot] = useState<{ data: DashboardResponse; updatedAt: number }>()
  if (overviewQuery.data?.data && !overviewQuery.isPlaceholderData
    && (!healthSnapshot || overviewQuery.dataUpdatedAt > healthSnapshot.updatedAt)) {
    // Structural sharing can preserve an identical payload's object identity;
    // a successful read still renews its retrieval time and resolves staleness.
    setHealthSnapshot({ data: overviewQuery.data.data, updatedAt: overviewQuery.dataUpdatedAt })
  }
  const [healthErrorAt, setHealthErrorAt] = useState(0)
  if (overviewQuery.errorUpdatedAt > healthErrorAt) {
    setHealthErrorAt(overviewQuery.errorUpdatedAt)
  }
  // Changing range can expose another cached query's "success" state. Only a
  // health payload retrieved after the failed read can establish all-clear.
  const healthStale = Boolean(healthSnapshot
    && (overviewQuery.isError || healthErrorAt > healthSnapshot.updatedAt))
  const upstreams = healthSnapshot?.data.upstreams ?? []
  const now = nowQuery.data

  const nowInitialError = nowQuery.isError && !now
  const nowStale = nowQuery.isRefetchError && Boolean(now)
  const nowPending = nowQuery.isPending && !now
  const runtimePending = runtimeQuery.isPending && !runtimeQuery.data

  const [retainedTrends, setRetainedTrends] = useState<TrendQueryData | undefined>(undefined)
  if (trendsQuery.data && trendsQuery.data !== retainedTrends) {
    setRetainedTrends(trendsQuery.data)
  }
  const trendData = trendsQuery.data ?? retainedTrends
  const trendPoints: DashboardTrendPoint[] = trendData?.response.data.points ?? []
  const trendRange = trendData?.range ?? range
  const trendsLoading = trendsQuery.isPending && !trendData
  const trendsInitialError = trendsQuery.isError && !trendData
  const trendsStale = Boolean(trendData && trendsQuery.isError)

  const overviewInitialError = overviewQuery.isError && !overview
  const overviewError = overviewInitialError ? getApiError(overviewQuery.error) : undefined
  const overviewStale = Boolean(overview && overviewQuery.isError)

  const [hasObservedRequests, setHasObservedRequests] = useState(false)
  const [hasObservedSuccessfulRequest, setHasObservedSuccessfulRequest] = useState(false)
  const successfulRequestEvidence = overview?.usage?.state === 'used'
  const historyEvidence = Boolean(now?.last_activity)
    || (now?.rate.measured === true && now.rate.has_data)
    || (overview?.window?.total_requests ?? 0) > 0
    || (overview?.prev?.total_requests ?? 0) > 0
    || (overview?.last_24h?.total_requests ?? 0) > 0
    || (overview?.prev_24h?.total_requests ?? 0) > 0
    || (recentQuery.data?.data.items.length ?? 0) > 0
  // Keep fallback activity separate from confirmed downloads: metadata and
  // failed requests must not override a complete never-used lifecycle record.
  if (historyEvidence && !hasObservedRequests) setHasObservedRequests(true)
  if (successfulRequestEvidence && !hasObservedSuccessfulRequest) setHasObservedSuccessfulRequest(true)
  const usage = deriveUsageState({
    period: overview?.window,
    hasHistoricalRequests: hasObservedRequests || historyEvidence,
    hasSuccessfulRequest: hasObservedSuccessfulRequest || successfulRequestEvidence,
    usage: overview?.usage,
  })
  const showConnectionGuide = (usage === 'no-recent-evidence' || usage === 'waiting-first-request') && !swapped && !overviewStale
    && !nowQuery.isPending && !recentQuery.isPending
  const overviewPending = overviewQuery.isPending && !overview
  // Idle is a property of the last policy payload. A failed transport refresh
  // makes it stale, but cannot turn a never-loaded snapshot into a failure.
  const policyPayloadIdle = policy.status?.status === 'unavailable'
    && policy.status.degraded === false && policy.status.refresh_failures === 0
    && !(policy.status.snapshot_loaded_at ?? policy.status.last_successful_refresh)
    && !policy.status.using_stale_snapshot

  const status = deriveServiceStatus({
    readiness: readinessQuery.data,
    readinessAvailable: Boolean(readinessQuery.data),
    readinessStale: readinessQuery.isRefetchError,
    nowAvailable: Boolean(now) && !nowQuery.isError,
    nowStatus: now?.status,
    upstreams,
    upstreamsAvailable: Boolean(healthSnapshot),
    upstreamsStale: healthStale,
    policyAvailable: Boolean(policy.status),
    policyStale: policy.unavailable && Boolean(policy.status),
    policyNeedsAttention: Boolean(policy.status) && (policy.isStale || (!policyPayloadIdle
      && policy.status?.status !== 'healthy' && policy.status?.status !== 'ready')),
    cacheUsagePercent: healthSnapshot?.data.cache_usage_percent,
  })
  const healthPending = (readinessQuery.isPending || nowPending || overviewPending || (!policy.status && !policy.unavailable))
    && status.health !== 'partial' && status.health !== 'unavailable'

  const lastUpdated = Math.max(
    overviewQuery.dataUpdatedAt,
    nowQuery.dataUpdatedAt,
    runtimeQuery.dataUpdatedAt,
  )

  function handleRangeChange(next: DashboardRange) {
    writeDashboardRange(next)
    setRange(next)
  }

  function refreshAll() {
    void readinessQuery.refetch()
    void recentQuery.refetch()
    policy.refresh()
    void overviewQuery.refetch()
    void nowQuery.refetch()
    void runtimeQuery.refetch()
    void trendsQuery.refetch()
  }

  function refreshStatus() {
    void readinessQuery.refetch()
    void overviewQuery.refetch()
    void nowQuery.refetch()
    policy.refresh()
  }

  const lastUpdatedLabel = lastUpdated > 0 ? formatTime(new Date(lastUpdated).toISOString(), 'time') : '—'

  return (
    <div className="dashboard-page mx-auto w-full min-w-0">
    <AdminPage
      actions={(
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
          <DashboardServiceStatus
            open={showProblems}
            onOpenChange={setShowProblems}
            status={status}
            pending={healthPending}
            refreshing={readinessQuery.isFetching || overviewQuery.isFetching || nowQuery.isFetching || policy.refreshing}
            readiness={readinessQuery.data}
            now={now}
            policyStale={policy.isStale}
            upstreamsStale={healthStale}
            upstreamsUpdatedAt={healthSnapshot?.updatedAt ?? 0}
            onRefresh={refreshStatus}
          />
          <span className="inline-flex items-center gap-1.5 text-[13px]" style={{ color: 'var(--text-soft)' }}>
            <Icon name="history" size="sm" />
            {t('overview.updatedAt', { time: lastUpdatedLabel })}
          </span>
          <button
            type="button"
            onClick={refreshAll}
            aria-busy={overviewQuery.isFetching || nowQuery.isFetching || undefined}
            className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-1.5 rounded-md border px-3 text-[14px] font-medium transition-colors duration-150 hover:bg-[var(--bg-hover)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text)' }}
          >
            <Icon name="refresh" size="sm" />
            {t('overview.refresh')}
          </button>
        </div>
      )}
    >
      <div
        data-dashboard-root
        data-query-key="dashboard-snapshot"
        data-dashboard-health
        className="dashboard-surface flex min-w-0 flex-col gap-4"
      >
          {showConnectionGuide && <ConnectionGuide blocked={status.health === 'unavailable'} firstUse={usage === 'waiting-first-request'} onOpenStatus={() => setShowProblems(true)} />}
          {!showConnectionGuide && status.health === 'unavailable' && (
            <p role="alert" className="flex flex-wrap items-center gap-x-3 text-[13px] text-[var(--dash-danger)]">
              {t('overview.connectionBlocked')}
              <button type="button" className="dash-focus min-h-[40px] rounded-md font-medium" onClick={() => setShowProblems(true)}>{t('overview.viewStatus')}</button>
            </p>
          )}
          {!showConnectionGuide && usage === 'active' && overview?.window?.hit_requests === 0 && (
            <p className="flex flex-wrap items-center gap-x-3 text-[13px] text-[var(--dash-muted)]">{t('overview.verifyCacheHint')}<Link className="dash-focus inline-flex min-h-[40px] items-center rounded-md text-[var(--dash-accent)]" to={getAdminRouteHref('connect')}>{t('overview.verifyCacheAction')}</Link></p>
          )}

          <div data-dashboard-period data-period-range={overviewRange} className="flex min-w-0 flex-col gap-4">
          <div className="dashboard-period-summary">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-2">
            <p className="min-w-0 text-[15px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
              {t('overview.periodTitle')} · {t(RANGE_KEY[overviewRange])}
            </p>
            <div
              role="group"
              aria-label={t('overview.rangeGroup')}
              className="flex shrink-0 items-center overflow-hidden rounded-md border"
              style={{ borderColor: 'var(--dash-border)' }}
            >
              {DASHBOARD_RANGES.map(value => {
                const active = range === value
                return (
                  <button
                    key={value}
                    type="button"
                    onClick={() => handleRangeChange(value)}
                    aria-pressed={active}
                    className="dash-focus min-h-[40px] px-3 text-[13px] font-medium transition-colors duration-150"
                    style={{
                      background: active ? 'var(--dash-accent-soft)' : 'transparent',
                      color: active ? 'var(--dash-accent)' : 'var(--dash-muted)',
                      boxShadow: active ? 'inset 0 0 0 1px var(--dash-accent)' : undefined,
                    }}
                  >
                    {t(RANGE_KEY[value])}
                  </button>
                )
              })}
            </div>
          </div>

          {overviewStale && <p role="status" className="rounded-md bg-[var(--dash-warn-soft)] px-3 py-2 text-[13px] text-[var(--dash-warn)]">{t('overview.overviewStale', { range: t(RANGE_KEY[overviewRange]) })}</p>}
          {swapped && <p role="status" className="text-[13px] text-[var(--dash-muted)]">{t('overview.switchingRange')}</p>}
          {usage === 'empty-period' && !swapped && !overviewStale && (
            <div data-dashboard-empty-period className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-md bg-[var(--dash-soft)] px-4 py-2">
              <p className="text-[14px]">{t('overview.emptyPeriod')} <span className="text-[13px] text-[var(--dash-muted)]">{t('overview.emptyPeriodHint')}</span></p>
              {range !== '30d' && <button className="dash-focus min-h-[40px] rounded-md text-[13px] font-medium text-[var(--dash-accent)]" onClick={() => handleRangeChange('30d')}>{t('overview.expandRange')}</button>}
            </div>
          )}
          <div className="dashboard-cache-card dash-card min-w-0">
          {overviewInitialError ? (
            <div className="p-5" data-dashboard-statistics-error>
              <QueryErrorState message={overviewError?.status === 403 ? t('common.permissionDenied') : t('overview.statisticsUnavailable')} onRetry={() => { void overviewQuery.refetch() }} />
              <p className="mt-2 text-[13px] text-[var(--dash-muted)]">{t('overview.statisticsFailureImpact')} <Link className="dash-focus inline-flex min-h-[40px] items-center rounded-md text-[var(--dash-accent)]" to={getAdminRouteHref('connect')}>{t('overview.connectClient')}</Link></p>
            </div>
          ) : <CacheBenefits period={overview?.window} prev={overview?.prev} pending={overviewPending} />}
          <div className="dashboard-cache-summary">
            <CacheStorageOverview runtime={runtimeQuery.data} runtimePending={runtimePending} />
            <Link className="dash-focus ml-auto inline-flex min-h-[40px] shrink-0 items-center gap-1 rounded-md px-3 text-[13px] font-medium text-[var(--dash-accent)] no-underline" to={getAdminRouteHref('cache')}>{t('dashboard.manageCache')}<Icon name="chevron_right" size="sm" /></Link>
          </div>
          {runtimeQuery.isError && <p role="status" className="px-5 pb-3 text-[13px] text-[var(--dash-warn)]">{t(runtimeQuery.data ? 'overview.resourcesStale' : 'overview.resourcesUnavailable')} <button className="dash-focus min-h-[40px] rounded-md px-2 font-medium" onClick={() => { void runtimeQuery.refetch() }}>{t('overview.refresh')}</button></p>}
          </div>
          </div>
          {!overviewInitialError && <TrafficOverview now={now} nowPending={nowPending} nowError={nowInitialError} nowStale={nowStale} periodPending={overviewPending} period={overview?.window} rangeStart={overview?.range?.start} coverage={overview?.origin_coverage} />}
          {/* Trends and the recent-request tail are independent queries, so a
              period-aggregate failure never blanks them. */}
          {/* Give the area chart a continuous, full-width time axis. The latest
              request tail stays separate from these range-scoped aggregates. */}
          <div className="dashboard-lower-grid flex min-w-0 flex-col gap-4">
            <div data-dashboard-trends-column className="flex min-w-0">
              {trendsLoading ? (
                <div className="dash-card w-full p-5" aria-busy="true" aria-label={t('overview.loadingStatistics')}>
                  <div aria-hidden className="h-[272px] animate-pulse rounded-md" style={{ background: 'var(--dash-soft)' }} />
                </div>
              ) : trendsInitialError ? (
                <div className="dash-card w-full p-5">
                  <QueryErrorState
                    message={getApiError(trendsQuery.error).status === 403 ? t('common.permissionDenied') : t('overview.statisticsUnavailable')}
                    onRetry={() => { void trendsQuery.refetch() }}
                  />
                </div>
              ) : (
                <ActivityTrends
                  raw={trendPoints}
                  range={range}
                  dataRange={trendRange}
                  isStale={trendsStale}
                  onRetry={() => { void trendsQuery.refetch() }}
                />
              )}
            </div>
            <div className="flex min-w-0">
              <RecentRequests query={recentQuery} limit={5} onOpenDetails={setDetailsLogId} />
            </div>
          </div>
          </div>
          <section data-dashboard-resources-panel aria-labelledby="overview-resources-heading" className="dashboard-resources-panel" style={{ borderColor: 'var(--dash-border)' }}>
            <h2 id="overview-resources-heading" className="dashboard-resources-heading" style={{ color: 'var(--dash-ink)' }}>
              {t('overview.resourcesTitle')}
            </h2>
            <button
              type="button"
              className="dash-focus dashboard-resources-toggle"
              style={{ color: 'var(--dash-ink)' }}
              aria-expanded={mobileResourcesOpen}
              aria-controls="overview-runtime-resources"
              onClick={() => setMobileResourcesOpen(open => !open)}
            >
              {t('overview.resourcesTitle')}
              <Icon name="expand_more" size="sm" className={mobileResourcesOpen ? 'rotate-180' : ''} />
            </button>
            <div id="overview-runtime-resources" className={`dash-card dashboard-resources-content ${mobileResourcesOpen ? '' : 'hidden sm:block'}`}>
              <RuntimeResources
                runtime={runtimeQuery.data}
                runtimePending={runtimePending}
                now={now}
                nowPending={nowPending}
                nowError={nowInitialError}
                nowStale={nowStale}
              />
            </div>
          </section>
      </div>

      <RequestDetailsDialog logId={detailsLogId} onClose={() => setDetailsLogId(null)} />
    </AdminPage>
    </div>
  )
}
