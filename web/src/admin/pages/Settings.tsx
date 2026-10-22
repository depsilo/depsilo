import { Skeleton } from '@/components/ui/skeleton'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Field, FieldLabel, FieldDescription, FieldError } from '@/components/ui/field'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import WebhookTab from '@/admin/components/WebhookTab'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'

import { Input } from '@/components/ui/input'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAppToast } from '@/components/Toast'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'
import { readSessionStorage, writeSessionStorage, removeSessionStorage } from '@/lib/storage'
import type {
  AdminSettingsResponse,
  AdminSettingsSnapshot,
  EditableSettingPath,
  SettingPath,
  UpdateAdminSettingsRequest,
  UpdateAdminSettingsResponse,
} from '@/lib/adminApi.types'

type TabKey = 'basic' | 'cache' | 'storage' | 'auth' | 'webhooks'

interface SettingsDraft {
  logLevel: AdminSettingsSnapshot['server']['log_level']
  maxSizeGB: string
  ttlIndex: string
  ttlBlob: string
  lruThreshold: string
  tokenTTL: string
}

type ValidatedDraftKey = Exclude<keyof SettingsDraft, 'logLevel'>
type SettingsValidationMessage =
  | 'settings.validation.positiveInteger'
  | 'settings.validation.thresholdRange'
  | 'settings.validation.duration'
  | 'settings.validation.tokenNever'
type SettingsValidationErrors = Partial<Record<ValidatedDraftKey, SettingsValidationMessage>>

const goDurationPattern = /^(?:0|[+-]?(?:(?:\d+(?:\.\d*)?|\.\d+)(?:ns|us|µs|μs|ms|s|m|h))+)$/
const validationOrder: ValidatedDraftKey[] = ['maxSizeGB', 'lruThreshold', 'ttlIndex', 'ttlBlob', 'tokenTTL']
const validationLocations: Record<ValidatedDraftKey, { tab: TabKey; id: string }> = {
  maxSizeGB: { tab: 'cache', id: 'setting-cache-max-size' },
  lruThreshold: { tab: 'cache', id: 'setting-cache-lru-threshold' },
  ttlIndex: { tab: 'cache', id: 'setting-cache-ttl-index' },
  ttlBlob: { tab: 'cache', id: 'setting-cache-ttl-blob' },
  tokenTTL: { tab: 'auth', id: 'setting-auth-token-ttl' },
}
const draftPaths: Record<keyof SettingsDraft, EditableSettingPath> = {
  logLevel: 'server.log_level', maxSizeGB: 'cache.max_size_gb', ttlIndex: 'cache.ttl_index',
  ttlBlob: 'cache.ttl_blob', lruThreshold: 'cache.lru_threshold', tokenTTL: 'auth.token_ttl',
}
const tabIcons = { basic: 'tune', cache: 'cached', storage: 'database', auth: 'shield', webhooks: 'notifications' } as const
const tabLabels: Record<TabKey, string> = { basic: 'settings.basic', cache: 'settings.cachePolicy', storage: 'settings.storageBackend', auth: 'settings.authSecurity', webhooks: 'settings.webhooks' }

function restoreDraft(key: string, configured: SettingsDraft): SettingsDraft | null {
  try {
    const stored = JSON.parse(readSessionStorage(key) ?? 'null')
    if (!stored?.draft || !stored?.configured) return null
    const next = { ...configured }
    for (const field of Object.keys(draftPaths) as (keyof SettingsDraft)[]) {
      if (typeof stored.draft[field] !== 'string' || typeof stored.configured[field] !== 'string') return null
      if (field === 'logLevel' && !['debug', 'info', 'warn', 'error'].includes(stored.draft[field])) return null
      if (stored.draft[field] !== stored.configured[field]) Object.assign(next, { [field]: stored.draft[field] })
    }
    return next
  } catch { return null }
}

function validateDraft(draft: SettingsDraft): SettingsValidationErrors {
  const errors: SettingsValidationErrors = {}
  const maxSize = Number(draft.maxSizeGB)
  const threshold = Number(draft.lruThreshold)
  if (!draft.maxSizeGB.trim() || !Number.isSafeInteger(maxSize) || maxSize <= 0) {
    errors.maxSizeGB = 'settings.validation.positiveInteger'
  }
  if (!draft.lruThreshold.trim() || !Number.isInteger(threshold) || threshold < 1 || threshold > 100) {
    errors.lruThreshold = 'settings.validation.thresholdRange'
  }
  if (!goDurationPattern.test(draft.ttlIndex)) errors.ttlIndex = 'settings.validation.duration'
  if (!goDurationPattern.test(draft.ttlBlob)) errors.ttlBlob = 'settings.validation.duration'
  if (draft.tokenTTL === 'never') {
    errors.tokenTTL = 'settings.validation.tokenNever'
  } else if (!goDurationPattern.test(draft.tokenTTL)) {
    errors.tokenTTL = 'settings.validation.duration'
  }
  return errors
}

const draftFrom = (settings: AdminSettingsSnapshot): SettingsDraft => ({
  logLevel: settings.server.log_level,
  maxSizeGB: String(settings.cache.max_size_gb),
  ttlIndex: settings.cache.ttl_index,
  ttlBlob: settings.cache.ttl_blob,
  lruThreshold: String(settings.cache.lru_threshold),
  tokenTTL: settings.auth.token_ttl,
})

function rebaseDraft(
  draft: SettingsDraft,
  previous: AdminSettingsSnapshot,
  next: AdminSettingsSnapshot,
): SettingsDraft {
  return {
    logLevel: draft.logLevel !== previous.server.log_level ? draft.logLevel : next.server.log_level,
    maxSizeGB: draft.maxSizeGB !== String(previous.cache.max_size_gb) ? draft.maxSizeGB : String(next.cache.max_size_gb),
    ttlIndex: draft.ttlIndex !== previous.cache.ttl_index ? draft.ttlIndex : next.cache.ttl_index,
    ttlBlob: draft.ttlBlob !== previous.cache.ttl_blob ? draft.ttlBlob : next.cache.ttl_blob,
    lruThreshold: draft.lruThreshold !== String(previous.cache.lru_threshold) ? draft.lruThreshold : String(next.cache.lru_threshold),
    tokenTTL: draft.tokenTTL !== previous.auth.token_ttl ? draft.tokenTTL : next.auth.token_ttl,
  }
}

function buildPatch(draft: SettingsDraft, base: AdminSettingsSnapshot): UpdateAdminSettingsRequest | null {
  const request: UpdateAdminSettingsRequest = {}
  if (draft.logLevel !== base.server.log_level) request.server = { log_level: draft.logLevel }
  const cache: NonNullable<UpdateAdminSettingsRequest['cache']> = {}
  if (draft.maxSizeGB !== String(base.cache.max_size_gb)) cache.max_size_gb = Number(draft.maxSizeGB)
  if (draft.ttlIndex !== base.cache.ttl_index) cache.ttl_index = draft.ttlIndex
  if (draft.ttlBlob !== base.cache.ttl_blob) cache.ttl_blob = draft.ttlBlob
  if (draft.lruThreshold !== String(base.cache.lru_threshold)) cache.lru_threshold = Number(draft.lruThreshold)
  if (Object.keys(cache).length) request.cache = cache
  if (draft.tokenTTL !== base.auth.token_ttl) request.auth = { token_ttl: draft.tokenTTL }
  return Object.keys(request).length ? request : null
}

function valueAt(snapshot: AdminSettingsSnapshot, path: SettingPath): string {
  switch (path) {
    case 'server.host': return snapshot.server.host
    case 'server.port': return String(snapshot.server.port)
    case 'server.log_level': return snapshot.server.log_level
    case 'database.driver': return snapshot.database.driver
    case 'storage.type': return snapshot.storage.type
    case 'storage.path': return snapshot.storage.path
    case 'cache.max_size_gb': return String(snapshot.cache.max_size_gb)
    case 'cache.ttl_index': return snapshot.cache.ttl_index
    case 'cache.ttl_blob': return snapshot.cache.ttl_blob
    case 'cache.lru_threshold': return String(snapshot.cache.lru_threshold)
    case 'auth.token_ttl': return snapshot.auth.token_ttl
  }
}

function mutationErrorMessage(error: unknown, fallback: string): string {
  if (typeof error !== 'object' || error === null || !('response' in error)) return fallback
  const response = (error as { response?: { data?: { code?: string; message?: string } } }).response
  const message = response?.data?.message
  const code = response?.data?.code
  if (message && code) return `${code}: ${message}`
  return message ?? fallback
}

export default function Settings() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const desktopTabs = useMediaQuery('(min-width: 768px)')
  const { principal, canWrite } = usePrincipal()
  const [activeTab, setActiveTab] = useState<TabKey>('basic')
  const [draft, setDraft] = useState<SettingsDraft | null>(null)
  const [validationErrors, setValidationErrors] = useState<SettingsValidationErrors>({})
  const [inlineError, setInlineError] = useState<string | null>(null)
  const [lastResult, setLastResult] = useState<UpdateAdminSettingsResponse | null>(null)
  const configuredRef = useRef<AdminSettingsSnapshot | null>(null)
  const [reviewOpen, setReviewOpen] = useState(false)
  const [restored, setRestored] = useState(false)
  const [draftStored, setDraftStored] = useState(true)
  const draftStorageKey = principal ? `depsilo:settings-draft:${window.location.origin}:${principal.id}` : null

  const settingsQuery = useQuery<AdminSettingsResponse>({
    queryKey: ['admin', 'settings'],
    queryFn: async ({ signal }) => (await adminApi.getSettings({ signal })).data,
  })

  useEffect(() => {
    if (!settingsQuery.data) return
    const previous = configuredRef.current
    const next = settingsQuery.data.configured
    const recovered = !previous && draftStorageKey ? restoreDraft(draftStorageKey, draftFrom(next)) : null
    if (recovered) setRestored(true)
    setDraft(current => {
      return current && previous
        ? rebaseDraft(current, previous, next)
        : recovered ?? draftFrom(next)
    })
    configuredRef.current = next
  }, [settingsQuery.data, draftStorageKey])

  const baseDraft = settingsQuery.data ? draftFrom(settingsQuery.data.configured) : null
  const changedFields = draft && baseDraft ? (Object.keys(draftPaths) as (keyof SettingsDraft)[]).filter(key => draft[key] !== baseDraft[key]) : []
  const dirty = changedFields.length > 0
  useEffect(() => {
    if (!draftStorageKey || !draft || !settingsQuery.data) return
    if (buildPatch(draft, settingsQuery.data.configured)) {
      setDraftStored(writeSessionStorage(draftStorageKey, JSON.stringify({ draft, configured: draftFrom(settingsQuery.data.configured) })))
    } else removeSessionStorage(draftStorageKey)
  }, [draft, settingsQuery.data, draftStorageKey])
  useEffect(() => {
    if (!dirty) return
    const protect = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', protect)
    return () => window.removeEventListener('beforeunload', protect)
  }, [dirty])

  const updateMutation = useMutation({
    mutationFn: async (request: UpdateAdminSettingsRequest) => (await adminApi.updateSettings(request)).data,
    onSuccess: response => {
      queryClient.setQueryData<AdminSettingsResponse>(['admin', 'settings'], response)
      configuredRef.current = response.configured
      setDraft(draftFrom(response.configured))
      setValidationErrors({})
      setInlineError(null)
      setLastResult(response)
      setReviewOpen(false)
      setRestored(false)
      const tone = response.blocked_by_override.length || response.restart_required.length ? 'warning' : 'success'
      const message = response.blocked_by_override.length
        ? t('settings.blockedOverrideTitle')
        : response.restart_required.length
          ? t('settings.pendingRestartTitle')
          : response.applied_now.length
            ? t('settings.appliedNowTitle')
            : t('settings.noChanges')
      toast.show({ tone, message })
    },
    onError: error => {
      setInlineError(mutationErrorMessage(error, t('settings.saveError')))
      setLastResult(null)
    },
  })

  const isolatedSection = (children: ReactNode) => (
    <AdminPage description={t('settings.subtitle')}>
      <Tabs value={activeTab} onValueChange={value => setActiveTab(value as TabKey)} orientation={desktopTabs ? 'vertical' : 'horizontal'} className="data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:items-start data-[orientation=vertical]:gap-6">
        <TabsList aria-label={t('settings.tabsLabel')} activateOnFocus className="data-[orientation=vertical]:w-full">
          {(Object.keys(tabLabels) as TabKey[]).map(key => <TabsTrigger key={key} value={key}><Icon name={tabIcons[key]} size="sm" />{t(tabLabels[key])}{dirty && key !== 'webhooks' && key !== 'storage' ? ' ·' : ''}</TabsTrigger>)}
        </TabsList>
        <TabsContent value={activeTab}>{children}</TabsContent>
      </Tabs>
    </AdminPage>
  )
  // Notification settings are database-owned and stay usable when config.toml cannot be read.
  if (activeTab === 'webhooks') return isolatedSection(<>
    {!canWrite && <Alert variant="warning" className="mb-4"><AlertDescription>{t('settings.readOnlyPrincipal')}</AlertDescription></Alert>}
    {dirty && <p className="mb-4 text-[13px] text-[var(--text-soft)]">{t('settings.draftRetained')}</p>}
    <WebhookTab />
  </>)

  if (settingsQuery.isPending) {
    return (
      <>{isolatedSection(
        <Skeleton role="status" aria-hidden={false} aria-busy="true" className="h-40">
          <span className="sr-only">{t('loading')}</span>
        </Skeleton>
      )}</>
    )
  }

  if (settingsQuery.isError && !settingsQuery.data) {
    return (
      <>{isolatedSection(
        <QueryErrorState message={t('settings.loadError')} onRetry={() => { void settingsQuery.refetch() }} />
      )}</>
    )
  }

  if (!settingsQuery.data || !draft) {
    return (
      <>{isolatedSection(
        <Skeleton role="status" aria-hidden={false} aria-busy="true" className="h-40">
          <span className="sr-only">{t('loading')}</span>
        </Skeleton>
      )}</>
    )
  }

  const data = settingsQuery.data
  const globallyReadOnly = !canWrite || !data.config_writable
  const isDisabled = (path: EditableSettingPath) => (
    globallyReadOnly || updateMutation.isPending || !data.editable.includes(path)
  )
  const fieldLabel = (path: SettingPath) => t(`settings.fields.${path}`)
  const formatFieldList = (paths: readonly SettingPath[]) => new Intl.ListFormat(
    i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en-US',
    { style: 'long', type: 'conjunction' },
  ).format(paths.map(fieldLabel))
  const sourceLabel = (path: SettingPath) => t(`settings.source${data.sources[path][0].toUpperCase()}${data.sources[path].slice(1)}`)
  const fieldHint = (path: SettingPath, extra?: string) => {
    const parts = [sourceLabel(path)]
    const variable = data.overrides[path]
    if (variable) parts.push(t('settings.envOverride', { variable }))
    const configuredValue = valueAt(data.configured, path)
    const effectiveValue = valueAt(data.effective, path)
    if (configuredValue !== effectiveValue) {
      parts.push(t('settings.configuredValue', { value: configuredValue }))
      parts.push(t('settings.effectiveValue', { value: effectiveValue }))
    }
    if (extra) parts.push(extra)
    return parts.join(' · ')
  }
  const resultList = (title: string, paths: EditableSettingPath[], tone: 'success' | 'warning') => paths.length ? (
    <Alert variant={tone}><AlertTitle>{title}</AlertTitle><AlertDescription>
      {formatFieldList(paths)}
    </AlertDescription></Alert>
  ) : null
  const section = (title: string, children: ReactNode) => (
    <section className="min-w-0 max-w-[720px] [&_input]:h-10 [&_select]:h-10">
      <SectionHeader title={title} />
      {children}
    </section>
  )
  const fields = (children: ReactNode) => (
    <div className="divide-y divide-[var(--border-soft)] [&>div]:grid [&>div]:gap-x-6 [&>div]:py-5 [&>div:first-child]:pt-0 [&_label]:mb-2 sm:[&>div]:grid-cols-[160px_minmax(0,1fr)] sm:[&_label]:mb-0 sm:[&_label]:pt-2.5 sm:[&_p]:col-start-2">
      {children}
    </div>
  )
  const updateDraft = <K extends keyof SettingsDraft>(key: K, value: SettingsDraft[K]) => {
    setDraft(current => current ? { ...current, [key]: value } : current)
    setValidationErrors(current => {
      if (!current[key as ValidatedDraftKey]) return current
      const next = { ...current }
      delete next[key as ValidatedDraftKey]
      return next
    })
    setInlineError(null)
    setLastResult(null)
  }
  const save = () => {
    if (globallyReadOnly || updateMutation.isPending) return
    const errors = validateDraft(draft)
    setValidationErrors(errors)
    const firstInvalid = validationOrder.find(key => errors[key])
    if (firstInvalid) {
      setReviewOpen(false)
      setInlineError(null)
      setLastResult(null)
      const location = validationLocations[firstInvalid]
      setActiveTab(location.tab)
      window.requestAnimationFrame(() => document.getElementById(location.id)?.focus())
      return
    }
    const request = buildPatch(draft, data.configured)
    if (!request) {
      setValidationErrors({})
      setInlineError(null)
      setLastResult(null)
      toast.show({ tone: 'success', message: t('settings.noChanges') })
      return
    }
    updateMutation.mutate(request)
  }
  const settingsForm = (tab: Exclude<TabKey, 'webhooks'>, children: ReactNode) => (
    <form
      id={`settings-form-${tab}`}
      noValidate
      onSubmit={event => {
        event.preventDefault()
        save()
      }}
    >
      {children}
    </form>
  )
  const fieldError = (key: ValidatedDraftKey) => {
    const message = validationErrors[key]
    return message ? t(message) : undefined
  }

  const tabs = [
    {
      key: 'basic',
      label: t('settings.basic'),
      icon: <Icon name="tune" size="sm" />,
      content: settingsForm('basic', section(t('settings.basic'), fields(<>
        <Field>
          <FieldLabel>{fieldLabel('server.host')}</FieldLabel>
          <Input value={data.configured.server.host} readOnly />
          {(fieldHint('server.host')) && <FieldDescription>{fieldHint('server.host')}</FieldDescription>}
        </Field>
        <Field>
          <FieldLabel>{fieldLabel('server.port')}</FieldLabel>
          <Input value={String(data.configured.server.port)} readOnly />
          {(fieldHint('server.port')) && <FieldDescription>{fieldHint('server.port')}</FieldDescription>}
        </Field>
        <Field disabled={isDisabled('server.log_level')}>
          <FieldLabel>{fieldLabel('server.log_level')}</FieldLabel>
          <NativeSelect
            value={draft.logLevel}
            onChange={event => updateDraft('logLevel', event.target.value as SettingsDraft['logLevel'])}
            disabled={isDisabled('server.log_level')}
          >
            <option value="debug">debug</option>
            <option value="info">info</option>
            <option value="warn">warn</option>
            <option value="error">error</option>
          </NativeSelect>
          {(fieldHint('server.log_level')) && <FieldDescription>{fieldHint('server.log_level')}</FieldDescription>}
        </Field>
      </>))),
    },
    {
      key: 'cache',
      label: t('settings.cachePolicy'),
      icon: <Icon name="cached" size="sm" />,
      content: settingsForm('cache', section(t('settings.cachePolicy'), fields(<>
        <Field invalid={Boolean(fieldError('maxSizeGB'))} disabled={isDisabled('cache.max_size_gb')}>
          <FieldLabel>{fieldLabel('cache.max_size_gb')}</FieldLabel>
          <Input aria-invalid={Boolean(fieldError('maxSizeGB')) || undefined} id="setting-cache-max-size" type="number" min={1} step={1} required value={draft.maxSizeGB} onChange={event => updateDraft('maxSizeGB', event.target.value)} disabled={isDisabled('cache.max_size_gb')} />
          {(fieldError('maxSizeGB')) ? <FieldError match>{fieldError('maxSizeGB')}</FieldError> : (fieldHint('cache.max_size_gb')) ? <FieldDescription>{fieldHint('cache.max_size_gb')}</FieldDescription> : null}
        </Field>
        <Field invalid={Boolean(fieldError('lruThreshold'))} disabled={isDisabled('cache.lru_threshold')}>
          <FieldLabel>{fieldLabel('cache.lru_threshold')}</FieldLabel>
          <Input aria-invalid={Boolean(fieldError('lruThreshold')) || undefined} id="setting-cache-lru-threshold" type="number" min={1} max={100} step={1} required value={draft.lruThreshold} onChange={event => updateDraft('lruThreshold', event.target.value)} disabled={isDisabled('cache.lru_threshold')} />
          {(fieldError('lruThreshold')) ? <FieldError match>{fieldError('lruThreshold')}</FieldError> : (fieldHint('cache.lru_threshold')) ? <FieldDescription>{fieldHint('cache.lru_threshold')}</FieldDescription> : null}
        </Field>
        <Field invalid={Boolean(fieldError('ttlIndex'))} disabled={isDisabled('cache.ttl_index')}>
          <FieldLabel>{fieldLabel('cache.ttl_index')}</FieldLabel>
          <Input aria-invalid={Boolean(fieldError('ttlIndex')) || undefined} className="font-mono" id="setting-cache-ttl-index" required value={draft.ttlIndex} onChange={event => updateDraft('ttlIndex', event.target.value)} disabled={isDisabled('cache.ttl_index')} />
          {(fieldError('ttlIndex')) ? <FieldError match>{fieldError('ttlIndex')}</FieldError> : (fieldHint('cache.ttl_index', t('settings.durationHint'))) ? <FieldDescription>{fieldHint('cache.ttl_index', t('settings.durationHint'))}</FieldDescription> : null}
        </Field>
        <Field invalid={Boolean(fieldError('ttlBlob'))} disabled={isDisabled('cache.ttl_blob')}>
          <FieldLabel>{fieldLabel('cache.ttl_blob')}</FieldLabel>
          <Input aria-invalid={Boolean(fieldError('ttlBlob')) || undefined} className="font-mono" id="setting-cache-ttl-blob" required value={draft.ttlBlob} onChange={event => updateDraft('ttlBlob', event.target.value)} disabled={isDisabled('cache.ttl_blob')} />
          {(fieldError('ttlBlob')) ? <FieldError match>{fieldError('ttlBlob')}</FieldError> : (fieldHint('cache.ttl_blob', t('settings.durationHint'))) ? <FieldDescription>{fieldHint('cache.ttl_blob', t('settings.durationHint'))}</FieldDescription> : null}
        </Field>
      </>))),
    },
    {
      key: 'storage',
      label: t('settings.storageBackend'),
      icon: <Icon name="database" size="sm" />,
      content: settingsForm('storage', section(t('settings.storageBackend'), fields(<>
        <Field>
          <FieldLabel>{fieldLabel('storage.type')}</FieldLabel>
          <Input value={data.configured.storage.type} readOnly />
          {(fieldHint('storage.type')) && <FieldDescription>{fieldHint('storage.type')}</FieldDescription>}
        </Field>
        <Field>
          <FieldLabel>{fieldLabel('storage.path')}</FieldLabel>
          <Input className="font-mono" value={data.configured.storage.path} readOnly />
          {(fieldHint('storage.path')) && <FieldDescription>{fieldHint('storage.path')}</FieldDescription>}
        </Field>
        <Field>
          <FieldLabel>{fieldLabel('database.driver')}</FieldLabel>
          <Input value={data.configured.database.driver} readOnly />
          {(fieldHint('database.driver')) && <FieldDescription>{fieldHint('database.driver')}</FieldDescription>}
        </Field>
      </>))),
    },
    {
      key: 'auth',
      label: t('settings.authSecurity'),
      icon: <Icon name="shield" size="sm" />,
      content: settingsForm('auth', section(t('settings.authSecurity'), fields(
        <Field invalid={Boolean(fieldError('tokenTTL'))} disabled={isDisabled('auth.token_ttl')}>
          <FieldLabel>{fieldLabel('auth.token_ttl')}</FieldLabel>
          <Input aria-invalid={Boolean(fieldError('tokenTTL')) || undefined} className="font-mono" id="setting-auth-token-ttl" required value={draft.tokenTTL} onChange={event => updateDraft('tokenTTL', event.target.value)} disabled={isDisabled('auth.token_ttl')} />
          {(fieldError('tokenTTL')) ? <FieldError match>{fieldError('tokenTTL')}</FieldError> : (fieldHint('auth.token_ttl', t('settings.sessionHint'))) ? <FieldDescription>{fieldHint('auth.token_ttl', t('settings.sessionHint'))}</FieldDescription> : null}
        </Field>
      ))),
    },
    {
      key: 'webhooks',
      label: t('settings.webhooks'),
      icon: <Icon name="notifications" size="sm" />,
      content: <div className="min-w-0 pt-5 md:pt-0"><WebhookTab /></div>,
    },
  ]

  return (
    <AdminPage
      description={t('settings.subtitle')}
      actions={(
        <Button type="submit" form={`settings-form-${activeTab}`} size="sm" aria-busy={updateMutation.isPending || undefined} disabled={globallyReadOnly || updateMutation.isPending}>
          <Icon name="save" size="sm" />
          {updateMutation.isPending ? t('saving') : t('settings.saveAll')}
        </Button>
      )}
    >
    <div className="min-w-0 space-y-4">
      {dirty && <div role="status" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-[var(--border)] bg-[var(--bg-soft)] p-4">
        <div><p className="text-[14px] font-medium">{t('settings.dirtyCount', { count: changedFields.length })}</p><p className="mt-1 text-[13px] text-[var(--text-soft)]">{t(!draftStored ? 'settings.draftNotStored' : restored ? 'settings.draftRestored' : 'settings.draftRetained')}</p></div>
        <div className="flex gap-2"><Button variant="outline" onClick={() => setReviewOpen(true)}>{t('settings.reviewChanges')}</Button><Button variant="ghost" disabled={updateMutation.isPending} onClick={() => { setDraft(draftFrom(data.configured)); setValidationErrors({}); setInlineError(null); setLastResult(null); setRestored(false) }}>{t('settings.discardChanges')}</Button></div>
      </div>}
      {settingsQuery.isError && settingsQuery.data && (
        <StaleDataNotice
          message={`${t('settings.stale')} ${mutationErrorMessage(settingsQuery.error, t('settings.stale'))}`}
          refreshing={settingsQuery.isFetching}
          onRefresh={() => settingsQuery.refetch()}
        />
      )}
      {inlineError && !reviewOpen && <Alert variant={"destructive"}><AlertTitle>{t('settings.saveError')}</AlertTitle><AlertDescription>{inlineError}</AlertDescription></Alert>}
      {lastResult && (
        <div className="space-y-2">
          {resultList(t('settings.appliedNowTitle'), lastResult.applied_now, 'success')}
          {resultList(t('settings.blockedOverrideTitle'), lastResult.blocked_by_override, 'warning')}
        </div>
      )}
      {data.pending_restart.length > 0 && (
        <Alert variant={"warning"}><AlertTitle>{lastResult?.restart_required.length ? t('settings.restartRequiredTitle') : t('settings.pendingRestartTitle')}</AlertTitle><AlertDescription>
          {t('settings.pendingRestartField', { fields: formatFieldList(data.pending_restart) })}
        </AlertDescription></Alert>
      )}
      {!canWrite && <Alert variant={"warning"}><AlertDescription>{t('settings.readOnlyPrincipal')}</AlertDescription></Alert>}
      {!data.config_writable && (
        <Alert variant={"warning"}><AlertTitle>{t('settings.configReadOnlyTitle')}</AlertTitle><AlertDescription>
          {t('settings.configReadOnlyBody')}
        </AlertDescription></Alert>
      )}
      {(
        <div className="flex min-w-0 items-start gap-2 border-b border-[var(--border)] pb-3 text-[12px] text-[var(--text-soft)]">
          <Icon name="info" size="sm" className="mt-0.5 shrink-0" />
          <span>{t('settings.hotReloadNote')}</span>
        </div>
      )}
        <Tabs value={activeTab} onValueChange={value => setActiveTab(value as TabKey)} orientation={desktopTabs ? 'vertical' : 'horizontal'} className="data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:items-start data-[orientation=vertical]:gap-6">
          <TabsList aria-label={t('settings.tabsLabel')} activateOnFocus variant="default" className="data-[orientation=vertical]:w-full">
            {tabs.map(item => <TabsTrigger key={item.key} value={item.key}>{item.icon}{item.label}{changedFields.some(key => (key === 'logLevel' ? 'basic' : validationLocations[key].tab) === item.key) ? ' ·' : ''}</TabsTrigger>)}
          </TabsList>
          <div className="min-w-0">
            {tabs.map(item => <TabsContent key={item.key} value={item.key}>{item.content}</TabsContent>)}
          </div>
        </Tabs>
      <Dialog open={reviewOpen} onOpenChange={setReviewOpen}>
        <DialogContent className="max-w-xl"><DialogTitle>{t('settings.reviewChanges')}</DialogTitle>
          <p className="text-[13px] text-[var(--text-soft)]">{t('settings.reviewHint')}</p>
          {inlineError && <Alert variant="destructive"><AlertDescription>{inlineError}</AlertDescription></Alert>}
          <dl className="divide-y divide-[var(--border)]">{changedFields.map(key => <div key={key} className="py-3"><dt className="text-[14px] font-medium">{fieldLabel(draftPaths[key])}</dt><dd className="mt-1 break-all font-mono text-[13px]">{baseDraft?.[key]} → {draft[key]}</dd><dd className="mt-1 text-[12px] text-[var(--text-soft)]">{data.overrides[draftPaths[key]] ? t('settings.envOverride', { variable: data.overrides[draftPaths[key]] }) : key === 'logLevel' ? t('settings.appliedNowTitle') : t('settings.requiresRestart')}</dd></div>)}</dl>
          <div className="flex justify-end gap-2"><Button variant="outline" onClick={() => setReviewOpen(false)}>{t('common.close')}</Button><Button disabled={globallyReadOnly || updateMutation.isPending} onClick={save}>{t('settings.saveAll')}</Button></div>
        </DialogContent>
      </Dialog>
    </div>
    </AdminPage>
  )
}
