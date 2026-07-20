import { useInfiniteQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import Button from '@/components/Button'
import QueryErrorState from '@/components/QueryErrorState'
import TableViewport from '@/components/TableViewport'
import AdminPage from '@/admin/components/AdminPage'
import { adminApi } from '@/lib/api'
import type { UpstreamUpdateResult } from '@/lib/adminApi.types'

export default function UpstreamUpdates() {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en-US'
  const resultLabels = {
    updated: t('upstreamUpdates.results.updated'),
    unchanged: t('upstreamUpdates.results.unchanged'),
    error: t('upstreamUpdates.results.error'),
  } satisfies Record<UpstreamUpdateResult, string>
  const detailLabels: Record<string, string> = {
    'cached metadata refreshed': t('upstreamUpdates.details.refreshed'),
    'upstream metadata not modified': t('upstreamUpdates.details.notModified'),
    'metadata refresh failed': t('upstreamUpdates.details.failed'),
    // Preserve the localized projection for rows written before the History
    // Module stopped promising diagnostics that the safe log did not contain.
    'metadata refresh failed; inspect server logs': t('upstreamUpdates.details.failed'),
  }
  const query = useInfiniteQuery({
    queryKey: ['admin', 'upstream-updates'],
    queryFn: async ({ pageParam }) => (
      await adminApi.listUpstreamUpdates({ limit: 100, cursor: pageParam ?? undefined })
    ).data,
    initialPageParam: null as string | null,
    getNextPageParam: lastPage => lastPage.next_cursor || undefined,
    refetchInterval: 30000,
    retry: false,
  })
  const events = query.data?.pages.flatMap(page => page.items) ?? []
  const formatTime = (value: string) => new Date(value).toLocaleString(locale)

  if (query.isPending) {
    return (
      <AdminPage description={t('upstreamUpdates.subtitle')}>
        <div role="status" aria-busy="true" className="py-16 text-center text-[13px] text-[var(--text-soft)]">
          {t('loading')}
        </div>
      </AdminPage>
    )
  }

  if (query.isError && !query.data) {
    return (
      <AdminPage description={t('upstreamUpdates.subtitle')}>
        <QueryErrorState
          message={t('upstreamUpdates.loadError')}
          onRetry={() => { void query.refetch() }}
        />
      </AdminPage>
    )
  }

  return (
    <AdminPage description={t('upstreamUpdates.subtitle')}>
    <div className="space-y-4">
      {(query.isRefetchError || query.isFetchNextPageError) && (
        <p role="alert" className="text-[13px] text-[var(--danger-text)]">
          {query.isFetchNextPageError
            ? t('upstreamUpdates.nextPageError')
            : t('upstreamUpdates.staleNotice')}
        </p>
      )}
      <div className="rounded-[6px] border border-[var(--border)]">
        <TableViewport label={t('upstreamUpdates.tableLabel')} minWidth={1000}>
          <table className="w-full text-[13px]">
            <caption className="sr-only">{t('upstreamUpdates.tableLabel')}</caption>
            <thead>
              <tr className="text-left text-[var(--text-soft)]">
                <th scope="col" className="p-3">{t('upstreamUpdates.time')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.ecosystem')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.upstream')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.package')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.result')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.detail')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.checks')}</th>
                <th scope="col" className="p-3">{t('upstreamUpdates.latency')}</th>
              </tr>
            </thead>
            <tbody>
              {events.map(event => {
                const occurrenceCount = event.occurrence_count ?? 1
                const firstSeenAt = event.first_seen_at ?? event.created_at
                const lastSeenAt = event.last_seen_at ?? event.created_at
                return (
                  <tr key={event.id} className="border-t border-[var(--border)]">
                    <td className="whitespace-nowrap p-3">
                      <time dateTime={lastSeenAt}>{formatTime(lastSeenAt)}</time>
                      {firstSeenAt !== lastSeenAt && (
                        <div className="mt-0.5 text-[11px] text-[var(--text-soft)]">
                          {t('upstreamUpdates.firstSeen', { time: formatTime(firstSeenAt) })}
                        </div>
                      )}
                    </td>
                    <td className="p-3">{event.ecosystem}</td>
                    <td className="p-3">{event.upstream}</td>
                    <td className="p-3 font-mono">{event.package}</td>
                    <td className="p-3">{resultLabels[event.result] ?? event.result}</td>
                    <td className="p-3">{detailLabels[event.detail] ?? event.detail}</td>
                    <td className="p-3 font-mono tabular-nums">
                      <span aria-label={t('upstreamUpdates.checkCount', { count: occurrenceCount })}>
                        {occurrenceCount > 1 ? `×${occurrenceCount}` : occurrenceCount}
                      </span>
                    </td>
                    <td className="p-3 font-mono tabular-nums">{event.latency_ms}ms</td>
                  </tr>
                )
              })}
              {!events.length && (
                <tr>
                  <td className="p-6 text-center text-[var(--text-soft)]" colSpan={8}>
                    {t('upstreamUpdates.empty')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </TableViewport>
        {query.hasNextPage && (
          <div className="flex justify-center border-t border-[var(--border)] p-3">
            <Button
              type="button"
              variant="secondary"
              disabled={query.isFetchingNextPage}
              aria-busy={query.isFetchingNextPage}
              onClick={() => { void query.fetchNextPage() }}
            >
              {query.isFetchingNextPage
                ? t('upstreamUpdates.loadingMore')
                : t('upstreamUpdates.loadMore')}
            </Button>
          </div>
        )}
      </div>
    </div>
    </AdminPage>
  )
}
