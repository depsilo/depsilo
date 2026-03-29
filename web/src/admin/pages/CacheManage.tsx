import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Input from '@/components/Input'
import Button from '@/components/Button'
import Badge from '@/components/Badge'
import DataTable from '@/components/DataTable'
import Icon from '@/components/Icon'
import Modal from '@/components/Modal'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

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

export default function CacheManage() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [page, setPage] = useState(1)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)

  const params: Record<string, any> = { page, page_size: 20 }
  if (search) params.search = search
  if (adapterType !== 'all') params.adapter_type = adapterType

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'cache', params],
    queryFn: () => adminApi.listCache(params),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteCache(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] })
      setDeleteTarget(null)
    },
  })

  const cleanupMutation = useMutation({
    mutationFn: () => adminApi.cleanupCache(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] })
      setCleanupOpen(false)
    },
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const totalPages = Math.ceil(total / 20)

  const columns = [
    {
      key: 'key',
      label: 'Key',
      render: (val: unknown) => (
        <span className="font-mono text-xs text-on-surface truncate block max-w-[240px]">{val as string}</span>
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
      key: 'size',
      label: '大小',
      render: (val: unknown) => <span className="text-sm text-on-surface">{formatBytes((val as number) || 0)}</span>,
    },
    {
      key: 'hit_count',
      label: '命中次数',
      render: (val: unknown) => <span className="font-mono text-sm text-on-surface">{(val as number) || 0}</span>,
    },
    {
      key: 'last_accessed',
      label: '最后访问',
      render: (val: unknown) => <span className="text-xs text-on-surface-variant">{formatTime(val as string)}</span>,
    },
    {
      key: 'id',
      label: '操作',
      render: (_val: unknown, row: any) => (
        <button
          onClick={(e) => { e.stopPropagation(); setDeleteTarget(row.id) }}
          className="bg-transparent text-error hover:text-error/80 cursor-pointer transition-colors p-1"
        >
          <Icon name="delete" size="sm" />
        </button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* Filters */}
      <Card className="flex flex-wrap items-center gap-3">
        <div className="flex-1">
          <Input
            placeholder="搜索缓存 key..."
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
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
        <Button
          variant="secondary"
          className="text-error border-error/30"
          onClick={() => setCleanupOpen(true)}
        >
          <Icon name="delete_sweep" size="sm" />
          清理过期
        </Button>
      </Card>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">加载中...</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">暂无缓存数据</div>
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

      {/* Delete Confirm Modal */}
      <Modal open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title="确认删除">
        <p className="text-sm text-on-surface-variant mb-6">
          确定要删除此缓存条目吗？此操作不可撤销。
        </p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setDeleteTarget(null)}>取消</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={deleteMutation.isPending}
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
          >
            {deleteMutation.isPending ? '删除中...' : '删除'}
          </Button>
        </div>
      </Modal>

      {/* Cleanup Confirm Modal */}
      <Modal open={cleanupOpen} onClose={() => setCleanupOpen(false)} title="清理过期缓存">
        <p className="text-sm text-on-surface-variant mb-6">
          将清理所有已过期的缓存文件和超过 LRU 阈值的旧文件，确定继续？
        </p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setCleanupOpen(false)}>取消</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={cleanupMutation.isPending}
            onClick={() => cleanupMutation.mutate()}
          >
            {cleanupMutation.isPending ? '清理中...' : '确认清理'}
          </Button>
        </div>
      </Modal>
    </div>
  )
}
