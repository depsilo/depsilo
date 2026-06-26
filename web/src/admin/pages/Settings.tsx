import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import SectionHeader from '@/components/SectionHeader'
import { useQueryClient } from '@tanstack/react-query'
import WebhookTab from '@/admin/components/WebhookTab'

type TabKey = 'basic' | 'cache' | 'storage' | 'auth' | 'webhooks'

export default function SettingsV2() {
  const { t } = useTranslation()
  const tabs = [
    { key: 'basic' as const, label: t('settings.basic'), icon: 'tune' },
    { key: 'cache' as const, label: t('settings.cachePolicy'), icon: 'cached' },
    { key: 'storage' as const, label: t('settings.storageBackend'), icon: 'database' },
    { key: 'auth' as const, label: t('settings.authSecurity'), icon: 'shield' },
    { key: 'webhooks' as const, label: t('settings.webhooks'), icon: 'notifications' },
  ]
  const [activeTab, setActiveTab] = useState<TabKey>('basic')
  const [settings, setSettings] = useState<Record<string, any>>({})

  const { data, isLoading } = useQuery({ queryKey: ['admin', 'settings'], queryFn: () => adminApi.getSettings() })

  useEffect(() => {
    if (data?.data) {
      const flat: Record<string, any> = {}; const d = data.data
      if (d.server) { flat.host = d.server.host; flat.port = d.server.port; flat.log_level = d.server.log_level; flat.metrics_enabled = d.server.metrics_enabled; flat.access_log_persist = d.server.access_log_persist }
      if (d.cache) { flat.max_size_gb = d.cache.max_size_gb; flat.lru_threshold = d.cache.lru_threshold; flat.ttl_index = d.cache.ttl_index; flat.ttl_blob = d.cache.ttl_blob; flat.lru_enabled = d.cache.lru_enabled }
      if (d.storage) { flat.storage_type = d.storage.type; flat.storage_path = d.storage.path; flat.s3_endpoint = d.storage.endpoint; flat.s3_bucket = d.storage.bucket; flat.s3_access_key = d.storage.access_key; flat.s3_secret_key = d.storage.secret_key; flat.s3_region = d.storage.region }
      if (d.database) { flat.db_driver = d.database.driver; flat.db_dsn = d.database.dsn }
      if (d.auth) { flat.auth_enabled = d.auth.enabled; flat.anonymous_proxy = d.auth.anonymous_proxy; flat.jwt_secret = d.auth.jwt_secret; flat.token_ttl = d.auth.token_ttl }
      setSettings(flat)
    }
  }, [data])

  const queryClient = useQueryClient()
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)

  const updateField = (key: string, value: any) => {
    setSettings(prev => ({ ...prev, [key]: value }))
    setSaved(false)
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      await adminApi.updateSettings({
        cache: {
          max_size_gb: settings.max_size_gb,
          ttl_index: settings.ttl_index,
          ttl_blob: settings.ttl_blob,
          lru_threshold: settings.lru_threshold,
        },
        server: {
          log_level: settings.log_level,
        },
        auth: {
          enabled: settings.auth_enabled,
          token_ttl: settings.token_ttl,
        },
      })
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
      queryClient.invalidateQueries({ queryKey: ['admin', 'settings'] })
    } finally {
      setSaving(false)
    }
  }

  if (isLoading) return <div className="h-40 rounded animate-pulse" style={{ background: 'var(--bg-soft)' }} />

  const roLabel = (text: string) => `${text} (${t('settings.requiresRestart')})`

  const activeTabLabel = tabs.find(t => t.key === activeTab)?.label || ''

  return (
    <div className="flex gap-8">
      {/* ── Vertical tab nav ────────────────────────────────────── */}
      <nav className="w-[180px] shrink-0 space-y-0.5">
        {tabs.map(tab => {
          const active = activeTab === tab.key
          return (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className="flex items-center gap-2 w-full px-3 py-2 text-[13px] font-[500] rounded-[4px] transition-[background,color,transform] duration-150 active:scale-[0.96] cursor-pointer bg-transparent text-left"
              style={{
                color: active ? 'var(--text)' : 'var(--text-soft)',
                background: active ? 'var(--brand-soft)' : 'transparent',
              }}
            >
              <Icon name={tab.icon} size="sm" />
              {tab.label}
            </button>
          )
        })}
      </nav>

      {/* ── Tab content ────────────────────────────────────────── */}
      <div className="flex-1 min-w-0">
        {/* Save bar (only show for editable tabs) */}
        {activeTab !== 'webhooks' && (
          <div className="flex items-center justify-between mb-6 pb-3" style={{ borderBottom: '1px solid var(--border)' }}>
            <div className="flex items-center gap-2 text-[12px]" style={{ color: 'var(--text-soft)' }}>
              <Icon name="info" size="sm" />
              {t('settings.hotReloadNote')}
            </div>
            <ButtonV2 size="sm" onClick={handleSave} disabled={saving}>
              <Icon name={saved ? 'check' : 'save'} size="sm" />
              {saving ? t('saving') : saved ? t('settings.saved') : t('save')}
            </ButtonV2>
          </div>
        )}

        {activeTab === 'basic' && (
          <section>
            <SectionHeader title={activeTabLabel} />
            <div className="space-y-5">
              <div className="grid gap-4 grid-cols-2">
                <InputV2 label={roLabel(t('settings.listenAddr'))} value={settings.host || '0.0.0.0'} disabled />
                <InputV2 label={roLabel(t('settings.listenPort'))} type="number" value={settings.port || 8080} disabled />
              </div>
              <SelectV2 label={t('settings.logLevel')} value={settings.log_level || 'info'} onChange={(e) => updateField('log_level', e.target.value)} className="w-48">
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </SelectV2>
            </div>
          </section>
        )}

        {activeTab === 'cache' && (
          <section>
            <SectionHeader title={activeTabLabel} />
            <div className="space-y-5">
              <div className="grid gap-4 grid-cols-2">
                <InputV2 label={t('settings.maxCacheSize')} type="number" value={settings.max_size_gb || 20} onChange={(e) => updateField('max_size_gb', parseInt(e.target.value) || 0)} />
                <InputV2 label={t('settings.cleanThreshold')} type="number" value={settings.lru_threshold || 90} onChange={(e) => updateField('lru_threshold', parseInt(e.target.value) || 0)} />
              </div>
              <div className="grid gap-4 grid-cols-2">
                <InputV2 label={t('settings.indexTTL')} mono value={settings.ttl_index || '1h'} onChange={(e) => updateField('ttl_index', e.target.value)} />
                <InputV2 label={t('settings.fileTTL')} mono value={settings.ttl_blob || '72h'} onChange={(e) => updateField('ttl_blob', e.target.value)} />
              </div>
            </div>
          </section>
        )}

        {activeTab === 'storage' && (
          <section>
            <SectionHeader title={activeTabLabel} />
            <div className="space-y-5">
              <SelectV2 label={roLabel(t('settings.storageType'))} value={settings.storage_type || 'local'} disabled className="w-48">
                <option value="local">{t('settings.localStorage')}</option>
                <option value="s3">{t('settings.s3Storage')}</option>
              </SelectV2>
              {settings.storage_type === 's3' ? (
                <div className="grid gap-4 grid-cols-2">
                  <InputV2 label={roLabel('Endpoint')} mono value={settings.s3_endpoint || ''} disabled />
                  <InputV2 label={roLabel('Bucket')} mono value={settings.s3_bucket || ''} disabled />
                  <InputV2 label={roLabel('Access Key')} value={settings.s3_access_key || ''} disabled />
                  <InputV2 label={roLabel('Secret Key')} type="password" value={settings.s3_secret_key || ''} disabled />
                  <InputV2 label={roLabel('Region')} value={settings.s3_region || ''} disabled />
                </div>
              ) : (
                <InputV2 label={roLabel(t('settings.cacheDir'))} mono value={settings.storage_path || './data/cache'} disabled />
              )}
              <SelectV2 label={roLabel(t('settings.dbType'))} value={settings.db_driver || 'sqlite'} disabled className="w-48">
                <option value="sqlite">SQLite</option>
                <option value="postgres">PostgreSQL</option>
              </SelectV2>
              {settings.db_driver === 'postgres' && <InputV2 label={roLabel('DSN')} mono value={settings.db_dsn || ''} disabled />}
            </div>
          </section>
        )}

        {activeTab === 'auth' && (
          <section>
            <SectionHeader title={activeTabLabel} />
            <div className="space-y-5">
              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={settings.auth_enabled ?? true}
                  onChange={(e) => updateField('auth_enabled', e.target.checked)}
                  className="h-4 w-4 rounded"
                  style={{ accentColor: 'var(--brand)' }}
                />
                <span className="text-[13px]" style={{ color: 'var(--text)' }}>{t('settings.enableAuth')}</span>
              </label>
              <div className="grid gap-4 grid-cols-2">
                <InputV2 label={roLabel('JWT Secret')} type="password" value={settings.jwt_secret || ''} disabled />
                <SelectV2 label={t('settings.tokenValidity')} value={settings.token_ttl || '168h'} onChange={(e) => updateField('token_ttl', e.target.value)}>
                  <option value="168h">{t('users.days7')}</option>
                  <option value="720h">{t('users.days30')}</option>
                  <option value="2160h">{t('users.days90')}</option>
                  <option value="never">{t('users.neverExpires')}</option>
                </SelectV2>
              </div>
            </div>
          </section>
        )}

        {activeTab === 'webhooks' && <WebhookTab />}
      </div>
    </div>
  )
}
