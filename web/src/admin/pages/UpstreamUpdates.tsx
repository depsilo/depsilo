import { ecosystemLabel } from '@/admin/operatorEcosystems'
import { isKnownEcosystemId } from '@/admin/operatorEcosystems'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { TableContainer, Table, TableCaption, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription, EmptyContent } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import { useTranslation } from 'react-i18next'

import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import EcosystemChips from '@/admin/components/EcosystemChips'
import TimeRangeControl from '@/admin/components/TimeRangeControl'
import { parseEcosystemList, parseTimeRange, resolveTimeWindow, serializeEcosystemList, writeTimeRange, type TimePreset, type TimeRangeValue } from '@/admin/listFilters'
import { formatTime as formatClock } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import EcosystemIcon from '@/components/EcosystemIcon'

import Icon from '@/components/Icon'
import SearchField from '@/components/SearchField'

import QueryErrorState from '@/components/QueryErrorState'
import { NativeSelect } from '@/components/ui/native-select'

import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import {
  isAdminEcosystem,
  type AdminUpstreamUpdateEvent,
  type AdminUpstreamUpdateQuery,
  type UpstreamUpdateResult,
} from '@/lib/adminApi.types'

type ResultFilter = 'all' | UpstreamUpdateResult

/** Refresh records open on everything kept, as before. */
const DEFAULT_RANGE: TimePreset = 'all'

const updateResults: UpstreamUpdateResult[] = ['error', 'updated', 'unchanged']

function isUpdateResult(value: string | null): value is UpstreamUpdateResult {
  return value !== null && updateResults.includes(value as UpstreamUpdateResult)
}

function latencyColor(latencyMs: number): string {
  if (latencyMs >= 1000) return 'var(--warn-text)'
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
  const ecosystems = parseEcosystemList(searchParams.get('ecosystem'), id => isKnownEcosystemId(id))
  const ecosystemFilter = serializeEcosystemList(ecosystems) ?? 'all'
  const timeRange = parseTimeRange(searchParams, DEFAULT_RANGE)
  // Presets count back from this moment; a manual refresh moves it to now.
  const [anchor, setAnchor] = useState(() => Date.now())
  // The list polls every 30 s for new events, so a preset stays open-ended.
  const timeWindow = resolveTimeWindow(timeRange, anchor, { openEnd: true })
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
    if (timeWindow.start) params.start = timeWindow.start
    if (timeWindow.end) params.end = timeWindow.end
    return params
  }, [ecosystemFilter, packageFilter, resultFilter, timeWindow.start, timeWindow.end])

  const queryKey = ['admin', 'upstream-updates', filterQuery] as const
  const query = useInfiniteQuery({
    queryKey,
    queryFn: async ({ pageParam, signal }) => (
      await adminApi.listUpstreamUpdates({
        ...filterQuery,
        cursor: pageParam ?? undefined,
      }, { signal })
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
    || timeRange.preset !== DEFAULT_RANGE
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

  // Like the result filter, a chip or range change also applies the package
  // text being typed, which the filter-change effect would otherwise reset.
  function updateParams(mutator: (next: URLSearchParams) => void) {
    const next = new URLSearchParams(searchParamsRef.current)
    const normalizedPackage = packageDraft.trim()
    if (normalizedPackage) next.set('package', normalizedPackage)
    else next.delete('package')
    mutator(next)
    next.delete('cursor')
    searchParamsRef.current = next
    setSearchParams(next, { replace: true })
  }

  function setEcosystems(ids: string[]) {
    updateParams(next => {
      const value = serializeEcosystemList(ids)
      if (value) next.set('ecosystem', value)
      else next.delete('ecosystem')
    })
  }

  function setTimeRange(value: TimeRangeValue) {
    setAnchor(Date.now())
    updateParams(next => writeTimeRange(next, value, DEFAULT_RANGE))
  }

  function updateFilter(name: 'result', value: string) {
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
    writeTimeRange(next, { preset: DEFAULT_RANGE }, DEFAULT_RANGE)
    next.delete('cursor')
    searchParamsRef.current = next
    setSearchParams(next, { replace: true })
  }

  async function refreshLatest() {
    // A relative window moves to now, which is a new first page.
    if (timeRange.preset !== 'custom' && timeRange.preset !== 'all') {
      setManualRefreshError(false)
      setAnchor(Date.now())
      return
    }
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
        variant={result === 'error' ? 'destructive' : result === 'updated' ? 'success' : 'neutral'}
        className="shrink-0 whitespace-nowrap"
      >
        {resultLabels[result]}
      </Badge>
    )
  }

  function source(event: AdminUpstreamUpdateEvent) {
    return (
      <Link className="flex min-w-0 items-center gap-1.5 underline-offset-4 hover:underline focus-visible:outline-2" to={`${getAdminRouteHref('upstreams')}?${new URLSearchParams({ search: event.ecosystem })}`}>
        {isAdminEcosystem(event.ecosystem) && (
          <EcosystemIcon type={event.ecosystem} size={13} useColor decorative />
        )}
        <span className="shrink-0" style={{ color: 'var(--text)' }}>
          {ecosystemLabel(event.ecosystem)}
        </span>
        <span aria-hidden="true" style={{ color: 'var(--text-subtle)' }}>·</span>
        <span className="min-w-0 truncate" title={event.upstream || undefined}>
          {event.upstream || t('upstreamUpdates.unknownUpstream')}
        </span>
      </Link>
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
      actions={(<>
        {query.dataUpdatedAt > 0 && (
          <span className="text-[12px] tabular-nums text-muted-foreground" data-refresh-updated>
            {t('filters.updatedAt', { time: formatClock(new Date(query.dataUpdatedAt).toISOString(), 'time') })}
          </span>
        )}
        <Button
          type="button"
          variant="outline"
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
      </>)}
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
          <div className="flex min-w-0 gap-2 lg:contents">
            <SearchField
              className="lg:max-w-[420px]"
              type="text"
              aria-label={t('upstreamUpdates.searchLabel')}
              placeholder={t('upstreamUpdates.searchPlaceholder')}
              value={packageDraft}
              onChange={event => setPackageDraft(event.target.value)}
              onKeyDown={event => {
                if (event.key === 'Escape') {
                  setPackageDraft('')
                  event.currentTarget.blur()
                }
              }}
            />
            <Button type="submit" variant="outline" className="shrink-0 lg:order-last">
              {t('search')}
            </Button>
          </div>

          <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
            <NativeSelect
              aria-label={t('upstreamUpdates.filterResult')}
              className="min-h-10 sm:w-[150px]"
              value={resultFilter}
              onChange={event => updateFilter('result', event.target.value)}
            >
              <option value="all">{t('upstreamUpdates.allResults')}</option>
              {updateResults.map(result => (
                <option key={result} value={result}>{resultLabels[result]}</option>
              ))}
            </NativeSelect>
            <TimeRangeControl value={timeRange} onChange={setTimeRange} />
          </div>

          <div className="flex flex-wrap items-center gap-2 lg:order-last">
            {hasFilters && (
              <Button
                type="button"
                variant="ghost"
                onClick={clearFilters}
              >
                {t('upstreamUpdates.clearFilters')}
              </Button>
            )}
          </div>
        </form>
        <EcosystemChips counts={query.data?.pages[0]?.ecosystems} selected={ecosystems} onChange={setEcosystems} />

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
              <Skeleton
                key={index}
                aria-hidden="true"
                className="h-14 rounded-md"
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
          <Empty style={{ minHeight: 240 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={hasFilters ? 'search' : 'history'} size="sm" /></EmptyMedia><EmptyTitle>{hasFilters
              ? t('upstreamUpdates.noMatches')
              : t('upstreamUpdates.emptyTitle')}</EmptyTitle><EmptyDescription>{hasFilters ? t('upstreamUpdates.noMatchesHint') : t('upstreamUpdates.emptyHint')}</EmptyDescription></EmptyHeader><EmptyContent>{hasFilters ? (
              <Button type="button" variant="outline" className="min-h-10" onClick={clearFilters}>
                {t('upstreamUpdates.clearFilters')}
              </Button>
            ) : undefined}</EmptyContent></Empty>
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
                      <Link to={`${getAdminRouteHref('cacheIndexes')}?${new URLSearchParams({ package: event.package })}`}
                        className="min-w-0 break-words font-mono text-[14px] font-[500] underline-offset-4 hover:underline focus-visible:outline-2"
                        style={{ color: 'var(--text)' }}
                      >
                        {event.package}
                      </Link>
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
              <TableContainer label={t('upstreamUpdates.tableLabel')} minWidth={860}>
                <Table data-upstream-update-table className="w-full text-[13px]">
                  <TableCaption className="sr-only">{t('upstreamUpdates.tableLabel')}</TableCaption>
                  <TableHeader>
                    <TableRow className="border-b border-[var(--border)] text-left">
                      {[
                        t('upstreamUpdates.time'),
                        t('upstreamUpdates.source'),
                        t('upstreamUpdates.package'),
                        t('upstreamUpdates.outcome'),
                        t('upstreamUpdates.latestLatency'),
                      ].map(heading => (
                        <TableHead
                          key={heading}
                          scope="col"
                          className="px-3 py-2 first:pl-0"
                        >
                          {heading}
                        </TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {events.map(event => (
                      <TableRow
                        key={event.id}
                        className="border-b border-[var(--border)] transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                      >
                        <TableCell className="w-[290px] px-3 py-3 pl-0 align-top">
                          {observation(event)}
                        </TableCell>
                        <TableCell className="max-w-[220px] px-3 py-3 align-top" style={{ color: 'var(--text-muted)' }}>
                          {source(event)}
                        </TableCell>
                        <TableCell
                          className="max-w-[260px] px-3 py-3 text-left align-top font-mono font-[500]"
                          style={{ color: 'var(--text)' }}
                        >
                          <Link className="block break-words underline-offset-4 hover:underline focus-visible:outline-2" to={`${getAdminRouteHref('cacheIndexes')}?${new URLSearchParams({ package: event.package })}`}>{event.package}</Link>
                        </TableCell>
                        <TableCell className="max-w-[360px] px-3 py-3 align-top">
                          <div className="flex min-w-0 items-start gap-2">
                            {resultBadge(event.result)}
                            <span className="min-w-0 leading-[1.5]" style={{ color: 'var(--text-soft)' }}>
                              {detailLabels[event.detail] ?? event.detail}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell
                          className="whitespace-nowrap px-3 py-3 align-top font-mono tabular-nums"
                          style={{ color: latencyColor(event.latency_ms) }}
                        >
                          {event.latency_ms}ms
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </div>

            <div className="space-y-3">
              {query.isFetchNextPageError && (
                <Alert variant={"destructive"}><AlertDescription>
                  {t('upstreamUpdates.nextPageError')}
                </AlertDescription></Alert>
              )}
              {query.hasNextPage && (
                <div className="flex justify-center">
                  <Button
                    type="button"
                    variant="outline"
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
