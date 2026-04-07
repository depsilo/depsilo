import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Input from '@/components/Input'
import Button from '@/components/Button'
import Badge from '@/components/Badge'
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

export default function AccessLogs() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [hitFilter, setHitFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [appliedSearch, setAppliedSearch] = useState('')

  const params: Record<string, any> = { page, page_size: 50 }
  if (appliedSearch) params.search = appliedSearch
  if (adapterType !== 'all') params.adapter_type = adapterType
  if (hitFilter === 'hit') params.hit = true
  if (hitFilter === 'miss') params.hit = false

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'logs', params],
    queryFn: () => adminApi.listLogs(params),
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const totalPages = Math.ceil(total / 50)

  function handleSearch() {
    setAppliedSearch(search)
    setPage(1)
  }

  const columns = [
    {
      key: 'created_at',
      label: t('logs.time'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface whitespace-nowrap">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'adapter_type',
      label: t('type'),
      render: (val: unknown) => (
        <Badge variant={(val as string) === 'pypi' ? 'pypi' : (val as string) === 'apt' ? 'apt' : 'default'}>
          {(val as string)?.toUpperCase()}
        </Badge>
      ),
    },
    {
      key: 'method',
      label: t('logs.method'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{(val as string) || 'GET'}</span>
      ),
    },
    {
      key: 'package_name',
      label: t('logs.packageName'),
      render: (val: unknown, row: any) => (
        <div className="max-w-[280px]">
          <span className="font-mono text-xs text-on-surface truncate block">
            {(val as string) || '-'}
          </span>
          <span className="font-mono text-[10px] text-on-surface-variant truncate block" title={row.cache_key}>
            {row.cache_key}
          </span>
        </div>
      ),
    },
    {
      key: 'hit',
      label: t('logs.result'),
      render: (val: unknown) => (
        <Badge variant={val ? 'success' : 'error'}>
          {val ? t('logs.hit') : t('logs.miss')}
        </Badge>
      ),
    },
    {
      key: 'upstream',
      label: t('logs.upstream'),
      render: (val: unknown) => (
        <span className="text-xs text-on-surface-variant">{(val as string) || '-'}</span>
      ),
    },
    {
      key: 'latency_ms',
      label: t('logs.latency'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface">{val as number} ms</span>
      ),
    },
    {
      key: 'client_ip',
      label: t('logs.clientIp'),
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface-variant">{val as string}</span>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* Filters */}
      <Card className="flex flex-wrap items-center gap-3">
        <div className="flex-1">
          <Input
            placeholder={t('logs.searchPlaceholder')}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          />
        </div>
        <select
          value={adapterType}
          onChange={(e) => { setAdapterType(e.target.value); setPage(1) }}
          className="bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
        >
          <option value="all">{t('all')}</option>
          <option value="pypi">PyPI</option>
          <option value="apt">APT</option>
          <option value="npm">npm</option>
          <option value="go">Go</option>
          <option value="cargo">Cargo</option>
          <option value="maven">Maven</option>
          <option value="rubygems">RubyGems</option>
          <option value="composer">Composer</option>
          <option value="nuget">NuGet</option>
          <option value="conda">Conda</option>
          <option value="cran">CRAN</option>
          <option value="helm">Helm</option>
        </select>
        <select
          value={hitFilter}
          onChange={(e) => { setHitFilter(e.target.value); setPage(1) }}
          className="bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
        >
          <option value="all">{t('all')}</option>
          <option value="hit">{t('logs.hit')}</option>
          <option value="miss">{t('logs.miss')}</option>
        </select>
        <Button variant="secondary" onClick={handleSearch}>{t('search')}</Button>
      </Card>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('logs.noLogs')}</div>
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
