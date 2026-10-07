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
          <div
            role="group"
            aria-label={t('overview.rangeGroup')}
            className="flex items-center overflow-hidden rounded-md border"
            style={{ borderColor: 'var(--border)' }}
          >
            {DASHBOARD_RANGES.map(value => {
              const active = range === value
              return (
                <button
                  key={value}
                  type="button"
                  onClick={() => handleRangeChange(value)}
                  aria-pressed={active}
                  className="stripe-focus-ring min-h-10 px-3 text-[13px] font-medium transition-colors duration-150"
                  style={{
                    background: active ? 'var(--btn)' : 'transparent',
                    color: active ? 'var(--btn-fg)' : 'var(--text-soft)',
                  }}
                >
                  {t(RANGE_KEY[value])}
                </button>
              )
            })}
          </div>
        </div>
      )}
    >
      <div
        data-dashboard-root
        data-query-key="dashboard-snapshot"
        data-dashboard-health
        className="dashboard-surface flex min-w-0 flex-col gap-5"
      >
          {/* Runtime resources come from /admin/runtime and stay mounted even
              when the period aggregate fails; they render as the status card's
              second row so the first screen keeps one surface. */}
          <DashboardStatusStrip
            now={now}
            nowPending={nowPending}
            nowStale={nowStale}
            nowError={nowInitialError}
            status={status}
            resources={(
              <RuntimeResources
                runtime={runtimeQuery.data}
                runtimePending={runtimePending}
                now={now}
                nowPending={nowPending}
              />
            )}
            onOpenProblems={() => setShowProblems(true)}
            onRefresh={refreshAll}
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
                range={overviewRange}
                rangeStart={overview?.range?.start}
                coverage={overview?.origin_coverage}
              />
              <div className="grid min-w-0 grid-cols-1 items-start gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)]">
                <CacheBenefits
                  period={overview?.window}
                  prev={overview?.prev}
                  range={overviewRange}
                  rangeStart={overview?.range?.start}
                  coverage={overview?.origin_coverage}
                />
                <DashboardAttentionPanel
                  problems={status.problems}
                  onOpenProblems={() => setShowProblems(true)}
                />
              </div>
            </>
          )}

          {/* Trends and the recent-request tail are independent queries, so a
              period-aggregate failure never blanks them. */}
          {/* Chart and request table share a row only while the table can keep
              its readable minimum width; otherwise the table wraps full-width
              onto the next line instead of squeezing into an unreadable column. */}
          <div className="flex min-w-0 flex-wrap items-start gap-5">
            <div className="min-w-[min(420px,100%)] grow basis-[480px]">
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
          {swapped && (
            <p role="status" className="text-[13px]" style={{ color: 'var(--dash-muted)' }}>{t('overview.switchingRange')}</p>
          )}
      </div>

      <ProblemsDialog open={showProblems} onClose={() => setShowProblems(false)} status={status} />
      <RequestDetailsDialog logId={detailsLogId} onClose={() => setDetailsLogId(null)} />
    </AdminPage>
  )
}

/** Category text for one issue, shared by the queue row and the dialog. */
function problemTitle(problem: ServiceProblem, t: TFunction): string {
  switch (problem.code) {
    case 'upstreams': return t('overview.problemUpstreams', { count: problem.count ?? 0, names: problem.names ?? '' })
    case 'policy': return t('overview.problemPolicy')
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

/** Attention queue: real issues only, each opening the centered problem dialog. */
function DashboardAttentionPanel({ problems, onOpenProblems }: {
  problems: ServiceProblem[]
  onOpenProblems: () => void
}) {
  const { t } = useTranslation()
  const visible = problems.slice(0, 3)

  return (
    <section
      data-dashboard-attention
      aria-labelledby="overview-attention-title"
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex items-center justify-between gap-2 border-b px-5 py-4" style={{ borderColor: 'var(--dash-border)' }}>
        <h2 id="overview-attention-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('dashboard.needsAttention')}
        </h2>
        <span
          className="font-mono text-[15px] font-semibold tabular-nums"
          style={{ color: problems.length > 0 ? 'var(--dash-warn)' : 'var(--dash-ok)' }}
          aria-label={t('dashboard.attentionCount', { count: problems.length })}
        >
          {problems.length}
        </span>
      </header>
      {visible.length === 0 ? (
        <div className="flex items-center gap-3 px-5 py-4">
          <span aria-hidden className="grid size-9 shrink-0 place-items-center rounded-full" style={{ background: 'var(--dash-ok-soft)', color: 'var(--dash-ok)' }}>
            <Icon name="check_circle" size="md" />
          </span>
          <div className="min-w-0">
            <p className="text-[15px] font-semibold" style={{ color: 'var(--dash-ink)' }}>{t('dashboard.noActiveIssues')}</p>
            <p className="mt-0.5 text-[13px]" style={{ color: 'var(--dash-muted)' }}>{t('dashboard.noActiveIssuesHint')}</p>
          </div>
        </div>
      ) : (
        <ul className="flex flex-col">
          {visible.map(problem => (
            <li key={problem.code} className="border-t first:border-t-0" style={{ borderColor: 'var(--dash-border)' }}>
              <button
                type="button"
                onClick={onOpenProblems}
                className="dash-focus flex min-h-16 w-full items-center gap-3 px-5 py-3 text-left transition-colors duration-150 hover:bg-[var(--dash-soft)]"
              >
                <span aria-hidden className="grid size-9 shrink-0 place-items-center rounded-full" style={{ background: 'var(--dash-warn-soft)', color: 'var(--dash-warn)' }}>
                  <Icon name="warning" size="md" />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-[15px] font-semibold" style={{ color: 'var(--dash-ink)' }}>{problemTitle(problem, t)}</p>
                  <p className="mt-0.5 text-[13px]" style={{ color: 'var(--dash-muted)' }}>{t('overview.problemSuggestion')}</p>
                </div>
                <Icon name="chevron_right" size="sm" className="shrink-0" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** Centered detail for the attention queue; metric tiles use hover tooltips. */
function ProblemsDialog({
  open,
  onClose,
  status,
}: {
  open: boolean
  onClose: () => void
  status: ServiceStatusModel
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
              {problemTitle(problem, t)}
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
