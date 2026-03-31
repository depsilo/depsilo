import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import Card from '@/components/Card'
import Button from '@/components/Button'
import Input from '@/components/Input'
import Icon from '@/components/Icon'
import Modal from '@/components/Modal'

interface UpstreamForm {
  name: string
  url: string
  priority: number
  proxy: string
  adapter_type: string
}

const emptyForm: UpstreamForm = {
  name: '',
  url: '',
  priority: 1,
  proxy: '',
  adapter_type: 'pypi',
}

export default function Upstreams() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<'pypi' | 'apt' | 'npm'>('pypi')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editId, setEditId] = useState<number | null>(null)
  const [form, setForm] = useState<UpstreamForm>(emptyForm)
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null)
  const [urlError, setUrlError] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: () => adminApi.listUpstreams(),
  })

  const allUpstreams: any[] = data?.data?.items || data?.data || []
  const filtered = allUpstreams.filter((u: any) => u.adapter_type === tab)

  const createMutation = useMutation({
    mutationFn: (d: any) => adminApi.createUpstream(d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
      closeDialog()
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data: d }: { id: number; data: any }) => adminApi.updateUpstream(id, d),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
      closeDialog()
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => adminApi.deleteUpstream(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
      setDeleteTarget(null)
    },
  })

  function closeDialog() {
    setDialogOpen(false)
    setEditId(null)
    setForm(emptyForm)
    setUrlError('')
  }

  function openCreate() {
    setEditId(null)
    setForm({ ...emptyForm, adapter_type: tab })
    setDialogOpen(true)
  }

  function openEdit(u: any) {
    setEditId(u.id)
    setForm({
      name: u.name,
      url: u.url,
      priority: u.priority,
      proxy: u.proxy || '',
      adapter_type: u.adapter_type,
    })
    setDialogOpen(true)
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    try {
      new URL(form.url)
    } catch {
      setUrlError(t('upstreams.invalidUrl'))
      return
    }
    setUrlError('')
    if (editId) {
      updateMutation.mutate({ id: editId, data: form })
    } else {
      createMutation.mutate(form)
    }
  }

  const isSaving = createMutation.isPending || updateMutation.isPending

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div />
        <Button onClick={openCreate}>
          <Icon name="add" size="sm" />
          {t('upstreams.addUpstream')}
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex gap-0 border-b border-outline-variant/10">
        {(['pypi', 'apt', 'npm'] as const).map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`px-6 py-2.5 text-sm font-medium transition-colors cursor-pointer border-b-2 bg-transparent ${
              tab === t
                ? 'border-primary text-on-surface'
                : 'border-transparent text-on-surface-variant hover:text-on-surface'
            }`}
          >
            {t.toUpperCase()}
          </button>
        ))}
      </div>

      {/* Upstream list */}
      <div className="space-y-3">
        {isLoading && <p className="text-sm text-on-surface-variant">{t('loading')}</p>}
        {!isLoading && filtered.length === 0 && (
          <p className="text-sm text-on-surface-variant">{t('upstreams.noUpstreams', { type: tab.toUpperCase() })}</p>
        )}
        {filtered.map((u: any) => (
          <Card key={u.id} className="flex items-center justify-between">
            <div className="flex items-center gap-3 min-w-0">
              <span className={`h-2.5 w-2.5 rounded-full shrink-0 ${u.healthy ? 'bg-success' : 'bg-error'}`} />
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-on-surface">{u.name}</span>
                  {u.proxy && (
                    <span className="text-xs text-secondary font-mono">
                      {'· ' + t('upstreams.proxy') + ':'} {u.proxy}
                    </span>
                  )}
                </div>
                <p className="font-mono text-sm text-on-surface-variant truncate">{u.url}</p>
              </div>
            </div>
            <div className="flex items-center gap-4 shrink-0">
              <div className="text-right">
                <p className="text-sm font-mono text-on-surface">{u.avg_latency_ms || 0} ms</p>
                <p className="text-[10px] text-on-surface-variant">
                  {t('upstreams.availability')} {((u.success_rate || 0) * 100).toFixed(1)}%
                </p>
              </div>
              <div className="flex gap-1">
                <button
                  onClick={() => openEdit(u)}
                  className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1.5"
                >
                  <Icon name="edit" size="sm" />
                </button>
                <button
                  onClick={() => setDeleteTarget(u.id)}
                  className="bg-transparent text-on-surface-variant hover:text-error cursor-pointer transition-colors p-1.5"
                >
                  <Icon name="delete" size="sm" />
                </button>
              </div>
            </div>
          </Card>
        ))}
      </div>

      {/* Create/Edit Modal */}
      <Modal
        open={dialogOpen}
        onClose={closeDialog}
        title={editId ? t('upstreams.editUpstream') : t('upstreams.addUpstream')}
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('name')}</label>
            <Input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="e.g. tuna"
              required
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('upstreams.url')}</label>
            <Input
              mono
              value={form.url}
              onChange={(e) => { setForm({ ...form, url: e.target.value }); setUrlError('') }}
              placeholder="https://pypi.tuna.tsinghua.edu.cn"
              required
            />
            {urlError && <p className="text-xs text-error mt-1">{urlError}</p>}
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('upstreams.priority')}</label>
            <Input
              type="number"
              min={1}
              value={form.priority}
              onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 1 })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-on-surface mb-1">{t('upstreams.httpProxy')}</label>
            <Input
              mono
              value={form.proxy}
              onChange={(e) => setForm({ ...form, proxy: e.target.value })}
              placeholder="http://127.0.0.1:7890"
            />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button type="button" variant="secondary" onClick={closeDialog}>{t('cancel')}</Button>
            <Button type="submit" disabled={isSaving}>
              {isSaving ? t('saving') : t('save')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirm Modal */}
      <Modal
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('upstreams.confirmDelete')}
      >
        <p className="text-sm text-on-surface-variant mb-6">{t('upstreams.confirmDeleteMsg')}</p>
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={() => setDeleteTarget(null)}>{t('cancel')}</Button>
          <Button
            variant="secondary"
            className="text-error border-error/30"
            disabled={deleteMutation.isPending}
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
          >
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </Button>
        </div>
      </Modal>
    </div>
  )
}
