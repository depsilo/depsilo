import { useState } from 'react'
import { useTranslation } from 'react-i18next'
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
  const { t } = useTranslation()
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
      label: t('type'),
      render: (val: unknown) => (
        <Badge variant={(val as string) === 'pypi' ? 'pypi' : 'apt'}>
          {(val as string)?.toUpperCase()}
        </Badge>
      ),
    },
    {
      key: 'size',
      label: t('cache.size'),
      render: (val: unknown) => <span className="text-sm text-on-surface">{formatBytes((val as number) || 0)}</span>,
    },
    {
      key: 'hit_count',
      label: t('cache.hitCount'),
      render: (val: unknown) => <span className="font-mono text-sm text-on-surface">{(val as number) || 0}</span>,
    },
    {
      key: 'last_accessed',
      label: t('cache.lastAccessed'),
      render: (val: unknown) => <span className="text-xs text-on-surface-variant">{formatTime(val as string)}</span>,
    },
    {
      key: 'id',
      label: t('actions'),
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
            placeholder={t('cache.searchPlaceholder')}
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
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
        </select>
        <Button
          variant="secondary"
          className="text-error border-error/30"
          onClick={() => setCleanupOpen(true)}
        >
          <Icon name="delete_sweep" size="sm" />
          {t('cache.cleanExpired')}
        </Button>
      </Card>

      {/* Table */}
      <Card className="p-0 overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-on-surface-variant text-sm">{t('cache.noCache')}</div>
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

      {/* Delete Confirm Modal */}
      <Modal open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('cache.confirmDelete')}>
        <p className="text-sm text-on-surface-variant mb-6">
          {t('cache.confirmDeleteMsg')}
        </p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={deleteMutation.isPending}
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
          >
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </Button>
        </div>
      </Modal>

      {/* Cleanup Confirm Modal */}
      <Modal open={cleanupOpen} onClose={() => setCleanupOpen(false)} title={t('cache.cleanExpiredTitle')}>
        <p className="text-sm text-on-surface-variant mb-6">
          {t('cache.cleanExpiredMsg')}
        </p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setCleanupOpen(false)}>{t('cancel')}</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={cleanupMutation.isPending}
            onClick={() => cleanupMutation.mutate()}
          >
            {cleanupMutation.isPending ? t('cache.cleaning') : t('cache.confirmClean')}
          </Button>
        </div>
      </Modal>
    </div>
  )
}
