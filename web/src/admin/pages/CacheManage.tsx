import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Meter, MeterTrack } from '@/components/ui/meter'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import Icon from '@/components/Icon'
import EcosystemIcon from '@/components/EcosystemIcon'

import QueryErrorState from '@/components/QueryErrorState'

import { useAppToast } from '@/components/Toast'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import ConfirmActionDialog from '@/admin/components/ConfirmActionDialog'
import SnapshotSection from '@/admin/components/SnapshotSection'
import DownloadTransfers from '@/admin/components/DownloadTransfers'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'
import { ECOSYSTEM_COLORS as ECO_COLORS } from '@/lib/ecosystemColors'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { CacheCleanupResponse, CacheQuery } from '@/lib/adminApi.types'

const WARMUP_ECOSYSTEMS = ['pypi', 'npm']
const WARMUP_STATUS_KEYS: Record<string, string> = {
  queued: 'cache.warmupStatus.queued', running: 'cache.warmupStatus.running',
  cancelling: 'cache.warmupStatus.cancelling', cancelled: 'cache.warmupStatus.cancelled',
  succeeded: 'cache.warmupStatus.succeeded', partial: 'cache.warmupStatus.partial',
  failed: 'cache.warmupStatus.failed', interrupted: 'cache.warmupStatus.interrupted',
}

type WarmupState =
  | { status: 'idle' }
  | { status: 'submitting' }
  | { status: 'accepted'; count: number }
  | { status: 'failed'; message: string }

export default function CacheManage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const warmupJobStorageKey = 'depsilo:last-warmup-job'
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const [kind, setKind] = useState<'all' | 'artifact' | 'metadata'>('all')
  const [adapterType, setAdapterType] = useState('all')
  const [page, setPage] = useState(1)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [cleanupResult, setCleanupResult] = useState<CacheCleanupResponse | null>(null)
  const [cleanupSession, setCleanupSession] = useState(0)
  const [warmupOpen, setWarmupOpen] = useState(false)
  const [cancelWarmupOpen, setCancelWarmupOpen] = useState(false)
  const [warmupEco, setWarmupEco] = useState('pypi')
  const [warmupText, setWarmupText] = useState('')
  const [warmupJobId, setWarmupJobId] = useState<string | null>(() => readLocalStorage(warmupJobStorageKey))
  const [warmupState, setWarmupState] = useState<WarmupState>({ status: 'idle' })

  const params: CacheQuery = { page, page_size: 20 }
  if (appliedSearch) params.search = appliedSearch
  if (kind !== 'all') params.cache_kind = kind
  if (adapterType !== 'all') params.adapter_type = adapterType

  const distributionQuery = useQuery({
    queryKey: ['admin', 'cache', 'distribution'],
    queryFn: ({ signal }) => adminApi.getCacheDistribution({ signal }),
    refetchInterval: 30000,
    retry: false,
  })
  const distData = distributionQuery.data
  const distribution = distData?.data

  const { data, error, isPending, isError, isRefetchError, isSuccess, isFetching, refetch } = useQuery({
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
    queryKey: ['admin', 'cache', 'cleanup-preview', cleanupSession],
    queryFn: ({ signal }) => adminApi.previewCacheCleanup({ page: 1, page_size: 8 }, { signal }),
    enabled: cleanupOpen,
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

  const cleanupMutation = useMutation({
    mutationFn: async () => {
      const planId = cleanupPreviewQuery.data?.data.plan_id
      return (await adminApi.cleanupCache(planId)).data
    },
    onSuccess: (result) => {
      setCleanupResult(result)
      const outcome = result.outcome || (result.failed || result.skipped ? 'partial' : 'succeeded')
      if (outcome === 'succeeded' && !result.interrupted) {
        setCleanupOpen(false)
        toast.show({ tone: 'success', message: result.message })
      } else {
        toast.show({ tone: outcome === 'failed' ? 'danger' : 'warning', message: result.message })
      }
    },
    onSettled: () => {
      void queryClient.invalidateQueries({
        queryKey: ['admin', 'cache'],
        predicate: query => query.queryKey[2] !== 'cleanup-preview',
      })
    },
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
    onSuccess: () => { setCancelWarmupOpen(false); void warmupJobQuery.refetch() },
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
  const selectedEntry = items.find(item => item.id === deleteTarget)
  const hasFilters = Boolean(appliedSearch || adapterType !== 'all' || kind !== 'all')
  function clearFilters() { setSearch(''); setAppliedSearch(''); setAdapterType('all'); setKind('all'); setPage(1) }
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message
  const distributionApiError = getApiError(distributionQuery.error)
  const distributionErrorMessage = distributionApiError.status === 403 ? t('common.permissionDenied') : distributionApiError.message

  return (
    <AdminPage
      description={t('cache.subtitle')}
      actions={canWrite ? (
        <>
          <Button type="button" variant="outline" size="sm" onClick={() => { setWarmupOpen(true); setWarmupState({ status: 'idle' }) }}>
            <Icon name="download" size="sm" />
            {t('cache.warmup')}
          </Button>
          <Button type="button" variant="destructive" size="sm" onClick={() => { cleanupMutation.reset(); setCleanupResult(null); setCleanupSession(value => value + 1); setCleanupOpen(true) }}>
            <Icon name="delete_sweep" size="sm" />
            {t('cache.cleanExpired')}
          </Button>
        </>
      ) : undefined}
    >
      <div className="space-y-6">
      <section aria-label={t('cache.storageOverview')} className="space-y-3">
        {distributionQuery.isPending ? <p role="status">{t('loading')}</p>
          : distributionQuery.isError && !distData ? <QueryErrorState message={distributionErrorMessage} onRetry={() => distributionQuery.refetch()} />
          : distribution && <>
            {distributionQuery.isRefetchError && <StaleDataNotice onRefresh={() => distributionQuery.refetch()} />}
            <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
              <div><p className="text-sm text-muted-foreground">{t('cache.storageOverview')}</p><p className="mt-1 font-mono text-2xl font-semibold tabular-nums">{formatBytes(distribution.total_size)}{distribution.max_size > 0 && <span className="ml-2 text-sm font-normal text-muted-foreground">/ {formatBytes(distribution.max_size)}</span>}</p></div>
              {distribution.max_size > 0 && <Meter className="w-44" value={distribution.total_size} max={distribution.max_size} aria-label={t('cache.storageOverview')} aria-valuetext={`${formatBytes(distribution.total_size)} / ${formatBytes(distribution.max_size)}`}><MeterTrack className="flex h-2">{distribution.by_type.map(bt => <div key={bt.type} className="h-full" style={{ width: `${bt.size / distribution.max_size * 100}%`, background: ECO_COLORS[bt.type] || 'var(--brand)' }} />)}</MeterTrack></Meter>}
              <div className="flex flex-wrap gap-x-5 gap-y-2 text-sm text-muted-foreground">{distribution.by_type.map(bt => <span key={bt.type} className="inline-flex items-center gap-2"><span className="size-2 rounded-full" style={{background:ECO_COLORS[bt.type] || 'var(--brand)'}} />{bt.type.toUpperCase()} <span className="font-mono">{formatBytes(bt.size)}</span><span>{t('cache.objectCount', {count:bt.file_count})}</span></span>)}</div>
            </div>
          </>}
      </section>
      <DownloadTransfers />
      <form onSubmit={event => { event.preventDefault(); setAppliedSearch(search.trim()); setPage(1) }} data-admin-filters className="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-sm px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
            <Input
              aria-label={t('cache.searchLabel')}
              className={"min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]" + ' h-9 rounded-none border-0 px-0 shadow-none focus-visible:ring-0 dark:bg-transparent'}
              style={{ color: 'var(--text)' }}
              placeholder={t('cache.searchPlaceholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
        </div>
          <NativeSelect className="min-h-10 sm:w-auto" aria-label={t('cache.ecosystemFilter')} value={adapterType} onChange={(e) => { setAdapterType(e.target.value); setPage(1) }}>
            <option value="all">{t('all')}</option>
            {operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
          </NativeSelect>
        <NativeSelect className="min-h-10 sm:w-auto" aria-label={t('cache.kindFilter')} value={kind} onChange={event => { setKind(event.target.value as typeof kind); setPage(1) }}><option value="all">{t('cache.allKinds')}</option><option value="artifact">{t('cache.artifact')}</option><option value="metadata">{t('cache.metadata')}</option></NativeSelect>
        <Button type="submit" variant="outline">{t('search')}</Button>
        {hasFilters && <Button type="button" variant="ghost" onClick={clearFilters}>{t('cache.clearFilters')}</Button>}
      </form>

      <div data-cache-list>
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
            <Empty style={{ minHeight: 200 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"inbox"} size="sm" /></EmptyMedia><EmptyTitle>{t(hasFilters ? 'cache.noMatches' : 'cache.noCache')}</EmptyTitle><EmptyDescription>{t(hasFilters ? 'cache.noMatchesHint' : 'cache.emptyHint')}</EmptyDescription></EmptyHeader></Empty>
          ) : (
            <TableContainer label={t('cache.table')} minWidth={820}>
            <Table className="w-full text-[12px]">
            <TableHeader>
              <TableRow style={{ borderBottom: '1px solid var(--border)' }}>
                {[t('cache.object'), t('type'), t('cache.size'), t('cache.hitCount'), t('cache.lastAccessed'), t('actions')].map(h => (
                  <TableHead key={h} scope="col" className="text-left text-[10px] font-mono font-[600] uppercase py-2 px-3 first:pl-0" style={{ color: 'var(--text-subtle)' }}>{h}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((row) => (
                <TableRow
                  key={row.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <TableCell className="py-2 px-3 pl-0 max-w-[260px]">
                    <details className="min-w-0"><summary className="cursor-pointer break-all font-medium">{row.package_name || row.key.split('/').pop() || row.key}</summary><code className="mt-2 block break-all text-xs text-muted-foreground">{row.key}</code></details>
                    {row.cache_kind && <span className="text-xs text-muted-foreground">{t(row.cache_kind === 'metadata' ? 'cache.metadata' : 'cache.artifact')}</span>}
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(row.adapter_type) && <EcosystemIcon type={row.adapter_type} size={13} />}
                      <Badge variant="default">{row.adapter_type?.toUpperCase()}</Badge>
                    </div>
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>{formatBytes(row.size || 0)}</span>
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>{row.hit_count || 0}</span>
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    <span style={{ color: 'var(--text-soft)' }}>{formatTime(row.last_accessed)}</span>
                  </TableCell>
                  <TableCell className="py-2 px-3">
                    {canWrite && <Tooltip>
                      <TooltipTrigger render={
                        <Button
                          onClick={(e) => { e.stopPropagation(); deleteMutation.reset(); setDeleteTarget(row.id) }}
                          variant="ghost"
                          size="icon"
                          data-icon-button
                          aria-label={t('cache.deleteNamed', { key: row.key })}
                          style={{ color: 'var(--danger-text)' }}
                        />
                      }>
                        <Icon name={"delete"} size="sm" />
                      </TooltipTrigger>
                      <TooltipContent>{t('cache.deleteNamed', { key: row.key })}</TooltipContent>
                    </Tooltip>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
            </Table>
            </TableContainer>
          )}
        </div>
      )}
      </div>

      {/* Pagination */}
      <AdminPagination ready={isSuccess && !isFetching} page={page} pageSize={20} total={total} onPageChange={setPage} />

      {/* Delete Modal */}
        <Dialog open={deleteTarget !== null} onOpenChange={(nextOpen) => {
          if (!nextOpen && !deleteMutation.isPending) (() => {
            if (deleteMutation.isPending) return
            deleteMutation.reset()
            setDeleteTarget(null)
          })()
        }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={deleteMutation.isPending}>
            <DialogTitle className="pr-10">{t('cache.confirmDelete')}</DialogTitle>
            <p className="text-[14px] mb-4" style={{ color: 'var(--text-soft)' }}>{t('cache.confirmDeleteMsg')}</p><code className="mb-4 block break-all text-sm">{selectedEntry?.key}</code>{selectedEntry && <p className="mb-4 text-sm text-muted-foreground">{selectedEntry.adapter_type.toUpperCase()} · {formatBytes(selectedEntry.size)}</p>}
            {deleteMutation.isError && <div className="mb-4"><Alert variant={"destructive"}><AlertDescription>{getApiError(deleteMutation.error).message}</AlertDescription></Alert></div>}
            <div className="flex justify-end gap-3">
              <Button variant="outline" disabled={deleteMutation.isPending} onClick={() => {
                deleteMutation.reset()
                setDeleteTarget(null)
              }}>{t('cancel')}</Button>
              <Button variant="destructive" aria-busy={deleteMutation.isPending || undefined} disabled={deleteMutation.isPending || !canWrite} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
                {deleteMutation.isPending ? t('deleting') : t('delete')}
              </Button>
            </div>
          </DialogContent>
        </Dialog>

      {/* Cleanup Modal */}
        <Dialog open={cleanupOpen} onOpenChange={(nextOpen) => {
          if (!nextOpen && !cleanupMutation.isPending) (() => {
            if (cleanupMutation.isPending) return
            cleanupMutation.reset()
            setCleanupResult(null)
            setCleanupOpen(false)
          })()
        }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={cleanupMutation.isPending}>
            <DialogTitle className="pr-10">{t('cache.cleanExpiredTitle')}</DialogTitle>
            <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('cache.cleanExpiredMsg')}</p>
            {cleanupPreviewQuery.isPending ? (
              <div aria-busy="true" className="mb-6 text-[13px] text-[var(--text-soft)]">{t('cache.previewLoading')}</div>
            ) : cleanupPreviewQuery.isError ? (
              <div className="mb-6 space-y-3"><Alert variant={"warning"}><AlertDescription>{t('cache.previewUnavailable')}</AlertDescription></Alert><Button variant="outline" onClick={() => cleanupPreviewQuery.refetch()}>{t('cache.retryPreview')}</Button></div>
            ) : cleanupPreviewQuery.data?.data ? (
              <div className="mb-6 space-y-3" data-testid="cache-cleanup-preview">
                <p className="text-[13px] text-[var(--text-soft)]">
                  {t('cache.previewSummary', {
                    count: cleanupPreviewQuery.data.data.candidate_count,
                    bytes: formatBytes(cleanupPreviewQuery.data.data.logical_bytes),
                  })}
                </p>
                <p className="text-[13px] text-[var(--text-soft)]">
                  {t('cache.previewPlanned', {
                    count: cleanupPreviewQuery.data.data.planned_count,
                    bytes: formatBytes(cleanupPreviewQuery.data.data.planned_bytes),
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
            {cleanupResult && (
              <div className="mb-4">
                <Alert variant={cleanupResult.outcome === 'failed' || cleanupResult.interrupted ? "destructive" : 'warning'}><AlertDescription>
                  {t('cache.cleanupResult', {
                    message: cleanupResult.message,
                    deleted: cleanupResult.deleted,
                    failed: cleanupResult.failed,
                    skipped: cleanupResult.skipped || 0,
                    planned: cleanupResult.planned_count || cleanupResult.planned || 0,
                  })}
                </AlertDescription></Alert>
              </div>
            )}
            {cleanupMutation.isError && <div className="mb-4 space-y-3"><Alert variant="destructive"><AlertDescription>{getApiError(cleanupMutation.error).message}</AlertDescription></Alert><Button variant="outline" onClick={() => { cleanupMutation.reset(); setCleanupSession(value => value + 1) }}>{t('cache.retryPreview')}</Button></div>}
            <div className="flex justify-end gap-3">
              <Button variant="outline" disabled={cleanupMutation.isPending} onClick={() => {
                cleanupMutation.reset()
                setCleanupResult(null)
                setCleanupOpen(false)
              }}>{t('cancel')}</Button>
              <Button variant="destructive" aria-busy={cleanupMutation.isPending || undefined} disabled={cleanupMutation.isPending || !!cleanupResult || !canWrite || !cleanupPreviewQuery.data?.data.plan_id} onClick={() => cleanupMutation.mutate()}>
                {cleanupMutation.isPending ? t('cache.cleaning') : t('cache.confirmClean')}
              </Button>
            </div>
          </DialogContent>
        </Dialog>

      {/* Warmup Modal */}
        <Dialog open={warmupOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !(warmupState.status === 'submitting')) setWarmupOpen(false) }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={warmupState.status === 'submitting'}>
            <DialogTitle className="pr-10">{t('cache.warmupTitle')}</DialogTitle>
            <div className="space-y-4">
              <Field>
                <FieldLabel>{t('cache.warmupEcosystem')}</FieldLabel>
                <NativeSelect value={warmupEco} onChange={(e) => setWarmupEco(e.target.value)}>
                  {WARMUP_ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
                </NativeSelect>
              </Field>
              <Field>
                <FieldLabel>{t(warmupEco === 'npm' ? 'cache.warmupPackagesNpm' : 'cache.warmupPackages')}</FieldLabel>
                <Textarea
                  className="resize-none font-mono"
                  style={{ height: 160 }}
                  placeholder={t(warmupEco === 'npm' ? 'cache.warmupPlaceholderNpm' : 'cache.warmupPlaceholder')}
                  value={warmupText}
                  onChange={(e) => setWarmupText(e.target.value)}
                />
              </Field>
              {warmupState.status === 'submitting' && <Alert variant={"warning"}><AlertDescription>{t('cache.warmupSubmitting')}</AlertDescription></Alert>}
              {warmupState.status === 'accepted' && <Alert variant={"info"}><AlertDescription>{t('cache.warmupAccepted', { count: warmupState.count })}</AlertDescription></Alert>}
              {warmupState.status === 'failed' && <Alert variant={"destructive"}><AlertDescription>{t('cache.warmupFailed', { reason: warmupState.message })}</AlertDescription></Alert>}
              {warmupJobId && warmupJobQuery.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(warmupJobQuery.error).status === 403 ? t('common.permissionDenied') : getApiError(warmupJobQuery.error).status === 404 ? t('cache.warmupJobExpired') : t('cache.warmupJobLoadFailed')}</AlertDescription></Alert>}
              {cancelWarmupMutation.isError && <Alert variant={"destructive"}><AlertDescription>{t('cache.warmupActionFailed')}</AlertDescription></Alert>}
              {retryWarmupMutation.isError && <Alert variant={"destructive"}><AlertDescription>{t('cache.warmupActionFailed')}</AlertDescription></Alert>}
              {warmupJobId && warmupJobQuery.data?.data && (
                <div className="space-y-3 rounded-md border border-[var(--border)] p-3" data-testid="warmup-job-status">
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
                    <Button type="button" variant="outline" size="sm" disabled={cancelWarmupMutation.isPending} onClick={() => { cancelWarmupMutation.reset(); setCancelWarmupOpen(true) }}>
                      {t('cache.warmupCancel')}
                    </Button>
                  )}
                  {['partial', 'failed', 'cancelled', 'interrupted'].includes(warmupJobQuery.data.data.status) && canWrite && warmupJobQuery.data.data.items.some(item => item.status === 'failed') && (
                    <Button type="button" variant="outline" size="sm" disabled={retryWarmupMutation.isPending} onClick={() => retryWarmupMutation.mutate()}>
                      {t('cache.warmupRetryFailed')}
                    </Button>
                  )}
                </div>
              )}
              <div className="flex justify-end gap-3">
                <Button variant="outline" disabled={warmupState.status === 'submitting'} onClick={() => setWarmupOpen(false)}>{t('cancel')}</Button>
                <Button
                  disabled={!canWrite || warmupState.status === 'submitting' || warmupText.split('\n').map(line => line.trim()).filter(line => line && !line.startsWith('#')).length === 0}
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
                </Button>
              </div>
            </div>
          </DialogContent>
        </Dialog>

      <ConfirmActionDialog open={cancelWarmupOpen} title={t('cache.warmupCancel')} description={t('cache.warmupCancelConfirm')} cancelLabel={t('cancel')} confirmLabel={t('cache.warmupCancel')} pendingLabel={t('loading')} pending={cancelWarmupMutation.isPending} errorMessage={cancelWarmupMutation.isError ? getApiError(cancelWarmupMutation.error).message : undefined} onClose={() => setCancelWarmupOpen(false)} onConfirm={() => { if (canWrite) cancelWarmupMutation.mutate() }} />
      {distribution && distribution.top_packages.length > 0 && <details className="border-t pt-4"><summary className="cursor-pointer text-sm font-medium">{t('cache.largeObjects')}</summary><p className="my-3 text-sm text-muted-foreground">{t('cache.largeObjectsHint')}</p><ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{distribution.top_packages.map(item => <li key={`${item.type}:${item.name}`} className="flex min-w-0 items-center justify-between gap-3 text-sm"><span className="truncate">{item.name} <span className="text-xs text-muted-foreground">{item.type}</span></span><span className="shrink-0 font-mono">{formatBytes(item.size)}</span></li>)}</ul></details>}
      <SnapshotSection />
      </div>
    </AdminPage>
  )
}
