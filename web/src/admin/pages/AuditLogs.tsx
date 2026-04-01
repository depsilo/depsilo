import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Input from '@/components/Input'
import Button from '@/components/Button'
import Badge from '@/components/Badge'
import Icon from '@/components/Icon'
import DataTable from '@/components/DataTable'

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

const ECOSYSTEMS = [
  'pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer',
  'nuget', 'conda', 'cran', 'helm',
]

function getEcosystemBadgeVariant(eco: string): 'pypi' | 'apt' | 'default' {
  if (eco === 'pypi') return 'pypi'
  if (eco === 'apt') return 'apt'
  return 'default'
}

function getTimeRange(preset: string): { start: string; end: string } {
  const now = new Date()
  const end = now.toISOString()
  const start = new Date(now)
  if (preset === 'today') {
    start.setHours(0, 0, 0, 0)
  } else if (preset === '7d') {
    start.setDate(start.getDate() - 7)
  } else if (preset === '30d') {
    start.setDate(start.getDate() - 30)
  }
  return { start: start.toISOString(), end }
}

export default function AuditLogs() {
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
  const params: Record<string, any> = {
    page,
    page_size: 50,
    start: range.start,
    end: range.end,
  }
  if (appliedSearch) params.search = appliedSearch
  if (appliedEcosystem !== 'all') params.ecosystem = appliedEcosystem
  if (appliedResult === 'hit') params.result = 'hit'
  if (appliedResult === 'miss') params.result = 'miss'
  if (appliedResult === 'error') params.result = 'error'

  const { data, isLoading, error } = useQuery({
    queryKey: ['admin', 'audit-logs', params],
    queryFn: () => adminApi.listAuditLogs(params),
    retry: (count, err: any) => {
      if (err?.response?.status === 402) return false
      return count < 2
    },
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const totalPages = Math.ceil(total / 50)

  function handleSearch() {
    setAppliedSearch(search)
    setAppliedEcosystem(ecosystem)
    setAppliedResult(resultFilter)
    setAppliedTimeRange(timeRange)
    setPage(1)
  }

  function handleExport() {
    adminApi.exportAuditLogs(params).then((res) => {
      const url = URL.createObjectURL(new Blob([res.data]))
      const a = document.createElement('a')
      a.href = url
      a.download = `depsilo-audit-${new Date().toISOString().slice(0, 10)}.csv`
      a.click()
      URL.revokeObjectURL(url)
    })
  }

  // 402 Pro required gate
  const axiosError = error as any
  if (axiosError?.response?.status === 402) {
    return (
      <div className="space-y-6">
        <Card className="text-center py-12">
          <div className="flex flex-col items-center gap-4">
            <Icon name="lock" size="lg" className="text-on-surface-variant" />
            <h3 className="text-lg font-semibold text-on-surface">{t('audit.proRequired')}</h3>
            <p className="text-sm text-on-surface-variant max-w-md">{t('audit.proDesc')}</p>
            <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer">
              <Button>{t('audit.upgrade')}</Button>
            </a>
          </div>
        </Card>
      </div>
    )
  }

  const columns = [
    {
      key: 'created_at',
      label: t('audit.time'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface whitespace-nowrap">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'ecosystem',
      label: t('audit.ecosystem'),
      render: (val: unknown) => (
        <Badge variant={getEcosystemBadgeVariant(val as string)}>
          {(val as string)?.toUpperCase()}
        </Badge>
      ),
    },
    {
      key: 'package_name',
      label: t('audit.packageName'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface truncate block max-w-[200px]">
          {(val as string) || '\u2014'}
        </span>
      ),
    },
    {
      key: 'version',
      label: t('audit.version'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">
          {(val as string) || '\u2014'}
        </span>
      ),
    },
    {
      key: 'result',
      label: t('audit.result'),
      render: (val: unknown) => {
        const r = val as string
        if (r === 'hit') return <Badge variant="success">{t('audit.hit')}</Badge>
        if (r === 'error') return <Badge variant="error">{t('audit.error')}</Badge>
        return <Badge variant="default">{t('audit.miss')}</Badge>
      },
    },
    {
      key: 'client_ip',
      label: t('audit.clientIp'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{val as string}</span>
      ),
    },
    {
      key: 'latency_ms',
      label: t('audit.latency'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface">{val as number}ms</span>
      ),
    },
    {
      key: 'bytes_sent',
      label: t('audit.bytes'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{formatBytes(val as number)}</span>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* Filters */}
      <Card className="flex flex-wrap items-center gap-3">
        <div className="flex-1 min-w-[180px]">
          <Input
            placeholder={t('audit.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          />
        </div>
        <select
          value={ecosystem}
          onChange={(e) => setEcosystem(e.target.value)}
          className="bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
        >
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map((eco) => (
            <option key={eco} value={eco}>{eco.toUpperCase()}</option>
          ))}
        </select>
        <select
          value={resultFilter}
          onChange={(e) => setResultFilter(e.target.value)}
          className="bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
        >
          <option value="all">{t('all')}</option>
          <option value="hit">{t('audit.hit')}</option>
          <option value="miss">{t('audit.miss')}</option>
          <option value="error">{t('audit.error')}</option>
        </select>
        <div className="flex gap-1">
          {(['today', '7d', '30d'] as const).map((r) => (
            <button
              key={r}
              onClick={() => setTimeRange(r)}
              className={`px-3 py-1.5 text-xs rounded-md transition-colors cursor-pointer ${
                timeRange === r
                  ? 'bg-primary text-on-primary font-medium'
                  : 'bg-surface-container text-on-surface-variant hover:bg-surface-container-high'
              }`}
            >
              {r === 'today' ? t('audit.today') : r === '7d' ? t('audit.days7') : t('audit.days30')}
            </button>
          ))}
        </div>
        <Button variant="secondary" onClick={handleSearch}>{t('search')}</Button>
        <div className="ml-auto">
          <Button variant="ghost" onClick={handleExport}>
            <Icon name="download" size="sm" className="mr-1" />
            {t('audit.exportCsv')}
          </Button>
        </div>
      </Card>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('audit.noLogs')}</div>
        ) : (
          <DataTable columns={columns} data={items} />
        )}
      </Card>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-on-surface-variant">
            {t('totalItems', { total, page, totalPages })}
          </p>
          <div className="flex gap-2">
            <Button variant="secondary" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              {t('prevPage')}
            </Button>
            <Button variant="secondary" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
              {t('nextPage')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
