import { Download, ReceiptText, Search } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/app/button'
import BadgeV2 from '@/components/app/badge'
import { cacheTone, deliveryTone, policyTone } from '@/lib/status'
import EcosystemIcon from '@/components/app/ecosystem-icon'
import EmptyState from '@/components/app/empty-state'
import QueryErrorState from '@/components/app/error-state'
import SelectV2 from '@/components/app/select'
import TableViewport from '@/components/app/table-viewport'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { getApiError } from '@/lib/apiError'
import { downloadBlob } from '@/lib/download'
import { useAppToast } from '@/components/app/toast'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AuditLog, AuditLogQuery } from '@/lib/adminApi.types'

function latencyColor(ms: number): string {
  if (ms < 100) return 'var(--success)'
  if (ms < 500) return 'var(--muted-foreground)'
  return 'var(--destructive)'
}

function resultBadge(result: string, t: (k: string) => string) {
  // The shared vocabulary, not a local opinion: a refusal is destructive, a
  // miss is neutral, and only a real failure is a failure.
  if (result === 'blocked') return <BadgeV2 variant={policyTone('deny')}>{t('audit.blocked')}</BadgeV2>
  if (result === 'hit') return <BadgeV2 variant={cacheTone('hit')}>{t('audit.hit')}</BadgeV2>
  if (result === 'miss') return <BadgeV2 variant={cacheTone('miss')}>{t('audit.miss')}</BadgeV2>
  if (result === 'error') return <BadgeV2 variant={deliveryTone('failed')}>{t('audit.error')}</BadgeV2>
  if (result === 'success') return <BadgeV2 variant={deliveryTone('completed')}>{t('audit.success')}</BadgeV2>
  return <BadgeV2>{result || '-'}</BadgeV2>
}

function getTimeRange(preset: string) {
  const now = new Date(); const end = now.toISOString(); const start = new Date(now)
  if (preset === 'today') start.setHours(0, 0, 0, 0)
  else if (preset === '7d') start.setDate(start.getDate() - 7)
  else if (preset === '30d') start.setDate(start.getDate() - 30)
  return { start: start.toISOString(), end }
}

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

  const ecosystem = current.get('ecosystem')?.trim() ?? ''
  if (ecosystem === 'admin' || operatorEcosystems.some(item => item.id === ecosystem)) next.set('ecosystem', ecosystem)
  else next.delete('ecosystem')

  const result = current.get('result')
  if (result === 'hit' || result === 'miss' || result === 'success' || result === 'error' || result === 'blocked') next.set('result', result)
  else next.delete('result')

  const range = current.get('range')
  if (range === '7d' || range === '30d') next.set('range', range)
  else next.delete('range')

  const page = parsePage(current.get('page'))
  if (page > 1) next.set('page', String(page))
  else next.delete('page')
  return next
}

export default function AuditLogsV2() {
  const { t } = useTranslation()
  const toast = useAppToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const serializedSearchParams = searchParams.toString()
  const canonicalSearchParams = canonicalizeSearchParams(searchParams)
  const serializedCanonicalSearchParams = canonicalSearchParams.toString()
  const searchParamsRef = useRef(canonicalSearchParams)
  const appliedSearch = canonicalSearchParams.get('package') ?? ''
  const ecosystem = canonicalSearchParams.get('ecosystem') ?? 'all'
  const resultFilter = canonicalSearchParams.get('result') ?? 'all'
  const timeRange = canonicalSearchParams.get('range') ?? 'today'
  const page = parsePage(canonicalSearchParams.get('page'))
  const [search, setSearch] = useState(appliedSearch)

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
    const range = getTimeRange(timeRange)
    const params: AuditLogQuery = { page, page_size: 50, start: range.start, end: range.end }
    if (appliedSearch) params.package = appliedSearch
    if (ecosystem !== 'all') params.ecosystem = ecosystem
    if (resultFilter === 'hit') params.result = 'hit'
    if (resultFilter === 'miss') params.result = 'miss'
    if (resultFilter === 'success') params.result = 'success'
    if (resultFilter === 'error') params.result = 'error'
    if (resultFilter === 'blocked') params.result = 'blocked'
    return params
  }

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'audit-logs', appliedSearch, ecosystem, resultFilter, timeRange, page],
    queryFn: ({ signal }) => adminApi.listAuditLogs(buildParams(), { signal }),
    retry: false,
  })

  const items = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  const exportMutation = useMutation({
    mutationFn: () => adminApi.exportAuditLogs(buildParams()),
    onSuccess: (response) => {
      const filename = `depsilo-audit-${new Date().toISOString().slice(0, 10)}.csv`
      downloadBlob(new Blob([response.data]), filename)
      toast.show({ tone: 'success', message: t('audit.exportSuccess', { filename }) })
    },
    onError: (mutationError) => {
      toast.show({ tone: 'destructive', message: t('audit.exportFailed', { reason: getApiError(mutationError).message }) })
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

  const hasFilters = Boolean(
    appliedSearch || ecosystem !== 'all' || resultFilter !== 'all' || timeRange !== 'today',
  )

  // Audit logs moved to open-source on 2026-06-28 — the page no longer
  // 402s, so there is no Pro paywall branch to render.

  const headers: Array<{ label: string; align?: 'end' }> = [
    { label: t('audit.time') }, { label: t('audit.ecosystem') },
    { label: t('audit.packageName') }, { label: t('audit.version') },
    { label: t('audit.action') }, { label: t('audit.result') },
    { label: t('audit.latency'), align: 'end' }, { label: t('audit.bytes'), align: 'end' },
    { label: t('audit.actor') }, { label: t('audit.clientIp') },
  ]

  return (
    <AdminPage
      description={t('audit.subtitle')}
      actions={(
        <ButtonV2
          type="button"
          variant="secondary"
          size="sm"
          aria-busy={exportMutation.isPending || undefined}
          disabled={exportMutation.isPending}
          onClick={() => exportMutation.mutate()}
        >
          <Download className="icon icon-sm" aria-hidden="true" />
          {exportMutation.isPending ? t('audit.exporting') : t('audit.exportCsv')}
        </ButtonV2>
      )}
    >
      <div className="space-y-6">
      <form
        data-admin-filters
        className="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center"
        onSubmit={(event) => { event.preventDefault(); handleSearch() }}
      >
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-sm px-3 py-1.5 border border-border">
          <Search className="icon icon-sm shrink-0 text-muted-foreground" aria-hidden="true" />
          <input
            aria-label={t('audit.searchLabel')}
            className="min-w-0 flex-1 bg-transparent text-field outline-none md:text-body text-foreground"
            placeholder={t('audit.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        <div className="grid grid-cols-2 gap-3 sm:contents">
          <SelectV2
            className="min-h-10 sm:w-auto"
            aria-label={t('audit.ecosystem')}
            value={ecosystem}
            onChange={(event) => updateParams((next) => {
              if (event.target.value === 'all') next.delete('ecosystem')
              else next.set('ecosystem', event.target.value)
            })}
          >
            <option value="all">{t('all')}</option>
            <option value="admin">{t('audit.management')}</option>
            {operatorEcosystems.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
          </SelectV2>

          <SelectV2
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
          </SelectV2>
        </div>

        <fieldset className="flex min-h-10 items-center gap-1" aria-label={t('audit.timeRange')}>
          <legend className="sr-only">{t('audit.timeRange')}</legend>
          {(['today', '7d', '30d'] as const).map(r => (
            <button
              type="button"
              key={r}
              onClick={() => updateParams((next) => {
                if (r === 'today') next.delete('range')
                else next.set('range', r)
              })}
              aria-pressed={timeRange === r}
              className={`cursor-pointer rounded-sm px-2.5 py-1 text-meta transition-[background,color,border-color,transform] duration-150 active:scale-[0.96] ${timeRange === r ? 'bg-primary text-primary-foreground' : 'border border-border bg-transparent text-muted-foreground'}`}
            >
              {r === 'today' ? t('audit.today') : r === '7d' ? t('audit.days7') : t('audit.days30')}
            </button>
          ))}
        </fieldset>

        <ButtonV2 type="submit" variant="primary" size="sm" className="min-h-10 self-start sm:min-h-0">
          {t('search')}
        </ButtonV2>
        {hasFilters && (
          <ButtonV2 type="button" variant="secondary" size="sm" className="min-h-10 self-start sm:min-h-0" onClick={clearFilters}>
            {t('audit.clearFilters')}
          </ButtonV2>
        )}
      </form>

      {isPending ? (
        <div aria-busy="true" className="py-8 text-center text-body text-muted-foreground">
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      ) : isError && !data ? (
        <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
      ) : (
        <div className="space-y-3">
          {data && isRefetchError && <StaleDataNotice onRefresh={() => { void refetch() }} />}
          {items.length === 0 ? (
            <EmptyState icon={ReceiptText} title={t('audit.noLogs')} hint={t('audit.noLogsHint')} minHeight={240} />
          ) : (
            <TableViewport label={t('audit.table')} minWidth={1180}>
          <table className="w-full text-label">
            <thead>
              <tr className="border-b border-border">
                {headers.map(h => (
                  <th key={h.label} scope="col" className={`text-meta font-mono font-semibold uppercase py-2 px-3 first:pl-0 text-muted-foreground ${h.align === 'end' ? 'text-right' : 'text-left'}`}>
                    {h.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: AuditLog) => (
                <tr
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-muted border-b border-border"
                >
                  <td className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums text-muted-foreground">{formatTime(row.created_at)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.ecosystem) && <EcosystemIcon type={row.ecosystem} size={13} />}
                      <span className="text-meta uppercase text-foreground">{row.ecosystem}</span>
                    </div>
                  </td>
                  <td className="py-2 px-3 max-w-[220px]">
                    <span className="font-mono truncate block text-foreground">{row.package_name || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono text-muted-foreground">{row.version || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono whitespace-nowrap text-foreground">{row.action || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    {resultBadge(row.cache_result, t)}
                  </td>
                  <td className="py-2 px-3 text-right whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: latencyColor(row.latency_ms) }}>{row.latency_ms}ms</span>
                  </td>
                  <td className="py-2 px-3 text-right whitespace-nowrap">
                    <span className="font-mono tabular-nums text-muted-foreground">{formatBytes(row.bytes_sent)}</span>
                  </td>
                  <td className="py-2 px-3 max-w-[180px]">
                    <span className="font-mono truncate block text-muted-foreground" title={row.user_agent || undefined}>{row.user_agent || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono text-muted-foreground">{row.client_ip || '-'}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
            </TableViewport>
          )}
        </div>
      )}

      {/* Pagination */}
      <AdminPagination page={page} pageSize={50} total={total} onPageChange={setPage} />
      </div>
    </AdminPage>
  )
}
