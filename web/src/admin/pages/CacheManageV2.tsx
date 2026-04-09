import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Treemap, ResponsiveContainer, Tooltip } from 'recharts'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import InputV2 from '@/components/InputV2'
import ButtonV2 from '@/components/ButtonV2'
import BadgeV2 from '@/components/BadgeV2'
import DataTableV2 from '@/components/DataTableV2'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/ModalV2'
import EcosystemIcon from '@/components/EcosystemIcon'
import SelectV2 from '@/components/SelectV2'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t); const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export default function CacheManageV2() {
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
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] }); setDeleteTarget(null) },
  })

  const cleanupMutation = useMutation({
    mutationFn: () => adminApi.cleanupCache(),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] }); setCleanupOpen(false) },
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const totalPages = Math.ceil(total / 20)

  const columns = [
    {
      key: 'key', label: 'Key',
      render: (val: unknown) => <span className="font-mono text-[12px] truncate block max-w-[240px]" style={{ color: 'var(--heading)' }}>{val as string}</span>,
    },
    {
      key: 'adapter_type', label: t('type'),
      render: (val: unknown) => (
        <div className="flex items-center gap-1.5">
          <EcosystemIcon type={val as any} size={14} />
          <BadgeV2 variant="ecosystem">{(val as string)?.toUpperCase()}</BadgeV2>
        </div>
      ),
    },
    {
      key: 'size', label: t('cache.size'),
      render: (val: unknown) => <span className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{formatBytes((val as number) || 0)}</span>,
    },
    {
      key: 'hit_count', label: t('cache.hitCount'),
      render: (val: unknown) => <span className="font-mono text-[13px] tabular-nums" style={{ color: 'var(--heading)' }}>{(val as number) || 0}</span>,
    },
    {
      key: 'last_accessed', label: t('cache.lastAccessed'),
      render: (val: unknown) => <span className="text-[12px]" style={{ color: 'var(--body)' }}>{formatTime(val as string)}</span>,
    },
    {
      key: 'id', label: t('actions'),
      render: (_val: unknown, row: any) => (
        <button
          onClick={(e) => { e.stopPropagation(); setDeleteTarget(row.id) }}
          className="bg-transparent cursor-pointer transition-colors duration-150 p-1 rounded-[4px]"
          style={{ color: 'var(--error)' }}
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
        <CardV2>
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>{t('cache.storageOverview')}</h3>
            <span className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--heading)' }}>
              {formatBytes(distribution.total_size)} / {formatBytes(distribution.max_size)}
              <span className="ml-1" style={{ color: 'var(--body)' }}>({distribution.usage_percent.toFixed(1)}%)</span>
            </span>
          </div>
          <div className="h-3 rounded-full overflow-hidden flex" style={{ background: 'var(--surface-container)' }}>
            {distribution.by_type.map((bt: any) => {
              const pct = distribution.max_size > 0 ? (bt.size / distribution.max_size) * 100 : 0
              return (
                <div key={bt.type} className="h-full transition-all" style={{ width: `${pct}%`, background: bt.type === 'pypi' ? 'var(--stripe-purple)' : 'var(--success)' }} title={`${bt.type.toUpperCase()}: ${formatBytes(bt.size)}`} />
              )
            })}
          </div>
          <div className="flex gap-6 mt-2">
            {distribution.by_type.map((bt: any) => (
              <div key={bt.type} className="flex items-center gap-2 text-[12px]" style={{ color: 'var(--body)' }}>
                <EcosystemIcon type={bt.type} size={12} />
                <span>{bt.type.toUpperCase()}</span>
                <span className="font-mono tabular-nums">{formatBytes(bt.size)}</span>
                <span>({bt.file_count} files)</span>
              </div>
            ))}
          </div>
        </CardV2>
      )}

      {/* Treemap */}
      {distribution && distribution.top_packages && distribution.top_packages.length > 0 && (
        <CardV2>
          <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('cache.storageDistribution')}</h3>
          <ResponsiveContainer width="100%" height={300}>
            <Treemap
              data={distribution.top_packages.map((p: any) => ({ name: p.name, size: p.size, type: p.type, hits: p.hit_count }))}
              dataKey="size" aspectRatio={4/3} stroke="var(--surface)"
              content={({ x, y, width, height, name, size }: any) => {
                if (width < 4 || height < 4) return <g />
                const showLabel = width > 70 && height > 44 && name
                return (
                  <g>
                    <rect x={x} y={y} width={width} height={height} fill="var(--stripe-purple)" fillOpacity={0.2 + Math.min(0.6, ((size || 0) / (distribution.top_packages[0]?.size || 1)) * 0.6)} stroke="var(--surface)" strokeWidth={2} rx={4} />
                    {showLabel && (
                      <foreignObject x={x} y={y} width={width} height={height}>
                        <div style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '4px', boxSizing: 'border-box', overflow: 'hidden' }}>
                          <span style={{ color: '#fff', fontSize: 13, fontWeight: 400, lineHeight: 1.2, textAlign: 'center', wordBreak: 'break-all' }}>{name}</span>
                          <span style={{ color: 'rgba(255,255,255,0.75)', fontSize: 11, fontWeight: 300, fontFamily: 'ui-monospace, monospace', lineHeight: 1.4 }}>{typeof size === 'number' ? formatBytes(size) : ''}</span>
                        </div>
                      </foreignObject>
                    )}
                  </g>
                )
              }}
            >
              <Tooltip content={({ payload }: any) => {
                if (!payload?.length) return null
                const item = payload[0]?.payload
                return (
                  <div className="rounded-[4px] p-2 text-[12px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}>
                    <p className="font-[400]" style={{ color: 'var(--heading)' }}>{item.name}</p>
                    <p style={{ color: 'var(--body)' }}>{item.type?.toUpperCase()} · {formatBytes(item.size)}</p>
                    <p style={{ color: 'var(--body)' }}>{item.hits} hits</p>
                  </div>
                )
              }} />
            </Treemap>
          </ResponsiveContainer>
        </CardV2>
      )}

      {/* Filters */}
      <CardV2 className="flex flex-wrap items-center gap-3">
        <div className="flex-1">
          <InputV2 placeholder={t('cache.searchPlaceholder')} value={search} onChange={(e) => { setSearch(e.target.value); setPage(1) }} />
        </div>
        <SelectV2 value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }} className="w-auto">
          <option value="all">{t('all')}</option>
          <option value="pypi">PyPI</option>
          <option value="apt">APT</option>
          <option value="npm">npm</option>
          <option value="go">Go</option>
          <option value="cargo">Cargo</option>
          <option value="maven">Maven</option>
        </SelectV2>
        <ButtonV2 variant="danger" onClick={() => setCleanupOpen(true)}>
          <Icon name="delete_sweep" size="sm" />
          {t('cache.cleanExpired')}
        </ButtonV2>
      </CardV2>

      {/* Table */}
      <CardV2 noPad>
        {isLoading ? (
          <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-[14px]" style={{ color: 'var(--body)' }}>{t('cache.noCache')}</div>
        ) : (
          <DataTableV2 columns={columns} data={items} />
        )}
      </CardV2>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-[13px]" style={{ color: 'var(--body)' }}>{t('totalItems', { total, page, totalPages })}</p>
          <div className="flex gap-2">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{t('prevPage')}</ButtonV2>
            <ButtonV2 variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>{t('nextPage')}</ButtonV2>
          </div>
        </div>
      )}

      {/* Delete Modal */}
      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('cache.confirmDelete')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--body)' }}>{t('cache.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>

      {/* Cleanup Modal */}
      <ModalV2 open={cleanupOpen} onClose={() => setCleanupOpen(false)} title={t('cache.cleanExpiredTitle')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--body)' }}>{t('cache.cleanExpiredMsg')}</p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setCleanupOpen(false)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={cleanupMutation.isPending} onClick={() => cleanupMutation.mutate()}>
            {cleanupMutation.isPending ? t('cache.cleaning') : t('cache.confirmClean')}
          </ButtonV2>
        </div>
      </ModalV2>
    </div>
  )
}
