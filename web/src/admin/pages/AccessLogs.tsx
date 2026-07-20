import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatTime } from '@/lib/utils'
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
import type { AccessLog, AccessLogQuery } from '@/lib/adminApi.types'

function latencyColor(ms: number): string {
  if (ms < 100) return 'var(--ok)'
  if (ms < 500) return 'var(--text-soft)'
  return 'var(--danger)'
}

export default function AccessLogsV2() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [hitFilter, setHitFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [appliedSearch, setAppliedSearch] = useState('')

  const params: AccessLogQuery = { page, page_size: 50 }
  if (appliedSearch) Object.assign(params, { search: appliedSearch })
  if (adapterType !== 'all') params.adapter_type = adapterType
  if (hitFilter === 'hit') params.hit = true
  if (hitFilter === 'miss') params.hit = false

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'logs', params],
    queryFn: () => adminApi.listLogs(params),
    retry: false,
  })

  const items: AccessLog[] = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  function handleSearch() {
    setAppliedSearch(search)
    setPage(1)
  }

  function handleExport() {
    void adminApi.exportLogs(params).then(res => {
      const url = URL.createObjectURL(new Blob([res.data]))
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = `depsilo-access-logs-${new Date().toISOString().slice(0, 10)}.csv`
      anchor.click()
      URL.revokeObjectURL(url)
    })
  }

  return (
    <AdminPage
      description={t('logs.subtitle')}
      actions={(
        <ButtonV2 type="button" variant="secondary" size="sm" onClick={handleExport}>
          <Icon name="download" size="sm" />
          {t('logs.export')}
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
            aria-label={t('logs.searchLabel')}
            className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]"
            style={{ color: 'var(--text)' }}
            placeholder={t('logs.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        <div className="grid grid-cols-2 gap-3 sm:contents">
          <SelectV2 className="min-h-10 sm:w-auto" aria-label={t('audit.ecosystem')} value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }}>
            <option value="all">{t('all')}</option>
            {operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
          </SelectV2>

          <SelectV2 className="min-h-10 sm:w-auto" aria-label={t('audit.result')} value={hitFilter} onChange={(e) => { setHitFilter(e.target.value); setPage(1) }}>
            <option value="all">{t('all')}</option>
            <option value="hit">{t('logs.hit')}</option>
            <option value="miss">{t('logs.miss')}</option>
          </SelectV2>
        </div>

        <ButtonV2 type="submit" variant="primary" size="sm" className="min-h-10 self-start sm:min-h-0">
          {t('search')}
        </ButtonV2>
      </form>

      {/* Table — bare */}
      <TableViewport
        label={t('logs.table')}
        minWidth={860}
      >
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
            <EmptyState icon="receipt_long" title={t('logs.noLogs')} minHeight={200} />
          ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {[t('logs.time'), t('type'), t('logs.packageName'), t('logs.result'), t('logs.latency'), t('logs.upstream'), t('logs.clientIp')].map(h => (
                  <th key={h} scope="col" className="text-left text-[10px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: AccessLog) => (
                <tr
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  {/* Time */}
                  <td className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatTime(row.created_at)}</span>
                  </td>

                  {/* Ecosystem */}
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.adapter_type) && <EcosystemIcon type={row.adapter_type} size={13} />}
                      <span className="text-[11px] uppercase" style={{ color: 'var(--text)' }}>{row.adapter_type}</span>
                    </div>
                  </td>

                  {/* Package name + cache key */}
                  <td className="py-2 px-3 max-w-[260px]">
                    <span className="font-mono truncate block" style={{ color: 'var(--text)' }}>{row.package_name || '-'}</span>
                    <span className="font-mono text-[10px] truncate block" style={{ color: 'var(--text-soft)' }} title={row.cache_key}>{row.cache_key}</span>
                  </td>

                  {/* Result */}
                  <td className="py-2 px-3">
                    <BadgeV2 variant={row.hit ? 'success' : 'error'}>
                      {row.hit ? 'HIT' : 'MISS'}
                    </BadgeV2>
                  </td>

                  {/* Latency */}
                  <td className="py-2 px-3 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: latencyColor(row.latency_ms) }}>
                      {row.latency_ms}ms
                    </span>
                  </td>

                  {/* Upstream */}
                  <td className="py-2 px-3">
                    <span style={{ color: 'var(--text-soft)' }}>{row.upstream || '-'}</span>
                  </td>

                  {/* Client IP */}
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

      <AdminPagination page={page} pageSize={50} total={total} onPageChange={setPage} />
      </div>
    </AdminPage>
  )
}
