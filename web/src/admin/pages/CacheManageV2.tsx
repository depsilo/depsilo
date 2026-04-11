import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Treemap, ResponsiveContainer, Tooltip } from 'recharts'
import { adminApi } from '@/lib/api'
import ButtonV2 from '@/components/ButtonV2'
import BadgeV2 from '@/components/BadgeV2'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/ModalV2'
import EcosystemIcon from '@/components/EcosystemIcon'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

function formatTime(t: string): string {
  if (!t) return '-'
  const d = new Date(t); const now = new Date()
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm']

// Assign distinct colors per ecosystem
const ECO_COLORS: Record<string, string> = {
  pypi: 'var(--stripe-purple)', apt: '#3bd671', npm: '#cb3837', go: '#00add8',
  cargo: '#dea584', maven: '#c71a36', rubygems: '#e9573f', composer: '#885630',
  nuget: '#004880', conda: '#44a833', cran: '#2266b8', helm: '#0f1689',
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

  const selStyle: React.CSSProperties = {
    background: 'var(--surface)', border: '1px solid var(--border)', color: 'var(--heading)',
    borderRadius: 4, padding: '4px 8px', fontSize: 12, outline: 'none', cursor: 'pointer',
  }

  return (
    <div className="space-y-4">
      {/* Storage overview + Treemap side by side */}
      {distribution && (
        <div className="grid grid-cols-3 gap-4">
          {/* Left: usage bar + ecosystem breakdown */}
          <div
            className="col-span-1 rounded-[5px] p-4"
            style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
          >
            {/* Total usage */}
            <div className="flex items-center justify-between mb-2">
              <span className="text-[12px] uppercase tracking-wider font-[400]" style={{ color: 'var(--body)' }}>
                {t('cache.storageOverview')}
              </span>
            </div>
            <p className="text-[20px] font-[300] font-mono tabular-nums mb-1" style={{ color: 'var(--heading)' }}>
              {formatBytes(distribution.total_size)}
              <span className="text-[12px] font-[400] ml-1" style={{ color: 'var(--body)' }}>
                / {formatBytes(distribution.max_size)}
              </span>
            </p>
            {/* Progress bar */}
            <div className="h-2 rounded-full overflow-hidden flex mb-4" style={{ background: 'var(--surface-container)' }}>
              {distribution.by_type.map((bt: any) => {
                const pct = distribution.max_size > 0 ? (bt.size / distribution.max_size) * 100 : 0
                return (
                  <div
                    key={bt.type}
                    className="h-full"
                    style={{ width: `${pct}%`, background: ECO_COLORS[bt.type] || 'var(--stripe-purple)' }}
                    title={`${bt.type.toUpperCase()}: ${formatBytes(bt.size)}`}
                  />
                )
              })}
            </div>

            {/* Ecosystem breakdown list */}
            <div className="space-y-2">
              {distribution.by_type.map((bt: any) => {
                const pct = distribution.total_size > 0 ? ((bt.size / distribution.total_size) * 100).toFixed(1) : '0'
                return (
                  <div key={bt.type} className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full shrink-0" style={{ background: ECO_COLORS[bt.type] || 'var(--stripe-purple)' }} />
                    <EcosystemIcon type={bt.type} size={12} />
                    <span className="text-[11px] uppercase flex-1" style={{ color: 'var(--heading)' }}>{bt.type}</span>
                    <span className="text-[11px] font-mono tabular-nums" style={{ color: 'var(--body)' }}>{formatBytes(bt.size)}</span>
                    <span className="text-[10px] font-mono tabular-nums w-10 text-right" style={{ color: 'var(--body)' }}>{pct}%</span>
                    <span className="text-[10px]" style={{ color: 'var(--body)' }}>{bt.file_count}f</span>
                  </div>
                )
              })}
            </div>
          </div>

          {/* Right: Treemap (compact) */}
          <div
            className="col-span-2 rounded-[5px] p-4"
            style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
          >
            <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-2" style={{ color: 'var(--body)' }}>
              {t('cache.storageDistribution')}
            </h3>
            {distribution.top_packages && distribution.top_packages.length > 0 ? (
              <ResponsiveContainer width="100%" height={160}>
                <Treemap
                  data={distribution.top_packages.map((p: any) => ({ name: p.name, size: p.size, type: p.type, hits: p.hit_count }))}
                  dataKey="size" aspectRatio={4 / 3} stroke="var(--surface)"
                  content={({ x, y, width, height, name, size }: any) => {
                    if (width < 4 || height < 4) return <g />
                    const showLabel = width > 60 && height > 30 && name
                    const type = distribution.top_packages.find((p: any) => p.name === name)?.type
                    return (
                      <g>
                        <rect x={x} y={y} width={width} height={height}
                          fill={ECO_COLORS[type] || 'var(--stripe-purple)'}
                          fillOpacity={0.25 + Math.min(0.5, ((size || 0) / (distribution.top_packages[0]?.size || 1)) * 0.5)}
                          stroke="var(--surface)" strokeWidth={1.5} rx={3}
                        />
                        {showLabel && (
                          <foreignObject x={x} y={y} width={width} height={height}>
                            <div style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: 3, boxSizing: 'border-box', overflow: 'hidden' }}>
                              <span style={{ color: '#fff', fontSize: 11, fontWeight: 400, lineHeight: 1.2, textAlign: 'center', wordBreak: 'break-all' }}>{name}</span>
                              <span style={{ color: 'rgba(255,255,255,0.7)', fontSize: 10, fontFamily: 'ui-monospace, monospace' }}>{formatBytes(size)}</span>
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
                      <div className="rounded-[4px] p-2 text-[11px]" style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}>
                        <p className="font-[400]" style={{ color: 'var(--heading)' }}>{item.name}</p>
                        <p style={{ color: 'var(--body)' }}>{item.type?.toUpperCase()} · {formatBytes(item.size)} · {item.hits} hits</p>
                      </div>
                    )
                  }} />
                </Treemap>
              </ResponsiveContainer>
            ) : (
              <p className="text-[13px] py-8 text-center" style={{ color: 'var(--body)' }}>{t('noData')}</p>
            )}
          </div>
        </div>
      )}

      {/* Filter bar — single row */}
      <div
        className="flex items-center gap-2 rounded-[5px] px-3 py-2"
        style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
      >
        <div className="flex items-center gap-1.5 flex-1 min-w-0">
          <Icon name="search" size="sm" style={{ color: 'var(--body)', flexShrink: 0 }} />
          <input
            className="flex-1 bg-transparent text-[13px] outline-none min-w-0"
            style={{ color: 'var(--heading)' }}
            placeholder={t('cache.searchPlaceholder')}
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
          />
        </div>
        <div className="h-5 w-px shrink-0" style={{ background: 'var(--border)' }} />
        <select value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }} style={selStyle}>
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
        </select>
        <ButtonV2 variant="danger" size="sm" onClick={() => setCleanupOpen(true)}>
          <Icon name="delete_sweep" size="sm" />
          {t('cache.cleanExpired')}
        </ButtonV2>
      </div>

      {/* Table */}
      <div
        className="rounded-[5px] overflow-hidden"
        style={{ background: 'var(--surface)', border: '1px solid var(--border)' }}
      >
        {isLoading ? (
          <div className="p-8 text-center text-[13px]" style={{ color: 'var(--body)' }}>{t('loading')}</div>
        ) : items.length === 0 ? (
          <div className="p-8 text-center text-[13px]" style={{ color: 'var(--body)' }}>{t('cache.noCache')}</div>
        ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Key', t('type'), t('cache.size'), t('cache.hitCount'), t('cache.lastAccessed'), t('actions')].map(h => (
                  <th key={h} className="text-left text-[11px] font-[400] uppercase tracking-wider py-2.5 px-3 first:pl-4" style={{ color: 'var(--body)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: any, i: number) => (
                <tr
                  key={i}
                  className="transition-colors duration-75"
                  style={{ borderBottom: '1px solid var(--border)' }}
                  onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--surface-low)' }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = '' }}
                >
                  <td className="py-2 px-3 pl-4 max-w-[260px]">
                    <span className="font-mono truncate block" style={{ color: 'var(--heading)' }}>{row.key}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      <EcosystemIcon type={row.adapter_type} size={13} />
                      <BadgeV2 variant="ecosystem">{row.adapter_type?.toUpperCase()}</BadgeV2>
                    </div>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{formatBytes(row.size || 0)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{row.hit_count || 0}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span style={{ color: 'var(--body)' }}>{formatTime(row.last_accessed)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <button
                      onClick={(e) => { e.stopPropagation(); setDeleteTarget(row.id) }}
                      className="bg-transparent cursor-pointer p-1 rounded-[3px] opacity-40 hover:opacity-100 transition-opacity"
                      style={{ color: 'var(--error)' }}
                    >
                      <Icon name="delete" size="sm" />
                    </button>
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
          <span className="text-[12px]" style={{ color: 'var(--body)' }}>
            {t('totalItems', { total, page, totalPages })}
          </span>
          <div className="flex items-center gap-1">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{t('prevPage')}</ButtonV2>
            <span className="text-[12px] font-mono tabular-nums px-2" style={{ color: 'var(--body)' }}>{page}/{totalPages}</span>
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
