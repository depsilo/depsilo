import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/app/button'
import BadgeV2 from '@/components/app/badge'
import EcosystemIcon from '@/components/app/ecosystem-icon'
import Icon from '@/components/app/icon'
import EmptyState from '@/components/app/empty-state'
import QueryErrorState from '@/components/app/error-state'
import SelectV2 from '@/components/app/select'
import TableViewport from '@/components/app/table-viewport'
import DrawerV2 from '@/components/app/drawer'
import InlineNotice from '@/components/app/notice'
import IconButton from '@/components/app/icon-button'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { getApiError } from '@/lib/apiError'
import { downloadBlob } from '@/lib/download'
import { copyText } from '@/lib/clipboard'
import { useAppToast } from '@/components/app/toast'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AccessLog, AccessLogDetail, AccessLogQuery } from '@/lib/adminApi.types'

function latencyColor(ms: number): string {
  if (ms < 100) return 'var(--success)'
  if (ms < 500) return 'var(--muted-foreground)'
  return 'var(--destructive)'
}

function parsePage(value: string | null): number {
  if (value === null || !/^[1-9]\d*$/.test(value)) return 1
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : 1
}

function parseDetailId(value: string | null): number | null {
  if (value === null || !/^[1-9]\d*$/.test(value)) return null
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : null
}

function canonicalizeSearchParams(current: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(current)
  const packageName = current.get('package')?.trim() ?? ''
  if (packageName) next.set('package', packageName)
  else next.delete('package')

  const ecosystem = current.get('ecosystem')?.trim() ?? ''
  if (operatorEcosystems.some(item => item.id === ecosystem)) next.set('ecosystem', ecosystem)
  else next.delete('ecosystem')

  const result = current.get('result')
  if (result === 'hit' || result === 'miss' || result === 'unknown') next.set('result', result)
  else next.delete('result')

  const page = parsePage(current.get('page'))
  if (page > 1) next.set('page', String(page))
  else next.delete('page')
  const detail = parseDetailId(current.get('detail'))
  if (detail !== null) next.set('detail', String(detail))
  else next.delete('detail')
  return next
}

export default function AccessLogsV2() {
  const { t } = useTranslation()
  const toast = useAppToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const serializedSearchParams = searchParams.toString()
  const canonicalSearchParams = canonicalizeSearchParams(searchParams)
  const serializedCanonicalSearchParams = canonicalSearchParams.toString()
  const searchParamsRef = useRef(canonicalSearchParams)
  const appliedSearch = canonicalSearchParams.get('package') ?? ''
  const adapterType = canonicalSearchParams.get('ecosystem') ?? 'all'
  const hitFilter = canonicalSearchParams.get('result') ?? 'all'
  const page = parsePage(canonicalSearchParams.get('page'))
  const [search, setSearch] = useState(appliedSearch)
  const detailId = parseDetailId(canonicalSearchParams.get('detail'))

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

  const params: AccessLogQuery = { page, page_size: 50 }
  if (appliedSearch) Object.assign(params, { search: appliedSearch })
  if (adapterType !== 'all') params.adapter_type = adapterType
  if (hitFilter === 'hit') params.hit = true
  if (hitFilter === 'miss') params.hit = false
  if (hitFilter === 'unknown') params.result = 'unknown'

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'logs', params],
    queryFn: ({ signal }) => adminApi.listLogs(params, { signal }),
    retry: false,
  })

  const items: AccessLog[] = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  const detailQuery = useQuery({
    queryKey: ['admin', 'logs', 'detail', detailId],
    queryFn: ({ signal }) => adminApi.getLogDetail(detailId as number, { signal }),
    enabled: detailId !== null,
    retry: false,
  })

  const exportMutation = useMutation({
    mutationFn: () => adminApi.exportLogs(params),
    onSuccess: (response) => {
      const filename = `depsilo-access-logs-${new Date().toISOString().slice(0, 10)}.csv`
      downloadBlob(new Blob([response.data]), filename)
      toast.show({ tone: 'success', message: t('logs.exportSuccess', { filename }) })
    },
    onError: (mutationError) => {
      toast.show({ tone: 'danger', message: t('logs.exportFailed', { reason: getApiError(mutationError).message }) })
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
    })
  }

  function setPage(nextPage: number) {
    const next = new URLSearchParams(searchParamsRef.current)
    if (nextPage <= 1) next.delete('page')
    else next.set('page', String(nextPage))
    searchParamsRef.current = next
    setSearchParams(next)
  }

  function setDetailId(nextId: number | null) {
    const next = new URLSearchParams(searchParamsRef.current)
    if (nextId === null) next.delete('detail')
    else next.set('detail', String(nextId))
    searchParamsRef.current = next
    setSearchParams(next)
  }

  async function copyDiagnosticSummary(detail: AccessLogDetail) {
    const summary = [
      `request_id: ${detail.request_id || t('logs.notRecorded')}`,
      `time: ${detail.created_at}`,
      `package: ${detail.package_name || '-'}`,
      `cache: ${detail.cache_result || t('logs.notRecorded')} (${detail.cache_reason || t('logs.notRecorded')})`,
      `policy: ${detail.policy_decision || t('logs.notRecorded')} (${detail.policy_reason || t('logs.notRecorded')})`,
      `delivery: ${detail.delivery_result || t('logs.notRecorded')} (${detail.delivery_reason || t('logs.notRecorded')})`,
      `upstream: ${detail.upstream || '-'}`,
    ].join('\n')
    if (await copyText(summary)) toast.show({ tone: 'success', message: t('logs.summaryCopied') })
  }

  const hasFilters = Boolean(appliedSearch || adapterType !== 'all' || hitFilter !== 'all')
  const cacheResult = (row: AccessLog) => row.cache_result || (row.hit ? 'hit' : 'unknown')

  return (
    <AdminPage
      description={t('logs.subtitle')}
      actions={(
        <ButtonV2
          type="button"
          variant="secondary"
          size="sm"
          aria-busy={exportMutation.isPending || undefined}
          disabled={exportMutation.isPending}
          onClick={() => exportMutation.mutate()}
        >
          <Icon name="download" size="sm" />
          {exportMutation.isPending ? t('logs.exporting') : t('logs.export')}
        </ButtonV2>
      )}
    >
      <div className="space-y-6">
      <form
        data-admin-filters
        className="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center"
        onSubmit={(event) => { event.preventDefault(); handleSearch() }}
      >
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-[4px] px-3 py-1.5 border border-border">
          <Icon name="search" size="sm" style={{ color: 'var(--muted-foreground)', flexShrink: 0 }} />
          <input
            aria-label={t('logs.searchLabel')}
            className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px] text-foreground"
            placeholder={t('logs.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        <div className="grid grid-cols-2 gap-3 sm:contents">
          <SelectV2
            className="min-h-10 sm:w-auto"
            aria-label={t('audit.ecosystem')}
            value={adapterType}
            onChange={(event) => updateParams((next) => {
              if (event.target.value === 'all') next.delete('ecosystem')
              else next.set('ecosystem', event.target.value)
            })}
          >
            <option value="all">{t('all')}</option>
            {operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
          </SelectV2>

          <SelectV2
            className="min-h-10 sm:w-auto"
            aria-label={t('audit.result')}
            value={hitFilter}
            onChange={(event) => updateParams((next) => {
              if (event.target.value === 'all') next.delete('result')
              else next.set('result', event.target.value)
            })}
          >
            <option value="all">{t('all')}</option>
            <option value="hit">{t('logs.hit')}</option>
            <option value="miss">{t('logs.miss')}</option>
            <option value="unknown">{t('logs.unknown')}</option>
          </SelectV2>
        </div>

        <ButtonV2 type="submit" variant="primary" size="sm" className="min-h-10 self-start sm:min-h-0">
          {t('search')}
        </ButtonV2>
        {hasFilters && (
          <ButtonV2 type="button" variant="secondary" size="sm" className="min-h-10 self-start sm:min-h-0" onClick={clearFilters}>
            {t('logs.clearFilters')}
          </ButtonV2>
        )}
      </form>

      {isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[13px] text-muted-foreground">
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      ) : isError && !data ? (
        <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
      ) : (
        <div className="space-y-3">
          {data && isRefetchError && <StaleDataNotice onRefresh={() => { void refetch() }} />}
          {items.length === 0 ? (
            <EmptyState icon="receipt_long" title={t('logs.noLogs')} hint={t('logs.noLogsHint')} minHeight={240} />
          ) : (
            <TableViewport label={t('logs.table')} minWidth={860}>
          <table className="w-full text-[12px]">
            <thead>
              <tr className="border-b border-border">
                {[t('logs.time'), t('type'), t('logs.packageName'), t('logs.result'), t('logs.latency'), t('logs.upstream'), t('logs.clientIp'), t('actions')].map(h => (
                  <th key={h} scope="col" className="text-left text-[11px] font-mono font-[600] uppercase py-2 px-3 first:pl-0 text-muted-foreground">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: AccessLog) => (
                <tr
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-muted border-b border-border"
                >
                  {/* Time */}
                  <td className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums text-muted-foreground">{formatTime(row.created_at)}</span>
                  </td>

                  {/* Ecosystem */}
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.adapter_type) && <EcosystemIcon type={row.adapter_type} size={13} />}
                      <span className="text-[11px] uppercase text-foreground">{row.adapter_type}</span>
                    </div>
                  </td>

                  {/* Package name + cache key */}
                  <td className="py-2 px-3 max-w-[260px]">
                    <span className="font-mono truncate block text-foreground">{row.package_name || '-'}</span>
                    <span className="font-mono text-[10px] truncate block text-muted-foreground" title={row.cache_key}>{row.cache_key}</span>
                  </td>

                  {/* Result */}
                  <td className="py-2 px-3">
                    <span title={t(cacheResult(row) === 'hit' ? 'logs.hitHint' : cacheResult(row) === 'miss' ? 'logs.missHint' : 'logs.unknownHint')}>
                      <BadgeV2 variant={cacheResult(row) === 'hit' ? 'success' : 'neutral'}>
                        {cacheResult(row) === 'hit' ? 'HIT' : cacheResult(row) === 'miss' ? 'MISS' : 'UNKNOWN'}
                      </BadgeV2>
                      <span className="sr-only">{t(cacheResult(row) === 'hit' ? 'logs.hitHint' : cacheResult(row) === 'miss' ? 'logs.missHint' : 'logs.unknownHint')}</span>
                    </span>
                  </td>

                  {/* Latency */}
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: latencyColor(row.latency_ms) }}>
                      {row.latency_ms}ms
                    </span>
                  </td>

                  {/* Upstream */}
                  <td className="py-2 px-3">
                    <span className="text-muted-foreground">{row.upstream || '-'}</span>
                  </td>

                  {/* Client IP */}
                  <td className="py-2 px-3">
                    <span className="font-mono text-muted-foreground">{row.client_ip}</span>
                  </td>

                  <td className="py-2 px-3 text-right">
                    <IconButton icon="chevron_right" label={t('logs.viewDetails')} onClick={() => setDetailId(row.id)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
            </TableViewport>
          )}
        </div>
      )}

      <AdminPagination page={page} pageSize={50} total={total} onPageChange={setPage} />
      </div>

      <DrawerV2
        open={detailId !== null}
        onOpenChange={(open) => { if (!open) setDetailId(null) }}
        title={t('logs.detailTitle')}
      >
        <div className="flex h-full flex-col gap-5 overflow-y-auto p-5 sm:p-6">
          <div className="flex items-start justify-between gap-4 pr-10">
            <div>
              <p className="font-mono text-[11px] uppercase text-muted-foreground">{t('logs.detailTitle')}</p>
              <h2 className="mt-1 break-words font-mono text-[16px] text-foreground">{detailQuery.data?.data.package_name || t('logs.loadingDetail')}</h2>
            </div>
            {detailQuery.data?.data && <IconButton icon="content_copy" label={t('logs.copySummary')} onClick={() => { void copyDiagnosticSummary(detailQuery.data!.data) }} />}
          </div>
          {detailQuery.isPending ? <div aria-busy="true" className="py-8 text-center text-[13px] text-muted-foreground">{t('loading')}</div> : detailQuery.isError ? <InlineNotice tone="danger">{getApiError(detailQuery.error).message}</InlineNotice> : detailQuery.data?.data && <DiagnosticDetail detail={detailQuery.data.data} t={t} />}
        </div>
      </DrawerV2>
    </AdminPage>
  )
}

function DiagnosticDetail({ detail, t }: { detail: AccessLogDetail; t: (key: string) => string }) {
  const value = (input: string) => input === '' || input === 'unknown' || input === 'not_recorded' || input === 'response_completion_not_recorded' ? t('logs.notRecorded') : input
  const facts = [
    [t('logs.requestId'), value(detail.request_id)],
    [t('logs.time'), formatTime(detail.created_at)],
    [t('logs.cacheFact'), `${value(detail.cache_result)} · ${value(detail.cache_reason)}`],
    [t('logs.policyFact'), `${value(detail.policy_decision)} · ${value(detail.policy_reason)}`],
    [t('logs.deliveryFact'), `${value(detail.delivery_result)} · ${value(detail.delivery_reason)}`],
    [t('logs.upstream'), value(detail.upstream)],
    [t('logs.status'), String(detail.status_code || '-')],
  ]
  return <div className="space-y-5">
    <dl className="grid gap-3 text-[12px]">
      {facts.map(([label, content]) => <div key={label} className="grid grid-cols-[minmax(0,8rem)_1fr] gap-3 border-b border-border pb-2"><dt className="text-muted-foreground">{label}</dt><dd className="min-w-0 break-words font-mono text-foreground">{content}</dd></div>)}
    </dl>
    <div>
      <h3 className="mb-2 text-[12px] font-semibold text-foreground">{t('logs.auditEvents')}</h3>
      {detail.audit_events.length === 0 ? <p className="text-[12px] text-muted-foreground">{t('logs.noAuditEvents')}</p> : <ul className="space-y-2">{detail.audit_events.map(event => <li key={event.id} className="rounded border border-border p-2 text-[11px]"><span className="font-mono">{event.action}</span> · {value(event.cache_result)} · {event.status_code || '-'} · {formatTime(event.created_at)}</li>)}</ul>}
    </div>
  </div>
}
