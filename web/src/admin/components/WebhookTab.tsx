import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { webhookApi, type WebhookConfig } from '@/lib/api'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import ModalV2 from '@/components/Modal'
import EmptyState from '@/components/EmptyState'

const PLATFORM_OPTIONS = [
  { value: 'slack', labelKey: 'webhook.platforms.slack' },
  { value: 'dingtalk', labelKey: 'webhook.platforms.dingtalk' },
  { value: 'wecom', labelKey: 'webhook.platforms.wecom' },
  { value: 'feishu', labelKey: 'webhook.platforms.feishu' },
  { value: 'generic', labelKey: 'webhook.platforms.generic' },
]

const EVENT_OPTIONS = [
  { value: 'upstream_down', labelKey: 'webhook.events_list.upstream_down' },
  { value: 'disk_high', labelKey: 'webhook.events_list.disk_high' },
  { value: 'vuln_critical', labelKey: 'webhook.events_list.vuln_critical' },
  { value: 'license_expiring', labelKey: 'webhook.events_list.license_expiring' },
]

export default function WebhookTab() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const [testResult, setTestResult] = useState<string | null>(null)

  const { data: webhooks, isLoading } = useQuery({
    queryKey: ['admin', 'webhooks'],
    queryFn: async () => {
      const res = await webhookApi.list()
      return res.data
    },
  })

  const [form, setForm] = useState({
    name: '',
    platform: 'dingtalk' as string,
    url: '',
    events: '*' as string,
    cooldown_minutes: 30,
  })

  const createMut = useMutation({
    mutationFn: (data: Partial<WebhookConfig>) => webhookApi.create(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
      closeModal()
    },
  })

  const updateMut = useMutation({
    mutationFn: ({ id, data }: { id: number; data: Partial<WebhookConfig> }) =>
      webhookApi.update(id, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
      closeModal()
    },
  })

  const deleteMut = useMutation({
    mutationFn: (id: number) => webhookApi.delete(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
      setDeleteId(null)
    },
  })

  const testMut = useMutation({
    mutationFn: (id: number) => webhookApi.test(id),
    onSuccess: () => setTestResult('sent'),
    onError: () => setTestResult('error'),
  })

  function openCreate() {
    setEditingId(null)
    setForm({ name: '', platform: 'dingtalk', url: '', events: '*', cooldown_minutes: 30 })
    setModalOpen(true)
  }

  function openEdit(w: WebhookConfig) {
    setEditingId(w.id)
    setForm({
      name: w.name,
      platform: w.platform,
      url: w.url,
      events: w.events,
      cooldown_minutes: w.cooldown_minutes,
    })
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingId(null)
  }

  function handleSave() {
    const data: Partial<WebhookConfig> = {
      name: form.name,
      platform: form.platform as WebhookConfig['platform'],
      url: form.url,
      events: form.events || '*',
      cooldown_minutes: form.cooldown_minutes,
    }
    if (editingId) {
      updateMut.mutate({ id: editingId, data })
    } else {
      createMut.mutate(data)
    }
  }

  function formatLastSent(ts?: string) {
    if (!ts) return t('webhook.never')
    const d = new Date(ts)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    const mins = Math.floor(diffMs / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    return `${Math.floor(hours / 24)}d ago`
  }

  function getGuideText(platform: string) {
    switch (platform) {
      case 'slack': return t('webhook.guideSlack')
      case 'dingtalk': return t('webhook.guideDingTalk')
      case 'wecom': return t('webhook.guideWeCom')
      case 'feishu': return t('webhook.guideFeishu')
      default: return ''
    }
  }

  const saving = createMut.isPending || updateMut.isPending

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <p style={{ color: 'var(--text-soft)', margin: 0, fontSize: 14 }}>{t('webhook.description')}</p>
        <button className="btn btn-primary" onClick={openCreate}>
          + {t('webhook.addWebhook')}
        </button>
      </div>

      {isLoading ? (
        <p>{t('loading')}</p>
      ) : !webhooks || webhooks.length === 0 ? (
        <EmptyState icon="notifications_off" title={t('webhook.noWebhooks')} minHeight={180} />
      ) : (
        <div>
          {webhooks.map((w: WebhookConfig, idx: number) => (
            <div
              key={w.id}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'flex-start',
                padding: '14px 0',
                borderBottom: idx < webhooks.length - 1 ? '1px solid var(--border)' : 'none',
              }}
            >
                <div style={{ flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                    <strong>{w.name}</strong>
                    <span style={{
                      fontSize: 11,
                      padding: '2px 8px',
                      borderRadius: 4,
                      background: 'var(--accent-soft)',
                      color: 'var(--accent)',
                      fontWeight: 500,
                    }}>
                      {t(`webhook.platforms.${w.platform}`)}
                    </span>
                    {!w.enabled && (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>(disabled)</span>
                    )}
                  </div>
                  <div style={{ fontSize: 13, color: 'var(--text-soft)', marginBottom: 4, wordBreak: 'break-all' }}>
                    {w.url}
                  </div>
                  <div style={{ display: 'flex', gap: 16, fontSize: 12, color: 'var(--text-muted)' }}>
                    <span>{t('webhook.events')}: {w.events === '*' ? t('webhook.eventsAll') : w.events}</span>
                    <span>{t('webhook.cooldown')}: {w.cooldown_minutes}min</span>
                    <span>{t('webhook.lastSent')}: {formatLastSent(w.last_sent_at)}</span>
                  </div>
                </div>
                <div style={{ display: 'flex', gap: 8, flexShrink: 0, marginLeft: 16 }}>
                  <button className="btn btn-sm" onClick={() => testMut.mutate(w.id)} disabled={testMut.isPending}>
                    Test
                  </button>
                  <button className="btn btn-sm" onClick={() => openEdit(w)}>Edit</button>
                  <button className="btn btn-sm btn-danger" onClick={() => setDeleteId(w.id)}>Delete</button>
                </div>
            </div>
          ))}
        </div>
      )}

      {/* Create/Edit Modal */}
      <ModalV2 open={modalOpen} title={editingId ? t('webhook.editWebhook') : t('webhook.addWebhook')} onClose={closeModal}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          <div>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 }}>{t('webhook.name')}</label>
            <InputV2
              value={form.name}
              onChange={(e: any) => setForm(p => ({ ...p, name: e.target.value }))}
              placeholder={t('webhook.namePlaceholder')}
            />
          </div>
          <div>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 }}>{t('webhook.platform')}</label>
            <SelectV2
              value={form.platform}
              onChange={(e: any) => setForm(p => ({ ...p, platform: e.target.value }))}
            >
              {PLATFORM_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{t(o.labelKey)}</option>
              ))}
            </SelectV2>
            {getGuideText(form.platform) && (
              <p style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
                {t('webhook.guideTitle')}: {getGuideText(form.platform)}
              </p>
            )}
          </div>
          <div>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 }}>{t('webhook.url')}</label>
            <InputV2
              value={form.url}
              onChange={(e: any) => setForm(p => ({ ...p, url: e.target.value }))}
              placeholder={t('webhook.urlPlaceholder')}
            />
          </div>
          <div>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 }}>{t('webhook.events')}</label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {EVENT_OPTIONS.map(ev => {
                const selected = form.events === '*' || form.events.split(',').map(s => s.trim()).includes(ev.value)
                return (
                  <label key={ev.value} style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 13, cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={selected}
                      onChange={() => {
                        const current = form.events === '*' ? EVENT_OPTIONS.map(e => e.value) : form.events.split(',').map(s => s.trim())
                        const next = selected ? current.filter(v => v !== ev.value) : [...current, ev.value]
                        setForm(p => ({ ...p, events: next.length === EVENT_OPTIONS.length ? '*' : next.join(',') }))
                      }}
                    />
                    {t(ev.labelKey)}
                  </label>
                )
              })}
            </div>
          </div>
          <div>
            <label style={{ display: 'block', marginBottom: 4, fontSize: 13, fontWeight: 500 }}>{t('webhook.cooldown')}</label>
            <InputV2
              type="number"
              value={String(form.cooldown_minutes)}
              onChange={(e: any) => setForm(p => ({ ...p, cooldown_minutes: parseInt(e.target.value) || 30 }))}
              min={5}
              max={1440}
            />
          </div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 8 }}>
            <button className="btn btn-sm" onClick={closeModal}>{t('cancel')}</button>
            <button className="btn btn-primary btn-sm" onClick={handleSave} disabled={saving || !form.name || !form.url}>
              {saving ? t('saving') : t('save')}
            </button>
          </div>
        </div>
      </ModalV2>

      {/* Delete confirmation */}
      <ModalV2 open={deleteId !== null} title={t('webhook.deleteConfirm')} onClose={() => setDeleteId(null)}>
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 12 }}>
          <button className="btn btn-sm" onClick={() => setDeleteId(null)}>{t('cancel')}</button>
          <button className="btn btn-sm btn-danger" onClick={() => { if (deleteId) deleteMut.mutate(deleteId) }} disabled={deleteMut.isPending}>
            {deleteMut.isPending ? t('deleting') : t('delete')}
          </button>
        </div>
      </ModalV2>

      {/* Test result toast */}
      {testResult && (
        <div style={{
          position: 'fixed', bottom: 20, right: 20, padding: '12px 20px', borderRadius: 8,
          background: testResult === 'sent' ? 'var(--green)' : 'var(--red)',
          color: '#fff', fontSize: 14, zIndex: 9999,
        }}>
          {testResult === 'sent' ? t('webhook.testSent') : t('webhook.testError')}
          <button onClick={() => setTestResult(null)} style={{ marginLeft: 12, background: 'none', border: 'none', color: '#fff', cursor: 'pointer' }}>×</button>
        </div>
      )}
    </div>
  )
}
