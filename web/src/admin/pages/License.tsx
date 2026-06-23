import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { licenseApi } from '@/lib/api'
import type { EntitlementStatus } from '@/lib/api'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import SectionHeader from '@/components/SectionHeader'

// State panel — soft tinted background, no border / shadow.
// Used to surface license/trial status with an icon + headline + body.
function StatePanel({
  tone,
  icon,
  title,
  description,
  children,
}: {
  tone: 'brand' | 'ok' | 'danger'
  icon: string
  title: React.ReactNode
  description?: React.ReactNode
  children?: React.ReactNode
}) {
  const tones = {
    brand: { bg: 'var(--brand-soft)', iconBg: 'var(--brand-soft)', iconColor: 'var(--brand-text)' },
    ok:    { bg: 'var(--ok-fill)',    iconBg: 'var(--ok-fill)',    iconColor: 'var(--ok-text)' },
    danger:{ bg: 'var(--danger-fill)',iconBg: 'var(--danger-fill)',iconColor: 'var(--danger-text)' },
  } as const
  const t = tones[tone]
  return (
    <div className="rounded-[6px] p-5" style={{ background: t.bg }}>
      <div className="flex items-center gap-3 mb-4">
        <span
          className="flex items-center justify-center w-10 h-10 rounded-[8px]"
          style={{ background: t.iconBg, color: t.iconColor }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <div>
          <p className="font-[500] text-[14px]" style={{ color: 'var(--text)' }}>{title}</p>
          {description && (
            <p className="text-[12px] mt-0.5" style={{ color: 'var(--text-soft)' }}>{description}</p>
          )}
        </div>
      </div>
      {children}
    </div>
  )
}

export default function License() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()

  const { data: statusData, isLoading } = useQuery({
    queryKey: ['license', 'status'],
    queryFn: async () => {
      const res = await licenseApi.status()
      return res.data as EntitlementStatus
    },
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
  })

  const status = statusData

  const activateTrial = useMutation({
    mutationFn: async () => {
      const res = await licenseApi.activateTrial()
      return res.data as EntitlementStatus
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
    },
  })

  const setKey = useMutation({
    mutationFn: async (key: string) => {
      const res = await licenseApi.setKey(key)
      return res.data as EntitlementStatus
    },
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
      if (s.source === 'paid') {
        setKeyInput('')
      }
    },
  })

  const clearKey = useMutation({
    mutationFn: async () => {
      const res = await licenseApi.clearKey()
      return res.data as EntitlementStatus
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
    },
  })

  const revalidate = useMutation({
    mutationFn: licenseApi.revalidate,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['license', 'status'] }),
  })

  const [keyInput, setKeyInput] = useState('')
  const [removeOpen, setRemoveOpen] = useState(false)
  const [keyExpanded, setKeyExpanded] = useState(false)

  if (isLoading || !status) {
    return (
      <div className="py-6 text-[14px]" style={{ color: 'var(--text-soft)' }}>
        {t('loading')}
      </div>
    )
  }

  const source = status.source
  const trialUsed = status.trial_used

  const formatDate = (iso?: string) =>
    iso ? new Date(iso).toLocaleDateString(i18n.language === 'zh' ? 'zh-CN' : 'en-US') : ''

  const formatRelative = (iso?: string) =>
    iso ? new Date(iso).toLocaleString(i18n.language === 'zh' ? 'zh-CN' : 'en-US') : ''

  return (
    <div className="space-y-10 max-w-3xl">
      {/* ── Page heading ───────────────────────────── */}
      <div>
        <h2 className="text-[20px] font-[600] tracking-[-0.02em]" style={{ color: 'var(--text)' }}>
          {t('license.title')}
        </h2>
        <p className="text-[13px] mt-1" style={{ color: 'var(--text-soft)' }}>
          {t('license.subtitle')}
        </p>
      </div>

      {/* ── State panel ────────────────────────────── */}
      {source === 'none' && !trialUsed && (
        <StatePanel
          tone="brand"
          icon="workspace_premium"
          title={t('license.trial.start_button')}
          description={t('license.trial.start_explainer')}
        >
          <div className="flex gap-2">
            <ButtonV2 onClick={() => activateTrial.mutate()} disabled={activateTrial.isPending}>
              {t('license.trial.start_button')}
            </ButtonV2>
            <ButtonV2 variant="secondary" onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}>
              {t('license.view_pricing')}
            </ButtonV2>
          </div>
        </StatePanel>
      )}

      {source === 'trial' && (
        <StatePanel
          tone="ok"
          icon="verified"
          title={`${t('license.status.pro')} · ${t('license.status.trial')}`}
          description={`${t('license.trial.days_left', { count: status.days_left })} · ${t('license.trial.expires_at', { date: formatDate(status.expires_at) })}`}
        >
          <ButtonV2 variant="secondary" onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}>
            {t('license.buy_pro')}
          </ButtonV2>
        </StatePanel>
      )}

      {source === 'none' && trialUsed && (
        <StatePanel
          tone="danger"
          icon="warning"
          title={t('license.trial.expired_message', { date: formatDate(status.expires_at) })}
        >
          <ButtonV2 onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}>
            {t('license.buy_pro')}
          </ButtonV2>
        </StatePanel>
      )}

      {source === 'paid' && (
        <StatePanel
          tone="ok"
          icon="verified"
          title={t('license.pro.activated')}
        >
          <div className="space-y-0">
            {[
              { label: t('license.pro.key_label'), value: <code className="font-mono text-[13px]">{status.license_key_masked}</code> },
              ...(status.expires_at
                ? [{ label: t('license.pro.expires_at', { date: '' }).replace('：', ':').split(':')[0], value: formatDate(status.expires_at) }]
                : []),
              { label: t('license.pro.last_checked', { relative_time: '' }).replace(/：.*$/, '').replace(/:.+$/, ''), value: formatRelative(status.last_checked) },
            ].map((item, i, arr) => (
              <div
                key={i}
                className="flex items-center justify-between py-2"
                style={{ borderBottom: i < arr.length - 1 ? '1px solid var(--border)' : 'none' }}
              >
                <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{item.label}</span>
                <span className="text-[13px]" style={{ color: 'var(--text)' }}>{item.value}</span>
              </div>
            ))}
          </div>
          <ButtonV2
            variant="secondary"
            onClick={() => revalidate.mutate()}
            disabled={revalidate.isPending}
            className="mt-4"
          >
            <Icon name="refresh" size="sm" />
            {t('license.revalidate')}
          </ButtonV2>
        </StatePanel>
      )}

      {/* ── License key entry (collapsible) ────────── */}
      <section>
        <button
          className="w-full flex items-center justify-between bg-transparent cursor-pointer pb-2"
          style={{ borderBottom: '1px solid var(--border)' }}
          onClick={() => setKeyExpanded(!keyExpanded)}
        >
          <span className="text-[13px] font-[600] tracking-[-0.005em]" style={{ color: 'var(--text)' }}>
            {t('license.key.title')}
          </span>
          <Icon name={keyExpanded ? 'expand_less' : 'expand_more'} size="sm" style={{ color: 'var(--text-soft)' }} />
        </button>

        {(keyExpanded || (source === 'none' && !trialUsed)) && (
          <div className="mt-4 space-y-3">
            {source !== 'paid' && (
              <>
                <div className="flex gap-2">
                  <InputV2
                    placeholder={t('license.key.placeholder')}
                    value={keyInput}
                    onChange={(e) => setKeyInput(e.target.value)}
                    disabled={setKey.isPending}
                    mono
                  />
                  <ButtonV2
                    onClick={() => setKey.mutate(keyInput)}
                    disabled={setKey.isPending || keyInput.trim() === ''}
                  >
                    {t('license.key.activate_button')}
                  </ButtonV2>
                </div>
                {setKey.data && setKey.data.source !== 'paid' && (
                  <div className="space-y-1">
                    <p className="text-[13px]" style={{ color: 'var(--warn-text)' }}>
                      {t('license.key.saved_pending_message')}
                    </p>
                    {setKey.data.license_error && (
                      <p className="text-[12px] opacity-70" style={{ color: 'var(--text-soft)' }}>
                        {setKey.data.license_error}
                      </p>
                    )}
                    <div className="flex gap-2">
                      <ButtonV2
                        variant="secondary"
                        size="sm"
                        onClick={() => revalidate.mutate()}
                        disabled={revalidate.isPending}
                      >
                        {t('license.key.try_revalidate')}
                      </ButtonV2>
                    </div>
                  </div>
                )}
              </>
            )}
            {status.license_key_masked && (
              <div className="flex gap-2">
                {source === 'paid' && (
                  <ButtonV2 variant="secondary" onClick={() => setKeyExpanded(true)}>
                    {t('license.key.change_button')}
                  </ButtonV2>
                )}
                <ButtonV2 variant="danger" onClick={() => setRemoveOpen(true)}>
                  {t('license.key.remove_button')}
                </ButtonV2>
              </div>
            )}
          </div>
        )}
      </section>

      {/* ── Feature comparison ─────────────────────── */}
      <section>
        <SectionHeader title={t('license.features.heading')} />
        <div className="grid grid-cols-2 gap-8">
          <div>
            <p className="text-[13px] font-[600] mb-3" style={{ color: 'var(--text)' }}>
              {t('license.status.free')}
            </p>
            <ul className="space-y-2">
              {[1, 2, 3, 4, 5, 6].map((i) => (
                <li key={i} className="flex items-center gap-2 text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  <Icon name="check_circle" size="sm" style={{ color: 'var(--text-subtle)' }} />
                  {t(`license.features.free.f${i}`)}
                </li>
              ))}
            </ul>
          </div>
          <div>
            <p className="text-[13px] font-[600] mb-3" style={{ color: 'var(--text)' }}>
              {t('license.status.pro')}
            </p>
            <ul className="space-y-2">
              {[1, 2, 3, 4, 5, 6].map((i) => (
                <li key={i} className="flex items-center gap-2 text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  <Icon name="star" size="sm" style={{ color: 'var(--brand)' }} />
                  {t(`license.features.pro.f${i}`)}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </section>

      {/* Remove key confirmation modal */}
      <ModalV2 open={removeOpen} onClose={() => setRemoveOpen(false)} title={t('license.key.remove_confirm_title')}>
        <p className="text-[14px] mb-5" style={{ color: 'var(--text-soft)' }}>
          {t('license.key.remove_confirm_body')}
        </p>
        <div className="flex justify-end gap-3">
          <ButtonV2 variant="secondary" onClick={() => setRemoveOpen(false)}>
            {t('license.paywall.dismiss')}
          </ButtonV2>
          <ButtonV2
            variant="danger"
            onClick={() => {
              clearKey.mutate()
              setRemoveOpen(false)
            }}
          >
            {t('license.key.remove_button')}
          </ButtonV2>
        </div>
      </ModalV2>
    </div>
  )
}
