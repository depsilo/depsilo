import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { getAdminRouteHref } from '@/admin/routes'
import EcosystemIcon from '@/components/EcosystemIcon'
import { Skeleton } from '@/components/ui/skeleton'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { requestOutcome } from '@/lib/dashboardOverview'
import { formatBytes, formatLatency, formatTime } from '@/lib/utils'

import { ecosystemLabel } from './cockpitFormat'
import { CardNote, CockpitCard, MoreLink } from './cockpitParts'

interface RecentRequestsProps {
  query: UseQueryResult<Awaited<ReturnType<typeof adminApi.listLogs>>>
  limit: number
  onOpenDetails: (id: number) => void
}

/**
 * The latest client requests. This is a live tail, independent of the selected
 * statistics range, and the card says so, so nobody reads it as range-scoped.
 */
export default function RecentRequests({ query, limit, onOpenDetails }: RecentRequestsProps) {
  const { t } = useTranslation()
  const items = query.data?.data.items.slice(0, limit) ?? []
  const stale = query.isRefetchError && query.data !== undefined
  const failed = query.isError && query.data === undefined

  return (
    <CockpitCard
      id="recent"
      title={t('overview.recentRequestsTitle')}
      chip={t('overview.recentLive')}
      aside={<MoreLink to={getAdminRouteHref('accessLogs')}>{t('overview.allLogs')}</MoreLink>}
      busy={query.isPending}
      data-dashboard-recent-requests=""
      data-dashboard-recent-empty={!query.isPending && !failed && items.length === 0 ? 'true' : undefined}
    >
      {stale && <CardNote tone="warning" role="status" onRetry={() => { void query.refetch() }} retryLabel={t('common.retry')}>{t('overview.dataStale')}</CardNote>}
      {query.isPending ? (
        <div aria-hidden className="dash-skeleton" data-recent-skeleton>
          {Array.from({ length: limit }, (_, index) => <Skeleton key={index} className="h-[20px]" />)}
        </div>
      ) : failed ? (
        <CardNote tone="warning" role="alert" onRetry={() => { void query.refetch() }} retryLabel={t('common.retry')}>
          {getApiError(query.error).status === 403 ? t('common.permissionDenied') : t('overview.recentRequestsUnavailable')}
        </CardNote>
      ) : items.length === 0 ? (
        <CardNote>{t('overview.recentEmpty')} {t('overview.recentEmptyHint')}</CardNote>
      ) : (
        <table className="dash-reqs">
          <thead className="sr-only">
            <tr>
              <th scope="col">{t('overview.colOutcome')}</th>
              <th scope="col">{t('overview.colPackage')}</th>
              <th scope="col" className="is-wide">{t('overview.colEcosystem')}</th>
              <th scope="col" className="is-wide">{t('overview.colLatency')}</th>
              <th scope="col">{t('overview.colSize')}</th>
              <th scope="col">{t('overview.colTime')}</th>
            </tr>
          </thead>
          <tbody>
            {items.map(item => {
              const outcome = requestOutcome(item)
              const name = item.package_name || t('recentDownloads.unknownPackage')
              const full = item.version ? `${name}@${item.version}` : name
              return (
                <tr key={item.id} data-recent-request-id={item.id} onClick={() => onOpenDetails(item.id)}>
                  <td>
                    <span className="dash-outcome" data-outcome={outcome}><i aria-hidden />{t(`overview.outcomeShort.${outcome}`)}</span>
                  </td>
                  <td className="is-name">
                    <button
                      type="button"
                      className="dash-reqs-open dash-focus"
                      title={full}
                      onClick={event => { event.stopPropagation(); onOpenDetails(item.id) }}
                    >
                      <span className="dash-pkg">{name}{item.version && <span>@{item.version}</span>}</span>
                    </button>
                  </td>
                  <td className="is-wide">
                    {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={13} decorative />}
                    <span className="sr-only">{ecosystemLabel(item.adapter_type)}</span>
                  </td>
                  <td className="is-mute is-wide">{outcome === 'miss' && item.upstream ? `${item.upstream} · ` : ''}{formatLatency(item.latency_ms)}</td>
                  <td>{formatBytes(item.bytes_sent)}</td>
                  <td className="is-mute"><time dateTime={item.created_at}>{formatTime(item.created_at, 'auto')}</time></td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </CockpitCard>
  )
}
