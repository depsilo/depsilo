import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Treemap, ResponsiveContainer, Tooltip } from 'recharts'
import type { TooltipContentProps, TooltipValueType, TreemapNode } from 'recharts'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import ButtonV2 from '@/components/Button'
import BadgeV2 from '@/components/Badge'
import SelectV2 from '@/components/Select'
import TextareaV2 from '@/components/Textarea'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import EcosystemIcon from '@/components/EcosystemIcon'
import SectionHeader from '@/components/SectionHeader'
import EmptyState from '@/components/EmptyState'
import InlineNotice from '@/components/InlineNotice'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import TableViewport from '@/components/TableViewport'
import { useAppToast } from '@/components/Toast'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'
import { ECOSYSTEM_COLORS as ECO_COLORS } from '@/lib/ecosystemColors'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { CacheQuery } from '@/lib/adminApi.types'

const WARMUP_ECOSYSTEMS = ['pypi', 'npm']
const WARMUP_STATUS_KEYS: Record<string, string> = {
  queued: 'cache.warmupStatus.queued', running: 'cache.warmupStatus.running',
  cancelling: 'cache.warmupStatus.cancelling', cancelled: 'cache.warmupStatus.cancelled',
  succeeded: 'cache.warmupStatus.succeeded', partial: 'cache.warmupStatus.partial',
  failed: 'cache.warmupStatus.failed', interrupted: 'cache.warmupStatus.interrupted',
}

interface CacheTreemapItem { name: string; size: number; type: string; hits: number }

type WarmupState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'accepted'; count: number }
  | { status: 'failed'; message: string }

function isCacheTreemapItem(value: unknown): value is CacheTreemapItem {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  return typeof item.name === 'string'
    && typeof item.size === 'number'
    && typeof item.type === 'string'
    && typeof item.hits === 'number'
}

export default function CacheManageV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const warmupJobStorageKey = 'depsilo:last-warmup-job'
  const [search, setSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [page, setPage] = useState(1)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [warmupOpen, setWarmupOpen] = useState(false)
  const [warmupEco, setWarmupEco] = useState('pypi')
  const [warmupText, setWarmupText] = useState('')
  const [warmupJobId, setWarmupJobId] = useState<string | null>(() => readLocalStorage(warmupJobStorageKey))
  const [warmupState, setWarmupState] = useState<WarmupState>({ status: 'idle' })

  const params: CacheQuery = { page, page_size: 20 }
  if (search) params.search = search
  if (adapterType !== 'all') params.adapter_type = adapterType

  const distributionQuery = useQuery({
    queryKey: ['admin', 'cache', 'distribution'],
    queryFn: ({ signal }) => adminApi.getCacheDistribution({ signal }),
    refetchInterval: 30000,
    retry: false,
  })
  const distData = distributionQuery.data
  const distribution = distData?.data

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'cache', params],
    queryFn: ({ signal }) => adminApi.listCache(params, { signal }),
    retry: false,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteCache(id),
    onSuccess: () => { setDeleteTarget(null) },
    onSettled: () => { void queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] }) },
  })

  const cleanupPreviewQuery = useQuery({
    queryKey: ['admin', 'cache', 'cleanup-preview'],
    queryFn: ({ signal }) => adminApi.previewCacheCleanup({ page: 1, page_size: 8 }, { signal }),
    enabled: cleanupOpen,
    retry: false,
  })

  const cleanupMutation = useMutation({
    mutationFn: async () => {
      const planId = cleanupPreviewQuery.data?.data.plan_id
      return (await adminApi.cleanupCache(planId)).data
    },
    onSuccess: (result) => {
      setCleanupOpen(false)
      toast.show({ tone: 'success', message: result.message })
    },
    onSettled: () => { void queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] }) },
  })

  const warmupJobQuery = useQuery({
    queryKey: ['admin', 'cache', 'warmup', warmupJobId],
    queryFn: ({ signal }) => adminApi.getWarmup(warmupJobId as string, { signal }),
    enabled: warmupOpen && warmupJobId !== null,
    refetchInterval: query => {
      if (query.state.error) return false
      const status = query.state.data?.data.status
      return status && ['succeeded', 'partial', 'failed', 'cancelled', 'interrupted'].includes(status) ? false : 1500
    },
    retry: false,
  })
  const cancelWarmupMutation = useMutation({
    mutationFn: () => adminApi.cancelWarmup(warmupJobId as string),
    onSuccess: () => { void warmupJobQuery.refetch() },
  })
  const retryWarmupMutation = useMutation({
    mutationFn: () => adminApi.retryWarmup(warmupJobId as string),
    onSuccess: (response) => {
      const nextID = response.data?.job_id
      if (nextID) {
        setWarmupJobId(nextID)
        writeLocalStorage(warmupJobStorageKey, nextID)
        setWarmupState({ status: 'accepted', count: response.data.packages || 0 })
      }
    },
  })

  const items = data?.data?.items || []
  const total = data?.data?.total || 0
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message
  const distributionApiError = getApiError(distributionQuery.error)
  const distributionErrorMessage = distributionApiError.status === 403 ? t('common.permissionDenied') : distributionApiError.message

  return (
    <AdminPage
      description={t('cache.subtitle')}
      actions={canWrite ? (
        <>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { setWarmupOpen(true); setWarmupState({ status: 'idle' }) }}>
            <Icon name="download" size="sm" />
            {t('cache.warmup')}
          </ButtonV2>
          <ButtonV2 type="button" variant="danger" size="sm" onClick={() => { cleanupMutation.reset(); setCleanupOpen(true) }}>
            <Icon name="delete_sweep" size="sm" />
            {t('cache.cleanExpired')}
          </ButtonV2>
        </>
      ) : undefined}
    >
      <div className="space-y-12">
      {/* ── Storage overview + Treemap (no card wrappers) ─────────── */}
      {distributionQuery.isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]"><span aria-hidden="true">{t('loading')}</span></div>
      ) : distributionQuery.isError && !distData ? (
        <QueryErrorState message={distributionErrorMessage} onRetry={() => { void distributionQuery.refetch() }} />
      ) : (
        <div className="space-y-3">
          {distData && distributionQuery.isRefetchError && (
            <StaleDataNotice onRefresh={() => { void distributionQuery.refetch() }} />
          )}
          {distribution ? (
        <div className="grid grid-cols-1 gap-y-12 xl:grid-cols-3 xl:gap-x-10">
          {/* Left: usage + ecosystem breakdown */}
          <section>
            <SectionHeader title={t('cache.storageOverview')} />
            <p data-metric-value className="mb-2 whitespace-nowrap font-mono tabular-nums" style={{ fontSize: 32, fontWeight: 600, color: 'var(--text)', lineHeight: 1.05 }}>
              {formatBytes(distribution.total_size)}
              <span className="text-[12px] font-[400] ml-2" style={{ color: 'var(--text-soft)' }}>
                / {formatBytes(distribution.max_size)}
              </span>
            </p>
            {/* Progress bar */}
            <div className="h-2 rounded-full overflow-hidden flex mb-4" style={{ background: 'var(--bg-soft)' }}>
              {distribution.by_type.map((bt) => {
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
              {distribution.by_type.map((bt) => {
                const pct = distribution.total_size > 0 ? ((bt.size / distribution.total_size) * 100).toFixed(1) : '0'
                return (
                  <div key={bt.type} className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full shrink-0" style={{ background: ECO_COLORS[bt.type] || 'var(--brand)' }} />
                    {isAdminEcosystem(bt.type) && <EcosystemIcon type={bt.type} size={12} />}
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
          <section className="xl:col-span-2">
            <SectionHeader title={t('cache.storageDistribution')} />
            {distribution.top_packages && distribution.top_packages.length > 0 ? (
              <ResponsiveContainer width="100%" height={200}>
                <Treemap
                  data={distribution.top_packages.map((p) => ({ name: p.name, size: p.size, type: p.type, hits: p.hit_count }))}
                  dataKey="size" aspectRatio={4 / 3} stroke="var(--bg)" isAnimationActive={false}
                  content={(node: TreemapNode) => {
                    const { x, y, width, height, name } = node
                    const size = typeof node.size === 'number' ? node.size : node.value
                    if (width < 4 || height < 4) return <g />
                    const showLabel = width > 60 && height > 30 && name
                    const type = distribution.top_packages.find((p) => p.name === name)?.type
                    const fill = type ? ECO_COLORS[type] || 'var(--brand)' : 'var(--brand)'
                    return (
                      <g>
                        <rect x={x} y={y} width={width} height={height}
                          fill={fill}
                          fillOpacity={0.25 + Math.min(0.5, ((size || 0) / (distribution.top_packages[0]?.size || 1)) * 0.5)}
                          stroke="var(--bg)" strokeWidth={1.5} rx={3}
                        />
                        {showLabel && (
                          <foreignObject x={x} y={y} width={width} height={height}>
                            <div style={{ width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 3, boxSizing: 'border-box', overflow: 'hidden' }}>
                              <div style={{ maxWidth: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', padding: '2px 4px', borderRadius: 3, background: 'var(--surface)', overflow: 'hidden' }}>
                                <span style={{ color: 'var(--text)', fontSize: 11, fontWeight: 500, lineHeight: 1.2, textAlign: 'center', wordBreak: 'break-all' }}>{name}</span>
                                <span style={{ color: 'var(--text-soft)', fontSize: 10, fontFamily: 'ui-monospace, monospace' }}>{formatBytes(size)}</span>
                              </div>
                            </div>
                          </foreignObject>
                        )}
                      </g>
                    )
                  }}
                >
                  <Tooltip content={({ payload }: TooltipContentProps<TooltipValueType, string | number>) => {
                    if (!payload?.length) return null
                    const item: unknown = payload[0]?.payload
                    if (!isCacheTreemapItem(item)) return null
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
          ) : <EmptyState icon="grid_view" title={t('noData')} minHeight={200} />}
        </div>
      )}

      <div data-admin-filters className="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-[4px] px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
          <input
            aria-label={t('cache.searchLabel')}
            className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]"
            style={{ color: 'var(--text)' }}
            placeholder={t('cache.searchPlaceholder')}
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1) }}
          />
        </div>
        <SelectV2 className="min-h-10 sm:w-auto" aria-label={t('cache.ecosystemFilter')} value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }}>
          <option value="all">{t('all')}</option>
          {operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
        </SelectV2>
      </div>

      {isPending ? (
        <div aria-busy="true" className="py-8 text-center text-[13px] text-[var(--text-soft)]">
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      ) : isError && !data ? (
        <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
      ) : (
        <div className="space-y-3">
          {data && isRefetchError && <StaleDataNotice onRefresh={() => { void refetch() }} />}
          {items.length === 0 ? (
            <EmptyState icon="inbox" title={t('cache.noCache')} minHeight={200} />
          ) : (
            <TableViewport label={t('cache.table')} minWidth={820}>
            <table className="w-full text-[12px]">
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border)' }}>
                {['Key', t('type'), t('cache.size'), t('cache.hitCount'), t('cache.lastAccessed'), t('actions')].map(h => (
                  <th key={h} scope="col" className="text-left text-[10px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((row) => (
                <tr
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <td className="py-2 px-3 pl-0 max-w-[260px]">
                    <span className="font-mono truncate block" style={{ color: 'var(--text)' }}>{row.key}</span>
                  </td>
                  <td className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.adapter_type) && <EcosystemIcon type={row.adapter_type} size={13} />}
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
                    {canWrite && <IconButton
                      icon="delete"
                      label={t('cache.deleteNamed', { key: row.key })}
                      tone="danger"
                      onClick={(e) => { e.stopPropagation(); deleteMutation.reset(); setDeleteTarget(row.id) }}
                    />}
                  </td>
                </tr>
              ))}
            </tbody>
            </table>
            </TableViewport>
          )}
        </div>
      )}

      {/* Pagination */}
      <AdminPagination page={page} pageSize={20} total={total} onPageChange={setPage} />

      {/* Delete Modal */}
      <ModalV2 open={deleteTarget !== null} onClose={() => {
        if (deleteMutation.isPending) return
        deleteMutation.reset()
        setDeleteTarget(null)
      }} title={t('cache.confirmDelete')} closeDisabled={deleteMutation.isPending}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('cache.confirmDeleteMsg')}</p>
        {deleteMutation.isError && <div className="mb-4"><InlineNotice tone="danger">{getApiError(deleteMutation.error).message}</InlineNotice></div>}
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" disabled={deleteMutation.isPending} onClick={() => {
            deleteMutation.reset()
            setDeleteTarget(null)
          }}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" aria-busy={deleteMutation.isPending || undefined} disabled={deleteMutation.isPending || !canWrite} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>

      {/* Cleanup Modal */}
      <ModalV2 open={cleanupOpen} onClose={() => {
        if (cleanupMutation.isPending) return
        cleanupMutation.reset()
        setCleanupOpen(false)
      }} title={t('cache.cleanExpiredTitle')} closeDisabled={cleanupMutation.isPending}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('cache.cleanExpiredMsg')}</p>
        {cleanupPreviewQuery.isPending ? (
          <div aria-busy="true" className="mb-6 text-[13px] text-[var(--text-soft)]">{t('cache.previewLoading')}</div>
        ) : cleanupPreviewQuery.isError ? (
          <div className="mb-6"><InlineNotice tone="warning">{t('cache.previewUnavailable')}</InlineNotice></div>
        ) : cleanupPreviewQuery.data?.data ? (
          <div className="mb-6 space-y-3" data-testid="cache-cleanup-preview">
            <p className="text-[13px] text-[var(--text-soft)]">
              {t('cache.previewSummary', {
                count: cleanupPreviewQuery.data.data.candidate_count,
                bytes: formatBytes(cleanupPreviewQuery.data.data.logical_bytes),
              })}
            </p>
            {cleanupPreviewQuery.data.data.items.length > 0 ? (
              <ul className="max-h-40 overflow-auto space-y-1 text-[12px] font-mono" aria-label={t('cache.previewItems')}>
                {cleanupPreviewQuery.data.data.items.map((item) => (
                  <li key={item.id} className="flex justify-between gap-3">
                    <span className="truncate">{item.package_name || item.key}</span>
                    <span className="shrink-0 text-[var(--text-soft)]">{formatBytes(item.size)} · {item.reason}</span>
                  </li>
                ))}
              </ul>
            ) : <p className="text-[12px] text-[var(--text-soft)]">{t('cache.previewEmpty')}</p>}
            <p className="text-[12px] text-[var(--text-soft)]">{t('cache.previewSnapshot')}</p>
          </div>
        ) : null}
        {cleanupMutation.isError && <div className="mb-4"><InlineNotice tone="danger">{getApiError(cleanupMutation.error).message}</InlineNotice></div>}
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" disabled={cleanupMutation.isPending} onClick={() => {
            cleanupMutation.reset()
            setCleanupOpen(false)
          }}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" aria-busy={cleanupMutation.isPending || undefined} disabled={cleanupMutation.isPending || !canWrite || !cleanupPreviewQuery.data?.data.plan_id} onClick={() => cleanupMutation.mutate()}>
            {cleanupMutation.isPending ? t('cache.cleaning') : t('cache.confirmClean')}
          </ButtonV2>
        </div>
      </ModalV2>

      {/* Warmup Modal */}
      <ModalV2 open={warmupOpen} onClose={() => setWarmupOpen(false)} title={t('cache.warmupTitle')} closeDisabled={warmupState.status === 'submitting'}>
        <div className="space-y-4">
          <SelectV2 label={t('cache.warmupEcosystem')} value={warmupEco} onChange={(e) => setWarmupEco(e.target.value)}>
            {WARMUP_ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
          </SelectV2>
          <TextareaV2
            label={t(warmupEco === 'npm' ? 'cache.warmupPackagesNpm' : 'cache.warmupPackages')}
            className="resize-none font-mono"
            style={{ height: 160 }}
            placeholder={t(warmupEco === 'npm' ? 'cache.warmupPlaceholderNpm' : 'cache.warmupPlaceholder')}
            value={warmupText}
            onChange={(e) => setWarmupText(e.target.value)}
          />
          {warmupState.status === 'submitting' && <InlineNotice tone="warning">{t('cache.warmupSubmitting')}</InlineNotice>}
          {warmupState.status === 'accepted' && <InlineNotice tone="info">{t('cache.warmupAccepted', { count: warmupState.count })}</InlineNotice>}
          {warmupState.status === 'failed' && <InlineNotice tone="danger">{t('cache.warmupFailed', { reason: warmupState.message })}</InlineNotice>}
          {warmupJobId && warmupJobQuery.isError && <InlineNotice tone="danger">{getApiError(warmupJobQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(warmupJobQuery.error).status === 404 ? t('cache.warmupJobExpired') : t('cache.warmupJobLoadFailed')}</InlineNotice>}
          {cancelWarmupMutation.isError && <InlineNotice tone="danger">{t('cache.warmupActionFailed')}</InlineNotice>}
          {retryWarmupMutation.isError && <InlineNotice tone="danger">{t('cache.warmupActionFailed')}</InlineNotice>}
          {warmupJobId && warmupJobQuery.data?.data && (
            <div className="space-y-3 rounded-[6px] border border-[var(--border)] p-3" data-testid="warmup-job-status">
              <div className="flex justify-between gap-3 text-[12px]">
                <span>{t('cache.warmupJobStatus', { status: t(WARMUP_STATUS_KEYS[warmupJobQuery.data.data.status] || 'cache.warmupStatus.unknown') })}</span>
                <span className="font-mono text-[var(--text-soft)]">{warmupJobId}</span>
              </div>
              <ul className="max-h-40 overflow-auto space-y-1 text-[12px]" aria-label={t('cache.warmupResults')}>
                {warmupJobQuery.data.data.items.map(item => (
                  <li key={item.package} className="flex justify-between gap-3">
                    <span className="truncate font-mono">{item.package}</span>
                    <span className="shrink-0 text-[var(--text-soft)]">{item.detail === 'metadata cached' ? t('cache.warmupItemMetadata') : item.detail === 'request failed' ? t('cache.warmupItemFailed') : item.detail === 'warmup stopped before this package completed' ? t('cache.warmupItemStopped') : t(WARMUP_STATUS_KEYS[item.status] || 'cache.warmupStatus.unknown')}</span>
                  </li>
                ))}
              </ul>
              {['queued', 'running', 'cancelling'].includes(warmupJobQuery.data.data.status) && canWrite && (
                <ButtonV2 type="button" variant="secondary" size="sm" disabled={cancelWarmupMutation.isPending} onClick={() => { if (window.confirm(t('cache.warmupCancelConfirm'))) cancelWarmupMutation.mutate() }}>
                  {t('cache.warmupCancel')}
                </ButtonV2>
              )}
              {['partial', 'failed', 'cancelled', 'interrupted'].includes(warmupJobQuery.data.data.status) && canWrite && warmupJobQuery.data.data.items.some(item => item.status === 'failed') && (
                <ButtonV2 type="button" variant="secondary" size="sm" disabled={retryWarmupMutation.isPending} onClick={() => retryWarmupMutation.mutate()}>
                  {t('cache.warmupRetryFailed')}
                </ButtonV2>
              )}
            </div>
          )}
          <div className="flex justify-end gap-3">
            <ButtonV2 variant="secondary" disabled={warmupState.status === 'submitting'} onClick={() => setWarmupOpen(false)}>{t('cancel')}</ButtonV2>
            <ButtonV2
              disabled={warmupState.status === 'submitting' || warmupText.split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('#')).length === 0}
              onClick={async () => {
                setWarmupState({ status: 'submitting' })
                try {
                  const packages = warmupText.split('\n').map(l => l.trim()).filter(l => l && !l.startsWith('#'))
                  const res = await adminApi.warmupCache({ ecosystem: warmupEco, packages })
                  setWarmupJobId(res.data?.job_id || null)
                  if (res.data?.job_id) writeLocalStorage(warmupJobStorageKey, res.data.job_id)
                  setWarmupState({ status: 'accepted', count: res.data?.packages || packages.length })
                } catch (error) {
                  setWarmupState({ status: 'failed', message: getApiError(error).message })
                }
              }}
            >
              <Icon name="download" size="sm" />
              {warmupState.status === 'submitting' ? t('cache.warmupLoading') : t('cache.warmupStart')}
            </ButtonV2>
          </div>
        </div>
      </ModalV2>
      </div>
    </AdminPage>
  )
}
