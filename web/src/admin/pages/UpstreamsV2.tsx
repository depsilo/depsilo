import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import ButtonV2 from '@/components/ButtonV2'
import InputV2 from '@/components/InputV2'
import SelectV2 from '@/components/SelectV2'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/ModalV2'
import EcosystemIcon from '@/components/EcosystemIcon'

interface UpstreamForm { name: string; url: string; priority: number; proxy: string; adapter_type: string }
const emptyForm: UpstreamForm = { name: '', url: '', priority: 1, proxy: '', adapter_type: 'pypi' }

const ECOSYSTEMS = ['pypi', 'apt', 'npm', 'go', 'cargo', 'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm'] as const

export default function UpstreamsV2() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<string>('pypi')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<UpstreamForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [urlError, setUrlError] = useState('')

  const { data, isLoading } = useQuery({ queryKey: ['admin', 'upstreams'], queryFn: () => adminApi.listUpstreams() })
  const allUpstreams: any[] = data?.data?.items || data?.data || []
  const filtered = allUpstreams.filter((u: any) => u.adapter_type === tab)

  const createMutation = useMutation({ mutationFn: (d: any) => adminApi.createUpstream(d), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); closeDialog() } })
  const updateMutation = useMutation({ mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateUpstream(id, d), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); closeDialog() } })
  const deleteMutation = useMutation({ mutationFn: (id: number) => adminApi.deleteUpstream(id), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] }); setDeleteTarget(null) } })

  function closeDialog() { setDialogOpen(false); setEditId(null); setForm(emptyForm); setUrlError('') }
  function openCreate() { setEditId(null); setForm({ ...emptyForm, adapter_type: tab }); setDialogOpen(true) }
  function openEdit(u: any) { setEditId(u.id); setForm({ name: u.name, url: u.url, priority: u.priority, proxy: u.proxy || '', adapter_type: u.adapter_type }); setDialogOpen(true) }
  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    try { new URL(form.url) } catch { setUrlError(t('upstreams.invalidUrl')); return }
    setUrlError('')
    if (editId) updateMutation.mutate({ id: editId, data: form })
    else createMutation.mutate(form)
  }
  const isSaving = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div />
        <ButtonV2 onClick={openCreate}><Icon name="add" size="sm" />{t('upstreams.addUpstream')}</ButtonV2>
      </div>

      {/* Ecosystem pills */}
      <div className="flex flex-wrap gap-1.5">
        {ECOSYSTEMS.map((eco) => (
          <button
            key={eco} onClick={() => setTab(eco)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-[13px] font-[400] rounded-[4px] transition-colors duration-150 cursor-pointer"
            style={{
              background: tab === eco ? 'var(--stripe-purple)' : 'var(--surface)',
              color: tab === eco ? 'var(--on-primary)' : 'var(--body)',
              border: tab === eco ? 'none' : '1px solid var(--border)',
            }}
          >
            {tab !== eco && <EcosystemIcon type={eco} size={14} useColor={false} />}
            {eco.toUpperCase()}
          </button>
        ))}
      </div>

      {/* Upstream list */}
      <div className="space-y-3">
        {isLoading && <p className="text-[14px]" style={{ color: 'var(--body)' }}>{t('loading')}</p>}
        {!isLoading && filtered.length === 0 && <p className="text-[14px]" style={{ color: 'var(--body)' }}>{t('upstreams.noUpstreams', { type: tab.toUpperCase() })}</p>}
        {filtered.map((u: any) => (
          <CardV2 key={u.id} className="flex items-center justify-between">
            <div className="flex items-center gap-3 min-w-0">
              <span className="relative flex h-2.5 w-2.5 shrink-0">
                {u.healthy && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--success)' }} />}
                <span className="relative inline-flex rounded-full h-2.5 w-2.5" style={{ background: u.healthy ? 'var(--success)' : 'var(--error)' }} />
              </span>
              <EcosystemIcon type={u.adapter_type} size={18} />
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-[400] text-[14px]" style={{ color: 'var(--heading)' }}>{u.name}</span>
                  {u.proxy && <span className="text-[12px] font-mono" style={{ color: 'var(--body)' }}>· {t('upstreams.proxy')}: {u.proxy}</span>}
                </div>
                <p className="font-mono text-[13px] truncate" style={{ color: 'var(--body)' }}>{u.url}</p>
              </div>
            </div>
            <div className="flex items-center gap-4 shrink-0">
              <div className="text-right">
                <p className="text-[13px] font-mono tabular-nums" style={{ color: 'var(--heading)' }}>{u.avg_latency_ms || 0} ms</p>
                <p className="text-[10px]" style={{ color: 'var(--body)' }}>{t('upstreams.availability')} {((u.success_rate || 0) * 100).toFixed(1)}%</p>
              </div>
              <div className="flex gap-1">
                <button onClick={() => openEdit(u)} className="bg-transparent cursor-pointer transition-colors duration-150 p-1.5 rounded-[4px]" style={{ color: 'var(--body)' }}><Icon name="edit" size="sm" /></button>
                <button onClick={() => setDeleteTarget(u.id)} className="bg-transparent cursor-pointer transition-colors duration-150 p-1.5 rounded-[4px]" style={{ color: 'var(--body)' }}><Icon name="delete" size="sm" /></button>
              </div>
            </div>
          </CardV2>
        ))}
      </div>

      {/* Create/Edit Modal */}
      <ModalV2 open={dialogOpen} onClose={closeDialog} title={editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}>
        <form onSubmit={handleSubmit} className="space-y-4">
          <SelectV2 label={t('type')} value={form.adapter_type} onChange={(e) => setForm({ ...form, adapter_type: e.target.value })} disabled={!!editId}>
            {ECOSYSTEMS.map(eco => <option key={eco} value={eco}>{eco.toUpperCase()}</option>)}
          </SelectV2>
          <InputV2 label={t('name')} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="e.g. tuna" required />
          <div>
            <InputV2 label={t('upstreams.url')} mono value={form.url} onChange={(e) => { setForm({ ...form, url: e.target.value }); setUrlError('') }} placeholder="https://pypi.tuna.tsinghua.edu.cn" required />
            {urlError && <p className="text-[12px] mt-1" style={{ color: 'var(--error)' }}>{urlError}</p>}
          </div>
          <InputV2 label={t('upstreams.priority')} type="number" min={1} value={form.priority} onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 1 })} />
          <InputV2 label={t('upstreams.httpProxy')} mono value={form.proxy} onChange={(e) => setForm({ ...form, proxy: e.target.value })} placeholder="http://127.0.0.1:7890" />
          <div className="flex justify-end gap-3 pt-2">
            <ButtonV2 type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="submit" disabled={isSaving}>{isSaving ? t('saving') : t('save')}</ButtonV2>
          </div>
        </form>
      </ModalV2>

      {/* Delete Modal */}
      <ModalV2 open={deleteTarget !== null} onClose={() => setDeleteTarget(null)} title={t('upstreams.confirmDelete')}>
        <p className="text-[14px] mb-6" style={{ color: 'var(--body)' }}>{t('upstreams.confirmDeleteMsg')}</p>
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
