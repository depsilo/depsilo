import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { isKnownEcosystemId } from '@/admin/operatorEcosystems'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import SearchField from '@/components/SearchField'

import QueryErrorState from '@/components/QueryErrorState'
import { NativeSelect } from '@/components/ui/native-select'

import AdminPage from '@/admin/components/AdminPage'
import SiemExportersSection from '@/admin/components/SiemExportersSection'
import AuditIntegrityNotice from '@/admin/components/AuditIntegrityNotice'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { getApiError } from '@/lib/apiError'
import { downloadBlob } from '@/lib/download'
import { useAppToast } from '@/components/Toast'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AuditLog, AuditLogQuery } from '@/lib/adminApi.types'
import { ecosystemLabel } from '@/admin/operatorEcosystems'
import EcosystemChips from '@/admin/components/EcosystemChips'
import RefreshControl from '@/admin/components/RefreshControl'
import TimeRangeControl from '@/admin/components/TimeRangeControl'
import { parseEcosystemList, parseTimeRange, resolveTimeWindow, serializeEcosystemList, writeTimeRange, type TimePreset, type TimeRangeValue } from '@/admin/listFilters'

function resultBadge(result: string, t: (k: string) => string) {
  if (result === 'hit') return <Badge variant="success">{t('audit.hit')}</Badge>
  if (result === 'success') return <Badge variant="success">{t('audit.success')}</Badge>
  if (result === 'error') return <Badge variant="destructive">{t('audit.error')}</Badge>
  if (result === 'blocked') return <Badge variant="warning">{t('audit.blocked')}</Badge>
  if (result === 'miss') return <Badge>{t('audit.miss')}</Badge>
  return <Badge>{result || '-'}</Badge>
}

/** The audit log opens on the last day, as its former "today" did. */
const DEFAULT_RANGE: TimePreset = '24h'

function parsePage(value: string | null): number {
  if (value === null || !/^[1-9]\d*$/.test(value)) return 1
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : 1
}

function canonicalizeSearchParams(current: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(current)
  const packageName = current.get('package')?.trim() ?? ''
  if (packageName) next.set('package', packageName)
  else next.delete('package')

  const ecosystems = serializeEcosystemList(parseEcosystemList(current.get('ecosystem'), id => id === 'admin' || isKnownEcosystemId(id)))
  if (ecosystems) next.set('ecosystem', ecosystems)
  else next.delete('ecosystem')

  const result = current.get('result')
  if (result === 'hit' || result === 'miss' || result === 'success' || result === 'error' || result === 'blocked') next.set('result', result)
  else next.delete('result')

  writeTimeRange(next, parseTimeRange(current, DEFAULT_RANGE), DEFAULT_RANGE)

  const page = parsePage(current.get('page'))
  if (page > 1) next.set('page', String(page))
  else next.delete('page')
  return next
}

export default function AuditLogs() {
  const { t } = useTranslation()
  const toast = useAppToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const serializedSearchParams = searchParams.toString()
  const canonicalSearchParams = canonicalizeSearchParams(searchParams)
  const serializedCanonicalSearchParams = canonicalSearchParams.toString()
  const searchParamsRef = useRef(canonicalSearchParams)
  const appliedSearch = canonicalSearchParams.get('package') ?? ''
  const ecosystems = parseEcosystemList(canonicalSearchParams.get('ecosystem'), id => id === 'admin' || isKnownEcosystemId(id))
  const resultFilter = canonicalSearchParams.get('result') ?? 'all'
  const timeRange = parseTimeRange(canonicalSearchParams, DEFAULT_RANGE)
  // Presets count back from this moment; refreshing moves it to now.
  const [anchor, setAnchor] = useState(() => Date.now())
  const page = parsePage(canonicalSearchParams.get('page'))
  const [search, setSearch] = useState(appliedSearch)
  const [selectedEvent, setSelectedEvent] = useState<AuditLog | null>(null)
  const deliveryView = searchParams.get('view') === 'delivery'
  function setView(delivery: boolean) {
    setSearchParams(current => { const next = new URLSearchParams(current); if (delivery) next.set('view', 'delivery'); else next.delete('view'); return next })
  }


  useEffect(() => {
    const next = new URLSearchParams(serializedCanonicalSearchParams)
    searchParamsRef.current = next
    if (serializedSearchParams !== serializedCanonicalSearchParams) {
      setSearchParams(next, { replace: true })
    }
  }, [serializedCanonicalSearchParams, serializedSearchParams, setSearchParams])

  useEffect(() => {
    setSearch(appliedSearch)
  }, [appliedSearch])

  function buildParams(): AuditLogQuery {
    const timeWindow = resolveTimeWindow(timeRange, anchor)
    const params: AuditLogQuery = { page, page_size: 50, ...timeWindow }
    if (appliedSearch) params.package = appliedSearch
    const ecosystemFilter = serializeEcosystemList(ecosystems)
    if (ecosystemFilter) params.ecosystem = ecosystemFilter
    if (resultFilter === 'hit') params.result = 'hit'
    if (resultFilter === 'miss') params.result = 'miss'
    if (resultFilter === 'success') params.result = 'success'
    if (resultFilter === 'error') params.result = 'error'
    if (resultFilter === 'blocked') params.result = 'blocked'
    return params
  }

  const { data, error, isPending, isError, isSuccess, isFetching, dataUpdatedAt, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'audit-logs', buildParams()],
    queryFn: async ({ signal }) => {
      const appliedParams = buildParams()
      const response = await adminApi.listAuditLogs(appliedParams, { signal })
      return { ...response, appliedParams }
    },
    retry: false,
  })

  const items = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  const exportMutation = useMutation({
    mutationFn: () => adminApi.exportAuditLogs(data?.appliedParams ?? buildParams()),
    onSuccess: (response) => {
      const filename = `depsilo-audit-${new Date().toISOString().slice(0, 10)}.csv`
      downloadBlob(new Blob([response.data]), filename)
      toast.show({ tone: 'success', message: t('audit.exportSuccess', { filename }) })
    },
    onError: (mutationError) => {
      toast.show({ tone: 'danger', message: t('audit.exportFailed', { reason: getApiError(mutationError).message }) })
    },
  })

  function updateParams(mutator: (next: URLSearchParams) => void) {
    const next = new URLSearchParams(searchParamsRef.current)
    mutator(next)
    next.delete('page')
    searchParamsRef.current = next
    setSearchParams(next)
  }

  function handleSearch() {
    updateParams((next) => {
      const normalized = search.trim()
      if (normalized) next.set('package', normalized)
      else next.delete('package')
    })
  }

  function clearFilters() {
    setSearch('')
    updateParams((next) => {
      next.delete('package')
      next.delete('ecosystem')
      next.delete('result')
      next.delete('range')
    })
  }

  function setPage(nextPage: number) {
    const next = new URLSearchParams(searchParamsRef.current)
    if (nextPage <= 1) next.delete('page')
    else next.set('page', String(nextPage))
    searchParamsRef.current = next
    setSearchParams(next)
  }

  function refreshRecords() {
    // A relative window moves to now (a new query); a fixed one refetches.
    if (timeRange.preset !== 'custom' && timeRange.preset !== 'all') setAnchor(Date.now())
    else void refetch()
  }

  function setEcosystems(ids: string[]) {
    updateParams((next) => {
      const value = serializeEcosystemList(ids)
      if (value) next.set('ecosystem', value)
      else next.delete('ecosystem')
    })
  }

  function setTimeRange(value: TimeRangeValue) {
    setAnchor(Date.now())
    updateParams((next) => writeTimeRange(next, value, DEFAULT_RANGE))
  }

  const hasFilters = Boolean(
    appliedSearch || ecosystems.length > 0 || resultFilter !== 'all' || timeRange.preset !== DEFAULT_RANGE,
  )

  // Audit logs moved to open-source on 2026-06-28 — the page no longer
  // 402s, so there is no Pro paywall branch to render.

  const headers = [t('audit.time'), t('audit.packageName'), t('audit.action'), t('audit.result'), t('audit.actor'), t('audit.eventDetails')]
  function actionLabel(action: string) {
    const key = `audit.actions.${action.replaceAll('.', '_')}`
    return t(key, { defaultValue: action || '—' })
  }

  return (
    <AdminPage
      description={t('audit.subtitle')}
      actions={<>
        <Button variant="outline" size="sm" onClick={() => setView(!deliveryView)}>{t(deliveryView ? 'audit.records' : 'audit.delivery')}</Button>
        {!deliveryView && <>
        <RefreshControl updatedAt={dataUpdatedAt} fetching={isFetching} onRefresh={refreshRecords} />
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-busy={exportMutation.isPending || undefined}
          disabled={exportMutation.isPending || !data || isFetching}
          onClick={() => exportMutation.mutate()}
        >
          <Icon name="download" size="sm" />
          {exportMutation.isPending ? t('audit.exporting') : t('audit.exportCsv')}
        </Button></>}
      </>}
    >
      {deliveryView ? <SiemExportersSection /> : <div className="space-y-5">
      <AuditIntegrityNotice />
      <form
        data-admin-filters
        className="flex flex-col items-stretch gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:gap-3"
        onSubmit={(event) => { event.preventDefault(); handleSearch() }}
      >
        <div className="flex min-w-0 gap-2 sm:contents">
          <SearchField
            aria-label={t('audit.searchLabel')}
            placeholder={t('audit.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Button type="submit" variant="outline" className="shrink-0 sm:order-last">
            {t('search')}
          </Button>
        </div>

        <div className="flex min-w-0 flex-col gap-2 sm:contents">
            <NativeSelect
              className="min-h-10 sm:w-auto"
              aria-label={t('audit.result')}
              value={resultFilter}
              onChange={(event) => updateParams((next) => {
                if (event.target.value === 'all') next.delete('result')
                else next.set('result', event.target.value)
              })}
            >
              <option value="all">{t('all')}</option>
              <option value="hit">{t('audit.hit')}</option>
              <option value="miss">{t('audit.miss')}</option>
              <option value="success">{t('audit.success')}</option>
              <option value="error">{t('audit.error')}</option>
              <option value="blocked">{t('audit.blocked')}</option>
            </NativeSelect>
            <TimeRangeControl value={timeRange} onChange={setTimeRange} />
        </div>


        {hasFilters && (
          <Button type="button" variant="ghost" className="sm:order-last" onClick={clearFilters}>
            {t('audit.clearFilters')}
          </Button>
        )}
      </form>
      <EcosystemChips
        counts={data?.data.ecosystems}
        selected={ecosystems}
        onChange={setEcosystems}
        labelFor={id => (id === 'admin' ? t('audit.management') : undefined)}
      />

      {isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      ) : isError && !data ? (
        <QueryErrorState message={errorMessage} onRetry={refreshRecords} />
      ) : (
        <div className="space-y-3">
          {data && isRefetchError && <StaleDataNotice onRefresh={() => { void refetch() }} />}
          {items.length === 0 ? (
            <Empty style={{ minHeight: 240 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"receipt_long"} size="sm" /></EmptyMedia><EmptyTitle>{hasFilters ? t('logs.noMatches') : t('audit.noLogs')}</EmptyTitle>{t('audit.noLogsHint') && <EmptyDescription>{hasFilters ? t('logs.noMatchesHint') : t('audit.noLogsHint')}</EmptyDescription>}</EmptyHeader></Empty>
          ) : (
            <TableContainer label={t('audit.table')} minWidth={720}>
          <Table className="w-full text-[13px]">
            <TableHeader>
              <TableRow style={{ borderBottom: '1px solid var(--border)' }}>
                {headers.map(h => (
                  <TableHead key={h} scope="col" className="text-left py-2 px-3 first:pl-0">
                    {h}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((row: AuditLog) => (
                <TableRow
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <TableCell className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatTime(row.created_at)}</span>
                  </TableCell>
                  <TableCell className="py-2 px-3 max-w-[260px]">
                    <span className="block break-words font-mono">{row.package_name || '—'}{row.version ? `@${row.version}` : ''}</span>
                    <span className="mt-1 flex items-center gap-1 text-[12px] text-muted-foreground">{isAdminEcosystem(row.ecosystem) && <EcosystemIcon type={row.ecosystem} size={13} />}{ecosystemLabel(row.ecosystem)}</span>
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    <span>{actionLabel(row.action)}</span>
                    {actionLabel(row.action) !== row.action && <code className="mt-1 block text-[12px] text-muted-foreground">{row.action}</code>}
                  </TableCell>
                  <TableCell className="py-2 px-3">{resultBadge(row.cache_result, t)}</TableCell>
                  <TableCell className="py-2 px-3 max-w-[180px] break-words text-muted-foreground">{row.user_agent || '—'}</TableCell>
                  <TableCell className="py-2 px-3 text-right"><Button variant="ghost" size="icon" aria-label={t('audit.viewDetails')} onClick={() => setSelectedEvent(row)}><Icon name="chevron_right" size="sm" /></Button></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
            </TableContainer>
          )}
        </div>
      )}

      {/* Pagination */}
      <AdminPagination page={page} pageSize={50} total={total} ready={isSuccess && !isFetching} onPageChange={setPage} />

      <p className="text-[12px] text-muted-foreground">{t('audit.exportScope')}</p>
      </div>}
      <Sheet open={selectedEvent !== null} onOpenChange={open => { if (!open) setSelectedEvent(null) }}>
        <SheetContent className="w-[min(580px,100vw)] overflow-y-auto p-6">
          <SheetTitle className="pr-10">{t('audit.eventDetails')}</SheetTitle>
          {selectedEvent && <dl className="grid gap-3 text-sm">
            {[
              [t('audit.time'), new Date(selectedEvent.created_at).toLocaleString()],
              [t('audit.packageName'), selectedEvent.package_name],
              [t('audit.version'), selectedEvent.version],
              [t('audit.ecosystem'), selectedEvent.ecosystem],
              [t('audit.action'), `${actionLabel(selectedEvent.action)} (${selectedEvent.action})`],
              [t('audit.result'), selectedEvent.cache_result],
              [t('audit.actor'), selectedEvent.user_agent],
              [t('audit.clientIp'), selectedEvent.client_ip],
              [t('logs.upstream'), selectedEvent.upstream_url],
              [t('logs.status'), selectedEvent.status_code || '—'],
              [t('audit.latency'), `${selectedEvent.latency_ms} ms`],
              [t('audit.bytes'), formatBytes(selectedEvent.bytes_sent)],
            ].map(([label, value]) => <div key={label} className="grid min-w-0 gap-1 border-b border-border pb-3 sm:grid-cols-[120px_minmax(0,1fr)]"><dt className="text-muted-foreground">{label}</dt><dd className="break-words [overflow-wrap:anywhere]">{value || '—'}</dd></div>)}
          </dl>}
        </SheetContent>
      </Sheet>
    </AdminPage>
  )
}
