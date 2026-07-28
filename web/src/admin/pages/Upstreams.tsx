import { useEffect, useMemo, useRef, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import ButtonV2 from '@/components/Button'
import EmptyState from '@/components/EmptyState'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import InlineNotice from '@/components/InlineNotice'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import StatusDot from '@/components/StatusDot'
import { useAppToast } from '@/components/Toast'
import { UpstreamGroupedPanel, type UpstreamItem } from '@/components/UpstreamCard'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { standardUpstreamEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import { upstreamStatus } from '@/lib/upstreamStatus'
import type {
  AdminUpstream,
  AdminUpstreamListResponse,
  CheckUpstreamResponse,
  UpstreamMutationRequest,
} from '@/lib/adminApi.types'

const runtimeEcosystemOrder = standardUpstreamEcosystems.map(ecosystem => ecosystem.id)
const runtimeEcosystemRank = new Map<string, number>(runtimeEcosystemOrder.map((name, index) => [name, index] as const))

function upsertRuntimeUpstream(current: AdminUpstreamListResponse | undefined, upstream: AdminUpstream): AdminUpstreamListResponse {
  const items = current?.items ?? []
  const index = items.findIndex((item) => item.id === upstream.id)
  const next = index < 0 ? [...items, upstream] : items.map((item) => item.id === upstream.id ? upstream : item)
  next.sort((a, b) => (runtimeEcosystemRank.get(a.adapter_type) ?? Number.MAX_SAFE_INTEGER) - (runtimeEcosystemRank.get(b.adapter_type) ?? Number.MAX_SAFE_INTEGER) || a.priority - b.priority || a.id - b.id)
  return { items: next, total: next.length }
}

function removeRuntimeUpstream(current: AdminUpstreamListResponse | undefined, deletedID: number): AdminUpstreamListResponse {
  const items = (current?.items ?? []).filter((item) => item.id !== deletedID)
  return { items, total: items.length }
}

function replaceRuntimeList(current: AdminUpstreamListResponse | undefined, replacements: AdminUpstream[]): AdminUpstreamListResponse {
  let next = current ?? { items: [], total: 0 }
  for (const replacement of replacements) next = upsertRuntimeUpstream(next, replacement)
  return next
}

interface RuntimeCheckBaseline {
  generation: number
  updatedAt: string
}

interface RuntimeCheckResponse {
  upstream: AdminUpstream
  check: CheckUpstreamResponse['check']
  baseline: RuntimeCheckBaseline
}

type UpstreamHealth = ReturnType<typeof upstreamStatus>
type UpstreamStatusFilter = 'all' | UpstreamHealth

interface UpstreamFieldErrors {
  name?: string
  url?: string
  proxy?: string
}

interface BulkCheckSummary {
  healthy: number
  degraded: number
  failed: number
  requestFailed: number
}

function mergeRuntimeChecks(
  current: AdminUpstreamListResponse | undefined,
  checks: RuntimeCheckResponse[],
  generations: ReadonlyMap<number, number>,
): AdminUpstreamListResponse | undefined {
  if (!current) return current
  const replacements = checks.flatMap(({ upstream, baseline }) => {
    return isRuntimeCheckCurrent(current, upstream.id, baseline, generations)
      ? [upstream]
      : []
  })
  return replaceRuntimeList(current, replacements)
}

function isRuntimeCheckCurrent(
  current: AdminUpstreamListResponse | undefined,
  upstreamID: number,
  baseline: RuntimeCheckBaseline,
  generations: ReadonlyMap<number, number>,
): boolean {
  const currentUpstream = current?.items.find((item) => item.id === upstreamID)
  return Boolean(
    currentUpstream
    && (generations.get(upstreamID) ?? 0) === baseline.generation
    && currentUpstream.updated_at === baseline.updatedAt,
  )
}

const emptyForm = (ecosystem: string): UpstreamMutationRequest => ({
  adapter_type: ecosystem,
  name: '', url: '', proxy: '', priority: 1,
  probe_mode: 'active', probe_interval: '30m',
})

function isHTTPURL(value: string, originOnly = false): boolean {
  try {
    const parsed = new URL(value)
    return Boolean(parsed.host)
      && (parsed.protocol === 'http:' || parsed.protocol === 'https:')
      && (!originOnly || (!parsed.search && !parsed.hash))
  } catch {
    return false
  }
}

function upstreamEndpointLabel(value: string): string {
  try {
    const parsed = new URL(value)
    return `${parsed.protocol}//${parsed.host}${parsed.pathname}`
  } catch {
    return value
  }
}

function upstreamErrorMessage(error: unknown, t: TFunction): string {
  const apiError = getApiError(error)
  if (apiError.status === 403) return t('common.permissionDenied')
  switch (apiError.code) {
    case 'CONFLICT':
      return t('upstreams.errors.conflict')
    case 'LAST_UPSTREAM':
      return t('upstreams.errors.lastUpstream')
    case 'ECOSYSTEM_NOT_ACTIVE':
      return t('upstreams.errors.ecosystemInactive')
    case 'IMMUTABLE_ECOSYSTEM':
      return t('upstreams.errors.immutableEcosystem')
    case 'INVALID_UPSTREAM':
      return t('upstreams.errors.invalid')
    case 'REGISTRY_RECONCILE_FAILED':
      return t('upstreams.errors.reconcile')
    case 'NOT_FOUND':
      return t('upstreams.errors.notFound')
    default:
      return t('upstreams.errors.generic', { reason: apiError.message })
  }
}

function summarizeSearch(value: string, maxLength = 48): string {
  const characters = Array.from(value)
  return characters.length <= maxLength
    ? value
    : `${characters.slice(0, maxLength - 1).join('')}…`
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export default function UpstreamsV2() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const resourceGenerations = useRef(new Map<number, number>())
  const searchInputRef = useRef<HTMLInputElement>(null)

  // CRUD state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<UpstreamMutationRequest>(() => emptyForm(''))
  const [deleteTarget, setDeleteTarget] = useState<AdminUpstream | null>(null)
  const [fieldErrors, setFieldErrors] = useState<UpstreamFieldErrors>({})

  // Manual check state
  const [checking, setChecking] = useState(false)
  const [checkingIds, setCheckingIds] = useState<ReadonlySet<number>>(() => new Set())
  const [checkProgress, setCheckProgress] = useState({ completed: 0, total: 0 })
  const [checkFailures, setCheckFailures] = useState<ReadonlySet<number>>(() => new Set())
  const [bulkCheckSummary, setBulkCheckSummary] = useState<BulkCheckSummary | null>(null)

  // Retrieval state. Filtering stays client-side because the list payload is
  // already the source of truth for every group and status count.
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<UpstreamStatusFilter>('all')

  useEffect(() => {
    function focusSearch(event: KeyboardEvent) {
      if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return
      const target = event.target
      if (
        document.querySelector('[role="dialog"]')
        || (target instanceof Element
          && target.closest('input, textarea, select, button, [contenteditable="true"]'))
      ) return
      event.preventDefault()
      searchInputRef.current?.focus()
    }
    document.addEventListener('keydown', focusSearch)
    return () => document.removeEventListener('keydown', focusSearch)
  }, [])

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: async () => (await adminApi.listUpstreams()).data,
    retry: false,
  })
  const allUpstreams = useMemo(() => data?.items ?? [], [data])

  // Map to UpstreamItem shape
  const upstreamItems: UpstreamItem[] = useMemo(() => allUpstreams.map((item) => ({
    id: item.id,
    name: item.name,
    adapter: item.adapter_type,
    healthy: item.healthy,
    avg_latency_ms: item.avg_latency_ms,
    success_rate: item.success_rate,
    url: item.url,
    proxy: item.proxy,
    priority: item.priority,
    probeMode: item.probe_mode,
    probeInterval: item.probe_interval,
    lastCheckedAt: item.last_checked_at,
    workerRunning: item.worker_running,
  })), [allUpstreams])

  const statusCounts = useMemo(() => {
    const counts: Record<UpstreamHealth, number> = { healthy: 0, degraded: 0, failed: 0 }
    for (const item of upstreamItems) counts[upstreamStatus(item)] += 1
    return counts
  }, [upstreamItems])

  const normalizedSearch = search.trim().toLowerCase()
  const checkedAtFormatter = useMemo(() => new Intl.DateTimeFormat(
    i18n.language.startsWith('zh') ? 'zh-CN' : 'en-US',
    { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' },
  ), [i18n.language])
  const visibleUpstreams = useMemo(() => upstreamItems.filter((item) => {
    const matchesStatus = statusFilter === 'all' || upstreamStatus(item) === statusFilter
    if (!matchesStatus) return false
    if (!normalizedSearch) return true
    return [
      item.name,
      item.adapter,
      item.url ?? '',
      item.proxy ?? '',
    ].some(value => value.toLowerCase().includes(normalizedSearch))
  }), [normalizedSearch, statusFilter, upstreamItems])

  const createMutation = useMutation({
    mutationFn: (request: UpstreamMutationRequest) => adminApi.createUpstream(request),
    onSuccess: ({ data: runtime }) => {
      advanceResourceGeneration(runtime.id)
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
      closeDialog()
      toast.show({ tone: 'success', message: t('upstreams.createdNamed', { name: runtime.name }) })
    },
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, request }: { id: number; request: UpstreamMutationRequest }) => adminApi.updateUpstream(id, request),
    onSuccess: ({ data: runtime }) => {
      advanceResourceGeneration(runtime.id)
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
      removeCheckFailure(runtime.id)
      closeDialog()
      toast.show({ tone: 'success', message: t('upstreams.updatedNamed', { name: runtime.name }) })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteUpstream(id),
    onSuccess: ({ data }) => {
      advanceResourceGeneration(data.deleted_id)
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => removeRuntimeUpstream(current, data.deleted_id))
      removeCheckFailure(data.deleted_id)
      const deletedName = deleteTarget?.name ?? t('upstreams.unnamed')
      closeDeleteDialog()
      toast.show({ tone: 'success', message: t('upstreams.deletedNamed', { name: deletedName }) })
    },
  })

  function advanceResourceGeneration(id: number) {
    resourceGenerations.current.set(id, (resourceGenerations.current.get(id) ?? 0) + 1)
  }

  function captureCheckBaseline(upstream: AdminUpstream): RuntimeCheckBaseline {
    return {
      generation: resourceGenerations.current.get(upstream.id) ?? 0,
      updatedAt: upstream.updated_at,
    }
  }

  function removeCheckFailure(id: number) {
    setCheckFailures((current) => {
      if (!current.has(id)) return current
      const next = new Set(current)
      next.delete(id)
      return next
    })
  }

  function closeDialog() {
    setDialogOpen(false)
    setEditId(null)
    setForm(emptyForm(''))
    setFieldErrors({})
  }

  function closeDeleteDialog() {
    setDeleteTarget(null)
    deleteMutation.reset()
  }

  function openCreate() {
    const ecosystem = standardUpstreamEcosystems[0].id
    createMutation.reset()
    setForm(emptyForm(ecosystem))
    setFieldErrors({})
    setEditId(null)
    setDialogOpen(true)
  }

  function openEdit(item: UpstreamItem) {
    const runtime = allUpstreams.find((candidate) => candidate.id === item.id)
    if (!runtime) return
    updateMutation.reset()
    setFieldErrors({})
    setEditId(runtime.id)
    setForm({ adapter_type: runtime.adapter_type, name: runtime.name, url: runtime.url, proxy: runtime.proxy, priority: runtime.priority, probe_mode: runtime.probe_mode, probe_interval: runtime.probe_interval })
    setDialogOpen(true)
  }

  function openDelete(item: UpstreamItem) {
    const runtime = allUpstreams.find((candidate) => candidate.id === item.id)
    if (!runtime) return
    deleteMutation.reset()
    setDeleteTarget(runtime)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!canWrite) return
    const normalized: UpstreamMutationRequest = {
      ...form,
      name: form.name.trim(),
      url: form.url.trim(),
      proxy: form.proxy.trim(),
    }
    const nextErrors: UpstreamFieldErrors = {}
    if (!normalized.name) nextErrors.name = t('upstreams.nameRequired')
    else if (utf8ByteLength(normalized.name) > 128) {
      nextErrors.name = t('upstreams.nameTooLong')
    }
    if (!isHTTPURL(normalized.url, true)) nextErrors.url = t('upstreams.invalidUrl')
    if (normalized.proxy && !isHTTPURL(normalized.proxy)) nextErrors.proxy = t('upstreams.invalidProxy')
    setFieldErrors(nextErrors)
    if (Object.keys(nextErrors).length > 0) return
    setForm(normalized)
    if (editId !== null) updateMutation.mutate({ id: editId, request: normalized })
    else createMutation.mutate(normalized)
  }

  async function checkAll() {
    if (checking || checkingIds.size > 0 || allUpstreams.length === 0) return
    setChecking(true)
    setBulkCheckSummary(null)
    setCheckFailures(new Set())
    setCheckProgress({ completed: 0, total: allUpstreams.length })
    try {
      const batch = allUpstreams.map((upstream) => ({
        upstream,
        baseline: captureCheckBaseline(upstream),
      }))
      const settled: PromiseSettledResult<RuntimeCheckResponse>[] = new Array(batch.length)
      let cursor = 0
      const workerCount = Math.min(4, batch.length)
      const workers = Array.from({ length: workerCount }, async () => {
        while (cursor < batch.length) {
          const index = cursor
          cursor += 1
          const { upstream, baseline } = batch[index]
          try {
            const { data: result } = await adminApi.checkUpstream(upstream.id)
            settled[index] = {
              status: 'fulfilled',
              value: { upstream: result.upstream, check: result.check, baseline },
            }
          } catch (reason) {
            settled[index] = { status: 'rejected', reason }
          } finally {
            setCheckProgress((current) => ({
              ...current,
              completed: current.completed + 1,
            }))
          }
        }
      })
      await Promise.all(workers)
      const fulfilled = settled.flatMap((result) => result.status === 'fulfilled' ? [result.value] : [])
      const latest = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
      const accepted = fulfilled.filter(({ upstream, baseline }) => (
        isRuntimeCheckCurrent(latest, upstream.id, baseline, resourceGenerations.current)
      ))
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        (current) => mergeRuntimeChecks(current, accepted, resourceGenerations.current),
      )
      const failedIDs = new Set(settled.flatMap((result, index) => (
        result.status === 'rejected'
        && isRuntimeCheckCurrent(
          latest,
          batch[index].upstream.id,
          batch[index].baseline,
          resourceGenerations.current,
        )
          ? [batch[index].upstream.id]
          : []
      )))
      setCheckFailures(failedIDs)
      const summary: BulkCheckSummary = {
        healthy: accepted.filter(result => upstreamStatus(result.upstream) === 'healthy').length,
        degraded: accepted.filter(result => upstreamStatus(result.upstream) === 'degraded').length,
        failed: accepted.filter(result => upstreamStatus(result.upstream) === 'failed').length,
        requestFailed: failedIDs.size,
      }
      setBulkCheckSummary(summary)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] })
    } finally {
      setChecking(false)
    }
  }

  async function checkOne(id: number) {
    if (checking || checkingIds.has(id)) return
    const current = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
    const upstream = current?.items.find((item) => item.id === id)
    if (!upstream) return
    const baseline = captureCheckBaseline(upstream)
    setBulkCheckSummary(null)
    removeCheckFailure(id)
    setCheckingIds((current) => new Set(current).add(id))
    try {
      const { data: result } = await adminApi.checkUpstream(id)
      const latest = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
      if (!isRuntimeCheckCurrent(latest, id, baseline, resourceGenerations.current)) return
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        (latest) => mergeRuntimeChecks(
          latest,
          [{ upstream: result.upstream, check: result.check, baseline }],
          resourceGenerations.current,
        ),
      )
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] })
      const resultStatus = upstreamStatus(result.upstream)
      toast.show({
        tone: resultStatus === 'healthy' ? 'success' : 'warning',
        message: resultStatus === 'healthy'
          ? t('upstreams.checkHealthy', {
              name: upstream.name,
              latency: result.check.latency_ms,
            })
          : resultStatus === 'degraded'
            ? t('upstreams.checkDegraded', {
                name: upstream.name,
                latency: result.check.latency_ms,
              })
            : t('upstreams.checkUnhealthy', {
              name: upstream.name,
              reason: result.check.error || t('upstreams.unreachable'),
            }),
      })
    } catch (checkError) {
      const latest = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
      if (!isRuntimeCheckCurrent(latest, id, baseline, resourceGenerations.current)) return
      setCheckFailures((current) => new Set(current).add(id))
      toast.show({
        tone: 'danger',
        message: t('upstreams.checkFailed', {
          name: upstream.name,
          reason: upstreamErrorMessage(checkError, t),
        }),
      })
    } finally {
      setCheckingIds((current) => {
        const next = new Set(current)
        next.delete(id)
        return next
      })
    }
  }

  const isSaving = createMutation.isPending || updateMutation.isPending
  const saveError = editId !== null ? updateMutation.error : createMutation.error
  const apiError = getApiError(error)
  const errorMessage = apiError.status === 403 ? t('common.permissionDenied') : apiError.message
  const summarizedSearch = summarizeSearch(search.trim())
  const hasActiveFilters = Boolean(normalizedSearch) || statusFilter !== 'all'
  const bulkCheckMessage = bulkCheckSummary
    ? bulkCheckSummary.requestFailed > 0
      ? t('upstreams.checkAllResultWithFailures', {
          healthy: bulkCheckSummary.healthy,
          degraded: bulkCheckSummary.degraded,
          failed: bulkCheckSummary.failed,
          requestFailed: bulkCheckSummary.requestFailed,
        })
      : t('upstreams.checkAllResult', {
          healthy: bulkCheckSummary.healthy,
          degraded: bulkCheckSummary.degraded,
          failed: bulkCheckSummary.failed,
        })
    : ''
  const statusOptions: Array<{
    value: UpstreamStatusFilter
    label: string
    count: number
    dot?: UpstreamHealth
  }> = [
    { value: 'all', label: t('all'), count: upstreamItems.length },
    { value: 'healthy', label: t('monitor.healthy'), count: statusCounts.healthy, dot: 'healthy' },
    { value: 'degraded', label: t('monitor.degraded'), count: statusCounts.degraded, dot: 'degraded' },
    { value: 'failed', label: t('monitor.failed'), count: statusCounts.failed, dot: 'failed' },
  ]

  return (
    <AdminPage
      description={t('upstreams.subtitle')}
      actions={canWrite ? (
        <>
          <ButtonV2
            type="button"
            variant="secondary"
            size="sm"
            className="min-h-[40px] w-[132px] sm:min-h-8"
            aria-busy={checking || undefined}
            onClick={checkAll}
            disabled={checking || checkingIds.size > 0 || allUpstreams.length === 0}
          >
            <Icon name="refresh" size="sm" />
            {checking ? t('upstreams.checkingProgress', checkProgress) : t('upstreams.checkAll')}
          </ButtonV2>
          <ButtonV2
            type="button"
            size="sm"
            className="min-h-[40px] sm:min-h-8"
            onClick={openCreate}
          >
            <Icon name="add" size="sm" />
            {t('upstreams.addUpstream')}
          </ButtonV2>
        </>
      ) : undefined}
    >
      <div className="space-y-6">
        <p className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {checking
            ? t('upstreams.checkingProgress', checkProgress)
            : hasActiveFilters
                ? t('upstreams.filterResults', { count: visibleUpstreams.length })
                : ''}
        </p>

        {isPending ? (
          <div aria-busy="true" className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <span className="sr-only">{t('upstreams.loading')}</span>
            <div aria-hidden="true" className="contents">
              {[...Array(4)].map((_, index) => (
                <div
                  key={index}
                  className="h-32 animate-pulse rounded-[6px]"
                  style={{ background: 'var(--bg-soft)' }}
                />
              ))}
            </div>
          </div>
        ) : isError && !data ? (
          <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
        ) : (
          <div className="space-y-5">
            {data && isRefetchError && (
              <StaleDataNotice onRefresh={() => { void refetch() }} />
            )}

            {upstreamItems.length > 0 && (
              <div
                data-upstream-toolbar
                className="flex min-w-0 flex-col gap-3 border-b border-[var(--border)] pb-4 lg:flex-row lg:items-center lg:justify-between"
              >
                <div
                  role="group"
                  aria-label={t('upstreams.statusFilterLabel')}
                  className="grid min-w-0 grid-cols-2 gap-0.5 rounded-[8px] border border-[var(--border)] bg-[var(--bg-soft)] p-[3px] sm:inline-grid sm:grid-cols-4"
                >
                  {statusOptions.map((option) => {
                    const active = statusFilter === option.value
                    return (
                      <button
                        key={option.value}
                        type="button"
                        aria-pressed={active}
                        className="stripe-focus-ring inline-flex min-h-[40px] min-w-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[5px] border px-2.5 text-[12px] transition-[background,color,border-color,transform] duration-150 active:scale-[0.96]"
                        style={{
                          background: active ? 'var(--bg-card)' : 'transparent',
                          borderColor: active ? 'var(--border-strong)' : 'transparent',
                          color: active ? 'var(--text)' : 'var(--text-muted)',
                        }}
                        onClick={() => setStatusFilter(option.value)}
                      >
                        {option.dot && <StatusDot status={option.dot} />}
                        <span>{option.label}</span>
                        <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                          {option.count}
                        </span>
                      </button>
                    )
                  })}
                </div>

                <div
                  role="search"
                  className="flex min-h-[40px] min-w-0 flex-1 items-center gap-2 rounded-[6px] border border-[var(--border)] px-3 lg:max-w-[360px]"
                  style={{ background: 'var(--bg-card)' }}
                >
                  <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
                  <input
                    ref={searchInputRef}
                    type="text"
                    value={search}
                    aria-label={t('upstreams.searchLabel')}
                    placeholder={t('upstreams.searchPlaceholder')}
                    className="min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]"
                    style={{ color: 'var(--text)' }}
                    onChange={(event) => setSearch(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Escape') {
                        setSearch('')
                        event.currentTarget.blur()
                      }
                    }}
                  />
                  {search ? (
                    <IconButton
                      icon="close"
                      label={t('upstreams.clearSearch')}
                      className="-mr-3"
                      onClick={() => {
                        setSearch('')
                        searchInputRef.current?.focus()
                      }}
                    />
                  ) : (
                    <kbd
                      aria-hidden="true"
                      className="hidden rounded-[4px] border border-[var(--border)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-subtle)] sm:inline"
                    >
                      /
                    </kbd>
                  )}
                </div>
              </div>
            )}

            {bulkCheckSummary && (
              <div data-upstream-check-summary role="status">
                <InlineNotice
                  tone={bulkCheckSummary.degraded > 0
                    || bulkCheckSummary.failed > 0
                    || bulkCheckSummary.requestFailed > 0
                    ? 'warning'
                    : 'success'}
                >
                  {bulkCheckMessage}
                </InlineNotice>
              </div>
            )}

            {upstreamItems.length === 0 ? (
              <EmptyState
                icon="hub"
                title={t('upstreams.emptyTitle')}
                hint={canWrite ? t('upstreams.emptyHint') : t('upstreams.emptyReadonlyHint')}
                minHeight={220}
                action={canWrite ? (
                  <ButtonV2 type="button" className="min-h-[40px]" onClick={openCreate}>
                    <Icon name="add" size="sm" />
                    {t('upstreams.addFirst')}
                  </ButtonV2>
                ) : undefined}
              />
            ) : visibleUpstreams.length === 0 ? (
              <EmptyState
                icon="search"
                title={normalizedSearch
                  ? t('upstreams.noMatch', { query: summarizedSearch })
                  : t('upstreams.noStatusMatch')}
                hint={t('upstreams.noMatchHint')}
                minHeight={220}
                action={(
                  <ButtonV2
                    type="button"
                    variant="secondary"
                    className="min-h-[40px]"
                    onClick={() => {
                      setSearch('')
                      setStatusFilter('all')
                    }}
                  >
                    {t('upstreams.clearFilters')}
                  </ButtonV2>
                )}
              />
            ) : (
              <UpstreamGroupedPanel
                upstreams={visibleUpstreams}
                layout="adaptive"
                renderMetadata={(upstream) => (
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                    <span
                      className="max-w-[34ch] truncate font-mono"
                      title={upstreamEndpointLabel(upstream.url ?? '')}
                    >
                      {upstreamEndpointLabel(upstream.url ?? '')}
                    </span>
                    <span>{t('upstreams.priorityValue', { priority: upstream.priority ?? 1 })}</span>
                    <span>
                      {upstream.probeMode === 'active' && upstream.workerRunning === false
                        ? t('upstreams.probeStopped')
                        : upstream.probeMode === 'active'
                          ? t('upstreams.activeProbeValue', { interval: upstream.probeInterval ?? '—' })
                        : t('upstreams.passiveProbeValue')}
                    </span>
                    <span>
                      {t('upstreams.successRateValue', {
                        rate: Math.round((upstream.success_rate ?? 0) * 100),
                      })}
                    </span>
                    {upstream.lastCheckedAt ? (
                      <time dateTime={upstream.lastCheckedAt}>
                        {t('upstreams.lastCheckedValue', {
                          time: checkedAtFormatter.format(new Date(upstream.lastCheckedAt)),
                        })}
                      </time>
                    ) : <span>{t('upstreams.neverChecked')}</span>}
                    {upstream.proxy && <span>{t('upstreams.proxyEnabled')}</span>}
                    {upstream.id && checkFailures.has(upstream.id) && (
                      <span style={{ color: 'var(--danger-text)' }}>
                        {t('upstreams.checkRequestFailedShort')}
                      </span>
                    )}
                  </span>
                )}
                renderActions={canWrite ? (upstream) => (
                  <div className="ml-1 flex gap-0.5">
                    {upstream.id && (
                      <IconButton
                        icon="refresh"
                        label={t('upstreams.checkNamed', { name: upstream.name })}
                        loading={checkingIds.has(upstream.id)}
                        disabled={checking}
                        onClick={() => { void checkOne(upstream.id!) }}
                      />
                    )}
                      <IconButton
                        icon="edit"
                        label={t('upstreams.editNamed', { name: upstream.name })}
                        onClick={() => openEdit(upstream)}
                      />
                    {upstream.id && (
                      <IconButton
                        icon="delete"
                        label={t('upstreams.deleteNamed', { name: upstream.name })}
                        tone="danger"
                        onClick={() => openDelete(upstream)}
                      />
                    )}
                  </div>
                ) : undefined}
              />
            )}
          </div>
        )}

        <ModalV2
          open={dialogOpen}
          onClose={closeDialog}
          title={editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}
          width={520}
          closeDisabled={isSaving}
        >
          <form onSubmit={handleSubmit} className="space-y-5">
            <fieldset className="space-y-3" disabled={isSaving}>
              <legend className="mb-3 text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
                {t('upstreams.connectionSection')}
              </legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <SelectV2
                  label={t('type')}
                  hint={editId ? t('upstreams.ecosystemLockedHint') : undefined}
                  value={form.adapter_type}
                  onChange={(event) => setForm({ ...form, adapter_type: event.target.value })}
                  disabled={editId !== null}
                >
                  {standardUpstreamEcosystems.map(ecosystem => (
                    <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
                  ))}
                </SelectV2>
                <InputV2
                  label={t('upstreams.priority')}
                  hint={t('upstreams.priorityHint')}
                  type="number"
                  min={1}
                  value={form.priority}
                  onChange={(event) => setForm({
                    ...form,
                    priority: Number.parseInt(event.target.value, 10) || 1,
                  })}
                />
              </div>
              <InputV2
                label={t('name')}
                value={form.name}
                error={fieldErrors.name}
                maxLength={128}
                autoComplete="off"
                onChange={(event) => {
                  setForm({ ...form, name: event.target.value })
                  setFieldErrors((current) => ({ ...current, name: undefined }))
                }}
                placeholder={t('upstreams.namePlaceholder')}
                required
              />
              <InputV2
                label={t('upstreams.url')}
                mono
                value={form.url}
                error={fieldErrors.url}
                autoCapitalize="none"
                autoComplete="url"
                spellCheck={false}
                onChange={(event) => {
                  setForm({ ...form, url: event.target.value })
                  setFieldErrors((current) => ({ ...current, url: undefined }))
                }}
                placeholder="https://pypi.example/simple"
                required
              />
              <InputV2
                label={t('upstreams.httpProxy')}
                hint={t('upstreams.proxyHint')}
                mono
                value={form.proxy}
                error={fieldErrors.proxy}
                autoCapitalize="none"
                spellCheck={false}
                onChange={(event) => {
                  setForm({ ...form, proxy: event.target.value })
                  setFieldErrors((current) => ({ ...current, proxy: undefined }))
                }}
                placeholder="http://127.0.0.1:7890"
              />
            </fieldset>

            <fieldset className="space-y-3 border-t border-[var(--border)] pt-4" disabled={isSaving}>
              <legend className="px-1 text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
                {t('upstreams.healthSection')}
              </legend>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <SelectV2
                  label={t('upstreams.probeMode')}
                  hint={t('upstreams.probeModeHint')}
                  value={form.probe_mode}
                  onChange={(event) => setForm({
                    ...form,
                    probe_mode: event.target.value as UpstreamMutationRequest['probe_mode'],
                  })}
                >
                  <option value="active">{t('upstreams.probeModeActive')}</option>
                  <option value="passive">{t('upstreams.probeModePassive')}</option>
                </SelectV2>
                {form.probe_mode === 'active' && (
                  <SelectV2
                    label={t('upstreams.probeInterval')}
                    value={form.probe_interval}
                    onChange={(event) => setForm({ ...form, probe_interval: event.target.value })}
                  >
                    <option value="15s">15s</option>
                    <option value="30s">30s</option>
                    <option value="1m">1m</option>
                    <option value="5m">5m</option>
                    <option value="10m">10m</option>
                    <option value="30m">30m</option>
                    <option value="1h">1h</option>
                  </SelectV2>
                )}
              </div>
            </fieldset>

            {saveError && (
              <InlineNotice tone="danger">{upstreamErrorMessage(saveError, t)}</InlineNotice>
            )}
            <div className="flex justify-end gap-3 pt-1">
              <ButtonV2
                type="button"
                variant="secondary"
                className="min-h-[40px]"
                disabled={isSaving}
                onClick={closeDialog}
              >
                {t('cancel')}
              </ButtonV2>
              <ButtonV2
                type="submit"
                className="min-h-[40px]"
                aria-busy={isSaving || undefined}
                disabled={isSaving || !canWrite}
              >
                {isSaving ? t('saving') : t('save')}
              </ButtonV2>
            </div>
          </form>
        </ModalV2>

        <ModalV2
          open={deleteTarget !== null}
          onClose={closeDeleteDialog}
          title={deleteTarget
            ? t('upstreams.confirmDeleteNamed', { name: deleteTarget.name })
            : t('upstreams.confirmDelete')}
          closeDisabled={deleteMutation.isPending}
        >
          {deleteTarget && (
            <div className="space-y-5">
              <p className="text-[14px] leading-6" style={{ color: 'var(--text-soft)' }}>
                {t('upstreams.confirmDeleteMsg', {
                  name: deleteTarget.name,
                  ecosystem: deleteTarget.adapter_type,
                })}
              </p>
              <dl className="space-y-2 border-y border-[var(--border)] py-3 text-[12px]">
                <div className="flex min-w-0 items-start justify-between gap-4">
                  <dt style={{ color: 'var(--text-muted)' }}>{t('upstreams.url')}</dt>
                  <dd
                    className="min-w-0 max-w-[70%] truncate font-mono text-right"
                    title={upstreamEndpointLabel(deleteTarget.url)}
                    style={{ color: 'var(--text)' }}
                  >
                    {upstreamEndpointLabel(deleteTarget.url)}
                  </dd>
                </div>
                <div className="flex items-center justify-between gap-4">
                  <dt style={{ color: 'var(--text-muted)' }}>{t('upstreams.priority')}</dt>
                  <dd className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                    {deleteTarget.priority}
                  </dd>
                </div>
              </dl>
              {deleteMutation.error && (
                <InlineNotice tone="danger">
                  {upstreamErrorMessage(deleteMutation.error, t)}
                </InlineNotice>
              )}
              <div className="flex justify-end gap-3">
                <ButtonV2
                  type="button"
                  variant="secondary"
                  className="min-h-[40px]"
                  disabled={deleteMutation.isPending}
                  onClick={closeDeleteDialog}
                >
                  {t('cancel')}
                </ButtonV2>
                <ButtonV2
                  type="button"
                  variant="danger"
                  className="min-h-[40px]"
                  aria-busy={deleteMutation.isPending || undefined}
                  disabled={deleteMutation.isPending}
                  onClick={() => deleteMutation.mutate(deleteTarget.id)}
                >
                  {deleteMutation.isPending ? t('deleting') : t('delete')}
                </ButtonV2>
              </div>
            </div>
          )}
        </ModalV2>
      </div>
    </AdminPage>
  )
}
