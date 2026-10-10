import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import ActivityTrends from '@/admin/components/dashboard/ActivityTrends'
import CacheBenefits from '@/admin/components/dashboard/CacheBenefits'
import DashboardInfoDialog from '@/admin/components/dashboard/DashboardInfoDialog'
import DashboardStatusStrip from '@/admin/components/dashboard/DashboardStatusStrip'
import RecentRequests from '@/admin/components/dashboard/RecentRequests'
import RequestDetailsDialog from '@/admin/components/dashboard/RequestDetailsDialog'
import RuntimeResources from '@/admin/components/dashboard/RuntimeResources'
import TrafficOverview from '@/admin/components/dashboard/TrafficOverview'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import { getAdminRouteHref } from '@/admin/routes'
import { usePolicyStatus } from '@/admin/usePolicyStatus'
import type { DashboardRange, DashboardResponse, DashboardTrendPoint, NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { adminApi, statsApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import {
  DASHBOARD_RANGES,
  deriveServiceStatus,
  readDashboardRange,
  type ServiceProblem,
  type ServiceProblemCode,
  type ServiceStatusModel,
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
  const upstreams = overview?.upstreams ?? []
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

  const status = deriveServiceStatus({
    nowAvailable: Boolean(now) && !nowInitialError,
    nowStatus: now?.status,
    upstreams,
    policyNeedsAttention: policy.needsAttention === true,
    cacheUsagePercent: overview?.cache_usage_percent,
  })

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
    void overviewQuery.refetch()
    void nowQuery.refetch()
    void runtimeQuery.refetch()
    void trendsQuery.refetch()
  }

  const lastUpdatedLabel = lastUpdated > 0 ? formatTime(new Date(lastUpdated).toISOString(), 'time') : '—'

  return (
    <AdminPage
      actions={(
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
          <span className="inline-flex items-center gap-1.5 text-[13px]" style={{ color: 'var(--text-soft)' }}>
            <Icon name="history" size="sm" />
            {t('overview.updatedAt', { time: lastUpdatedLabel })}
          </span>
          <button
            type="button"
            onClick={refreshAll}
            aria-busy={overviewQuery.isFetching || nowQuery.isFetching || undefined}
            className="stripe-focus-ring inline-flex min-h-10 items-center gap-1.5 rounded-md border px-3 text-[14px] font-medium transition-colors duration-150 hover:bg-[var(--bg-hover)]"
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
          {/* One scope row states what the filter covers, so the cards do not
              each repeat the period and the control stops competing with the
              page title in the toolbar. */}
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-2">
            <p className="min-w-0 text-[13px]" style={{ color: 'var(--dash-muted)' }}>
              {t('overview.scopeNote')}
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
          <DashboardStatusStrip
            now={now}
            nowPending={nowPending}
            nowStale={nowStale}
            nowError={nowInitialError}
            status={status}
            onRefresh={refreshAll}
            onOpenProblems={() => setShowProblems(true)}
          />

          {overviewStale && (
            <p role="status" className="rounded-md px-3 py-2 text-[13px]" style={{ background: 'var(--dash-warn-soft)', color: 'var(--dash-warn)' }}>
              {t('overview.overviewStale', { range: t(RANGE_KEY[overviewRange]) })}
            </p>
          )}

          {overviewInitialError ? (
            <div className="dash-card p-5">
              <QueryErrorState
                message={overviewError?.status === 403 ? t('common.permissionDenied') : (overviewError?.message ?? t('common.loadFailed'))}
                onRetry={() => { void overviewQuery.refetch() }}
              />
            </div>
          ) : (
            <>
              <TrafficOverview
                now={now}
                nowPending={nowPending}
                period={overview?.window}
                rangeStart={overview?.range?.start}
                coverage={overview?.origin_coverage}
              />
              <CacheBenefits
                period={overview?.window}
                prev={overview?.prev}
                rangeStart={overview?.range?.start}
                coverage={overview?.origin_coverage}
              />
            </>
          )}

          {/* Trends and the recent-request tail are independent queries, so a
              period-aggregate failure never blanks them. */}
          {/* Chart and request table share a row only while the table can keep
              its readable minimum width; otherwise the table wraps full-width
              onto the next line instead of squeezing into an unreadable column. */}
          <div className="dashboard-lower-grid flex min-w-0 flex-wrap items-start gap-5">
            <div data-dashboard-trends-column className="min-w-[min(420px,100%)] grow basis-[480px]">
              {trendsLoading ? (
                <div className="dash-card p-5">
                  <div aria-hidden className="h-[272px] animate-pulse rounded-md" style={{ background: 'var(--dash-soft)' }} />
                </div>
              ) : trendsInitialError ? (
                <div className="dash-card p-5">
                  <QueryErrorState
                    message={getApiError(trendsQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(trendsQuery.error).message}
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
            <div className="min-w-[min(600px,100%)] grow basis-[600px]">
              <RecentRequests limit={5} onOpenDetails={setDetailsLogId} />
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
              />
            </div>
          </section>
          {swapped && (
            <p role="status" className="text-[13px]" style={{ color: 'var(--dash-muted)' }}>{t('overview.switchingRange')}</p>
          )}
      </div>

      <ProblemsDialog open={showProblems} onClose={() => setShowProblems(false)} status={status} policyStale={policy.isStale} />
      <RequestDetailsDialog logId={detailsLogId} onClose={() => setDetailsLogId(null)} />
    </AdminPage>
  )
}

/** Category text for one issue in the service-status dialog. */
function problemTitle(problem: ServiceProblem, t: TFunction, policyStale: boolean): string {
  switch (problem.code) {
    case 'upstreams': return t('overview.problemUpstreams', { count: problem.count ?? 0, names: problem.names ?? '' })
    case 'policy': return t(policyStale ? 'overview.problemPolicyStale' : 'overview.problemPolicy')
    case 'cache': return t('overview.problemCache', { percent: (problem.percent ?? 0).toFixed(1) })
    case 'status-unavailable': return t('overview.problemUnavailable')
    default: return t('overview.problemDegraded')
  }
}

/** Existing page that owns each issue category. */
function problemEntry(code: ServiceProblemCode): { href: string; labelKey: string } | null {
  switch (code) {
    case 'upstreams': return { href: getAdminRouteHref('upstreams'), labelKey: 'dashboard.viewUpstreams' }
    case 'cache': return { href: getAdminRouteHref('cache'), labelKey: 'dashboard.manageCache' }
    case 'policy': return { href: getAdminRouteHref('rules'), labelKey: 'policy.reviewRules' }
    default: return null
  }
}

/** Centered service-status detail; metric tiles use hover tooltips. */
function ProblemsDialog({
  open,
  onClose,
  status,
  policyStale,
}: {
  open: boolean
  onClose: () => void
  status: ServiceStatusModel
  policyStale: boolean
}) {
  const { t } = useTranslation()
  return (
    <DashboardInfoDialog
      open={open}
      onClose={onClose}
      title={t('overview.problemsTitle')}
      description={t('overview.problemsDescription')}
    >
      <ul className="flex flex-col gap-3">
        {status.problems.map(problem => (
          <li key={problem.code} className="rounded-md border px-3 py-2" style={{ borderColor: 'var(--border)' }}>
            <p className="text-[14px] font-semibold" style={{ color: 'var(--text)' }}>
              {problemTitle(problem, t, policyStale)}
            </p>
            <p className="mt-1 text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('overview.problemSuggestion')}</p>
            {problemEntry(problem.code) && (
              <Link
                to={problemEntry(problem.code)!.href}
                onClick={onClose}
                className="dash-focus mt-2 inline-flex min-h-8 items-center gap-1.5 rounded-md border px-2.5 text-[13px] no-underline"
                style={{ borderColor: 'var(--border)', color: 'var(--text)' }}
              >
                {t(problemEntry(problem.code)!.labelKey)}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </DashboardInfoDialog>
  )
}
