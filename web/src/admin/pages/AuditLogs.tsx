import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/Card'
import InputV2 from '@/components/Input'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import Icon from '@/components/Icon'
import DataTableV2 from '@/components/DataTable'
import EcosystemIcon from '@/components/EcosystemIcon'
import SelectV2 from '@/components/Select'

const ECOSYSTEMS = ['pypi','apt','npm','go','cargo','maven','rubygems','composer','nuget','conda','cran','helm']

function formatTime(t: string): string { if (!t) return '-'; const d = new Date(t); const now = new Date(); if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }); return `${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}` }
function formatBytes(bytes: number): string { if (!bytes || bytes === 0) return '0 B'; const units = ['B','KB','MB','GB','TB']; const i = Math.floor(Math.log(bytes)/Math.log(1024)); return `${(bytes/Math.pow(1024,i)).toFixed(i===0?0:1)} ${units[i]}` }
function getTimeRange(preset: string) { const now = new Date(); const end = now.toISOString(); const start = new Date(now); if (preset === 'today') start.setHours(0,0,0,0); else if (preset === '7d') start.setDate(start.getDate()-7); else if (preset === '30d') start.setDate(start.getDate()-30); return { start: start.toISOString(), end } }

export default function AuditLogsV2() {
  const { t } = useTranslation()
  const [search, setSearch] = useState(''); const [ecosystem, setEcosystem] = useState('all'); const [resultFilter, setResultFilter] = useState('all'); const [timeRange, setTimeRange] = useState('today'); const [page, setPage] = useState(1)
  const [appliedSearch, setAppliedSearch] = useState(''); const [appliedEcosystem, setAppliedEcosystem] = useState('all'); const [appliedResult, setAppliedResult] = useState('all'); const [appliedTimeRange, setAppliedTimeRange] = useState('today')

  const range = getTimeRange(appliedTimeRange)
  const params: Record<string, any> = { page, page_size: 50, start: range.start, end: range.end }
  if (appliedSearch) params.search = appliedSearch; if (appliedEcosystem !== 'all') params.ecosystem = appliedEcosystem
  if (appliedResult === 'hit') params.result = 'hit'; if (appliedResult === 'miss') params.result = 'miss'; if (appliedResult === 'error') params.result = 'error'

  const { data, isLoading, error } = useQuery({ queryKey: ['admin', 'audit-logs', appliedSearch, appliedEcosystem, appliedResult, appliedTimeRange, page], queryFn: () => adminApi.listAuditLogs(params), retry: false })
  const items = data?.data?.items || []; const total = data?.data?.total || 0; const totalPages = Math.ceil(total / 50)
  function handleSearch() { setAppliedSearch(search); setAppliedEcosystem(ecosystem); setAppliedResult(resultFilter); setAppliedTimeRange(timeRange); setPage(1) }
  function handleExport() { adminApi.exportAuditLogs(params).then(res => { const url = URL.createObjectURL(new Blob([res.data])); const a = document.createElement('a'); a.href = url; a.download = `depsilo-audit-${new Date().toISOString().slice(0,10)}.csv`; a.click(); URL.revokeObjectURL(url) }) }

  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <div className="space-y-6">
        <div className="text-center py-12 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.04)', border: '1px solid var(--border-purple)' }}>
          <div className="flex flex-col items-center gap-4">
            <div className="flex items-center justify-center w-14 h-14 rounded-[8px]" style={{ background: 'rgba(83,58,253,0.08)' }}><Icon name="lock" size="lg" style={{ color: 'var(--stripe-purple)' }} /></div>
            <h3 className="text-[18px] font-[300]" style={{ color: 'var(--heading)' }}>{t('audit.proRequired')}</h3>
            <p className="text-[14px] max-w-md" style={{ color: 'var(--body)' }}>{t('audit.proDesc')}</p>
            <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer"><ButtonV2>{t('audit.upgrade')}</ButtonV2></a>
          </div>
        </div>
      </div>
    )
  }

  const columns = [
    { key: 'created_at', label: t('audit.time'), render: (v: unknown) => <span className="font-mono text-[12px] whitespace-nowrap tabular-nums" style={{ color: 'var(--heading)' }}>{formatTime(v as string)}</span> },
    { key: 'ecosystem', label: t('audit.ecosystem'), render: (v: unknown) => <div className="flex items-center gap-1.5"><EcosystemIcon type={v as any} size={14} /><BadgeV2 variant="ecosystem">{(v as string)?.toUpperCase()}</BadgeV2></div> },
    { key: 'package_name', label: t('audit.packageName'), render: (v: unknown) => <span className="font-mono text-[12px] truncate block max-w-[200px]" style={{ color: 'var(--heading)' }}>{(v as string) || '—'}</span> },
    { key: 'version', label: t('audit.version'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--body)' }}>{(v as string) || '—'}</span> },
    { key: 'result', label: t('audit.result'), render: (v: unknown) => { const r = v as string; if (r === 'hit') return <BadgeV2 variant="success">{t('audit.hit')}</BadgeV2>; if (r === 'error') return <BadgeV2 variant="error">{t('audit.error')}</BadgeV2>; return <BadgeV2>{t('audit.miss')}</BadgeV2> } },
    { key: 'client_ip', label: t('audit.clientIp'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--body)' }}>{v as string}</span> },
    { key: 'latency_ms', label: t('audit.latency'), render: (v: unknown) => <span className="font-mono text-[12px] tabular-nums" style={{ color: 'var(--heading)' }}>{v as number}ms</span> },
    { key: 'bytes_sent', label: t('audit.bytes'), render: (v: unknown) => <span className="font-mono text-[12px]" style={{ color: 'var(--body)' }}>{formatBytes(v as number)}</span> },
  ]

  return (
    <div className="space-y-6">
      <CardV2 className="flex flex-wrap items-center gap-3">
        <div className="flex-1 min-w-[180px]"><InputV2 placeholder={t('audit.searchPlaceholder')} value={search} onChange={(e) => setSearch(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && handleSearch()} /></div>
        <SelectV2 value={ecosystem} onChange={(e) => setEcosystem(e.target.value)} className="w-auto"><option value="all">{t('all')}</option>{ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}</SelectV2>
        <SelectV2 value={resultFilter} onChange={(e) => setResultFilter(e.target.value)} className="w-auto"><option value="all">{t('all')}</option><option value="hit">{t('audit.hit')}</option><option value="miss">{t('audit.miss')}</option><option value="error">{t('audit.error')}</option></SelectV2>
        <div className="flex gap-1">
          {(['today','7d','30d'] as const).map(r => (
            <button key={r} onClick={() => setTimeRange(r)} className="px-3 py-1.5 text-[12px] rounded-[4px] transition-colors duration-150 cursor-pointer" style={{ background: timeRange === r ? 'var(--stripe-purple)' : 'var(--surface)', color: timeRange === r ? 'var(--on-primary)' : 'var(--body)', border: timeRange === r ? 'none' : '1px solid var(--border)' }}>{r === 'today' ? t('audit.today') : r === '7d' ? t('audit.days7') : t('audit.days30')}</button>
          ))}
        </div>
        <ButtonV2 variant="secondary" size="sm" onClick={handleSearch}>{t('search')}</ButtonV2>
        <div className="ml-auto"><ButtonV2 variant="ghost" size="sm" onClick={handleExport}><Icon name="download" size="sm" />{t('audit.exportCsv')}</ButtonV2></div>
      </CardV2>
      <CardV2 noPad>{isLoading ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('loading')}</div> : items.length === 0 ? <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('audit.noLogs')}</div> : <DataTableV2 columns={columns} data={items} />}</CardV2>
      {totalPages > 1 && (<div className="flex items-center justify-between"><p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('totalItems', { total, page, totalPages })}</p><div className="flex gap-2"><ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p-1)}>{t('prevPage')}</ButtonV2><ButtonV2 variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => setPage(p => p+1)}>{t('nextPage')}</ButtonV2></div></div>)}
    </div>
  )
}
