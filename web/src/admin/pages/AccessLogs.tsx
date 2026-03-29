import { useState } from 'react'
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
      label: '时间',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface whitespace-nowrap">{formatTime(val as string)}</span>
      ),
    },
    {
      key: 'adapter_type',
      label: '类型',
      render: (val: unknown) => (
        <Badge variant={(val as string) === 'pypi' ? 'pypi' : 'apt'}>
          {(val as string)?.toUpperCase()}
        </Badge>
      ),
    },
    {
      key: 'package_name',
      label: '包名',
      render: (val: unknown, row: any) => (
        <span className="font-mono text-xs text-on-surface truncate block max-w-[200px]">
          {(val as string) || row.cache_key}
        </span>
      ),
    },
    {
      key: 'hit',
      label: '结果',
      render: (val: unknown) => (
        <Badge variant={val ? 'success' : 'error'}>
          {val ? '命中' : '未命中'}
        </Badge>
      ),
    },
    {
      key: 'upstream',
      label: '上游',
      render: (val: unknown) => (
        <span className="text-xs text-on-surface-variant">{(val as string) || '-'}</span>
      ),
    },
    {
      key: 'latency_ms',
      label: '耗时',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface">{val as number} ms</span>
      ),
    },
    {
      key: 'client_ip',
      label: '客户端 IP',
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
            placeholder="搜索包名..."
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
          <option value="all">全部</option>
          <option value="pypi">PyPI</option>
          <option value="apt">APT</option>
        </select>
        <select
          value={hitFilter}
          onChange={(e) => { setHitFilter(e.target.value); setPage(1) }}
          className="bg-surface-low border-b-2 border-transparent focus:border-primary text-base text-on-surface px-3 py-2 rounded-[0.125rem] outline-none transition-colors cursor-pointer"
        >
          <option value="all">全部</option>
          <option value="hit">命中</option>
          <option value="miss">未命中</option>
        </select>
        <Button variant="secondary" onClick={handleSearch}>搜索</Button>
      </Card>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">加载中...</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">暂无日志</div>
        ) : (
          <DataTable columns={columns} data={items} />
        )}
      </Card>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-on-surface-variant">
            共 {total} 条，第 {page}/{totalPages} 页
          </p>
          <div className="flex gap-2">
            <Button variant="secondary" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              上一页
            </Button>
            <Button variant="secondary" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
              下一页
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
