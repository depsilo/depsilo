import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { licenseApi } from '@/lib/api'
import type { EntitlementStatus } from '@/lib/api'
import CardV2 from '@/components/Card'
import ButtonV2 from '@/components/Button'
import InputV2 from '@/components/Input'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'

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
    onError: () => {
      // no-op, error visible via UI state
    },
  })

  const setKey = useMutation({
    mutationFn: async (key: string) => {
      const res = await licenseApi.setKey(key)
      return res.data as EntitlementStatus
    },
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['license', 'status'] })
      // Clear input only when the key actually granted paid status. During a
      // trial, is_pro can be true via trial even if the saved key is invalid —
      // gate on `source === 'paid'` so the user keeps their input visible
      // alongside the "saved-pending" warning.
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
      <div className="p-6 text-[14px]" style={{ color: 'var(--text-soft)' }}>
        {t('loading')}
      </div>
    )
  }

  const source = status.source
  const trialUsed = status.trial_used

  const formatDate = (iso?: string) =>
    iso
      ? new Date(iso).toLocaleDateString(i18n.language === 'zh' ? 'zh-CN' : 'en-US')
      : ''

  const formatRelative = (iso?: string) =>
    iso
      ? new Date(iso).toLocaleString(i18n.language === 'zh' ? 'zh-CN' : 'en-US')
      : ''

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h2 className="text-[19px] font-[600] tracking-[-0.02em]" style={{ color: 'var(--text)' }}>
          {t('license.title')}
        </h2>
        <p className="text-[13px] mt-0.5" style={{ color: 'var(--text-soft)' }}>
          {t('license.subtitle')}
        </p>
      </div>

      {/* --- State card: no trial, trial not used --- */}
      {source === 'none' && !trialUsed && (
        <CardV2>
          <div className="flex items-center gap-3 mb-4">
            <span
              className="flex items-center justify-center w-10 h-10 rounded-[8px]"
              style={{ background: 'rgba(99,102,241,0.12)' }}
            >
              <Icon name="workspace_premium" size="sm" style={{ color: 'var(--brand)' }} />
            </span>
            <div>
              <p className="font-[400]" style={{ color: 'var(--text)' }}>
                {t('license.trial.start_button')}
              </p>
              <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
                {t('license.trial.start_explainer')}
              </p>
            </div>
          </div>
          <div className="flex gap-2">
            <ButtonV2
              onClick={() => activateTrial.mutate()}
              disabled={activateTrial.isPending}
            >
              {t('license.trial.start_button')}
            </ButtonV2>
            <ButtonV2
              variant="secondary"
              onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}
            >
              {t('license.view_pricing')}
            </ButtonV2>
          </div>
        </CardV2>
      )}

      {/* --- State card: trial active --- */}
      {source === 'trial' && (
        <CardV2>
          <div className="flex items-center gap-3 mb-4">
            <span
              className="flex items-center justify-center w-10 h-10 rounded-[8px]"
              style={{ background: 'rgba(21,190,83,0.15)' }}
            >
              <Icon name="verified" size="sm" style={{ color: 'var(--success, #15BE53)' }} />
            </span>
            <div>
              <p className="font-[400]" style={{ color: 'var(--text)' }}>
                {t('license.status.pro')} · {t('license.status.trial')}
              </p>
              <p className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
                {t('license.trial.days_left', { count: status.days_left })}
                {' · '}
                {t('license.trial.expires_at', { date: formatDate(status.expires_at) })}
              </p>
            </div>
          </div>
          <ButtonV2
            variant="secondary"
            onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}
          >
            {t('license.buy_pro')}
          </ButtonV2>
        </CardV2>
      )}

      {/* --- State card: trial expired --- */}
      {source === 'none' && trialUsed && (
        <CardV2>
          <div className="flex items-center gap-3 mb-4">
            <span
              className="flex items-center justify-center w-10 h-10 rounded-[8px]"
              style={{ background: 'rgba(239,68,68,0.12)' }}
            >
              <Icon name="warning" size="sm" style={{ color: 'var(--danger, #EF4444)' }} />
            </span>
            <div>
              <p className="font-[400]" style={{ color: 'var(--text)' }}>
                {t('license.trial.expired_message', { date: formatDate(status.expires_at) })}
              </p>
            </div>
          </div>
          <ButtonV2
            onClick={() => window.open('https://depsilo.com/#pricing', '_blank')}
          >
            {t('license.buy_pro')}
          </ButtonV2>
        </CardV2>
      )}

      {/* --- State card: paid pro active --- */}
      {source === 'paid' && (
        <CardV2>
          <div className="flex items-center gap-3 mb-4">
            <span
              className="flex items-center justify-center w-10 h-10 rounded-[8px]"
              style={{ background: 'rgba(21,190,83,0.15)' }}
            >
              <Icon name="verified" size="sm" style={{ color: 'var(--success, #15BE53)' }} />
            </span>
            <div>
              <p className="font-[400]" style={{ color: 'var(--text)' }}>
                {t('license.pro.activated')}
              </p>
            </div>
          </div>
          <div className="space-y-2 mb-4">
            {[
              { label: t('license.pro.key_label'), value: <code className="font-mono text-[13px]">{status.license_key_masked}</code> },
              ...(status.expires_at
                ? [{ label: t('license.pro.expires_at', { date: '' }).replace('：', ':').split(':')[0], value: formatDate(status.expires_at) }]
                : []),
              { label: t('license.pro.last_checked', { relative_time: '' }).replace(/：.*$/, '').replace(/:.+$/, ''), value: formatRelative(status.last_checked) },
            ].map((item, i) => (
              <div
                key={i}
                className="flex items-center justify-between py-2"
                style={{ borderBottom: '1px solid var(--border)' }}
              >
                <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  {item.label}
                </span>
                <span className="text-[13px]" style={{ color: 'var(--text)' }}>
                  {item.value}
                </span>
              </div>
            ))}
          </div>
          <ButtonV2
            variant="secondary"
            onClick={() => revalidate.mutate()}
            disabled={revalidate.isPending}
          >
            <Icon name="refresh" size="sm" />
            {t('license.revalidate')}
          </ButtonV2>
        </CardV2>
      )}

      {/* --- License key entry section --- */}
      <CardV2>
        <button
          className="w-full flex items-center justify-between bg-transparent cursor-pointer"
          onClick={() => setKeyExpanded(!keyExpanded)}
        >
          <span className="text-[14px] font-[500]" style={{ color: 'var(--text)' }}>
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
                    <p className="text-[13px]" style={{ color: 'var(--warning, #F59E0B)' }}>
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
            {/* Remove key button: surface whenever a key is persisted in DB,
                regardless of paid/trial source — so a user who saved a bad key
                during trial can still clear it. */}
            {status.license_key_masked && (
              <div className="flex gap-2">
                {source === 'paid' && (
                  <ButtonV2 variant="secondary" onClick={() => setKeyExpanded(true)}>
                    {t('license.key.change_button')}
                  </ButtonV2>
                )}
                <ButtonV2
                  variant="danger"
                  onClick={() => setRemoveOpen(true)}
                >
                  {t('license.key.remove_button')}
                </ButtonV2>
              </div>
            )}
          </div>
        )}
      </CardV2>

      {/* --- Feature comparison --- */}
      <CardV2>
        <p
          className="text-[12px] uppercase tracking-wider font-[400] mb-4"
          style={{ color: 'var(--text-subtle, var(--text-soft))' }}
        >
          {t('license.features.heading')}
        </p>
        <div className="grid grid-cols-2 gap-6">
          <div>
            <p
              className="text-[13px] font-[500] mb-2"
              style={{ color: 'var(--text)' }}
            >
              {t('license.status.free')}
            </p>
            <ul className="space-y-1.5">
              {[1, 2, 3, 4, 5, 6].map((i) => (
                <li key={i} className="flex items-center gap-2 text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  <Icon name="check_circle" size="sm" style={{ color: 'var(--text-subtle, var(--text-soft))' }} />
                  {t(`license.features.free.f${i}`)}
                </li>
              ))}
            </ul>
          </div>
          <div>
            <p
              className="text-[13px] font-[500] mb-2"
              style={{ color: 'var(--text)' }}
            >
              {t('license.status.pro')}
            </p>
            <ul className="space-y-1.5">
              {[1, 2, 3, 4, 5, 6].map((i) => (
                <li key={i} className="flex items-center gap-2 text-[13px]" style={{ color: 'var(--text-soft)' }}>
                  <Icon name="star" size="sm" style={{ color: 'var(--brand)' }} />
                  {t(`license.features.pro.f${i}`)}
                </li>
              ))}
            </ul>
          </div>
        </div>
      </CardV2>

      {/* --- Remove key confirmation modal --- */}
      <ModalV2
        open={removeOpen}
        onClose={() => setRemoveOpen(false)}
        title={t('license.key.remove_confirm_title')}
      >
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
