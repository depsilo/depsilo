import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import EmptyState from '@/components/EmptyState'
import QueryErrorState from '@/components/QueryErrorState'
import SelectV2 from '@/components/Select'
import TableViewport from '@/components/TableViewport'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { getApiError } from '@/lib/apiError'
import { downloadBlob } from '@/lib/download'
import { useAppToast } from '@/components/Toast'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AuditLog, AuditLogQuery } from '@/lib/adminApi.types'

function latencyColor(ms: number): string {
  if (ms < 100) return 'var(--ok)'
  if (ms < 500) return 'var(--text-soft)'
  return 'var(--danger)'
}

function resultBadge(result: string, t: (k: string) => string) {
  if (result === 'hit') return <BadgeV2 variant="success">{t('audit.hit')}</BadgeV2>
  if (result === 'success') return <BadgeV2 variant="success">{t('audit.success')}</BadgeV2>
  if (result === 'error') return <BadgeV2 variant="error">{t('audit.error')}</BadgeV2>
  if (result === 'miss') return <BadgeV2>{t('audit.miss')}</BadgeV2>
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
  if (result === 'hit' || result === 'miss' || result === 'success' || result === 'error') next.set('result', result)
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
    return params
  }

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'audit-logs', appliedSearch, ecosystem, resultFilter, timeRange, page],
    queryFn: () => adminApi.listAuditLogs(buildParams()),
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

  const hasFilters = Boolean(
    appliedSearch || ecosystem !== 'all' || resultFilter !== 'all' || timeRange !== 'today',
  )

  // Audit logs moved to open-source on 2026-06-28 — the page no longer
  // 402s, so there is no Pro paywall branch to render.

  const headers = [
    t('audit.time'), t('audit.ecosystem'), t('audit.packageName'), t('audit.version'),
    t('audit.action'), t('audit.result'), t('audit.latency'), t('audit.bytes'),
    t('audit.actor'), t('audit.clientIp'),
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
          <Icon name="download" size="sm" />
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
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-[4px] px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
          <input
            aria-label={t('audit.searchLabel')}
            className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]"
            style={{ color: 'var(--text)' }}
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
              className="px-2.5 py-1 text-[11px] rounded-[4px] transition-[background,color,border-color,transform] duration-150 cursor-pointer active:scale-[0.96]"
              style={{
                background: timeRange === r ? 'var(--btn)' : 'transparent',
                color: timeRange === r ? 'var(--btn-fg)' : 'var(--text-soft)',
                border: timeRange === r ? 'none' : '1px solid var(--border)',
              }}
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

      {/* Table — bare */}
      <TableViewport label={t('audit.table')} minWidth={1180}>
        {isPending ? (
          <div aria-busy="true" className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}><span aria-hidden="true">{t('loading')}</span></div>
        ) : isError && !data ? (
          <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
        ) : (
          <div>
          {data && isRefetchError && (
            <StaleDataNotice onRefresh={() => { void refetch() }} />
          )}
          {items.length === 0 ? (
            <EmptyState icon="receipt_long" title={t('audit.noLogs')} hint={t('audit.noLogsHint')} minHeight={240} />
          ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {headers.map(h => (
                  <th key={h} scope="col" className="text-left text-[11px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: AuditLog) => (
                <tr
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <td className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatTime(row.created_at)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.ecosystem) && <EcosystemIcon type={row.ecosystem} size={13} />}
                      <span className="text-[11px] uppercase" style={{ color: 'var(--text)' }}>{row.ecosystem}</span>
                    </div>
                  </td>
                  <td className="py-2 px-3 max-w-[220px]">
                    <span className="font-mono truncate block" style={{ color: 'var(--text)' }}>{row.package_name || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono" style={{ color: 'var(--text-soft)' }}>{row.version || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono whitespace-nowrap" style={{ color: 'var(--text)' }}>{row.action || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    {resultBadge(row.cache_result, t)}
                  </td>
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: latencyColor(row.latency_ms) }}>{row.latency_ms}ms</span>
                  </td>
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatBytes(row.bytes_sent)}</span>
                  </td>
                  <td className="py-2 px-3 max-w-[180px]">
                    <span className="font-mono truncate block" title={row.user_agent || undefined} style={{ color: 'var(--text-soft)' }}>{row.user_agent || '-'}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono" style={{ color: 'var(--text-soft)' }}>{row.client_ip || '-'}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          )}
          </div>
        )}
      </TableViewport>

      {/* Pagination */}
      <AdminPagination page={page} pageSize={50} total={total} onPageChange={setPage} />
      </div>
    </AdminPage>
  )
}
