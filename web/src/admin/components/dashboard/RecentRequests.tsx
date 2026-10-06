import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import TableViewport from '@/components/TableViewport'
import { getAdminRouteHref } from '@/admin/routes'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { REQUEST_OUTCOME_META, requestOutcome } from '@/lib/dashboardOverview'
import { formatBytes, formatLatency, formatTime } from '@/lib/utils'

const REFRESH_MS = 5_000

interface RecentRequestsProps {
  limit?: number
  onOpenDetails: (id: number) => void
}

/**
 * Latest client requests. This is a live tail independent of the selected
 * statistics range, which the caption states explicitly so operators do not
 * read it as range-scoped data.
 */
export default function RecentRequests({ limit = 5, onOpenDetails }: RecentRequestsProps) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['admin', 'dashboard', 'recent-requests', limit],
    queryFn: ({ signal }) => adminApi.listLogs({ page: 1, page_size: limit }, { signal }),
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
    staleTime: REFRESH_MS - 1_000,
    retry: false,
  })

  const items = query.data?.data.items ?? []
  const isStale = query.isRefetchError && query.data !== undefined
  const initialError = query.isError && query.data === undefined

  return (
    <section
      data-dashboard-recent-requests
      aria-labelledby="overview-recent-title"
      aria-busy={query.isPending || undefined}
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex flex-wrap items-center justify-between gap-2 px-5 pb-2 pt-4">
        <div className="min-w-0">
          <h2 id="overview-recent-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
            {t('overview.recentRequestsTitle')}
          </h2>
          <p className="mt-0.5 text-[13px]" style={{ color: 'var(--dash-muted)' }}>
            {t('overview.recentRequestsCaption')}
          </p>
        </div>
        <Link
          to={getAdminRouteHref('accessLogs')}
          className="dash-focus inline-flex min-h-9 items-center gap-1 rounded-md px-2 text-[14px] font-medium no-underline"
          style={{ color: 'var(--dash-accent)' }}
        >
          {t('dashboard.viewAll')}
          <Icon name="chevron_right" size="sm" />
        </Link>
      </header>

      {isStale && (
        <p role="status" className="mx-5 mb-2 rounded-md px-3 py-1.5 text-[13px]" style={{ background: 'var(--dash-warn-soft)', color: 'var(--dash-warn)' }}>
          {t('overview.dataStale')}
        </p>
      )}

      {query.isPending ? (
        <div aria-hidden className="flex flex-col gap-3 px-5 pb-5">
          {Array.from({ length: limit }, (_, index) => (
            <div key={index} className="h-10 animate-pulse rounded-md" style={{ background: 'var(--dash-soft)' }} />
          ))}
        </div>
      ) : initialError ? (
        <p role="alert" className="px-5 pb-5 text-[14px]" style={{ color: 'var(--dash-warn)' }}>
          {getApiError(query.error).status === 403 ? t('common.permissionDenied') : t('overview.recentRequestsUnavailable')}
        </p>
      ) : items.length === 0 ? (
        <div className="flex min-h-[160px] flex-col items-center justify-center gap-2 px-5 pb-6 text-center">
          <span aria-hidden className="grid size-9 place-items-center rounded-full" style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}>
            <Icon name="download" size="md" />
          </span>
          <p className="text-[14px]" style={{ color: 'var(--dash-muted)' }}>{t('recentDownloads.empty')}</p>
        </div>
      ) : (
        <TableViewport label={t('overview.recentRequestsTitle')} minWidth={600}>
          <table className="w-full border-collapse text-left">
            <thead>
              <tr style={{ color: 'var(--dash-muted)' }}>
                <th className="px-4 py-2.5 text-[13px] font-medium">{t('overview.colPackage')}</th>
                <th className="px-3 py-2.5 text-[13px] font-medium whitespace-nowrap">{t('overview.colEcosystem')}</th>
                <th className="px-3 py-2.5 text-[13px] font-medium whitespace-nowrap">{t('overview.colOutcome')}</th>
                <th className="px-3 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colSize')}</th>
                <th className="px-3 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colLatency')}</th>
                <th className="px-4 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colTime')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map(item => {
                const outcome = requestOutcome(item)
                const meta = REQUEST_OUTCOME_META[outcome]
                const name = item.package_name || t('recentDownloads.unknownPackage')
                return (
                  <tr
                    key={item.id}
                    data-recent-request-id={item.id}
                    className="cursor-pointer border-t transition-colors duration-150 hover:bg-[var(--dash-soft)]"
                    style={{ borderColor: 'var(--dash-border)' }}
                    onClick={() => onOpenDetails(item.id)}
                  >
                    <td className="max-w-[180px] px-4 py-4">
                      <button
                        type="button"
                        onClick={event => { event.stopPropagation(); onOpenDetails(item.id) }}
                        className="dash-focus block min-w-0 max-w-full truncate text-left font-mono text-[14px] font-medium"
                        style={{ color: 'var(--dash-ink)' }}
                        title={item.version ? `${name}@${item.version}` : name}
                      >
                        {name}
                        {item.version && <span style={{ color: 'var(--dash-muted)' }}>@{item.version}</span>}
                      </button>
                    </td>
                    <td className="px-3 py-4">
                      <span className="inline-flex items-center gap-1.5 text-[13px]" style={{ color: 'var(--dash-muted)' }}>
                        {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={14} decorative />}
                        {item.adapter_type || '—'}
                      </span>
                    </td>
                    <td className="px-3 py-4">
                      <BadgeV2 variant={meta.variant}>{t(meta.key)}</BadgeV2>
                    </td>
                    <td className="px-3 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-ink)' }}>
                      {formatBytes(item.bytes_sent)}
                    </td>
                    <td className="px-3 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-muted)' }}>
                      {formatLatency(item.latency_ms)}
                    </td>
                    <td className="px-4 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-muted)' }}>
                      <time dateTime={item.created_at}>{formatTime(item.created_at, 'auto')}</time>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </TableViewport>
      )}
    </section>
  )
}
