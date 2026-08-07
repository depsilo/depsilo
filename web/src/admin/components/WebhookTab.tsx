import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import EmptyState from '@/components/EmptyState'
import IconButton from '@/components/IconButton'
import InputV2 from '@/components/Input'
import ModalV2 from '@/components/Modal'
import QueryErrorState from '@/components/QueryErrorState'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import SelectV2 from '@/components/Select'
import { useAppToast } from '@/components/Toast'
import { usePrincipal } from '@/hooks/usePrincipal'
import { webhookApi, type WebhookConfig } from '@/lib/api'
import { getApiError } from '@/lib/apiError'

const PLATFORM_OPTIONS = ['slack', 'dingtalk', 'wecom', 'feishu', 'generic'] as const
const WEBHOOK_EVENTS = [
  'upstream_down',
  'disk_high',
  'vuln_critical',
  'license_expiring',
  'quarantine_blocked',
  'malware_blocked',
  'tamper_detected',
] as const

function eventValues(events: string): string[] {
  if (events === '*') return [...WEBHOOK_EVENTS]
  return [...new Set(events.split(',').map(value => value.trim()).filter(Boolean))]
}

interface WebhookForm {
  name: string
  platform: WebhookConfig['platform']
  url: string
  events: string
  cooldown_minutes: number
}

const emptyForm = (): WebhookForm => ({
  name: '',
  platform: 'dingtalk',
  url: '',
  events: '*',
  cooldown_minutes: 30,
})

export default function WebhookTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const deleteTriggerRef = useRef<HTMLButtonElement>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const [form, setForm] = useState<WebhookForm>(emptyForm)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const updateNow = () => setNow(Date.now())
    const interval = window.setInterval(updateNow, 60_000)
    window.addEventListener('focus', updateNow)
    return () => {
      window.clearInterval(interval)
      window.removeEventListener('focus', updateNow)
    }
  }, [])

  const query = useQuery({
    queryKey: ['admin', 'webhooks'],
    queryFn: async ({ signal }) => (await webhookApi.list({ signal })).data,
    retry: false,
  })

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
  const showFailure = (error: unknown, fallback: string) => {
    toast.show({ tone: 'danger', message: getApiError(error).message || fallback })
  }

  const createMutation = useMutation({
    mutationFn: (data: Partial<WebhookConfig>) => webhookApi.create(data),
    onSuccess: () => {
      void refresh()
      closeForm()
      toast.show({ tone: 'success', message: t('webhook.created') })
    },
    onError: error => showFailure(error, t('webhook.saveError')),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: Partial<WebhookConfig> }) => webhookApi.update(id, data),
    onSuccess: () => {
      void refresh()
      closeForm()
      toast.show({ tone: 'success', message: t('webhook.updated') })
    },
    onError: error => showFailure(error, t('webhook.saveError')),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => webhookApi.delete(id),
    onSuccess: () => {
      void refresh()
      setDeleteId(null)
      toast.show({ tone: 'success', message: t('webhook.deleted') })
    },
    onError: error => showFailure(error, t('webhook.deleteError')),
  })

  const testMutation = useMutation({
    mutationFn: (id: number) => webhookApi.test(id),
    onSuccess: () => {
      setNow(Date.now())
      void refresh()
      toast.show({ tone: 'success', message: t('webhook.testSent') })
    },
    onError: error => showFailure(error, t('webhook.testError')),
  })

  const openCreate = () => {
    setEditingId(null)
    setForm(emptyForm())
    setFormOpen(true)
  }

  const openEdit = (webhook: WebhookConfig) => {
    setEditingId(webhook.id)
    setForm({
      name: webhook.name,
      platform: webhook.platform,
      url: webhook.url,
      events: webhook.events,
      cooldown_minutes: webhook.cooldown_minutes,
    })
    setFormOpen(true)
  }

  const closeForm = () => {
    setFormOpen(false)
    setEditingId(null)
  }

  const save = () => {
    if (eventValues(form.events).length === 0) return
    const data: Partial<WebhookConfig> = {
      name: form.name.trim(),
      platform: form.platform,
      url: form.url.trim(),
      events: form.events,
      cooldown_minutes: form.cooldown_minutes,
    }
    if (editingId !== null) updateMutation.mutate({ id: editingId, data })
    else createMutation.mutate(data)
  }

  const formatLastSent = (timestamp: string | null) => {
    if (timestamp === null) return t('webhook.never')
    const sentAt = Date.parse(timestamp)
    if (!Number.isFinite(sentAt)) return t('webhook.never')
    const minutes = Math.max(0, Math.floor((now - sentAt) / 60_000))
    if (minutes < 1) return t('webhook.justNow')
    if (minutes < 60) return t('webhook.minutesAgo', { count: minutes })
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return t('webhook.hoursAgo', { count: hours })
    return t('webhook.daysAgo', { count: Math.floor(hours / 24) })
  }

  const guide = t(`webhook.guides.${form.platform}`, { defaultValue: '' })
  const saving = createMutation.isPending || updateMutation.isPending
  const hasEvents = eventValues(form.events).length > 0
  const deletingWebhook = query.data?.find(webhook => webhook.id === deleteId)

  return (
    <div className="min-w-0">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p className="min-w-0 text-[13px] leading-5 text-[var(--text-soft)]">{t('webhook.description')}</p>
        {canWrite && (
          <ButtonV2 type="button" onClick={openCreate}>{t('webhook.addWebhook')}</ButtonV2>
        )}
      </div>

      <div aria-busy={query.isPending || undefined} className="min-w-0">
        {query.isPending ? (
          <div className="h-40 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
        ) : query.isError && !query.data ? (
          <QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : getApiError(query.error).message} onRetry={() => { void query.refetch() }} />
        ) : (
          <div>
          {query.data && query.isRefetchError && (
            <div className="mb-3">
              <StaleDataNotice refreshing={query.isFetching} onRefresh={() => query.refetch()} />
            </div>
          )}
          {!query.data?.length ? <EmptyState icon="notifications_off" title={t('webhook.noWebhooks')} minHeight={180} /> : <div className="divide-y divide-[var(--border)]">
            {query.data.map(webhook => (
              <article key={webhook.id} className="flex min-w-0 flex-col gap-3 py-4 md:flex-row md:items-start md:justify-between">
                <div className="min-w-0 flex-1">
                  <div className="mb-2 flex flex-wrap items-center gap-2">
                    <strong className="text-[14px] font-[600] text-[var(--text)]">{webhook.name}</strong>
                    <BadgeV2>{t(`webhook.platforms.${webhook.platform}`)}</BadgeV2>
                    {!webhook.enabled && <BadgeV2 variant="warning">{t('webhook.disabled')}</BadgeV2>}
                  </div>
                  <p className="break-all font-mono text-[12px] leading-5 text-[var(--text-soft)]">{webhook.url}</p>
                  <dl className="mt-2 flex min-w-0 flex-col gap-1 text-[12px] text-[var(--text-muted)] sm:flex-row sm:flex-wrap sm:gap-x-4">
                    <div className="flex min-w-0 gap-1"><dt className="shrink-0">{t('webhook.events')}:</dt><dd className="min-w-0 break-words">{webhook.events === '*' ? t('webhook.eventsAll') : webhook.events}</dd></div>
                    <div className="flex min-w-0 gap-1"><dt className="shrink-0">{t('webhook.cooldown')}:</dt><dd className="min-w-0 break-words">{t('webhook.minutes', { count: webhook.cooldown_minutes })}</dd></div>
                    <div className="flex min-w-0 gap-1"><dt className="shrink-0">{t('webhook.lastSent')}:</dt><dd className="min-w-0 break-words">{formatLastSent(webhook.last_sent_at)}</dd></div>
                  </dl>
                </div>
                {canWrite && (
                  <div className="flex shrink-0 items-center gap-1 self-end md:self-start">
                    <ButtonV2
                      type="button"
                      variant="secondary"
                      size="sm"
                      className="min-h-10 min-w-[72px]"
                      aria-label={t('webhook.testNamed', { name: webhook.name })}
                      aria-busy={testMutation.isPending && testMutation.variables === webhook.id || undefined}
                      disabled={testMutation.isPending}
                      onClick={() => testMutation.mutate(webhook.id)}
                    >
                      {t('webhook.test')}
                    </ButtonV2>
                    <IconButton icon="edit" label={t('webhook.editNamed', { name: webhook.name })} onClick={() => openEdit(webhook)} />
                    <IconButton
                      icon="delete"
                      label={t('webhook.deleteNamed', { name: webhook.name })}
                      tone="danger"
                      onClick={event => {
                        deleteTriggerRef.current = event.currentTarget
                        setDeleteId(webhook.id)
                      }}
                    />
                  </div>
                )}
              </article>
            ))}
          </div>}
          </div>
        )}
      </div>

      <ModalV2 open={formOpen} title={editingId === null ? t('webhook.addWebhook') : t('webhook.editWebhook')} onClose={closeForm} closeDisabled={saving}>
        <div className="flex flex-col gap-4">
          <InputV2 label={t('webhook.name')} value={form.name} onChange={event => setForm(current => ({ ...current, name: event.target.value }))} placeholder={t('webhook.namePlaceholder')} />
          <SelectV2 label={t('webhook.platform')} value={form.platform} onChange={event => setForm(current => ({ ...current, platform: event.target.value as WebhookConfig['platform'] }))}>
            {PLATFORM_OPTIONS.map(platform => <option key={platform} value={platform}>{t(`webhook.platforms.${platform}`)}</option>)}
          </SelectV2>
          {guide && <p className="text-[12px] leading-5 text-[var(--text-muted)]">{t('webhook.guideTitle')}: {guide}</p>}
          <InputV2 label={t('webhook.url')} value={form.url} onChange={event => setForm(current => ({ ...current, url: event.target.value }))} placeholder={t('webhook.urlPlaceholder')} />
          <fieldset aria-invalid={!hasEvents || undefined} aria-describedby={!hasEvents ? 'webhook-events-error' : undefined}>
            <legend className="mb-2 text-[14px] text-[var(--text-muted)]">{t('webhook.events')}</legend>
            <div className="grid gap-2 sm:grid-cols-2">
              {WEBHOOK_EVENTS.map(eventName => {
                const current = eventValues(form.events)
                const selected = current.includes(eventName)
                return (
                  <label key={eventName} className="flex min-h-10 cursor-pointer items-center gap-2 text-[13px] text-[var(--text-soft)]">
                    <input
                      type="checkbox"
                      className="h-4 w-4 accent-[var(--brand)] stripe-focus-ring"
                      checked={selected}
                      onChange={() => {
                        const next = selected ? current.filter(value => value !== eventName) : [...current, eventName]
                        setForm(value => ({ ...value, events: next.join(',') }))
                      }}
                    />
                    {t(`webhook.events_list.${eventName}`)}
                  </label>
                )
              })}
            </div>
            {!hasEvents && (
              <p id="webhook-events-error" role="alert" className="mt-2 text-[12px] text-[var(--danger-text)]">
                {t('webhook.eventRequired')}
              </p>
            )}
          </fieldset>
          <InputV2 label={t('webhook.cooldown')} type="number" value={form.cooldown_minutes} min={5} max={1440} onChange={event => setForm(current => ({ ...current, cooldown_minutes: Number(event.target.value) || 30 }))} />
          <div className="flex justify-end gap-2 pt-1">
            <ButtonV2 type="button" variant="secondary" disabled={saving} onClick={closeForm}>{t('cancel')}</ButtonV2>
            <ButtonV2 type="button" aria-busy={saving || undefined} disabled={saving || !form.name.trim() || !form.url.trim() || !hasEvents} onClick={save}>{saving ? t('saving') : t('save')}</ButtonV2>
          </div>
        </div>
      </ModalV2>

      <ModalV2
        open={deleteId !== null}
        title={t('webhook.deleteConfirmNamed', { name: deletingWebhook?.name ?? '' })}
        onClose={() => setDeleteId(null)}
        finalFocus={deleteTriggerRef}
        closeDisabled={deleteMutation.isPending}
      >
        <p className="mb-5 text-[13px] leading-5 text-[var(--text-soft)]">{t('webhook.deleteWarning')}</p>
        <div className="flex justify-end gap-2">
          <ButtonV2 type="button" variant="secondary" disabled={deleteMutation.isPending} onClick={() => setDeleteId(null)}>{t('cancel')}</ButtonV2>
          <ButtonV2 type="button" variant="danger" aria-busy={deleteMutation.isPending || undefined} disabled={deleteMutation.isPending} onClick={() => { if (deleteId !== null) deleteMutation.mutate(deleteId) }}>
            {deleteMutation.isPending ? t('deleting') : t('delete')}
          </ButtonV2>
        </div>
      </ModalV2>
    </div>
  )
}
