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
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import TableViewport from '@/components/TableViewport'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { AccessLog, AccessLogQuery } from '@/lib/adminApi.types'

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'docker']

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
  const totalPages = Math.ceil(total / 50)
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message

  function handleSearch() { setAppliedSearch(search); setPage(1) }

  // Inline select style
  const selStyle: React.CSSProperties = {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    color: 'var(--text)',
    borderRadius: 4,
    padding: '4px 8px',
    fontSize: 12,
    outline: 'none',
    cursor: 'pointer',
  }

  return (
    <div className="space-y-6">
      {/* Filter bar — bare row, only the search input gets a border */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1.5 flex-1 min-w-[240px] rounded-[4px] px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
          <input
            className="flex-1 bg-transparent text-[13px] outline-none min-w-0"
            style={{ color: 'var(--text)' }}
            placeholder={t('logs.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          />
        </div>

        {/* Ecosystem filter */}
        <select value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }} style={selStyle}>
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
        </select>

        {/* Hit/Miss filter */}
        <select value={hitFilter} onChange={(e) => { setHitFilter(e.target.value); setPage(1) }} style={selStyle}>
          <option value="all">{t('all')}</option>
          <option value="hit">{t('logs.hit')}</option>
          <option value="miss">{t('logs.miss')}</option>
        </select>

        {/* Export button */}
        <ButtonV2 variant="ghost" size="sm" onClick={() => {
          adminApi.exportLogs(params).then(res => {
            const url = URL.createObjectURL(new Blob([res.data]))
            const a = document.createElement('a')
            a.href = url
            a.download = `depsilo-access-logs-${new Date().toISOString().slice(0, 10)}.csv`
            a.click()
            URL.revokeObjectURL(url)
          })
        }}>
          <Icon name="download" size="sm" />
          {t('logs.export')}
        </ButtonV2>

        {/* Search button */}
        <ButtonV2 variant="primary" size="sm" onClick={handleSearch}>
          {t('search')}
        </ButtonV2>
      </div>

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
            <InlineNotice tone="warning"><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void refetch() }}>{t('now.refresh')}</ButtonV2></div></InlineNotice>
          )}
          {items.length === 0 ? (
            <EmptyState icon="receipt_long" title={t('logs.noLogs')} minHeight={200} />
          ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {[t('logs.time'), t('type'), t('logs.packageName'), t('logs.result'), t('logs.latency'), t('logs.upstream'), t('logs.clientIp')].map(h => (
                  <th key={h} className="text-left text-[10px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>
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

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('totalItems', { total, page, totalPages })}
          </span>
          <div className="flex items-center gap-1">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>
              {t('prevPage')}
            </ButtonV2>
            <span className="text-[12px] font-mono tabular-nums px-2" style={{ color: 'var(--text-soft)' }}>
              {page}/{totalPages}
            </span>
            <ButtonV2 variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>
              {t('nextPage')}
            </ButtonV2>
          </div>
        </div>
      )}
    </div>
  )
}
