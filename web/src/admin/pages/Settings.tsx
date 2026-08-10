import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import WebhookTab from '@/admin/components/WebhookTab'
import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import SelectV2 from '@/components/Select'
import TabsV2 from '@/components/Tabs'
import { useAppToast } from '@/components/Toast'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'
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

export default function SettingsV2() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const desktopTabs = useMediaQuery('(min-width: 768px)')
  const { canWrite } = usePrincipal()
  const [activeTab, setActiveTab] = useState<TabKey>('basic')
  const [draft, setDraft] = useState<SettingsDraft | null>(null)
  const [validationErrors, setValidationErrors] = useState<SettingsValidationErrors>({})
  const [inlineError, setInlineError] = useState<string | null>(null)
  const [lastResult, setLastResult] = useState<UpdateAdminSettingsResponse | null>(null)
  const configuredRef = useRef<AdminSettingsSnapshot | null>(null)

  const settingsQuery = useQuery<AdminSettingsResponse>({
    queryKey: ['admin', 'settings'],
    queryFn: async ({ signal }) => (await adminApi.getSettings({ signal })).data,
  })

  useEffect(() => {
    if (!settingsQuery.data) return
    const previous = configuredRef.current
    const next = settingsQuery.data.configured
    setDraft(current => {
      return current && previous
        ? rebaseDraft(current, previous, next)
        : draftFrom(next)
    })
    configuredRef.current = next
  }, [settingsQuery.data])

  const updateMutation = useMutation({
    mutationFn: async (request: UpdateAdminSettingsRequest) => (await adminApi.updateSettings(request)).data,
    onSuccess: response => {
      queryClient.setQueryData<AdminSettingsResponse>(['admin', 'settings'], response)
      configuredRef.current = response.configured
      setDraft(draftFrom(response.configured))
      setValidationErrors({})
      setInlineError(null)
      setLastResult(response)
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

  if (settingsQuery.isPending) {
    return (
      <AdminPage description={t('settings.subtitle')}>
        <div role="status" aria-busy="true" className="h-40 animate-pulse rounded-[6px] bg-[var(--bg-soft)]">
          <span className="sr-only">{t('loading')}</span>
        </div>
      </AdminPage>
    )
  }

  if (settingsQuery.isError && !settingsQuery.data) {
    return (
      <AdminPage description={t('settings.subtitle')}>
        <QueryErrorState message={t('settings.loadError')} onRetry={() => { void settingsQuery.refetch() }} />
      </AdminPage>
    )
  }

  if (!settingsQuery.data || !draft) {
    return (
      <AdminPage description={t('settings.subtitle')}>
        <div role="status" aria-busy="true" className="h-40 animate-pulse rounded-[6px] bg-[var(--bg-soft)]">
          <span className="sr-only">{t('loading')}</span>
        </div>
      </AdminPage>
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
    <InlineNotice tone={tone} title={title}>
      {formatFieldList(paths)}
    </InlineNotice>
  ) : null
  const section = (title: string, children: ReactNode) => (
    <section className="min-w-0 pt-5 md:pt-0">
      <SectionHeader title={title} />
      {children}
    </section>
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
    const errors = validateDraft(draft)
    setValidationErrors(errors)
    const firstInvalid = validationOrder.find(key => errors[key])
    if (firstInvalid) {
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
      content: settingsForm('basic', section(t('settings.basic'), (
        <div className="space-y-5">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <InputV2 label={fieldLabel('server.host')} value={data.configured.server.host} readOnly hint={fieldHint('server.host')} />
            <InputV2 label={fieldLabel('server.port')} value={String(data.configured.server.port)} readOnly hint={fieldHint('server.port')} />
          </div>
          <SelectV2
            label={fieldLabel('server.log_level')}
            value={draft.logLevel}
            onChange={event => updateDraft('logLevel', event.target.value as SettingsDraft['logLevel'])}
            disabled={isDisabled('server.log_level')}
            hint={fieldHint('server.log_level')}
          >
            <option value="debug">debug</option>
            <option value="info">info</option>
            <option value="warn">warn</option>
            <option value="error">error</option>
          </SelectV2>
        </div>
      ))),
    },
    {
      key: 'cache',
      label: t('settings.cachePolicy'),
      icon: <Icon name="cached" size="sm" />,
      content: settingsForm('cache', section(t('settings.cachePolicy'), (
        <div className="space-y-5">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <InputV2 id="setting-cache-max-size" label={fieldLabel('cache.max_size_gb')} type="number" min={1} step={1} required value={draft.maxSizeGB} onChange={event => updateDraft('maxSizeGB', event.target.value)} disabled={isDisabled('cache.max_size_gb')} hint={fieldHint('cache.max_size_gb')} error={fieldError('maxSizeGB')} />
            <InputV2 id="setting-cache-lru-threshold" label={fieldLabel('cache.lru_threshold')} type="number" min={1} max={100} step={1} required value={draft.lruThreshold} onChange={event => updateDraft('lruThreshold', event.target.value)} disabled={isDisabled('cache.lru_threshold')} hint={fieldHint('cache.lru_threshold')} error={fieldError('lruThreshold')} />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <InputV2 id="setting-cache-ttl-index" label={fieldLabel('cache.ttl_index')} mono required value={draft.ttlIndex} onChange={event => updateDraft('ttlIndex', event.target.value)} disabled={isDisabled('cache.ttl_index')} hint={fieldHint('cache.ttl_index', t('settings.durationHint'))} error={fieldError('ttlIndex')} />
            <InputV2 id="setting-cache-ttl-blob" label={fieldLabel('cache.ttl_blob')} mono required value={draft.ttlBlob} onChange={event => updateDraft('ttlBlob', event.target.value)} disabled={isDisabled('cache.ttl_blob')} hint={fieldHint('cache.ttl_blob', t('settings.durationHint'))} error={fieldError('ttlBlob')} />
          </div>
        </div>
      ))),
    },
    {
      key: 'storage',
      label: t('settings.storageBackend'),
      icon: <Icon name="database" size="sm" />,
      content: settingsForm('storage', section(t('settings.storageBackend'), (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <InputV2 label={fieldLabel('storage.type')} value={data.configured.storage.type} readOnly hint={fieldHint('storage.type')} />
          <InputV2 label={fieldLabel('storage.path')} value={data.configured.storage.path} readOnly mono hint={fieldHint('storage.path')} />
          <InputV2 label={fieldLabel('database.driver')} value={data.configured.database.driver} readOnly hint={fieldHint('database.driver')} />
        </div>
      ))),
    },
    {
      key: 'auth',
      label: t('settings.authSecurity'),
      icon: <Icon name="shield" size="sm" />,
      content: settingsForm('auth', section(t('settings.authSecurity'), (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <InputV2 id="setting-auth-token-ttl" label={fieldLabel('auth.token_ttl')} mono required value={draft.tokenTTL} onChange={event => updateDraft('tokenTTL', event.target.value)} disabled={isDisabled('auth.token_ttl')} hint={fieldHint('auth.token_ttl', t('settings.durationHint'))} error={fieldError('tokenTTL')} />
        </div>
      ))),
    },
    {
      key: 'webhooks',
      label: t('settings.webhooks'),
      icon: <Icon name="notifications" size="sm" />,
      content: <div className="min-w-0 pt-5 md:pt-0"><WebhookTab /></div>,
    },
  ]

  const configurationTab = activeTab !== 'webhooks'

  return (
    <AdminPage
      description={t('settings.subtitle')}
      actions={configurationTab ? (
        <ButtonV2 type="submit" form={`settings-form-${activeTab}`} size="sm" aria-busy={updateMutation.isPending || undefined} disabled={globallyReadOnly || updateMutation.isPending}>
          <Icon name="save" size="sm" />
          {updateMutation.isPending ? t('saving') : t('save')}
        </ButtonV2>
      ) : undefined}
    >
    <div className="min-w-0 space-y-4">
      {settingsQuery.isError && settingsQuery.data && (
        <StaleDataNotice
          message={`${t('settings.stale')} ${mutationErrorMessage(settingsQuery.error, t('settings.stale'))}`}
          refreshing={settingsQuery.isFetching}
          onRefresh={() => settingsQuery.refetch()}
        />
      )}
      {configurationTab && inlineError && <InlineNotice tone="danger" title={t('settings.saveError')}>{inlineError}</InlineNotice>}
      {configurationTab && lastResult && (
        <div className="space-y-2">
          {resultList(t('settings.appliedNowTitle'), lastResult.applied_now, 'success')}
          {resultList(t('settings.blockedOverrideTitle'), lastResult.blocked_by_override, 'warning')}
        </div>
      )}
      {data.pending_restart.length > 0 && (
        <InlineNotice
          tone="warning"
          title={lastResult?.restart_required.length ? t('settings.restartRequiredTitle') : t('settings.pendingRestartTitle')}
        >
          {t('settings.pendingRestartField', { fields: formatFieldList(data.pending_restart) })}
        </InlineNotice>
      )}
      {!canWrite && <InlineNotice tone="warning">{t('settings.readOnlyPrincipal')}</InlineNotice>}
      {configurationTab && !data.config_writable && (
        <InlineNotice tone="warning" title={t('settings.configReadOnlyTitle')}>
          {t('settings.configReadOnlyBody')}
        </InlineNotice>
      )}
      {configurationTab && (
        <div className="flex min-w-0 items-start gap-2 border-b border-[var(--border)] pb-3 text-[12px] text-[var(--text-soft)]">
          <Icon name="info" size="sm" className="mt-0.5 shrink-0" />
          <span>{t('settings.hotReloadNote')}</span>
        </div>
      )}
      <TabsV2
        items={tabs}
        value={activeTab}
        onValueChange={value => setActiveTab(value as TabKey)}
        ariaLabel={t('settings.tabsLabel')}
        orientation={desktopTabs ? 'vertical' : 'horizontal'}
      />
    </div>
    </AdminPage>
  )
}
