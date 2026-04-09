import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import CardV2 from '@/components/CardV2'
import InputV2 from '@/components/InputV2'
import SelectV2 from '@/components/SelectV2'
import ButtonV2 from '@/components/ButtonV2'
import Icon from '@/components/Icon'
import { useMutation, useQueryClient } from '@tanstack/react-query'

type TabKey = 'basic' | 'cache' | 'storage' | 'auth' | 'license'

function LicenseTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data } = useQuery({ queryKey: ['admin', 'license'], queryFn: () => adminApi.getLicense() })
  const revalidateMutation = useMutation({ mutationFn: () => adminApi.revalidateLicense(), onSuccess: () => { setTimeout(() => queryClient.invalidateQueries({ queryKey: ['admin', 'license'] }), 2000) } })
  const license = data?.data; const isPro = license?.is_pro

  if (isPro) {
    return (
      <CardV2>
        <div className="flex items-center gap-3 mb-6">
          <span className="flex items-center justify-center w-10 h-10 rounded-[8px]" style={{ background: 'rgba(21,190,83,0.15)' }}><Icon name="verified" size="sm" className="text-success" /></span>
          <div><p className="font-[400]" style={{ color: 'var(--heading)' }}>{t('settings.licenseProActive')}</p><p className="text-[12px]" style={{ color: 'var(--body)' }}>{t('settings.licenseProDesc')}</p></div>
        </div>
        <div className="space-y-3">
          {[
            { label: t('settings.licenseKey'), value: license.key_masked || '—' },
            ...(license.activated_at ? [{ label: t('settings.licenseActivated'), value: new Date(license.activated_at).toLocaleDateString() }] : []),
            { label: t('settings.licenseExpires'), value: license.expires_at ? new Date(license.expires_at).toLocaleDateString() : t('users.neverExpires') },
            { label: t('settings.licenseLastChecked'), value: license.last_checked ? new Date(license.last_checked).toLocaleString() : '—' },
          ].map(item => (
            <div key={item.label} className="flex items-center justify-between py-2" style={{ borderBottom: '1px solid var(--border)' }}>
              <span className="text-[14px]" style={{ color: 'var(--body)' }}>{item.label}</span>
              <span className="text-[14px] font-mono" style={{ color: 'var(--heading)' }}>{item.value}</span>
            </div>
          ))}
        </div>
        <div className="mt-6"><ButtonV2 variant="secondary" onClick={() => revalidateMutation.mutate()} disabled={revalidateMutation.isPending}><Icon name="refresh" size="sm" />{revalidateMutation.isPending ? t('settings.licenseRevalidating') : t('settings.licenseRevalidate')}</ButtonV2></div>
      </CardV2>
    )
  }

  return (
    <CardV2>
      <div className="text-center py-4">
        <h3 className="text-[18px] font-[300]" style={{ color: 'var(--heading)' }}>{t('settings.licenseCommunityTitle')}</h3>
        <p className="text-[14px] mt-1 mb-6" style={{ color: 'var(--body)' }}>{t('settings.licenseCommunityDesc')}</p>
        <div className="text-left max-w-sm mx-auto mb-8 space-y-2">
          {[
            { ok: true, label: t('settings.licenseFeature12eco') },
            { ok: true, label: t('settings.licenseFeatureWebUI') },
            { ok: false, label: t('settings.licenseFeatureAudit') },
            { ok: false, label: t('settings.licenseFeatureRules') },
            { ok: false, label: t('settings.licenseFeatureS3') },
            { ok: false, label: t('settings.licenseFeaturePG') },
          ].map(f => (
            <div key={f.label} className="flex items-center gap-2 text-[14px]">
              <Icon name={f.ok ? 'check_circle' : 'cancel'} size="sm" style={{ color: f.ok ? 'var(--success)' : 'var(--body)' }} />
              <span style={{ color: f.ok ? 'var(--heading)' : 'var(--body)' }}>{f.label}</span>
            </div>
          ))}
        </div>
        <div className="flex flex-col items-center gap-2">
          <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer"><ButtonV2>{t('settings.licenseUpgrade')}</ButtonV2></a>
          <a href="https://depsilo.com/#pricing" target="_blank" rel="noopener noreferrer"><ButtonV2 variant="ghost" size="sm">{t('settings.licenseYearly')}</ButtonV2></a>
        </div>
      </div>
    </CardV2>
  )
}

export default function SettingsV2() {
  const { t } = useTranslation()
  const tabs = [
    { key: 'basic' as const, label: t('settings.basic'), icon: 'tune' },
    { key: 'cache' as const, label: t('settings.cachePolicy'), icon: 'cached' },
    { key: 'storage' as const, label: t('settings.storageBackend'), icon: 'database' },
    { key: 'auth' as const, label: t('settings.authSecurity'), icon: 'shield' },
    { key: 'license' as const, label: t('settings.license'), icon: 'license' },
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

  if (isLoading) return <div className="h-40 rounded-[5px] animate-pulse" style={{ background: 'var(--surface-low)' }} />

  return (
    <div className="space-y-6">
      {/* Vertical tabs layout */}
      <div className="flex gap-6">
        <div className="w-[200px] shrink-0 space-y-1">
          {tabs.map(tab => (
            <button key={tab.key} onClick={() => setActiveTab(tab.key)}
              className="flex items-center gap-2 w-full px-3 py-2 text-[14px] font-[400] rounded-[4px] transition-colors duration-150 cursor-pointer bg-transparent text-left"
              style={{ color: activeTab === tab.key ? 'var(--heading)' : 'var(--body)', background: activeTab === tab.key ? 'rgba(83,58,253,0.06)' : 'transparent', borderLeft: activeTab === tab.key ? '2px solid var(--stripe-purple)' : '2px solid transparent' }}
            ><Icon name={tab.icon} size="sm" />{tab.label}</button>
          ))}
        </div>
        <div className="flex-1">
          <div className="flex items-center gap-2 text-[12px] rounded-[4px] px-4 py-2.5 mb-4" style={{ background: 'var(--surface-low)', color: 'var(--body)', border: '1px solid var(--border)' }}>
            <Icon name="info" size="sm" />{t('settings.restartNote')}
          </div>

          {activeTab === 'basic' && (
            <CardV2>
              <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('settings.basic')}</h3>
              <div className="space-y-4">
                <div className="grid gap-4 grid-cols-2">
                  <InputV2 label={t('settings.listenAddr')} value={settings.host || '0.0.0.0'} disabled />
                  <InputV2 label={t('settings.listenPort')} type="number" value={settings.port || 8080} disabled />
                </div>
                <SelectV2 label={t('settings.logLevel')} value={settings.log_level || 'info'} disabled className="w-48"><option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></SelectV2>
                <div className="space-y-3">
                  <label className="flex items-center gap-3 opacity-70"><input type="checkbox" checked={settings.metrics_enabled ?? true} disabled className="h-4 w-4 rounded" style={{ accentColor: 'var(--stripe-purple)' }} /><span className="text-[14px]" style={{ color: 'var(--heading)' }}>{t('settings.enableMetrics')}</span></label>
                  <label className="flex items-center gap-3 opacity-70"><input type="checkbox" checked={settings.access_log_persist ?? true} disabled className="h-4 w-4 rounded" style={{ accentColor: 'var(--stripe-purple)' }} /><span className="text-[14px]" style={{ color: 'var(--heading)' }}>{t('settings.persistAccessLog')}</span></label>
                </div>
              </div>
            </CardV2>
          )}

          {activeTab === 'cache' && (
            <CardV2>
              <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('settings.cachePolicy')}</h3>
              <div className="space-y-4">
                <div className="grid gap-4 grid-cols-2"><InputV2 label={t('settings.maxCacheSize')} type="number" value={settings.max_size_gb || 20} disabled /><InputV2 label={t('settings.cleanThreshold')} type="number" value={settings.lru_threshold || 90} disabled /></div>
                <div className="grid gap-4 grid-cols-2"><InputV2 label={t('settings.indexTTL')} mono value={settings.ttl_index || '5m'} disabled /><InputV2 label={t('settings.fileTTL')} mono value={settings.ttl_blob || '72h'} disabled /></div>
                <label className="flex items-center gap-3 opacity-70"><input type="checkbox" checked={settings.lru_enabled ?? true} disabled className="h-4 w-4 rounded" style={{ accentColor: 'var(--stripe-purple)' }} /><span className="text-[14px]" style={{ color: 'var(--heading)' }}>{t('settings.lruAutoEvict')}</span></label>
              </div>
            </CardV2>
          )}

          {activeTab === 'storage' && (
            <CardV2>
              <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('settings.storageBackend')}</h3>
              <div className="space-y-4">
                <SelectV2 label={t('settings.storageType')} value={settings.storage_type || 'local'} disabled className="w-48"><option value="local">{t('settings.localStorage')}</option><option value="s3">{t('settings.s3Storage')}</option></SelectV2>
                {settings.storage_type === 's3' ? (
                  <div className="grid gap-4 grid-cols-2">
                    <InputV2 label="Endpoint" mono value={settings.s3_endpoint || ''} disabled />
                    <InputV2 label="Bucket" mono value={settings.s3_bucket || ''} disabled />
                    <InputV2 label="Access Key" value={settings.s3_access_key || ''} disabled />
                    <InputV2 label="Secret Key" type="password" value={settings.s3_secret_key || ''} disabled />
                    <InputV2 label="Region" value={settings.s3_region || ''} disabled />
                  </div>
                ) : <InputV2 label={t('settings.cacheDir')} mono value={settings.storage_path || './data/cache'} disabled />}
                <SelectV2 label={t('settings.dbType')} value={settings.db_driver || 'sqlite'} disabled className="w-48"><option value="sqlite">SQLite</option><option value="postgres">PostgreSQL</option></SelectV2>
                {settings.db_driver === 'postgres' && <InputV2 label="DSN" mono value={settings.db_dsn || ''} disabled />}
              </div>
            </CardV2>
          )}

          {activeTab === 'auth' && (
            <CardV2>
              <h3 className="text-[12px] uppercase tracking-wider font-[400] mb-4" style={{ color: 'var(--body)' }}>{t('settings.authSecurity')}</h3>
              <div className="space-y-4">
                <div className="space-y-3">
                  <label className="flex items-center gap-3 opacity-70"><input type="checkbox" checked={settings.auth_enabled ?? true} disabled className="h-4 w-4 rounded" style={{ accentColor: 'var(--stripe-purple)' }} /><span className="text-[14px]" style={{ color: 'var(--heading)' }}>{t('settings.enableAuth')}</span></label>
                  <label className="flex items-center gap-3 opacity-70"><input type="checkbox" checked={settings.anonymous_proxy ?? true} disabled className="h-4 w-4 rounded" style={{ accentColor: 'var(--stripe-purple)' }} /><span className="text-[14px]" style={{ color: 'var(--heading)' }}>{t('settings.anonymousProxy')}</span></label>
                </div>
                <div className="grid gap-4 grid-cols-2">
                  <InputV2 label="JWT Secret" type="password" value={settings.jwt_secret || ''} disabled />
                  <SelectV2 label={t('settings.tokenValidity')} value={settings.token_ttl || '168h'} disabled><option value="168h">{t('users.days7')}</option><option value="720h">{t('users.days30')}</option><option value="2160h">{t('users.days90')}</option><option value="never">{t('users.neverExpires')}</option></SelectV2>
                </div>
              </div>
            </CardV2>
          )}

          {activeTab === 'license' && <LicenseTab />}
        </div>
      </div>
    </div>
  )
}
