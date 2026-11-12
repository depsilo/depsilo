import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import ActivityCard from '@/admin/components/dashboard/ActivityCard'
import AttentionCard from '@/admin/components/dashboard/AttentionCard'
import { EcosystemsCard, TopPackagesCard } from '@/admin/components/dashboard/BreakdownCards'
import ConnectionGuide from '@/admin/components/dashboard/ConnectionGuide'
import DashboardServiceStatus from '@/admin/components/dashboard/DashboardServiceStatus'
import LiveStrip from '@/admin/components/dashboard/LiveStrip'
import OverviewTiles from '@/admin/components/dashboard/OverviewTiles'
import RecentRequests from '@/admin/components/dashboard/RecentRequests'
import RequestDetailsDialog from '@/admin/components/dashboard/RequestDetailsDialog'
import RuntimeCard from '@/admin/components/dashboard/RuntimeCard'
import SecurityCard from '@/admin/components/dashboard/SecurityCard'
import StorageCard from '@/admin/components/dashboard/StorageCard'
import UpstreamsCard from '@/admin/components/dashboard/UpstreamsCard'
import {
  COCKPIT_QUERY_KEY,
  useBreakdown,
  useCacheDistribution,
  useCapabilityFacts,
  useSecuritySummary,
  useSuggestionsTotal,
  useUpstreamList,
} from '@/admin/components/dashboard/overviewQueries'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import { getAdminRouteHref } from '@/admin/routes'
import { usePolicyStatus } from '@/admin/usePolicyStatus'
import { useImmersiveMode } from '@/hooks/useImmersiveMode'
import type { DashboardRange, DashboardResponse, DashboardTrendPoint, NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { adminApi, statsApi } from '@/lib/api'
import { getReadiness } from '@/lib/readiness'
import { getApiError } from '@/lib/apiError'
import {
  DASHBOARD_RANGES,
  deriveServiceStatus,
  originCoverageNote,
  deriveUsageState,
  readDashboardRange,
  writeDashboardRange,
} from '@/lib/dashboardOverview'
import { activitySpan, rebucketActivity, type ActivityBucket } from '@/lib/overviewCockpit'
import { formatTime } from '@/lib/utils'
import './Dashboard.css'

/** Rows in the live request tail. */
const RECENT_REQUESTS = 8
/** Bars in a tile spark: enough to show when traffic came, few enough to read at 44px. */
const SPARK_BARS = 14
const NO_POINTS: DashboardTrendPoint[] = []
const NO_BUCKETS: ActivityBucket[] = []

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
  const [detailsLogId, setDetailsLogId] = useState<number | null>(null)
  const policy = usePolicyStatus()
  const immersive = useImmersiveMode()
  const queryClient = useQueryClient()
  const breakdownQuery = useBreakdown(range)
  const upstreamListQuery = useUpstreamList()
  const suggestionsQuery = useSuggestionsTotal()
  const securityQuery = useSecuritySummary()
  const capabilitiesQuery = useCapabilityFacts()
  const distributionQuery = useCacheDistribution()

  const readinessQuery = useQuery({
    queryKey: ['admin', 'readiness'],
    queryFn: async ({ signal }) => (await getReadiness({ signal })).data,
    refetchInterval: 30_000,
    staleTime: 25_000,
    retry: false,
  })

  const recentQuery = useQuery({
    queryKey: ['admin', 'dashboard', 'recent-requests', RECENT_REQUESTS],
    queryFn: ({ signal }) => adminApi.listLogs({ page: 1, page_size: RECENT_REQUESTS }, { signal }),
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
  const trendPoints: DashboardTrendPoint[] = trendData?.response.data.points ?? NO_POINTS
  const sparkBuckets = useMemo(() => rebucketActivity(trendPoints, activitySpan(trendPoints, SPARK_BARS)), [trendPoints])
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
    void capabilitiesQuery.refetch()
    void queryClient.refetchQueries({ queryKey: COCKPIT_QUERY_KEY, type: 'active' })
  }

  function refreshStatus() {
    void readinessQuery.refetch()
    void overviewQuery.refetch()
    void nowQuery.refetch()
    policy.refresh()
  }

  const lastUpdatedLabel = lastUpdated > 0 ? formatTime(new Date(lastUpdated).toISOString(), 'time') : '—'
  const fullscreenLabel = t(immersive.active ? 'overview.fullscreenExit' : 'overview.fullscreenEnter')
  // Where the Fullscreen API is missing, the mode only hides the navigation
  // rail, so the control is offered where that rail is on screen.
  const fullscreenSupported = typeof document !== 'undefined' && document.fullscreenEnabled
  // A tile spark sits under the selected period's totals, so it is drawn only
  // when the trend payload describes that same period.
  const tileSpark = trendData && trendRange === overviewRange ? sparkBuckets : NO_BUCKETS
  const originNote = originCoverageNote(overview?.origin_coverage, overview?.range?.start)
  const trendsError = trendsInitialError
    ? t(getApiError(trendsQuery.error).status === 403 ? 'common.permissionDenied' : 'overview.statisticsUnavailable')
    : undefined

  return (
    <div className="dashboard-surface mx-auto w-full min-w-0">
    <AdminPage
      titleAccessory={(
        <>
          <div data-dashboard-status-group className="flex min-w-0 items-center">
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
          </div>
          <LiveStrip now={now} pending={nowPending} error={nowInitialError} stale={nowStale} />
        </>
      )}
      actions={(
        <div data-dashboard-refresh-group className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-[12px] tabular-nums" style={{ color: 'var(--text-soft)' }}>
            <Icon name="history" size="sm" />
            {t('overview.updatedAt', { time: lastUpdatedLabel })}
          </span>
          <Button
            variant="outline"
            size="sm"
            type="button"
            onClick={refreshAll}
            aria-busy={overviewQuery.isFetching || nowQuery.isFetching || undefined}
          >
            <Icon name="refresh" size="sm" />
            {t('overview.refresh')}
          </Button>
          <Tooltip>
            <TooltipTrigger
              render={(
                <Button
                  variant="outline"
                  size="icon-sm"
                  type="button"
                  data-dashboard-fullscreen
                  aria-label={fullscreenLabel}
                  onClick={immersive.toggle}
                  className={fullscreenSupported ? undefined : 'hidden lg:inline-flex'}
                />
              )}
            >
              <Icon name={immersive.active ? 'fullscreen_exit' : 'fullscreen'} size="sm" />
            </TooltipTrigger>
            <TooltipContent>{fullscreenLabel}</TooltipContent>
          </Tooltip>
          <ToggleGroup
            value={[range]}
            onValueChange={values => {
              const next = DASHBOARD_RANGES.find(value => value === values[0])
              if (next) handleRangeChange(next)
            }}
            spacing={0}
            aria-label={t('overview.rangeGroup')}
            className="flex shrink-0 items-center overflow-hidden rounded-md border"
            style={{ borderColor: 'var(--dash-border)' }}
          >
            {DASHBOARD_RANGES.map(value => {
              const active = range === value
              return (
                <ToggleGroupItem
                  key={value}
                  value={value}
                  className="dash-focus min-h-10 rounded-none px-3 text-[13px] font-medium transition-colors duration-150 sm:min-h-9"
                  style={{
                    background: active ? 'var(--dash-accent-soft)' : 'transparent',
                    color: active ? 'var(--brand-text)' : 'var(--dash-muted)',
                    boxShadow: active ? 'inset 0 0 0 1px var(--dash-accent)' : undefined,
                  }}
                >
                  {t(RANGE_KEY[value])}
                </ToggleGroupItem>
              )
            })}
          </ToggleGroup>
        </div>
      )}
    >
      <div
        data-dashboard-root
        data-query-key="dashboard-snapshot"
        data-dashboard-health
        className="dash-cockpit"
      >
        <div className="dash-notices text-[13px]">
          {showConnectionGuide && <ConnectionGuide blocked={status.health === 'unavailable'} firstUse={usage === 'waiting-first-request'} onOpenStatus={() => setShowProblems(true)} />}
          {!showConnectionGuide && status.health === 'unavailable' && (
            <p role="alert" className="flex flex-wrap items-center gap-x-3 text-[var(--dash-danger)]">
              {t('overview.connectionBlocked')}
              <Button variant="ghost" size="sm" type="button" className="dash-focus font-medium" onClick={() => setShowProblems(true)}>{t('overview.viewStatus')}</Button>
            </p>
          )}
          {!showConnectionGuide && usage === 'active' && overview?.window?.hit_requests === 0 && (
            <p className="flex flex-wrap items-center gap-x-3 text-[var(--dash-muted)]">{t('overview.verifyCacheHint')}<Link className="dash-more dash-focus" to={getAdminRouteHref('connect')}>{t('overview.verifyCacheAction')}</Link></p>
          )}
          {overviewStale && <p role="status" className="rounded-md bg-[var(--dash-warn-soft)] px-3 py-2 text-[var(--dash-warn)]">{t('overview.overviewStale', { range: t(RANGE_KEY[overviewRange]) })}</p>}
          {swapped && <p role="status" className="text-[var(--dash-muted)]">{t('overview.switchingRange')}</p>}
          {usage === 'empty-period' && !swapped && !overviewStale && (
            <div data-dashboard-empty-period className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-md bg-[var(--dash-soft)] px-3 py-1">
              <p>{t('overview.emptyPeriod')} <span className="text-[var(--dash-muted)]">{t('overview.emptyPeriodHint')}</span></p>
              {range !== '30d' && <Button variant="ghost" size="sm" className="dash-focus font-medium text-[var(--brand-text)]" onClick={() => handleRangeChange('30d')}>{t('overview.expandRange')}</Button>}
            </div>
          )}
        </div>

        <div data-dashboard-period data-period-range={overviewRange} className="min-w-0">
          {overviewInitialError ? (
            <Card className="p-4 text-[13px]" data-dashboard-statistics-error>
              <QueryErrorState message={overviewError?.status === 403 ? t('common.permissionDenied') : t('overview.statisticsUnavailable')} onRetry={() => { void overviewQuery.refetch() }} />
              <p className="mt-2 text-[var(--dash-muted)]">{t('overview.statisticsFailureImpact')} <Link className="dash-more dash-focus" to={getAdminRouteHref('connect')}>{t('overview.connectClient')}</Link></p>
            </Card>
          ) : (
            <OverviewTiles
              period={overview?.window}
              prev={overview?.prev}
              range={overview?.range}
              coverage={overview?.origin_coverage}
              pending={overviewPending}
              runtime={runtimeQuery.data}
              runtimePending={runtimePending}
              spark={tileSpark}
            />
          )}
        </div>

        {/* Four groups in order of urgency: what needs an operator, traffic,
            upstreams and storage, then security and the process. The stylesheet
            lays them out as three columns, two, or one; in the single column
            they read in this order. Each card owns its query state, so one
            failure never blanks its neighbours. */}
        <div className="dash-cols">
          <div className="dash-col" data-dashboard-column="attention">
            <AttentionCard
              status={status}
              pending={healthPending}
              readiness={readinessQuery.data}
              upstreamsAvailable={Boolean(healthSnapshot)}
              policyStale={policy.isStale}
              upstreamList={upstreamListQuery.data}
              suggestionsTotal={suggestionsQuery.data}
              suggestionsKnown={suggestionsQuery.data !== undefined}
              security={securityQuery.data}
              onOpenStatus={() => setShowProblems(true)}
            />
          </div>
          <div className="dash-col" data-dashboard-column="traffic">
            <div data-dashboard-trends-column className="flex min-w-0 flex-col">
              <ActivityCard
                points={trendPoints}
                range={range}
                dataRange={trendRange}
                loading={trendsLoading}
                error={trendsError}
                stale={trendsStale}
                onRetry={() => { void trendsQuery.refetch() }}
              />
            </div>
            <RecentRequests query={recentQuery} limit={RECENT_REQUESTS} onOpenDetails={setDetailsLogId} />
            <TopPackagesCard query={breakdownQuery} />
          </div>
          <div className="dash-col" data-dashboard-column="upstreams">
            <UpstreamsCard
              status={status}
              available={Boolean(healthSnapshot)}
              stale={healthStale}
              pending={overviewPending}
              upstreamList={upstreamListQuery.data}
              breakdown={breakdownQuery}
              originMeasured={originNote.measured}
              originPartial={originNote.partial}
            />
            <EcosystemsCard query={breakdownQuery} />
            <StorageCard
              runtime={runtimeQuery.data}
              runtimePending={runtimePending}
              runtimeError={runtimeQuery.isError}
              onRetryRuntime={() => { void runtimeQuery.refetch() }}
              distribution={distributionQuery}
            />
          </div>
          <div className="dash-col" data-dashboard-column="control">
            <SecurityCard
              security={securityQuery}
              suggestionsTotal={suggestionsQuery.data}
              capabilities={capabilitiesQuery.data}
              capabilitiesUnavailable={capabilitiesQuery.isError && capabilitiesQuery.data === undefined}
            />
            <RuntimeCard
              runtime={runtimeQuery.data}
              runtimePending={runtimePending}
              runtimeError={runtimeQuery.isError}
              onRetryRuntime={() => { void runtimeQuery.refetch() }}
              now={now}
            />
          </div>
        </div>
      </div>

      <RequestDetailsDialog logId={detailsLogId} onClose={() => setDetailsLogId(null)} />
    </AdminPage>
    </div>
  )
}
