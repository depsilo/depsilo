import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Treemap, ResponsiveContainer, Tooltip } from 'recharts'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import EcosystemIcon from '@/components/EcosystemIcon'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import { ECOSYSTEM_COLORS as ECO_COLORS } from '@/lib/ecosystemColors'

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'docker']

export default function CacheManageV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [page, setPage] = useState(1)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [warmupOpen, setWarmupOpen] = useState(false)
  const [warmupEco, setWarmupEco] = useState('pypi')
  const [warmupText, setWarmupText] = useState('')
  const [warmupLoading, setWarmupLoading] = useState(false)
  const [warmupResult, setWarmupResult] = useState<string | null>(null)

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
    background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)',
    borderRadius: 4, padding: '4px 8px', fontSize: 12, outline: 'none', cursor: 'pointer',
  }

  return (
    <div className="space-y-12">
      {/* ── Storage overview + Treemap (no card wrappers) ─────────── */}
      {distribution && (
        <div className="grid grid-cols-3 gap-x-10 gap-y-12">
          {/* Left: usage + ecosystem breakdown */}
          <section className="col-span-1">
            <SectionHeader title={t('cache.storageOverview')} />
            <p className="font-mono tabular-nums mb-2" style={{ fontSize: 32, fontWeight: 600, color: 'var(--text)', letterSpacing: '-0.035em', lineHeight: 1.05 }}>
              {formatBytes(distribution.total_size)}
              <span className="text-[12px] font-[400] ml-2" style={{ color: 'var(--text-soft)' }}>
                / {formatBytes(distribution.max_size)}
              </span>
            </p>
            {/* Progress bar */}
            <div className="h-2 rounded-full overflow-hidden flex mb-4" style={{ background: 'var(--bg-soft)' }}>
              {distribution.by_type.map((bt: any) => {
                const pct = distribution.max_size > 0 ? (bt.size / distribution.max_size) * 100 : 0
                return (
                  <div
                    key={bt.type}
                    className="h-full"
                    style={{ width: `${pct}%`, background: ECO_COLORS[bt.type] || 'var(--brand)' }}
                    title={`${bt.type.toUpperCase()}: ${formatBytes(bt.size)}`}
                  />
                )
              })}
            </div>
            {/* Ecosystem breakdown */}
            <div className="space-y-1.5">
              {distribution.by_type.map((bt: any) => {
                const pct = distribution.total_size > 0 ? ((bt.size / distribution.total_size) * 100).toFixed(1) : '0'
                return (
                  <div key={bt.type} className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full shrink-0" style={{ background: ECO_COLORS[bt.type] || 'var(--brand)' }} />
                    <EcosystemIcon type={bt.type} size={12} />
                    <span className="text-[11px] uppercase flex-1" style={{ color: 'var(--text)' }}>{bt.type}</span>
                    <span className="text-[11px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>{formatBytes(bt.size)}</span>
                    <span className="text-[10px] font-mono tabular-nums w-10 text-right" style={{ color: 'var(--text-subtle)' }}>{pct}%</span>
                    <span className="text-[10px]" style={{ color: 'var(--text-subtle)' }}>{bt.file_count}f</span>
                  </div>
                )
              })}
            </div>
          </section>

          {/* Right: Treemap */}
          <section className="col-span-2">
            <SectionHeader title={t('cache.storageDistribution')} />
            {distribution.top_packages && distribution.top_packages.length > 0 ? (
              <ResponsiveContainer width="100%" height={200}>
                <Treemap
                  data={distribution.top_packages.map((p: any) => ({ name: p.name, size: p.size, type: p.type, hits: p.hit_count }))}
                  dataKey="size" aspectRatio={4 / 3} stroke="var(--bg)" isAnimationActive={false}
                  content={({ x, y, width, height, name, size }: any) => {
                    if (width < 4 || height < 4) return <g />
                    const showLabel = width > 60 && height > 30 && name
                    const type = distribution.top_packages.find((p: any) => p.name === name)?.type
                    return (
                      <g>
                        <rect x={x} y={y} width={width} height={height}
                          fill={ECO_COLORS[type] || 'var(--brand)'}
                          fillOpacity={0.25 + Math.min(0.5, ((size || 0) / (distribution.top_packages[0]?.size || 1)) * 0.5)}
                          stroke="var(--bg)" strokeWidth={1.5} rx={3}
                        />
                        {showLabel && (
                          <foreignObject x={x} y={y} width={width} height={height}>
                            <div style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: 3, boxSizing: 'border-box', overflow: 'hidden' }}>
                              <span style={{ color: '#fff', fontSize: 11, fontWeight: 500, lineHeight: 1.2, textAlign: 'center', wordBreak: 'break-all' }}>{name}</span>
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
                      <div className="rounded-[4px] p-2 text-[11px]" style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}>
                        <p className="font-[500]" style={{ color: 'var(--text)' }}>{item.name}</p>
                        <p style={{ color: 'var(--text-soft)' }}>{item.type?.toUpperCase()} · {formatBytes(item.size)} · {item.hits} hits</p>
                      </div>
                    )
                  }} />
                </Treemap>
              </ResponsiveContainer>
            ) : (
              <EmptyState icon="grid_view" title={t('noData')} minHeight={200} />
            )}
          </section>
        </div>
      )}

      {/* ── Filter row + actions ─────────────────────────────────── */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="flex items-center gap-1.5 flex-1 min-w-[240px] rounded-[4px] px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
          <input
            className="flex-1 bg-transparent text-[13px] outline-none min-w-0"
            style={{ color: 'var(--text)' }}
            placeholder={t('cache.searchPlaceholder')}
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
          />
        </div>
        <select value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }} style={selStyle}>
          <option value="all">{t('all')}</option>
          {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
        </select>
        <ButtonV2 variant="secondary" size="sm" onClick={() => { setWarmupOpen(true); setWarmupResult(null) }}>
          <Icon name="download" size="sm" />
          {t('cache.warmup')}
        </ButtonV2>
        <ButtonV2 variant="danger" size="sm" onClick={() => setCleanupOpen(true)}>
          <Icon name="delete_sweep" size="sm" />
          {t('cache.cleanExpired')}
        </ButtonV2>
      </div>

      {/* ── Cache entries table (bare) ───────────────────────────── */}
      <div>
        {isLoading ? (
          <div className="py-8 text-center text-[13px]" style={{ color: 'var(--text-soft)' }}>{t('loading')}</div>
        ) : items.length === 0 ? (
          <EmptyState icon="inbox" title={t('cache.noCache')} minHeight={200} />
        ) : (
          <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Key', t('type'), t('cache.size'), t('cache.hitCount'), t('cache.lastAccessed'), t('actions')].map(h => (
                  <th key={h} className="text-left text-[10px] font-mono font-[600] uppercase tracking-[0.08em] py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row: any, i: number) => (
                <tr
                  key={i}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <td className="py-2 px-3 pl-0 max-w-[260px]">
                    <span className="font-mono truncate block" style={{ color: 'var(--text)' }}>{row.key}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      <EcosystemIcon type={row.adapter_type} size={13} />
                      <BadgeV2 variant="ecosystem">{row.adapter_type?.toUpperCase()}</BadgeV2>
                    </div>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>{formatBytes(row.size || 0)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>{row.hit_count || 0}</span>
                  </td>
                  <td className="py-2 px-3">
                    <span style={{ color: 'var(--text-soft)' }}>{formatTime(row.last_accessed)}</span>
                  </td>
                  <td className="py-2 px-3">
                    <button
                      onClick={(e) => { e.stopPropagation(); setDeleteTarget(row.id) }}
                      className="bg-transparent cursor-pointer p-1.5 rounded-[3px] opacity-40 hover:opacity-100 transition-[opacity,transform] active:scale-[0.96]"
                      style={{ color: 'var(--danger)' }}
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
          <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
            {t('totalItems', { total, page, totalPages })}
          </span>
          <div className="flex items-center gap-1">
            <ButtonV2 variant="secondary" size="sm" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{t('prevPage')}</ButtonV2>
            <span className="text-[12px] font-mono tabular-nums px-2" style={{ color: 'var(--text-soft)' }}>{page}/{totalPages}</span>
            <ButtonV2 variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>{t('nextPage')}</ButtonV2>
          </div>
        </div>
      )}

      {/* Delete Modal */}
      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('cache.confirmDelete')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('cache.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>

      {/* Cleanup Modal */}
      <ModalV2 open={cleanupOpen} onClose={() => setCleanupOpen(false)} title={t('cache.cleanExpiredTitle')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('cache.cleanExpiredMsg')}</p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setCleanupOpen(false)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={cleanupMutation.isPending} onClick={() => cleanupMutation.mutate()}>
            {cleanupMutation.isPending ? t('cache.cleaning') : t('cache.confirmClean')}
          </ButtonV2>
        </div>
      </ModalV2>

      {/* Warmup Modal */}
      <ModalV2 open={warmupOpen} onClose={() => setWarmupOpen(false)} title={t('cache.warmupTitle')}>
        <div className="space-y-4">
          <SelectV2 label={t('cache.warmupEcosystem')} value={warmupEco} onChange={(e) => setWarmupEco(e.target.value)}>
            {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
          </SelectV2>
          <div>
            <label className="block text-[14px] font-[400] mb-1" style={{ color: 'var(--text-muted)' }}>
              {t('cache.warmupPackages')}
            </label>
            <textarea
              className="w-full rounded-[4px] px-3 py-2 text-[13px] font-mono resize-none"
              style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', color: 'var(--text)', outline: 'none', height: 160 }}
              placeholder={t('cache.warmupPlaceholder')}
              value={warmupText}
              onChange={(e) => setWarmupText(e.target.value)}
            />
          </div>
          {warmupResult && (
            <div className="rounded-[4px] px-3 py-2 text-[13px]" style={{ background: 'var(--ok-fill)', color: 'var(--ok-text)', border: '1px solid var(--ok-border)' }}>
              {warmupResult}
            </div>
          )}
          <div className="flex justify-end gap-3">
            <ButtonV2 variant="secondary" onClick={() => setWarmupOpen(false)}>{t('cancel')}</ButtonV2>
            <ButtonV2
              disabled={warmupLoading || !warmupText.trim()}
              onClick={async () => {
                setWarmupLoading(true)
                setWarmupResult(null)
                try {
                  const packages = warmupText.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'))
                  const res = await adminApi.warmupCache({ ecosystem: warmupEco, packages })
                  setWarmupResult(t('cache.warmupStarted', { count: res.data?.packages || packages.length }))
                } catch { setWarmupResult('Failed') }
                finally { setWarmupLoading(false) }
              }}
            >
              <Icon name="download" size="sm" />
              {warmupLoading ? t('cache.warmupLoading') : t('cache.warmupStart')}
            </ButtonV2>
          </div>
        </div>
      </ModalV2>
    </div>
  )
}
