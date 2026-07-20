import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import InlineNotice from '@/components/InlineNotice'
import IconButton from '@/components/IconButton'
import QueryErrorState from '@/components/QueryErrorState'
import { UpstreamGroupedPanel, type UpstreamItem } from '@/components/UpstreamCard'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { standardUpstreamEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'
import type { AdminUpstream, AdminUpstreamListResponse, UpstreamMutationRequest } from '@/lib/adminApi.types'

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
  baseline: RuntimeCheckBaseline
}

function mergeRuntimeChecks(
  current: AdminUpstreamListResponse | undefined,
  checks: RuntimeCheckResponse[],
  generations: ReadonlyMap<number, number>,
): AdminUpstreamListResponse | undefined {
  if (!current) return current
  const replacements = checks.flatMap(({ upstream, baseline }) => {
    const currentUpstream = current.items.find((item) => item.id === upstream.id)
    const currentGeneration = generations.get(upstream.id) ?? 0
    return currentUpstream
      && currentGeneration === baseline.generation
      && currentUpstream.updated_at === baseline.updatedAt
      ? [upstream]
      : []
  })
  return replaceRuntimeList(current, replacements)
}

const emptyForm = (ecosystem: string): UpstreamMutationRequest => ({
  adapter_type: ecosystem,
  name: '', url: '', proxy: '', priority: 1,
  probe_mode: 'active', probe_interval: '30m',
})

export default function UpstreamsV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = usePrincipal()
  const resourceGenerations = useRef(new Map<number, number>())

  // CRUD state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<UpstreamMutationRequest>(() => emptyForm(''))
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [urlError, setUrlError] = useState('')

  // Manual check state
  const [checking, setChecking] = useState(false)
  const [checkingIds, setCheckingIds] = useState<ReadonlySet<number>>(() => new Set())

  const { data, error, isPending, isError, isRefetchError, refetch } = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: async () => (await adminApi.listUpstreams()).data,
    retry: false,
  })
  const allUpstreams = data?.items ?? []

  // Map to UpstreamItem shape
  const upstreamItems: UpstreamItem[] = allUpstreams.map((item) => ({
    id: item.id,
    name: item.name,
    adapter: item.adapter_type,
    healthy: item.healthy,
    avg_latency_ms: item.avg_latency_ms,
    success_rate: item.success_rate,
    url: item.url,
    proxy: item.proxy,
    priority: item.priority,
  }))

  const createMutation = useMutation({
    mutationFn: (request: UpstreamMutationRequest) => adminApi.createUpstream(request),
    onSuccess: ({ data: runtime }) => {
      advanceResourceGeneration(runtime.id)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
      closeDialog()
    },
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, request }: { id: number; request: UpstreamMutationRequest }) => adminApi.updateUpstream(id, request),
    onSuccess: ({ data: runtime }) => {
      advanceResourceGeneration(runtime.id)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => upsertRuntimeUpstream(current, runtime))
      closeDialog()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteUpstream(id),
    onSuccess: ({ data }) => {
      advanceResourceGeneration(data.deleted_id)
      queryClient.setQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'], (current) => removeRuntimeUpstream(current, data.deleted_id))
      setDeleteTarget(null)
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

  function closeDialog() { setDialogOpen(false); setEditId(null); setForm(emptyForm('')); setUrlError('') }
  function openCreate() {
    const ecosystem = standardUpstreamEcosystems[0].id
    createMutation.reset()
    setForm(emptyForm(ecosystem))
    setEditId(null)
    setDialogOpen(true)
  }
  function openEdit(item: UpstreamItem) {
    const runtime = allUpstreams.find((candidate) => candidate.id === item.id)
    if (!runtime) return
    updateMutation.reset()
    setEditId(runtime.id)
    setForm({ adapter_type: runtime.adapter_type, name: runtime.name, url: runtime.url, proxy: runtime.proxy, priority: runtime.priority, probe_mode: runtime.probe_mode, probe_interval: runtime.probe_interval })
    setDialogOpen(true)
  }
  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!canWrite) return
    try { new URL(form.url) } catch { setUrlError(t('upstreams.invalidUrl')); return }
    setUrlError('')
    if (editId !== null) updateMutation.mutate({ id: editId, request: form })
    else createMutation.mutate(form)
  }

  async function checkAll() {
    setChecking(true)
    try {
      const checks = allUpstreams.map(async (upstream): Promise<RuntimeCheckResponse> => {
        const baseline = captureCheckBaseline(upstream)
        const { data } = await adminApi.checkUpstream(upstream.id)
        return { upstream: data.upstream, baseline }
      })
      const settled = await Promise.allSettled(checks)
      const fulfilled = settled.flatMap((result) => result.status === 'fulfilled' ? [result.value] : [])
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        (current) => mergeRuntimeChecks(current, fulfilled, resourceGenerations.current),
      )
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] })
    } finally {
      setChecking(false)
    }
  }

  async function checkOne(id: number) {
    const current = queryClient.getQueryData<AdminUpstreamListResponse>(['admin', 'upstreams'])
    const upstream = current?.items.find((item) => item.id === id)
    if (!upstream) return
    const baseline = captureCheckBaseline(upstream)
    setCheckingIds((current) => new Set(current).add(id))
    try {
      const { data: result } = await adminApi.checkUpstream(id)
      queryClient.setQueryData<AdminUpstreamListResponse>(
        ['admin', 'upstreams'],
        (latest) => mergeRuntimeChecks(latest, [{ upstream: result.upstream, baseline }], resourceGenerations.current),
      )
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams', 'latencies', '24h'] })
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

  return (
    <AdminPage
      description={t('upstreams.subtitle')}
      actions={canWrite ? (
        <>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={checkAll} disabled={checking || allUpstreams.length === 0}>
            <Icon name="refresh" size="sm" />
            {checking ? t('upstreams.checking') : t('upstreams.checkAll')}
          </ButtonV2>
          <ButtonV2 type="button" size="sm" onClick={openCreate}>
            <Icon name="add" size="sm" />
            {t('upstreams.addUpstream')}
          </ButtonV2>
        </>
      ) : undefined}
    >
      <div className="space-y-6">
      {/* Upstream grid with heartbeat bars */}
      {isPending ? (
        <div aria-busy="true" className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div aria-hidden="true" className="contents">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-32 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
          ))}
          </div>
        </div>
      ) : isError && !data ? (
        <QueryErrorState message={errorMessage} onRetry={() => { void refetch() }} />
      ) : (
        <div className="space-y-3">
        {data && isRefetchError && (
          <StaleDataNotice onRefresh={() => { void refetch() }} />
        )}
        <UpstreamGroupedPanel
          upstreams={upstreamItems}
          renderActions={canWrite ? (u) => (
            <div className="flex gap-0.5 ml-1">
              {u.id && (
                <IconButton
                  icon="refresh"
                  label={t('upstreams.checkNamed', { name: u.name })}
                  loading={checkingIds.has(u.id)}
                  onClick={() => checkOne(u.id!)}
                />
              )}
              <IconButton
                icon="edit"
                label={t('upstreams.editNamed', { name: u.name })}
                onClick={() => openEdit(u)}
              />
              {u.id && (
                <IconButton
                  icon="delete"
                  label={t('upstreams.deleteNamed', { name: u.name })}
                  tone="danger"
                  onClick={() => setDeleteTarget(u.id!)}
                />
              )}
            </div>
          ) : undefined}
        />
        </div>
      )}

      {/* Create/Edit Modal */}
      <ModalV2 open={dialogOpen} onClose={closeDialog} title={editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}>
        <form onSubmit={handleSubmit} className="space-y-4">
          <SelectV2 label={t('type')} value={form.adapter_type} onChange={(e) => setForm({ ...form, adapter_type: e.target.value })} disabled={!!editId}>
            {standardUpstreamEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
          </SelectV2>
          <InputV2 label={t('name')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. tuna" required />
          <div>
            <InputV2 label={t('upstreams.url')} mono value={form.url} onChange={(e) => { setForm({ ...form, url: e.target.value }); setUrlError('') }} placeholder="https://pypi.tuna.tsinghua.edu.cn" required />
            {urlError && <p className="text-[12px] mt-1" style={{ color: 'var(--danger)' }}>{urlError}</p>}
          </div>
          <InputV2 label={t('upstreams.priority')} type="number" min={1} value={form.priority} onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 1 })} />
          <InputV2 label={t('upstreams.httpProxy')} mono value={form.proxy} onChange={(e) => setForm({ ...form, proxy: e.target.value })} placeholder="http://127.0.0.1:7890" />
          <SelectV2
            label={t('upstreams.probeMode')}
            value={form.probe_mode}
            onChange={(e) => setForm({ ...form, probe_mode: e.target.value as UpstreamMutationRequest['probe_mode'] })}
          >
            <option value="active">{t('upstreams.probeModeActive')}</option>
            <option value="passive">{t('upstreams.probeModePassive')}</option>
          </SelectV2>
          {form.probe_mode === 'active' && (
            <SelectV2
              label={t('upstreams.probeInterval')}
              value={form.probe_interval}
              onChange={(e) => setForm({ ...form, probe_interval: e.target.value })}
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
          {saveError && <InlineNotice tone="danger">{getApiError(saveError).message}</InlineNotice>}
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="submit" aria-busy={isSaving || undefined} disabled={isSaving || !canWrite}>{isSaving ? t('saving') : t('save')}</ButtonV2>
          </div>
        </form>
      </ModalV2>

      {/* Delete Modal */}
      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('upstreams.confirmDelete')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--text-soft)' }}>{t('upstreams.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</ButtonV2>
          <ButtonV2 variant="danger" disabled={deleteMutation.isPending} onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>
      </div>
    </AdminPage>
  )
}
