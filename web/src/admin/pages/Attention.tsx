import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import { getAdminRouteHref } from '@/admin/routes'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import EcosystemIcon from '@/components/EcosystemIcon'
import EmptyState from '@/components/EmptyState'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import SectionHeader from '@/components/SectionHeader'
import { adminApi } from '@/lib/api'
import type { DashboardResponse, SecuritySuggestionPage } from '@/lib/adminApi.types'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { formatTime } from '@/lib/utils'
import { upstreamStatus } from '@/lib/upstreamStatus'

interface AttentionQuarantineEvent {
  id: number
  ecosystem: string
  package: string
  version: string
  action: string
  reason: string
  created_at: string
}

const ATTENTION_SUGGESTIONS_PAGE = { page: 1 } as const
const ATTENTION_SUGGESTIONS_PARAMS = { page: 1, per_page: 20 } as const
const ATTENTION_QUARANTINE_PARAMS = { limit: 100 } as const

interface QueueItemProps {
  icon: string
  title: string
  detail: string
  count?: number
  tone: 'danger' | 'warning'
  href: string
  action: string
}

function QueueItem({ icon, title, detail, count, tone, href, action }: QueueItemProps) {
  return (
    <li className="flex min-w-0 flex-col gap-3 py-4 first:pt-0 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <span
          aria-hidden
          className="mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[6px]"
          style={{
            background: tone === 'danger' ? 'var(--danger-fill)' : 'var(--warn-fill)',
            color: tone === 'danger' ? 'var(--danger-text)' : 'var(--warn-text)',
          }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[13px] font-[600] text-[var(--text)]">{title}</h3>
            {count !== undefined && (
              <BadgeV2 variant={tone === 'danger' ? 'error' : 'warning'}>{count.toLocaleString()}</BadgeV2>
            )}
          </div>
          <p className="mt-1 max-w-2xl text-[12px] leading-5 text-[var(--text-soft)]">{detail}</p>
        </div>
      </div>
      <Link
        to={href}
        className="stripe-focus-ring inline-flex min-h-10 shrink-0 items-center justify-center gap-1 rounded-[5px] px-2.5 text-[12px] font-[600] no-underline text-[var(--brand-text)] transition-colors duration-150 hover:bg-[var(--bg-hover)] sm:self-center"
      >
        {action}
        <span aria-hidden>→</span>
      </Link>
    </li>
  )
}

function AttentionSkeleton() {
  return (
    <div aria-busy="true" className="space-y-3">
      {[0, 1, 2].map(index => (
        <div key={index} aria-hidden className="h-16 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
      ))}
    </div>
  )
}

export default function Attention() {
  const { t } = useTranslation()

  const dashboardQuery = useQuery({
    queryKey: ['admin', 'dashboard'],
    queryFn: ({ signal }) => adminApi.getDashboard({ signal }),
    refetchInterval: 30_000,
    retry: false,
  })
  const suggestionsQuery = useQuery({
    queryKey: ['admin', 'security', 'suggestions', ATTENTION_SUGGESTIONS_PAGE],
    queryFn: ({ signal }) => adminApi.listSuggestions(ATTENTION_SUGGESTIONS_PARAMS, { signal }),
    refetchInterval: 30_000,
    retry: false,
  })
  const quarantineQuery = useQuery({
    queryKey: ['admin', 'quarantine', 'events', ATTENTION_QUARANTINE_PARAMS],
    queryFn: async ({ signal }) => {
      const response = await adminApi.listQuarantineEvents(ATTENTION_QUARANTINE_PARAMS, { signal })
      return response.data as { items: AttentionQuarantineEvent[]; total: number }
    },
    refetchInterval: 30_000,
    retry: false,
  })

  const dashboard = dashboardQuery.data?.data as DashboardResponse | undefined
  const suggestions = suggestionsQuery.data?.data as SecuritySuggestionPage | undefined
  const unhealthyUpstreams = dashboard?.upstreams.filter(item => upstreamStatus(item) !== 'healthy') ?? []
  const suggestionCount = suggestions?.total ?? 0
  const cacheUsagePercent = dashboard?.cache_usage_percent
  const cacheNeedsAttention = cacheUsagePercent !== undefined && cacheUsagePercent > 80
  const isQueueLoading = dashboardQuery.isPending || suggestionsQuery.isPending
  const queueInitialFailure =
    (dashboardQuery.isError && !dashboardQuery.data) ||
    (suggestionsQuery.isError && !suggestionsQuery.data)
  const queueRefreshFailure = dashboardQuery.isRefetchError || suggestionsQuery.isRefetchError
  const quarantineInitialFailure = quarantineQuery.isError && !quarantineQuery.data
  const quarantineRefreshFailure = quarantineQuery.isRefetchError
  const queueIsEmpty = unhealthyUpstreams.length === 0 && suggestionCount === 0 && !cacheNeedsAttention
  const quarantineEvents = quarantineQuery.data?.items.slice(0, 5) ?? []

  function refreshQueue() {
    void Promise.all([
      dashboardQuery.refetch(),
      suggestionsQuery.refetch(),
    ])
  }

  return (
    <AdminPage description={t('attention.subtitle')}>
      <div className="space-y-10">
        <section aria-labelledby="attention-queue-title">
          <SectionHeader
            title={t('attention.queueTitle')}
            hint={t('attention.queueHint')}
          />
          {isQueueLoading ? (
            <AttentionSkeleton />
          ) : (
            <div className="space-y-3">
              {queueInitialFailure && (
                <InlineNotice tone="danger">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{t('attention.queueUnavailable')}</span>
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      size="sm"
                      aria-busy={dashboardQuery.isFetching || suggestionsQuery.isFetching || undefined}
                      disabled={dashboardQuery.isFetching || suggestionsQuery.isFetching}
                      onClick={refreshQueue}
                    >
                      {t('attention.retry')}
                    </ButtonV2>
                  </div>
                </InlineNotice>
              )}
              {queueRefreshFailure && !queueInitialFailure && (
                <InlineNotice tone="warning">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{t('attention.queueStale')}</span>
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      size="sm"
                      aria-busy={dashboardQuery.isFetching || suggestionsQuery.isFetching || undefined}
                      disabled={dashboardQuery.isFetching || suggestionsQuery.isFetching}
                      onClick={refreshQueue}
                    >
                      {t('attention.retry')}
                    </ButtonV2>
                  </div>
                </InlineNotice>
              )}
              {!queueInitialFailure && queueIsEmpty ? (
                <EmptyState
                  icon="task_alt"
                  title={t('attention.allClearTitle')}
                  hint={t('attention.allClearHint')}
                  minHeight={180}
                />
              ) : !queueIsEmpty ? (
                <ul className="divide-y divide-[var(--border)]">
                  {unhealthyUpstreams.length > 0 && (
                    <QueueItem
                      icon="cloud_off"
                      title={t('attention.upstreamsTitle')}
                      detail={t('attention.upstreamsDetail', {
                        count: unhealthyUpstreams.length,
                        names: unhealthyUpstreams.slice(0, 3).map(item => item.name).join(t('dashboard.listSeparator')),
                      })}
                      count={unhealthyUpstreams.length}
                      tone={unhealthyUpstreams.some(item => upstreamStatus(item) === 'failed') ? 'danger' : 'warning'}
                      href={getAdminRouteHref('upstreams')}
                      action={t('attention.reviewUpstreams')}
                    />
                  )}
                  {suggestionCount > 0 && (
                    <QueueItem
                      icon="gpp_maybe"
                      title={t('attention.securityTitle')}
                      detail={t('attention.securityDetail', { count: suggestionCount })}
                      count={suggestionCount}
                      tone="warning"
                      href={`${getAdminRouteHref('security')}?tab=suggestions`}
                      action={t('attention.reviewSecurity')}
                    />
                  )}
                  {cacheNeedsAttention && (
                    <QueueItem
                      icon="storage"
                      title={t('attention.cacheTitle')}
                      detail={t('attention.cacheDetail', { percent: cacheUsagePercent.toFixed(1) })}
                      tone={cacheUsagePercent > 95 ? 'danger' : 'warning'}
                      href={getAdminRouteHref('cache')}
                      action={t('attention.manageCache')}
                    />
                  )}
                </ul>
              ) : null}
            </div>
          )}
        </section>

        <section aria-labelledby="attention-recent-title">
          <SectionHeader
            title={t('attention.recentTitle')}
            hint={t('attention.recentHint')}
            action={(
              <Link
                to={getAdminRouteHref('quarantine')}
                className="stripe-focus-ring inline-flex min-h-10 items-center rounded-[5px] px-2 text-[12px] font-[600] no-underline text-[var(--brand-text)] hover:bg-[var(--bg-hover)]"
              >
                {t('attention.viewQuarantine')}
              </Link>
            )}
          />
          {quarantineQuery.isPending ? (
            <AttentionSkeleton />
          ) : (
            <div className="space-y-3">
              {quarantineInitialFailure && (
                <InlineNotice tone="danger">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{t('attention.recentUnavailable')}</span>
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      size="sm"
                      aria-busy={quarantineQuery.isFetching || undefined}
                      disabled={quarantineQuery.isFetching}
                      onClick={() => { void quarantineQuery.refetch() }}
                    >
                      {t('attention.retryRecent')}
                    </ButtonV2>
                  </div>
                </InlineNotice>
              )}
              {quarantineRefreshFailure && !quarantineInitialFailure && (
                <InlineNotice tone="warning">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <span>{t('attention.recentStale')}</span>
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      size="sm"
                      aria-busy={quarantineQuery.isFetching || undefined}
                      disabled={quarantineQuery.isFetching}
                      onClick={() => { void quarantineQuery.refetch() }}
                    >
                      {t('attention.retryRecent')}
                    </ButtonV2>
                  </div>
                </InlineNotice>
              )}
              {!quarantineInitialFailure && quarantineEvents.length === 0 ? (
                <EmptyState
                  icon="shield"
                  title={t('attention.noRecentTitle')}
                  hint={t('attention.noRecentHint')}
                  minHeight={160}
                />
              ) : quarantineEvents.length > 0 ? (
                <ol>
                  {quarantineEvents.map((event, index) => (
                    <li
                      key={event.id}
                      className="flex min-w-0 flex-col gap-2 py-3 sm:flex-row sm:items-center"
                      style={{ borderBottom: index < quarantineEvents.length - 1 ? '1px solid var(--border)' : 'none' }}
                    >
                      <div className="flex min-w-0 flex-1 items-start gap-2.5">
                        {isAdminEcosystem(event.ecosystem) && (
                          <EcosystemIcon type={event.ecosystem} size={16} />
                        )}
                        <div className="min-w-0">
                          <p className="break-all font-mono text-[13px] font-[500] text-[var(--text)]">
                            {event.package}
                            {event.version ? <span className="text-[var(--text-soft)]"> @{event.version}</span> : null}
                          </p>
                          <p className="mt-1 line-clamp-2 text-[12px] leading-5 text-[var(--text-soft)]">
                            {event.reason || t('attention.noReason')}
                          </p>
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center justify-between gap-3 pl-6 sm:justify-end sm:pl-0">
                        <BadgeV2 variant={event.action.includes('blocked') || event.action === 'tamper_detected' ? 'error' : 'warning'}>
                          {t(`quarantine.action.${event.action}`)}
                        </BadgeV2>
                        <time className="font-mono text-[11px] tabular-nums text-[var(--text-subtle)]" dateTime={event.created_at}>
                          {formatTime(event.created_at)}
                        </time>
                      </div>
                    </li>
                  ))}
                </ol>
              ) : null}
            </div>
          )}
        </section>
      </div>
    </AdminPage>
  )
}
