import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import { UpstreamGroupedPanel, type UpstreamItem } from '@/components/UpstreamCard'

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'alpine', 'helm', 'docker'] as const

interface UpstreamForm { name: string; url: string; priority: number; proxy: string; adapter_type: string; probe_mode: string; probe_interval: string }
const emptyForm: UpstreamForm = { name: '', url: '', priority: 1, proxy: '', adapter_type: 'pypi', probe_mode: 'active', probe_interval: '30m' }

export default function UpstreamsV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // CRUD state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<UpstreamForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [urlError, setUrlError] = useState('')

  // Manual check state
  const [checking, setChecking] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: () => adminApi.listUpstreams(),
  })
  const allUpstreams = data?.data.items ?? []

  // Map to UpstreamItem shape
  const upstreamItems: UpstreamItem[] = allUpstreams.map((u: any) => ({
    id: u.id,
    name: u.name,
    adapter: u.adapter_type,
    healthy: u.healthy,
    avg_latency_ms: u.avg_latency_ms || 0,
    success_rate: u.success_rate || 0,
    url: u.url,
    proxy: u.proxy,
    priority: u.priority,
  }))

  const createMutation = useMutation({
    mutationFn: (d: any) => adminApi.createUpstream(d),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); closeDialog() },
  })
  const updateMutation = useMutation({
    mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateUpstream(id, d),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); closeDialog() },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteUpstream(id),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); setDeleteTarget(null) },
  })

  function closeDialog() { setDialogOpen(false); setEditId(null); setForm(emptyForm); setUrlError('') }
  function openCreate() { setEditId(null); setForm({ ...emptyForm }); setDialogOpen(true) }
  function openEdit(u: any) {
    const orig = allUpstreams.find((x: any) => x.id === u.id || x.name === u.name)
    if (!orig) return
    setEditId(orig.id)
    setForm({ name: orig.name, url: orig.url, priority: orig.priority, proxy: orig.proxy || '', adapter_type: orig.adapter_type, probe_mode: orig.probe_mode || 'active', probe_interval: orig.probe_interval || '30s' })
    setDialogOpen(true)
  }
  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    try { new URL(form.url) } catch { setUrlError(t('upstreams.invalidUrl')); return }
    setUrlError('')
    if (editId) updateMutation.mutate({ id: editId, data: form })
    else createMutation.mutate(form)
  }

  async function checkAll() {
    setChecking(true)
    try {
      await Promise.allSettled(
        allUpstreams.filter((u: any) => u.id).map((u: any) => adminApi.checkUpstream(u.id))
      )
      queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
    } finally {
      setChecking(false)
    }
  }

  async function checkOne(id: number) {
    await adminApi.checkUpstream(id)
    queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
  }

  const isSaving = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      {/* Toolbar: manual check + add button. Periodic probing is configured
          per-upstream in the edit dialog (default: every 30m). */}
      <div className="flex items-center gap-3 flex-wrap">
        {/* Manual check */}
        <ButtonV2
          variant="secondary"
          size="sm"
          onClick={checkAll}
          disabled={checking}
        >
          <Icon name="refresh" size="sm" />
          {checking ? t('upstreams.checking') : t('upstreams.checkAll')}
        </ButtonV2>

        <div className="flex-1" />

        {/* Add upstream */}
        <ButtonV2 size="sm" onClick={openCreate}>
          <Icon name="add" size="sm" />
          {t('upstreams.addUpstream')}
        </ButtonV2>
      </div>

      {/* Upstream grid with heartbeat bars */}
      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="h-32 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />
          ))}
        </div>
      ) : (
        <UpstreamGroupedPanel
          upstreams={upstreamItems}
          renderActions={(u) => (
            <div className="flex gap-0.5 ml-1">
              {u.id && (
                <button
                  onClick={() => checkOne(u.id!)}
                  className="bg-transparent cursor-pointer p-1.5 rounded-[3px] transition-[opacity,transform] duration-100 opacity-40 hover:opacity-100 active:scale-[0.96]"
                  style={{ color: 'var(--text-soft)' }}
                  title={t('upstreams.checkOne')}
                >
                  <Icon name="refresh" size="sm" />
                </button>
              )}
              <button
                onClick={() => openEdit(u as any)}
                className="bg-transparent cursor-pointer p-1.5 rounded-[3px] transition-[opacity,transform] duration-100 opacity-40 hover:opacity-100 active:scale-[0.96]"
                style={{ color: 'var(--text-soft)' }}
              >
                <Icon name="edit" size="sm" />
              </button>
              {u.id && (
                <button
                  onClick={() => setDeleteTarget(u.id!)}
                  className="bg-transparent cursor-pointer p-1.5 rounded-[3px] transition-[opacity,transform] duration-100 opacity-40 hover:opacity-100 active:scale-[0.96]"
                  style={{ color: 'var(--text-soft)' }}
                >
                  <Icon name="delete" size="sm" />
                </button>
              )}
            </div>
          )}
        />
      )}

      {/* Create/Edit Modal */}
      <ModalV2 open={dialogOpen} onClose={closeDialog} title={editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}>
        <form onSubmit={handleSubmit} className="space-y-4">
          <SelectV2 label={t('type')} value={form.adapter_type} onChange={(e) => setForm({ ...form, adapter_type: e.target.value })} disabled={!!editId}>
            {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
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
            onChange={(e) => setForm({ ...form, probe_mode: e.target.value })}
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
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="submit" disabled={isSaving}>{isSaving ? t('saving') : t('save')}</ButtonV2>
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
  )
}
