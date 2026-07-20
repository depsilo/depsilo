import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
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
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AuditLog, AuditLogQuery } from '@/lib/adminApi.types'

function latencyColor(ms: number): string {
  if (ms < 100) return 'var(--ok)'
  if (ms < 500) return 'var(--text-soft)'
  return 'var(--danger)'
}

function resultBadge(result: string, t: (k: string) => string) {
  if (result === 'hit') return <BadgeV2 variant="success">{t('audit.hit')}</BadgeV2>
  if (result === 'error') return <BadgeV2 variant="error">{t('audit.error')}</BadgeV2>
  return <BadgeV2>{t('audit.miss')}</BadgeV2>
}

function getTimeRange(preset: string) {
  const now = new Date(); const end = now.toISOString(); const start = new Date(now)
  if (preset === 'today') start.setHours(0, 0, 0, 0)
  else if (preset === '7d') start.setDate(start.getDate() - 7)
  else if (preset === '30d') start.setDate(start.getDate() - 30)
  return { start: start.toISOString(), end }
}

export default function AuditLogsV2() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [ecosystem, setEcosystem] = useState('all')
  const [resultFilter, setResultFilter] = useState('all')
  const [timeRange, setTimeRange] = useState('today')
  const [page, setPage] = useState(1)
  const [appliedSearch, setAppliedSearch] = useState('')
  const [appliedEcosystem, setAppliedEcosystem] = useState('all')
  const [appliedResult, setAppliedResult] = useState('all')
  const [appliedTimeRange, setAppliedTimeRange] = useState('today')

  const range = getTimeRange(appliedTimeRange)
  const params: AuditLogQuery = { page, page_size: 50, start: range.start, end: range.end }
  if (appliedSearch) params.package = appliedSearch
  if (appliedEcosystem !== 'all') params.ecosystem = appliedEcosystem
  if (appliedResult === 'hit') params.result = 'hit'
  if (appliedResult === 'miss') params.result = 'miss'
  if (appliedResult === 'error') params.result = 'error'

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'audit-logs', appliedSearch, appliedEcosystem, appliedResult, appliedTimeRange, page],
    queryFn: () => adminApi.listAuditLogs(params),
    retry: false,
  })

  const items = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  function handleSearch() {
    setAppliedSearch(search); setAppliedEcosystem(ecosystem)
    setAppliedResult(resultFilter); setAppliedTimeRange(timeRange); setPage(1)
  }

  function handleExport() {
    adminApi.exportAuditLogs(params).then(res => {
      const url = URL.createObjectURL(new Blob([res.data]))
      const a = document.createElement('a')
      a.href = url; a.download = `depsilo-audit-${new Date().toISOString().slice(0, 10)}.csv`
      a.click(); URL.revokeObjectURL(url)
    })
  }

  // Audit logs moved to open-source on 2026-06-28 — the page no longer
  // 402s, so there is no Pro paywall branch to render.

  const headers = [
    t('audit.time'), t('audit.ecosystem'), t('audit.packageName'), t('audit.version'),
    t('audit.result'), t('audit.latency'), t('audit.bytes'), t('audit.clientIp'),
  ]

  return (
    <AdminPage
      description={t('audit.subtitle')}
      actions={(
        <ButtonV2 type="button" variant="secondary" size="sm" onClick={handleExport}>
          <Icon name="download" size="sm" />
          {t('audit.exportCsv')}
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
          <SelectV2 className="min-h-10 sm:w-auto" aria-label={t('audit.ecosystem')} value={ecosystem} onChange={(e) => setEcosystem(e.target.value)}>
            <option value="all">{t('all')}</option>
            {operatorEcosystems.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
          </SelectV2>

          <SelectV2 className="min-h-10 sm:w-auto" aria-label={t('audit.result')} value={resultFilter} onChange={(e) => setResultFilter(e.target.value)}>
            <option value="all">{t('all')}</option>
            <option value="hit">{t('audit.hit')}</option>
            <option value="miss">{t('audit.miss')}</option>
            <option value="error">{t('audit.error')}</option>
          </SelectV2>
        </div>

        <fieldset className="flex min-h-10 items-center gap-1" aria-label={t('audit.timeRange')}>
          <legend className="sr-only">{t('audit.timeRange')}</legend>
          {(['today', '7d', '30d'] as const).map(r => (
            <button
              type="button"
              key={r}
              onClick={() => setTimeRange(r)}
              aria-pressed={timeRange === r}
              className="px-2.5 py-1 text-[11px] rounded-[4px] transition-[background,color,border-color,transform] duration-150 cursor-pointer active:scale-[0.96]"
              style={{
                background: timeRange === r ? 'var(--btn-primary-bg)' : 'transparent',
                color: timeRange === r ? 'white' : 'var(--text-soft)',
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
      </form>

      {/* Table — bare */}
      <TableViewport label={t('audit.table')} minWidth={980}>
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
            <EmptyState icon="receipt_long" title={t('audit.noLogs')} minHeight={200} />
          ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {headers.map(h => (
                  <th key={h} scope="col" className="text-left text-[10px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>
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
                    {resultBadge(row.cache_result, t)}
                  </td>
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: latencyColor(row.latency_ms) }}>{row.latency_ms}ms</span>
                  </td>
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatBytes(row.bytes_sent)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono" style={{ color: 'var(--text-soft)' }}>{row.client_ip}</span>
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
