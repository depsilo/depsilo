import { useEffect, useMemo, useRef, useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { standardUpstreamEcosystems } from '@/admin/operatorEcosystems'
import Badge from '@/components/Badge'
import Button from '@/components/Button'
import EcosystemIcon from '@/components/EcosystemIcon'
import EmptyState from '@/components/EmptyState'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import Select from '@/components/Select'
import TableViewport from '@/components/TableViewport'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import {
  isAdminEcosystem,
  type AdminUpstreamUpdateEvent,
  type AdminUpstreamUpdateQuery,
  type UpstreamUpdateResult,
} from '@/lib/adminApi.types'

type ResultFilter = 'all' | UpstreamUpdateResult

const updateResults: UpstreamUpdateResult[] = ['error', 'updated', 'unchanged']

function isUpdateResult(value: string | null): value is UpstreamUpdateResult {
  return value !== null && updateResults.includes(value as UpstreamUpdateResult)
}

function latencyColor(latencyMs: number): string {
  if (latencyMs >= 1000) return 'var(--danger-text)'
  if (latencyMs >= 500) return 'var(--warn-text)'
  return 'var(--text-muted)'
}

export default function UpstreamUpdates() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const searchParamsRef = useRef(searchParams)
  const serializedSearchParams = searchParams.toString()

  const locale = i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en-US'
  const packageFilter = searchParams.get('package')?.trim() ?? ''
  const requestedEcosystem = searchParams.get('ecosystem')?.trim() ?? ''
  const ecosystemFilter = standardUpstreamEcosystems.some(
    ecosystem => ecosystem.id === requestedEcosystem,
  ) ? requestedEcosystem : 'all'
  const requestedResult = searchParams.get('result')
  const resultFilter: ResultFilter = isUpdateResult(requestedResult)
    ? requestedResult
    : 'all'
  const [packageDraft, setPackageDraft] = useState(packageFilter)
  const [manualRefreshing, setManualRefreshing] = useState(false)
  const [manualRefreshError, setManualRefreshError] = useState(false)

  useEffect(() => {
    searchParamsRef.current = new URLSearchParams(serializedSearchParams)
  }, [serializedSearchParams])

  useEffect(() => {
    setPackageDraft(packageFilter)
    setManualRefreshError(false)
  }, [ecosystemFilter, packageFilter, resultFilter])

  const timeFormatter = useMemo(() => new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }), [locale])

  const filterQuery = useMemo<AdminUpstreamUpdateQuery>(() => {
    const params: AdminUpstreamUpdateQuery = { limit: 100 }
    if (packageFilter) params.package = packageFilter
    if (ecosystemFilter !== 'all') params.ecosystem = ecosystemFilter
    if (resultFilter !== 'all') params.result = resultFilter
    return params
  }, [ecosystemFilter, packageFilter, resultFilter])

  const queryKey = ['admin', 'upstream-updates', filterQuery] as const
  const query = useInfiniteQuery({
    queryKey,
    queryFn: async ({ pageParam }) => (
      await adminApi.listUpstreamUpdates({
        ...filterQuery,
        cursor: pageParam ?? undefined,
      })
    ).data,
    initialPageParam: null as string | null,
    getNextPageParam: lastPage => lastPage.next_cursor || undefined,
    refetchInterval: currentQuery => (
      (currentQuery.state.data?.pages.length ?? 0) > 1 ? false : 30000
    ),
    retry: false,
  })

  const events = useMemo(
    () => query.data?.pages.flatMap(page => page.items) ?? [],
    [query.data],
  )
  const loadedChecks = useMemo(
    () => events.reduce((sum, event) => sum + (event.occurrence_count ?? 1), 0),
    [events],
  )
  const total = query.data?.pages[0]?.total ?? 0
  const pageCount = query.data?.pages.length ?? 0
  const autoRefreshPaused = pageCount > 1
  const backgroundRefreshing = manualRefreshing || (
    query.isFetching
    && !query.isFetchingNextPage
    && !query.isPending
  )
  const hasFilters = Boolean(packageFilter)
    || ecosystemFilter !== 'all'
    || resultFilter !== 'all'
  const apiError = getApiError(query.error)
  const errorMessage = apiError.status === 403
    ? t('common.permissionDenied')
    : t('upstreamUpdates.loadError')

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

  function updateFilter(name: 'ecosystem' | 'result', value: string) {
    const normalizedPackage = packageDraft.trim()
    const next = new URLSearchParams(searchParamsRef.current)
    if (normalizedPackage) next.set('package', normalizedPackage)
    else next.delete('package')
    if (value === 'all') next.delete(name)
    else next.set(name, value)
    next.delete('cursor')
    searchParamsRef.current = next
    setSearchParams(next, { replace: true })
  }

  function applyPackageFilter() {
    const normalized = packageDraft.trim()
    const next = new URLSearchParams(searchParamsRef.current)
    if (normalized) next.set('package', normalized)
    else next.delete('package')
    next.delete('cursor')
    searchParamsRef.current = next
    setSearchParams(next, { replace: true })
  }

  function clearFilters() {
    setPackageDraft('')
    const next = new URLSearchParams(searchParamsRef.current)
    next.delete('package')
    next.delete('ecosystem')
    next.delete('result')
    next.delete('cursor')
    searchParamsRef.current = next
    setSearchParams(next, { replace: true })
  }

  async function refreshLatest() {
    if (!autoRefreshPaused) {
      setManualRefreshError(false)
      await query.refetch()
      return
    }
    setManualRefreshing(true)
    setManualRefreshError(false)
    try {
      const { data: latestPage } = await adminApi.listUpstreamUpdates(filterQuery)
      queryClient.setQueryData(queryKey, {
        pages: [latestPage],
        pageParams: [null],
      })
    } catch {
      setManualRefreshError(true)
    } finally {
      setManualRefreshing(false)
    }
  }

  function formatTime(value: string): string {
    return timeFormatter.format(new Date(value))
  }

  function resultBadge(result: UpstreamUpdateResult) {
    return (
      <Badge
        variant={result === 'error' ? 'error' : result === 'updated' ? 'success' : 'neutral'}
        className="shrink-0 whitespace-nowrap"
      >
        {resultLabels[result]}
      </Badge>
    )
  }

  function source(event: AdminUpstreamUpdateEvent) {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        {isAdminEcosystem(event.ecosystem) && (
          <EcosystemIcon type={event.ecosystem} size={13} useColor decorative />
        )}
        <span className="shrink-0 uppercase" style={{ color: 'var(--text)' }}>
          {event.ecosystem}
        </span>
        <span aria-hidden="true" style={{ color: 'var(--text-subtle)' }}>·</span>
        <span className="min-w-0 truncate" title={event.upstream || undefined}>
          {event.upstream || t('upstreamUpdates.unknownUpstream')}
        </span>
      </span>
    )
  }

  function observation(event: AdminUpstreamUpdateEvent) {
    const occurrenceCount = event.occurrence_count ?? 1
    const firstSeenAt = event.first_seen_at ?? event.created_at
    const lastSeenAt = event.last_seen_at ?? event.created_at
    const firstSeen = formatTime(firstSeenAt)
    const lastSeen = formatTime(lastSeenAt)
    const isEpisode = firstSeenAt !== lastSeenAt

    return (
      <div
        className="min-w-0"
        aria-label={isEpisode
          ? t('upstreamUpdates.observationRange', {
              first: firstSeen,
              last: lastSeen,
              count: occurrenceCount,
            })
          : t('upstreamUpdates.singleObservation', {
              time: firstSeen,
              count: occurrenceCount,
            })}
      >
        <div className="font-mono text-[12px] tabular-nums" style={{ color: 'var(--text)' }}>
          <time dateTime={firstSeenAt}>{firstSeen}</time>
          {isEpisode && (
            <>
              <span className="mx-1" aria-hidden="true" style={{ color: 'var(--text-subtle)' }}>→</span>
              <time dateTime={lastSeenAt}>{lastSeen}</time>
            </>
          )}
        </div>
        <div className="mt-0.5 text-[12px]" style={{ color: 'var(--text-muted)' }}>
          <span aria-label={t('upstreamUpdates.checkCount', { count: occurrenceCount })}>
            {occurrenceCount > 1 ? `×${occurrenceCount}` : occurrenceCount}
          </span>
        </div>
      </div>
    )
  }

  const summaryStatus = backgroundRefreshing
    ? t('upstreamUpdates.refreshing')
    : autoRefreshPaused
      ? t('upstreamUpdates.autoRefreshPaused')
      : t('upstreamUpdates.autoRefreshActive')

  return (
    <AdminPage
      description={t('upstreamUpdates.subtitle')}
      actions={(
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="min-h-[40px] sm:min-h-8"
          aria-busy={backgroundRefreshing || undefined}
          disabled={query.isPending || query.isFetching || manualRefreshing}
          onClick={() => { void refreshLatest() }}
        >
          <Icon
            name="refresh"
            size="sm"
            className={backgroundRefreshing ? 'animate-spin' : ''}
          />
          {autoRefreshPaused
            ? t('upstreamUpdates.backToLatest')
            : backgroundRefreshing
              ? t('upstreamUpdates.refreshing')
              : t('upstreamUpdates.refresh')}
        </Button>
      )}
    >
      <div className="space-y-5">
        <form
          role="search"
          aria-label={t('upstreamUpdates.filtersLabel')}
          data-upstream-updates-toolbar
          className="flex flex-col gap-3 border-b border-[var(--border)] pb-4 lg:flex-row lg:items-center"
          onSubmit={(event) => {
            event.preventDefault()
            applyPackageFilter()
          }}
        >
          <div
            className="flex min-h-10 min-w-0 flex-1 items-center gap-2 rounded-[6px] border border-[var(--border)] px-3 lg:max-w-[420px]"
            style={{ background: 'var(--bg-card)' }}
          >
            <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
            <input
              type="text"
              aria-label={t('upstreamUpdates.searchLabel')}
              placeholder={t('upstreamUpdates.searchPlaceholder')}
              className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]"
              style={{ color: 'var(--text)' }}
              value={packageDraft}
              onChange={event => setPackageDraft(event.target.value)}
              onKeyDown={event => {
                if (event.key === 'Escape') {
                  setPackageDraft('')
                  event.currentTarget.blur()
                }
              }}
            />
          </div>

          <div className="grid grid-cols-2 gap-3 sm:flex sm:items-center">
            <Select
              aria-label={t('upstreamUpdates.filterEcosystem')}
              className="min-h-10 sm:w-[160px]"
              value={ecosystemFilter}
              onChange={event => updateFilter('ecosystem', event.target.value)}
            >
              <option value="all">{t('upstreamUpdates.allEcosystems')}</option>
              {standardUpstreamEcosystems.map(ecosystem => (
                <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
              ))}
            </Select>
            <Select
              aria-label={t('upstreamUpdates.filterResult')}
              className="min-h-10 sm:w-[150px]"
              value={resultFilter}
              onChange={event => updateFilter('result', event.target.value)}
            >
              <option value="all">{t('upstreamUpdates.allResults')}</option>
              {updateResults.map(result => (
                <option key={result} value={result}>{resultLabels[result]}</option>
              ))}
            </Select>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" size="sm" className="min-h-10 sm:min-h-8">
              {t('search')}
            </Button>
            {hasFilters && (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="min-h-10 sm:min-h-8"
                onClick={clearFilters}
              >
                {t('upstreamUpdates.clearFilters')}
              </Button>
            )}
          </div>
        </form>

        {query.data && (
          <div
            data-upstream-updates-summary
            className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 text-[12px]"
            style={{ color: 'var(--text-muted)' }}
          >
            <span>
              {t('upstreamUpdates.loadedSummary', {
                loaded: events.length,
                total,
                checks: loadedChecks,
              })}
            </span>
            <span role="status" aria-live="polite" aria-atomic="true">
              {summaryStatus}
            </span>
          </div>
        )}

        {query.data
          && (manualRefreshError || (query.isRefetchError && !query.isFetchNextPageError))
          && (
          <div role="alert">
            <StaleDataNotice
              message={t('upstreamUpdates.staleNotice')}
              refreshing={backgroundRefreshing}
              onRefresh={refreshLatest}
            />
          </div>
        )}

        {query.isPending ? (
          <div aria-busy="true" className="space-y-3 py-2">
            <span className="sr-only">{t('loading')}</span>
            {[...Array(5)].map((_, index) => (
              <div
                key={index}
                aria-hidden="true"
                className="h-14 animate-pulse rounded-[6px]"
                style={{ background: 'var(--bg-soft)' }}
              />
            ))}
          </div>
        ) : query.isError && !query.data ? (
          <QueryErrorState
            message={errorMessage}
            onRetry={() => { void query.refetch() }}
          />
        ) : events.length === 0 ? (
          <EmptyState
            icon={hasFilters ? 'search' : 'history'}
            title={hasFilters
              ? t('upstreamUpdates.noMatches')
              : t('upstreamUpdates.emptyTitle')}
            hint={hasFilters
              ? t('upstreamUpdates.noMatchesHint')
              : t('upstreamUpdates.emptyHint')}
            minHeight={240}
            action={hasFilters ? (
              <Button type="button" variant="secondary" className="min-h-10" onClick={clearFilters}>
                {t('upstreamUpdates.clearFilters')}
              </Button>
            ) : undefined}
          />
        ) : (
          <>
            <ul
              data-upstream-update-mobile-list
              className="divide-y divide-[var(--border)] md:hidden"
            >
              {events.map(event => {
                const detail = detailLabels[event.detail] ?? event.detail
                return (
                  <li key={event.id} className="space-y-2.5 py-4 first:pt-0">
                    <div className="flex min-w-0 items-start justify-between gap-3">
                      <span
                        className="min-w-0 break-words font-mono text-[13px] font-[550]"
                        style={{ color: 'var(--text)' }}
                      >
                        {event.package}
                      </span>
                      {resultBadge(event.result)}
                    </div>
                    <div className="text-[12px]" style={{ color: 'var(--text-muted)' }}>
                      {source(event)}
                    </div>
                    <p className="text-[13px] leading-[1.5]" style={{ color: 'var(--text-soft)' }}>
                      {detail}
                    </p>
                    <div className="grid grid-cols-1 gap-2 text-[12px] sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                      {observation(event)}
                      <span
                        className="font-mono tabular-nums"
                        style={{ color: latencyColor(event.latency_ms) }}
                      >
                        {t('upstreamUpdates.latestLatencyValue', { latency: event.latency_ms })}
                      </span>
                    </div>
                  </li>
                )
              })}
            </ul>

            <div className="hidden md:block">
              <TableViewport label={t('upstreamUpdates.tableLabel')} minWidth={860}>
                <table data-upstream-update-table className="w-full text-[12px]">
                  <caption className="sr-only">{t('upstreamUpdates.tableLabel')}</caption>
                  <thead>
                    <tr className="border-b border-[var(--border)] text-left">
                      {[
                        t('upstreamUpdates.time'),
                        t('upstreamUpdates.source'),
                        t('upstreamUpdates.package'),
                        t('upstreamUpdates.outcome'),
                        t('upstreamUpdates.latestLatency'),
                      ].map(heading => (
                        <th
                          key={heading}
                          scope="col"
                          className="px-3 py-2 text-[11px] font-[600] first:pl-0"
                          style={{ color: 'var(--text-muted)' }}
                        >
                          {heading}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {events.map(event => (
                      <tr
                        key={event.id}
                        className="border-b border-[var(--border)] transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                      >
                        <td className="w-[290px] px-3 py-3 pl-0 align-top">
                          {observation(event)}
                        </td>
                        <td className="max-w-[220px] px-3 py-3 align-top" style={{ color: 'var(--text-muted)' }}>
                          {source(event)}
                        </td>
                        <td
                          className="max-w-[260px] px-3 py-3 text-left align-top font-mono font-[500]"
                          style={{ color: 'var(--text)' }}
                        >
                          <span className="block break-words">{event.package}</span>
                        </td>
                        <td className="max-w-[360px] px-3 py-3 align-top">
                          <div className="flex min-w-0 items-start gap-2">
                            {resultBadge(event.result)}
                            <span className="min-w-0 leading-[1.5]" style={{ color: 'var(--text-soft)' }}>
                              {detailLabels[event.detail] ?? event.detail}
                            </span>
                          </div>
                        </td>
                        <td
                          className="whitespace-nowrap px-3 py-3 align-top font-mono tabular-nums"
                          style={{ color: latencyColor(event.latency_ms) }}
                        >
                          {event.latency_ms}ms
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </TableViewport>
            </div>

            <div className="space-y-3">
              {query.isFetchNextPageError && (
                <InlineNotice tone="danger">
                  {t('upstreamUpdates.nextPageError')}
                </InlineNotice>
              )}
              {query.hasNextPage && (
                <div className="flex justify-center">
                  <Button
                    type="button"
                    variant="secondary"
                    className="min-h-10"
                    disabled={query.isFetchingNextPage}
                    aria-busy={query.isFetchingNextPage || undefined}
                    onClick={() => { void query.fetchNextPage() }}
                  >
                    {query.isFetchingNextPage
                      ? t('upstreamUpdates.loadingMore')
                      : t('upstreamUpdates.loadMore')}
                  </Button>
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </AdminPage>
  )
}
