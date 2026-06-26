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
import ProRequiredCallout from '@/admin/components/ProRequiredCallout'

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'docker']

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
  const params: Record<string, any> = { page, page_size: 50, start: range.start, end: range.end }
  if (appliedSearch) params.search = appliedSearch
  if (appliedEcosystem !== 'all') params.ecosystem = appliedEcosystem
  if (appliedResult === 'hit') params.result = 'hit'
  if (appliedResult === 'miss') params.result = 'miss'
  if (appliedResult === 'error') params.result = 'error'

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'audit-logs', appliedSearch, appliedEcosystem, appliedResult, appliedTimeRange, page],
    queryFn: () => adminApi.listAuditLogs(params),
    retry: false,
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const totalPages = Math.ceil(total / 50)

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

  // Pro paywall
  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <ProRequiredCallout
        icon="lock"
        title={t('audit.proRequired')}
        description={t('audit.proDesc')}
        upgradeLabel={t('audit.upgrade')}
      />
    )
  }

  const headers = [
    t('audit.time'), t('audit.ecosystem'), t('audit.packageName'), t('audit.version'),
    t('audit.result'), t('audit.latency'), t('audit.bytes'), t('audit.clientIp'),
  ]

  return (
    <div className="space-y-6">
      {/* Filter bar — bare row, only the search input gets a border */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1.5 flex-1 min-w-[240px] rounded-[4px] px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
          <input
            className="flex-1 bg-transparent text-[13px] outline-none min-w-0"
            style={{ color: 'var(--text)' }}
            placeholder={t('audit.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          />
        </div>

        {/* Ecosystem */}
        <select value={ecosystem} onChange={(e) => setEcosystem(e.target.value)} style={selStyle}>
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
        </select>

        {/* Result */}
        <select value={resultFilter} onChange={(e) => setResultFilter(e.target.value)} style={selStyle}>
          <option value="all">{t('all')}</option>
          <option value="hit">{t('audit.hit')}</option>
          <option value="miss">{t('audit.miss')}</option>
          <option value="error">{t('audit.error')}</option>
        </select>

        {/* Time range pills */}
        <div className="flex gap-1">
          {(['today', '7d', '30d'] as const).map(r => (
            <button
              key={r}
              onClick={() => setTimeRange(r)}
              className="px-2.5 py-1 text-[11px] rounded-[4px] transition-[background,color,border-color,transform] duration-150 cursor-pointer active:scale-[0.96]"
              style={{
                background: timeRange === r ? 'var(--brand)' : 'transparent',
                color: timeRange === r ? 'white' : 'var(--text-soft)',
                border: timeRange === r ? 'none' : '1px solid var(--border)',
              }}
            >
              {r === 'today' ? t('audit.today') : r === '7d' ? t('audit.days7') : t('audit.days30')}
            </button>
          ))}
        </div>

        {/* Export */}
        <ButtonV2 variant="ghost" size="sm" onClick={handleExport}>
          <Icon name="download" size="sm" />
          {t('audit.exportCsv')}
        </ButtonV2>

        {/* Search */}
        <ButtonV2 variant="primary" size="sm" onClick={handleSearch}>
          {t('search')}
        </ButtonV2>
      </div>

      {/* Table — bare */}
      <div>
        {isLoading ? (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
        ) : items.length === 0 ? (
          <EmptyState icon="receipt_long" title={t('audit.noLogs')} minHeight={200} />
        ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {headers.map(h => (
                  <th key={h} className="text-left text-[10px] font-mono font-[600] uppercase tracking-[0.08em] py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: any, i: number) => (
                <tr
                  key={i}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <td className="py-2 px-3 pl-0 whitespace-nowrap">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatTime(row.created_at)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      <EcosystemIcon type={row.ecosystem} size={13} />
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
                    {resultBadge(row.result || (row.cache_result === 'hit' ? 'hit' : row.cache_result === 'error' ? 'error' : 'miss'), t)}
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
