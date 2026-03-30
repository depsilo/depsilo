import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Treemap, ResponsiveContainer, Tooltip } from 'recharts'
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

  const { data: distData } = useQuery({
    queryKey: ['admin', 'cache', 'distribution'],
    queryFn: () => adminApi.getCacheDistribution(),
    refetchInterval: 30000,
  })

  const distribution = distData?.data

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
      {/* Storage Overview */}
      {distribution && (
        <Card>
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium">
              {t('cache.storageOverview')}
            </h3>
            <span className="text-sm font-mono text-on-surface">
              {formatBytes(distribution.total_size)} / {formatBytes(distribution.max_size)}
              <span className="text-on-surface-variant ml-1">({distribution.usage_percent.toFixed(1)}%)</span>
            </span>
          </div>

          {/* Segmented progress bar */}
          <div className="h-3 rounded-full bg-surface-container overflow-hidden flex">
            {distribution.by_type.map((bt: any) => {
              const pct = distribution.max_size > 0
                ? (bt.size / distribution.max_size) * 100
                : 0
              return (
                <div
                  key={bt.type}
                  className={`h-full transition-all ${bt.type === 'pypi' ? 'bg-primary' : 'bg-success'}`}
                  style={{ width: `${pct}%` }}
                  title={`${bt.type.toUpperCase()}: ${formatBytes(bt.size)} (${bt.file_count} files)`}
                />
              )
            })}
          </div>

          {/* Legend */}
          <div className="flex gap-6 mt-2">
            {distribution.by_type.map((bt: any) => (
              <div key={bt.type} className="flex items-center gap-2 text-xs text-on-surface-variant">
                <span className={`h-2 w-2 rounded-full ${bt.type === 'pypi' ? 'bg-primary' : 'bg-success'}`} />
                <span>{bt.type.toUpperCase()}</span>
                <span className="font-mono">{formatBytes(bt.size)}</span>
                <span>({bt.file_count} files)</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      {/* Storage Distribution Treemap */}
      {distribution && distribution.top_packages && distribution.top_packages.length > 0 && (
        <Card>
          <h3 className="text-xs uppercase tracking-wider text-on-surface-variant font-medium mb-4">
            {t('cache.storageDistribution')}
          </h3>
          <ResponsiveContainer width="100%" height={300}>
            <Treemap
              data={distribution.top_packages.map((p: any) => ({
                name: p.name,
                size: p.size,
                type: p.type,
                hits: p.hit_count,
              }))}
              dataKey="size"
              aspectRatio={4 / 3}
              stroke="var(--surface-low)"
              content={({ x, y, width, height, name, size }: any) => {
                if (width < 4 || height < 4) return <g />
                return (
                  <g>
                    <rect
                      x={x}
                      y={y}
                      width={width}
                      height={height}
                      fill="var(--primary)"
                      fillOpacity={0.25 + Math.min(0.6, ((size || 0) / (distribution.top_packages[0]?.size || 1)) * 0.6)}
                      stroke="var(--surface-low)"
                      strokeWidth={2}
                      rx={3}
                    />
                    {width > 70 && height > 44 && name && (
                      <>
                        <text
                          x={x + width / 2}
                          y={y + height / 2 - 8}
                          textAnchor="middle"
                          dominantBaseline="middle"
                          fill="#ffffff"
                          fontSize={14}
                          fontWeight={700}
                          fontFamily="system-ui, sans-serif"
                        >
                          {name}
                        </text>
                        <text
                          x={x + width / 2}
                          y={y + height / 2 + 12}
                          textAnchor="middle"
                          dominantBaseline="middle"
                          fill="rgba(255,255,255,0.8)"
                          fontSize={12}
                          fontWeight={500}
                          fontFamily="ui-monospace, monospace"
                        >
                          {typeof size === 'number' ? formatBytes(size) : ''}
                        </text>
                      </>
                    )}
                  </g>
                )
              }}
            >
              <Tooltip
                content={({ payload }: any) => {
                  if (!payload || !payload.length) return null
                  const item = payload[0]?.payload
                  return (
                    <div className="bg-surface-container border border-outline-variant rounded-[0.25rem] p-2 text-xs">
                      <p className="font-medium text-on-surface">{item.name}</p>
                      <p className="text-on-surface-variant">{item.type?.toUpperCase()} · {formatBytes(item.size)}</p>
                      <p className="text-on-surface-variant">{item.hits} hits</p>
                    </div>
                  )
                }}
              />
            </Treemap>
          </ResponsiveContainer>
        </Card>
      )}

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
