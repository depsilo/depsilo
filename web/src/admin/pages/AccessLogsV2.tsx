import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import InputV2 from '@/components/InputV2'
import ButtonV2 from '@/components/ButtonV2'
import BadgeV2 from '@/components/BadgeV2'
import DataTableV2 from '@/components/DataTableV2'
import EcosystemIcon from '@/components/EcosystemIcon'
import SelectV2 from '@/components/SelectV2'

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t); const now = new Date()
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const ECOSYSTEMS = ['pypi','apt','npm','go','cargo','maven','rubygems','composer','nuget','conda','cran','helm']

export default function AccessLogsV2() {
  const { t } = useTranslation()
  const [search, setSearch] = useState(''); const [adapterType, setAdapterType] = useState('all')
  const [hitFilter, setHitFilter] = useState('all'); const [page, setPage] = useState(1); const [appliedSearch, setAppliedSearch] = useState('')

  const params: Record<string, any> = { page, page_size: 50 }
  if (appliedSearch) params.search = appliedSearch
  if (adapterType !== 'all') params.adapter_type = adapterType
  if (hitFilter === 'hit') params.hit = true
  if (hitFilter === 'miss') params.hit = false

  const { data, isLoading } = useQuery({ queryKey: ['admin', 'logs', params], queryFn: () => adminApi.listLogs(params) })
  const items = data?.data?.items || []; const total = data?.data?.total || 0; const totalPages = Math.ceil(total / 50)

  function handleSearch() { setAppliedSearch(search); setPage(1) }

  const columns = [
    { key: 'created_at', label: t('logs.time'), render: (val: unknown) => <span className="font-mono text-[12px] whitespace-nowrap tabular-nums" style={{ color: 'var(--heading)' }}>{formatTime(val as string)}</span> },
    { key: 'adapter_type', label: t('type'), render: (val: unknown) => <div className="flex items-center gap-1.5"><EcosystemIcon type={val as any} size={14} /><BadgeV2 variant="ecosystem">{(val as string)?.toUpperCase()}</BadgeV2></div> },
    { key: 'method', label: t('logs.method'), render: (val: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--body)' }}>{(val as string) || 'GET'}</span> },
    { key: 'package_name', label: t('logs.packageName'), render: (val: unknown, row: any) => (<div className="max-w-[280px]"><span className="font-mono text-[12px] truncate block" style={{ color: 'var(--heading)' }}>{(val as string) || '-'}</span><span className="font-mono text-[10px] truncate block" style={{ color: 'var(--body)' }} title={row.cache_key}>{row.cache_key}</span></div>) },
    { key: 'hit', label: t('logs.result'), render: (val: unknown) => <BadgeV2 variant={val ? 'success' : 'error'}>{val ? t('logs.hit') : t('logs.miss')}</BadgeV2> },
    { key: 'upstream', label: t('logs.upstream'), render: (val: unknown) => <span className="text-[12px]" style={{ color: 'var(--body)' }}>{(val as string) || '-'}</span> },
    { key: 'latency_ms', label: t('logs.latency'), render: (val: unknown) => <span className="font-mono text-[12px] tabular-nums" style={{ color: 'var(--heading)' }}>{val as number} ms</span> },
    { key: 'client_ip', label: t('logs.clientIp'), render: (val: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--body)' }}>{val as string}</span> },
  ]

  return (
    <div className="space-y-6">
      <CardV2 className="flex flex-wrap items-center gap-3">
        <div className="flex-1"><InputV2 placeholder={t('logs.searchPlaceholder')} value={search} onChange={(e) => setSearch(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && handleSearch()} /></div>
        <SelectV2 value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }} className="w-auto">
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
        </SelectV2>
        <SelectV2 value={hitFilter} onChange={(e) => { setHitFilter(e.target.value); setPage(1) }} className="w-auto">
          <option value="all">{t('all')}</option>
          <option value="hit">{t('logs.hit')}</option>
          <option value="miss">{t('logs.miss')}</option>
        </SelectV2>
        <ButtonV2 variant="secondary" onClick={handleSearch}>{t('search')}</ButtonV2>
      </CardV2>
      <CardV2 noPad>
        {isLoading ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('loading')}</div>
        : items.length === 0 ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('logs.noLogs')}</div>
        : <DataTableV2 columns={columns} data={items} />}
      </CardV2>
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('totalItems', { total, page, totalPages })}</p>
          <div className="flex gap-2">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{t('prevPage')}</ButtonV2>
            <ButtonV2 variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>{t('nextPage')}</ButtonV2>
          </div>
        </div>
      )}
    </div>
  )
}
