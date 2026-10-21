import { Toggle } from '@/components/ui/toggle'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription, EmptyContent } from '@/components/ui/empty'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Field, FieldLabel, FieldDescription, FieldError } from '@/components/ui/field'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { Button } from '@/components/ui/button'

import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import Icon from '@/components/Icon'

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
import {
  createUpstreamRuntimeModel,
  type RuntimeCheckOutcome,
} from '@/admin/upstreams/runtimeModel'
import type {
  AdminUpstream,
  AdminUpstreamListResponse,
  UpstreamMutationRequest,
} from '@/lib/adminApi.types'

import { normalizeProbeInterval, probeIntervals } from '@/admin/upstreams/probeInterval'
import { CheckResults, type CheckRecord } from '@/admin/upstreams/CheckResults'

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

const emptyForm = (ecosystem: string): UpstreamMutationRequest => ({
  adapter_type: ecosystem,
  name: '', url: '', proxy: '', priority: 1,
  probe_mode: 'active', probe_interval: '30m', via: '',
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

export default function Upstreams() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const [runtimeModel] = useState(createUpstreamRuntimeModel)
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
  const [checkRecords, setCheckRecords] = useState<CheckRecord[]>([])
  const [resultsOpen, setResultsOpen] = useState(false)

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

  const { data, error, isPending, isError, isRefetchError, isFetching, dataUpdatedAt, refetch } = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: async ({ signal }) => (await adminApi.listUpstreams({ signal })).data,
    retry: false,
    refetchInterval: checking || checkingIds.size > 0 || dialogOpen || deleteTarget ? false : 60_000,
    refetchOnWindowFocus: !checking && checkingIds.size === 0 && !dialogOpen && !deleteTarget,
  })
  const { data: cascadeInfo, isPending: cascadePending, isError: cascadeError, refetch: refetchCascade } = useQuery({
    queryKey: ['admin', 'cascade'],
    queryFn: async ({ signal }) => (await adminApi.getCascadeInfo({ signal })).data,
    retry: false,
    staleTime: 5 * 60 * 1000,
  })
  const allUpstreams = useMemo(() => data?.items ?? [], [data])

  // Map to UpstreamItem shape
  const upstreamItems: UpstreamItem[] = useMemo(() => allUpstreams.map((item) => ({
    id: item.id,
    name: item.name,
    adapter: item.adapter_type,
    healthy: item.healthy,
    has_observations: item.has_observations,
    runtime_sample_count: item.runtime_sample_count,
    avg_latency_ms: item.avg_latency_ms,
    success_rate: item.success_rate,
    url: item.url,
    proxy: item.proxy,
    via: item.via,
    priority: item.priority,
    probeMode: item.probe_mode,
    probeInterval: item.probe_interval,
    lastCheckedAt: item.last_checked_at,
    workerRunning: item.worker_running,
  })), [allUpstreams])

  const statusCounts = useMemo(() => {
    const counts: Record<UpstreamHealth, number> = { healthy: 0, degraded: 0, failed: 0, unknown: 0 }
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
      item.via ?? '',
    ].some(value => value.toLowerCase().includes(normalizedSearch))
  }), [normalizedSearch, statusFilter, upstreamItems])

  const cancelListRead = () => queryClient.cancelQueries({ queryKey: ['admin', 'upstreams'], exact: true })

  const createMutation = useMutation({
    onMutate: cancelListRead,
    mutationFn: (request: UpstreamMutationRequest) => adminApi.createUpstream(request),
    onSuccess: ({ data: runtime }) => {
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        current => runtimeModel.applyMutation(current, { kind: 'saved', upstream: runtime }),
      )
      closeDialog()
      toast.show({ tone: 'success', message: t('upstreams.createdNamed', { name: runtime.name }) })
    },
  })
  const updateMutation = useMutation({
    onMutate: cancelListRead,
    mutationFn: ({ id, request }: { id: number; request: UpstreamMutationRequest }) => adminApi.updateUpstream(id, request),
    onSuccess: ({ data: runtime }) => {
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        current => runtimeModel.applyMutation(current, { kind: 'saved', upstream: runtime }),
      )
      removeCheckFailure(runtime.id)
      closeDialog()
      toast.show({ tone: 'success', message: t('upstreams.updatedNamed', { name: runtime.name }) })
    },
  })
  const deleteMutation = useMutation({
    onMutate: cancelListRead,
    mutationFn: (id: number) => adminApi.deleteUpstream(id),
    onSuccess: ({ data }) => {
      setBulkCheckSummary(null)
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        current => runtimeModel.applyMutation(current, { kind: 'deleted', upstreamId: data.deleted_id }),
      )
      removeCheckFailure(data.deleted_id)
      const deletedName = deleteTarget?.name ?? t('upstreams.unnamed')
      closeDeleteDialog()
      toast.show({ tone: 'success', message: t('upstreams.deletedNamed', { name: deletedName }) })
    },
  })

  function removeCheckFailure(id: number) {
    setCheckRecords(current => current.filter(record => record.upstream.id !== id))
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
    const ecosystem = allUpstreams[0]?.adapter_type ?? standardUpstreamEcosystems[0].id
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
    setForm({ adapter_type: runtime.adapter_type, name: runtime.name, url: runtime.url, proxy: runtime.proxy, priority: runtime.priority, probe_mode: runtime.probe_mode, probe_interval: normalizeProbeInterval(runtime.probe_interval), via: runtime.via ?? '' })
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
      via: (form.via ?? '').trim(),
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

  function rememberChecks(outcomes: RuntimeCheckOutcome[]) {
    const latest = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
    const reconciliation = runtimeModel.applyChecks(latest, outcomes)
    if (reconciliation.data) queryClient.setQueryData(['admin', 'upstreams'], reconciliation.data)
    const receivedAt = new Date().toISOString()
    const records: CheckRecord[] = reconciliation.accepted.map(result => ({ upstream: result.upstream, result, receivedAt }))
    for (const failure of reconciliation.failures) {
      const upstream = latest?.items.find(item => item.id === failure.upstreamId)
      if (upstream) records.push({ upstream, receivedAt, requestError: upstreamErrorMessage(failure.reason, t) })
    }
    if (records.length) {
      setCheckRecords(current => {
        const next = new Map(current.map(record => [record.upstream.id, record]))
        for (const record of records) next.set(record.upstream.id, record)
        return [...next.values()]
      })
    }
    return reconciliation
  }

  async function refreshStatus() {
    await Promise.allSettled([
      refetch(),
      refetchCascade(),
      queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] }),
    ])
  }

  async function checkAll() {
    if (checking || checkingIds.size > 0 || allUpstreams.length === 0) return
    setChecking(true)
    setBulkCheckSummary(null)
    setCheckFailures(new Set())
    setCheckRecords([])
    setCheckProgress({ completed: 0, total: allUpstreams.length })
    try {
      await cancelListRead()
      const batch = allUpstreams.map((upstream) => ({
        upstream,
        ticket: runtimeModel.beginCheck(upstream),
      }))
      const outcomes: RuntimeCheckOutcome[] = new Array(batch.length)
      let cursor = 0
      const workerCount = Math.min(4, batch.length)
      const workers = Array.from({ length: workerCount }, async () => {
        while (cursor < batch.length) {
          const index = cursor
          cursor += 1
          const { upstream, ticket } = batch[index]
          try {
            const { data: result } = await adminApi.checkUpstream(upstream.id)
            outcomes[index] = {
              status: 'fulfilled',
              ticket,
              result,
            }
          } catch (reason) {
            outcomes[index] = { status: 'rejected', ticket, reason }
          } finally {
            rememberChecks([outcomes[index]])
            setCheckProgress((current) => ({
              ...current,
              completed: current.completed + 1,
            }))
          }
        }
      })
      await Promise.all(workers)
      const latest = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
      const reconciliation = runtimeModel.applyChecks(latest, outcomes)
      if (reconciliation.data) {
        queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], reconciliation.data)
      }
      const accepted = reconciliation.accepted
      const failedIDs = new Set(reconciliation.failures.map(failure => failure.upstreamId))
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
    const ticket = runtimeModel.beginCheck(upstream)
    setBulkCheckSummary(null)
    removeCheckFailure(id)
    setCheckingIds((current) => new Set(current).add(id))
    try {
      await cancelListRead()
      const { data: result } = await adminApi.checkUpstream(id)
      const reconciliation = rememberChecks([{ status: 'fulfilled', ticket, result }])
      const acceptedResult = reconciliation.accepted[0]
      if (!acceptedResult || !reconciliation.data) return
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], reconciliation.data)
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] })
      const probe = acceptedResult.check
      toast.show({
        tone: probe.healthy ? 'success' : 'warning',
        message: probe.healthy
          ? t('upstreams.checkHealthy', { name: upstream.name, latency: probe.latency_ms })
          : t('upstreams.checkUnhealthy', { name: upstream.name, reason: probe.error || t('upstreams.checkReasonUnavailable') }),
      })
    } catch (checkError) {
      const reconciliation = rememberChecks([{
        status: 'rejected',
        ticket,
        reason: checkError,
      }])
      if (reconciliation.failures.length === 0) return
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
    ...(statusCounts.unknown > 0 || statusFilter === 'unknown' ? [{ value: 'unknown' as const, label: t('monitor.unknown'), count: statusCounts.unknown, dot: 'unknown' as const }] : []),
  ]

  return (
    <AdminPage
      description={t('upstreams.subtitle')}
      actions={(
        <>
          {dataUpdatedAt > 0 && <span className="text-[12px] text-muted-foreground">{t('upstreams.updatedAt', { time: checkedAtFormatter.format(new Date(dataUpdatedAt)) })}</span>}
          {checkRecords.length > 0 && <Button variant="outline" className="min-h-[40px] text-[13px]" onClick={() => setResultsOpen(true)}>{t('upstreams.checkResults')} · {checkRecords.length}</Button>}
          <Button type="button" variant="ghost" className="min-h-[40px] text-[13px]" onClick={() => { void refreshStatus() }} disabled={isFetching || checking || checkingIds.size > 0}>
            <Icon name="refresh" size="sm" className={isFetching ? 'animate-spin motion-reduce:animate-none' : ''} />
            {t('upstreams.refreshStatus')}
          </Button>
          {canWrite && <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="min-h-[40px] text-[13px]"
            aria-busy={checking || undefined}
            onClick={checkAll}
            disabled={checking || checkingIds.size > 0 || allUpstreams.length === 0}
          >
            <Icon name="refresh" size="sm" />
            {checking ? t('upstreams.checkingProgress', checkProgress) : t('upstreams.checkAll')}
          </Button>
          <Button
            type="button"
            size="sm"
            className="min-h-[40px] text-[13px]"
            onClick={openCreate}
          >
            <Icon name="add" size="sm" />
            {t('upstreams.addUpstream')}
          </Button>
          </>}
        </>
      )}
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
                <Skeleton
                  key={index}
                  className="h-32 rounded-md"
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
                  className="grid min-w-0 grid-cols-2 gap-0.5 rounded-md border border-[var(--border)] bg-[var(--bg-soft)] p-[3px] sm:flex sm:flex-wrap"
                >
                  {statusOptions.map((option) => {
                    const active = statusFilter === option.value
                    return (
                      <Toggle
                        key={option.value}
                        type="button"
                        pressed={active}
                        className="stripe-focus-ring inline-flex min-h-[40px] min-w-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-sm border px-3 text-[13px] transition-[background,color,border-color,transform] duration-150 active:scale-[0.96]"
                        style={{
                          background: active ? 'var(--bg-card)' : 'transparent',
                          borderColor: active ? 'var(--border-strong)' : 'transparent',
                          color: active ? 'var(--text)' : 'var(--text-muted)',
                        }}
                        onPressedChange={() => setStatusFilter(option.value)}
                      >
                        {option.dot && <StatusDot status={option.dot} />}
                        <span>{option.label}</span>
                        <span className="font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                          {option.count}
                        </span>
                      </Toggle>
                    )
                  })}
                </div>

                <div
                  role="search"
                  className="flex min-h-[40px] min-w-0 flex-1 items-center gap-2 rounded-md border border-[var(--border)] px-3 lg:max-w-[360px]"
                  style={{ background: 'var(--bg-card)' }}
                >
                  <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
                      <Input
                        ref={searchInputRef}
                        type="text"
                        value={search}
                        aria-label={t('upstreams.searchLabel')}
                        placeholder={t('upstreams.searchPlaceholder')}
                        className={"min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]" + ' h-9 rounded-none border-0 px-0 shadow-none focus-visible:ring-0 dark:bg-transparent'}
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
                        <Tooltip>
                          <TooltipTrigger render={
                            <Button
                              onClick={() => {
                                setSearch('')
                                searchInputRef.current?.focus()
                              }}
                              variant="ghost"
                              size="icon"
                              data-icon-button
                              aria-label={t('upstreams.clearSearch')}
                              className="-mr-3"
                              style={{ color: 'var(--text-soft)' }}
                            />
                          }>
                            <Icon name={"close"} size="sm" />
                          </TooltipTrigger>
                          <TooltipContent>{t('upstreams.clearSearch')}</TooltipContent>
                        </Tooltip>
                  ) : (
                    <kbd
                      aria-hidden="true"
                      className="hidden rounded-sm border border-[var(--border)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-subtle)] sm:inline"
                    >
                      /
                    </kbd>
                  )}
                </div>
              </div>
            )}

            {bulkCheckSummary && (
              <div data-upstream-check-summary role="status">
                <Alert variant={bulkCheckSummary.degraded > 0
                    || bulkCheckSummary.failed > 0
                    || bulkCheckSummary.requestFailed > 0
                    ? 'warning'
                    : 'success'}><AlertDescription>
                  {bulkCheckMessage}
                </AlertDescription></Alert>
              </div>
            )}

            {upstreamItems.length === 0 ? (
              <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"hub"} size="sm" /></EmptyMedia><EmptyTitle>{t('upstreams.emptyTitle')}</EmptyTitle><EmptyDescription>{canWrite ? t('upstreams.emptyHint') : t('upstreams.emptyReadonlyHint')}</EmptyDescription></EmptyHeader></Empty>
            ) : visibleUpstreams.length === 0 ? (
              <Empty style={{ minHeight: 220 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"search"} size="sm" /></EmptyMedia><EmptyTitle>{normalizedSearch
                  ? t('upstreams.noMatch', { query: summarizedSearch })
                  : t('upstreams.noStatusMatch')}</EmptyTitle>{t('upstreams.noMatchHint') && <EmptyDescription>{t('upstreams.noMatchHint')}</EmptyDescription>}</EmptyHeader><EmptyContent>{(
                  <Button
                    type="button"
                    variant="outline"
                    className="min-h-[40px]"
                    onClick={() => {
                      setSearch('')
                      setStatusFilter('all')
                    }}
                  >
                    {t('upstreams.clearFilters')}
                  </Button>
                )}</EmptyContent></Empty>
            ) : (
              <UpstreamGroupedPanel
                upstreams={visibleUpstreams}
                layout="adaptive"
                renderMetadata={(upstream) => (
                  <div className="space-y-2 text-[13px] leading-relaxed">
                    <p className="break-all font-mono text-[12px]" title={upstreamEndpointLabel(upstream.url ?? '')}>{upstreamEndpointLabel(upstream.url ?? '')}</p>
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      <span>{t('upstreams.priorityValue', { priority: upstream.priority ?? 1 })}</span>
                      <span>{upstream.probeMode === 'active' && upstream.workerRunning === false
                        ? t('upstreams.probeStopped')
                        : upstream.probeMode === 'active'
                          ? t('upstreams.activeProbeValue', { interval: normalizeProbeInterval(upstream.probeInterval ?? '—') })
                          : t('upstreams.passiveProbeValue')}</span>
                      {upstream.lastCheckedAt ? <time dateTime={upstream.lastCheckedAt}>{t('upstreams.lastCheckedValue', { time: checkedAtFormatter.format(new Date(upstream.lastCheckedAt)) })}</time> : <span>{t('upstreams.neverChecked')}</span>}
                    </div>
                    <details className="group">
                      <summary className="w-fit cursor-pointer rounded-sm text-[12px] focus-visible:outline-2 focus-visible:outline-ring">{t('upstreams.connectionDetails')}</summary>
                      <div className="mt-2 space-y-1 text-[12px]">
                        <p>{upstream.has_observations === false ? t('upstreams.successRateUnknown') : t('upstreams.successRateValue', { rate: Math.round((upstream.success_rate ?? 0) * 100) })}</p>
                        <p>{t('upstreams.successRateHint')}</p>
                        <p>{upstream.proxy ? t('upstreams.proxyEnabled') : t('upstreams.proxyNotConfigured')}</p>
                        <p>{!upstream.via ? t('upstreams.viaDirect')
                          : cascadePending || cascadeError ? t('upstreams.viaUnknown', { peer: upstream.via })
                          : !cascadeInfo?.enabled ? t('upstreams.viaDisabled', { peer: upstream.via })
                          : !cascadeInfo.peers.some(peer => peer.name === upstream.via) ? t('upstreams.viaNotConfigured', { peer: upstream.via })
                          : t('upstreams.viaValue', { peer: upstream.via })}</p>
                      </div>
                    </details>
                    {upstream.id && checkFailures.has(upstream.id) && <p className="text-destructive">{t('upstreams.checkRequestFailedShort')}</p>}
                  </div>
                )}
                renderActions={canWrite ? (upstream) => (
                  <div className="ml-1 flex gap-0.5">
                    {upstream.id && (
                      <Tooltip>
                        <TooltipTrigger render={
                          <Button
                            onClick={() => { void checkOne(upstream.id!) }}
                            variant="ghost"
                            size="icon"
                            data-icon-button
                            aria-label={t('upstreams.checkNamed', { name: upstream.name })}
                            aria-busy={checkingIds.has(upstream.id) || undefined}
                            disabled={(checking) || (checkingIds.has(upstream.id))}
                            style={{ color: 'var(--text-soft)' }}
                          />
                        }>
                          <Icon name={checkingIds.has(upstream.id) ? 'progress_activity' : ("refresh")} size="sm" className={checkingIds.has(upstream.id) ? 'animate-spin' : ''} />
                        </TooltipTrigger>
                        <TooltipContent>{t('upstreams.checkNamed', { name: upstream.name })}</TooltipContent>
                      </Tooltip>
                    )}
                    <Tooltip>
                      <TooltipTrigger render={
                        <Button
                          onClick={() => openEdit(upstream)}
                          variant="ghost"
                          size="icon"
                          data-icon-button
                          aria-label={t('upstreams.editNamed', { name: upstream.name })}
                          style={{ color: 'var(--text-soft)' }}
                        />
                      }>
                        <Icon name={"edit"} size="sm" />
                      </TooltipTrigger>
                      <TooltipContent>{t('upstreams.editNamed', { name: upstream.name })}</TooltipContent>
                    </Tooltip>
                    {upstream.id && (
                      <Tooltip>
                        <TooltipTrigger render={
                          <Button
                            onClick={() => openDelete(upstream)}
                            variant="ghost"
                            size="icon"
                            data-icon-button
                            aria-label={t('upstreams.deleteNamed', { name: upstream.name })}
                            className="text-muted-foreground hover:text-destructive"
                          />
                        }>
                          <Icon name={"delete"} size="sm" />
                        </TooltipTrigger>
                        <TooltipContent>{t('upstreams.deleteNamed', { name: upstream.name })}</TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                ) : undefined}
              />
            )}
          </div>
        )}

        <CheckResults open={resultsOpen} onOpenChange={setResultsOpen} records={checkRecords} />

        <Dialog open={dialogOpen} onOpenChange={(nextOpen) => { if (!nextOpen && !isSaving) closeDialog() }} modal>
          <DialogContent style={{ maxWidth: 560 }} className="text-[14px] [&_input]:h-[40px] [&_input]:text-[14px] [&_select]:h-[40px] [&_select]:text-[14px] [&_[data-slot=field-label]]:text-[13px] [&_[data-slot=field-description]]:text-[13px] [&_[data-slot=field-description]]:leading-relaxed [&_[data-slot=field-error]]:text-[13px] [&_button]:text-[13px]" finalFocus={true} closeDisabled={isSaving}>
            <DialogTitle className="pr-10 text-[18px]">{editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}</DialogTitle>
            <form onSubmit={handleSubmit} className="space-y-5">
              <fieldset className="space-y-3" disabled={isSaving}>
                <legend className="mb-3 text-[14px] font-[600]" style={{ color: 'var(--text)' }}>
                  {t('upstreams.connectionSection')}
                </legend>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Field disabled={editId !== null}>
                    <FieldLabel>{t('type')}</FieldLabel>
                    <NativeSelect
                      value={form.adapter_type}
                      onChange={(event) => setForm({ ...form, adapter_type: event.target.value })}
                      disabled={editId !== null}
                    >
                      {standardUpstreamEcosystems.map(ecosystem => (
                        <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>
                      ))}
                    </NativeSelect>
                    <FieldDescription>{editId ? t('upstreams.ecosystemLockedHint') : t('upstreams.ecosystemActivationHint')}</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel>{t('upstreams.priority')}</FieldLabel>
                    <Input
                      type="number"
                      min={1}
                      value={form.priority}
                      onChange={(event) => setForm({
                        ...form,
                        priority: Number.parseInt(event.target.value, 10) || 1,
                      })}
                    />
                    {(t('upstreams.priorityHint')) && <FieldDescription>{t('upstreams.priorityHint')}</FieldDescription>}
                  </Field>
                </div>
                <Field invalid={Boolean(fieldErrors.name)}>
                  <FieldLabel>{t('name')}</FieldLabel>
                  <Input aria-invalid={Boolean(fieldErrors.name) || undefined}
                    value={form.name}
                    maxLength={128}
                    autoComplete="off"
                    onChange={(event) => {
                      setForm({ ...form, name: event.target.value })
                      setFieldErrors((current) => ({ ...current, name: undefined }))
                    }}
                    placeholder={t('upstreams.namePlaceholder')}
                    required
                  />
                  {(fieldErrors.name) && <FieldError match>{fieldErrors.name}</FieldError>}
                </Field>
                <Field invalid={Boolean(fieldErrors.url)}>
                  <FieldLabel>{t('upstreams.url')}</FieldLabel>
                  <Input aria-invalid={Boolean(fieldErrors.url) || undefined} className="font-mono"
                    value={form.url}
                    autoCapitalize="none"
                    autoComplete="url"
                    spellCheck={false}
                    onChange={(event) => {
                      setForm({ ...form, url: event.target.value })
                      setFieldErrors((current) => ({ ...current, url: undefined }))
                    }}
                    placeholder={form.adapter_type === 'pypi' ? 'https://pypi.example/simple/' : 'https://mirror.example/'}
                    required
                  />
                  {(fieldErrors.url) && <FieldError match>{fieldErrors.url}</FieldError>}
                </Field>
                <Field invalid={Boolean(fieldErrors.proxy)}>
                  <FieldLabel>{t('upstreams.httpProxy')}</FieldLabel>
                  <Input aria-invalid={Boolean(fieldErrors.proxy) || undefined} className="font-mono"
                    value={form.proxy}
                    autoCapitalize="none"
                    spellCheck={false}
                    onChange={(event) => {
                      setForm({ ...form, proxy: event.target.value })
                      setFieldErrors((current) => ({ ...current, proxy: undefined }))
                    }}
                    placeholder="http://127.0.0.1:7890"
                  />
                  {(fieldErrors.proxy) ? <FieldError match>{fieldErrors.proxy}</FieldError> : (t('upstreams.proxyHint')) ? <FieldDescription>{t('upstreams.proxyHint')}</FieldDescription> : null}
                </Field>
                <Field>
                  <FieldLabel>{t('upstreams.via')}</FieldLabel>
                  <NativeSelect value={form.via ?? ''} onChange={(event) => setForm({ ...form, via: event.target.value })}>
                    <option value="">{t('upstreams.viaDirect')}</option>
                    {!cascadeError && cascadeInfo?.enabled && cascadeInfo.peers.map(peer => <option key={peer.name} value={peer.name}>{t('upstreams.viaPeerOption', { name: peer.name, url: peer.url })}</option>)}
                    {form.via && (cascadeError || !cascadeInfo?.enabled || !cascadeInfo.peers.some(peer => peer.name === form.via)) && <option value={form.via}>{t('upstreams.viaSaved', { peer: form.via })}</option>}
                  </NativeSelect>
                  <FieldDescription>{cascadePending ? t('upstreams.cascadeLoading')
                    : cascadeError ? t('upstreams.cascadeUnavailable')
                    : form.via && !cascadeInfo?.enabled ? t('upstreams.viaDisabled', { peer: form.via })
                    : form.via && !cascadeInfo?.peers.some(peer => peer.name === form.via) ? t('upstreams.viaNotConfigured', { peer: form.via })
                    : t('upstreams.viaHint')}</FieldDescription>
                  {cascadeError && <Button type="button" variant="outline" className="w-fit min-h-[36px]" onClick={() => { void refetchCascade() }}>{t('upstreams.refreshStatus')}</Button>}
                </Field>
              </fieldset>

              <fieldset className="space-y-3 border-t border-[var(--border)] pt-4" disabled={isSaving}>
                <legend className="px-1 text-[14px] font-[600]" style={{ color: 'var(--text)' }}>
                  {t('upstreams.healthSection')}
                </legend>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Field>
                    <FieldLabel>{t('upstreams.probeMode')}</FieldLabel>
                    <NativeSelect
                      value={form.probe_mode}
                      onChange={(event) => setForm({
                        ...form,
                        probe_mode: event.target.value as UpstreamMutationRequest['probe_mode'],
                      })}
                    >
                      <option value="active">{t('upstreams.probeModeActive')}</option>
                      <option value="passive">{t('upstreams.probeModePassive')}</option>
                    </NativeSelect>
                    {(t('upstreams.probeModeHint')) && <FieldDescription>{t('upstreams.probeModeHint')}</FieldDescription>}
                  </Field>
                  {form.probe_mode === 'active' && (
                    <Field>
                      <FieldLabel>{t('upstreams.probeInterval')}</FieldLabel>
                      <NativeSelect
                        value={form.probe_interval}
                        onChange={(event) => setForm({ ...form, probe_interval: event.target.value })}
                      >
                        {probeIntervals.map(interval => <option key={interval} value={interval}>{interval}</option>)}
                        {!probeIntervals.some(interval => interval === form.probe_interval) && <option value={form.probe_interval}>{form.probe_interval}</option>}
                      </NativeSelect>
                    </Field>
                  )}
                </div>
              </fieldset>

              {saveError && (
                <Alert variant={"destructive"}><AlertDescription>{upstreamErrorMessage(saveError, t)}</AlertDescription></Alert>
              )}
              <div className="flex justify-end gap-3 pt-1">
                <Button
                  type="button"
                  variant="outline"
                  className="min-h-[40px]"
                  disabled={isSaving}
                  onClick={closeDialog}
                >
                  {t('cancel')}
                </Button>
                <Button
                  type="submit"
                  className="min-h-[40px]"
                  aria-busy={isSaving || undefined}
                  disabled={isSaving || !canWrite}
                >
                  {isSaving ? t('saving') : t('save')}
                </Button>
              </div>
            </form>
          </DialogContent>
        </Dialog>

        <Dialog open={deleteTarget !== null} onOpenChange={(nextOpen) => { if (!nextOpen && !deleteMutation.isPending) closeDeleteDialog() }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true} closeDisabled={deleteMutation.isPending}>
            <DialogTitle className="pr-10 text-[18px]">{deleteTarget
              ? t('upstreams.confirmDeleteNamed', { name: deleteTarget.name })
              : t('upstreams.confirmDelete')}</DialogTitle>
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
                  <Alert variant={"destructive"}><AlertDescription>
                    {upstreamErrorMessage(deleteMutation.error, t)}
                  </AlertDescription></Alert>
                )}
                <div className="flex justify-end gap-3">
                  <Button
                    type="button"
                    variant="outline"
                    className="min-h-[40px]"
                    disabled={deleteMutation.isPending}
                    onClick={closeDeleteDialog}
                  >
                    {t('cancel')}
                  </Button>
                  <Button
                    type="button"
                    variant="destructive"
                    className="min-h-[40px]"
                    aria-busy={deleteMutation.isPending || undefined}
                    disabled={deleteMutation.isPending}
                    onClick={() => deleteMutation.mutate(deleteTarget.id)}
                  >
                    {deleteMutation.isPending ? t('deleting') : t('delete')}
                  </Button>
                </div>
              </div>
            )}
          </DialogContent>
        </Dialog>
      </div>
    </AdminPage>
  )
}
