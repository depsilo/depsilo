import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import ModalV2 from '@/components/Modal'
import ButtonV2 from '@/components/Button'
import { licenseApi } from '@/lib/api'

interface ProRequiredDetail {
  code: string
  message?: string
  upgrade?: string
  trial_available: boolean
}

type FeedbackTone = 'ok' | 'error'

export default function ProRequiredModal() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [open, setOpen] = useState(false)
  const [detail, setDetail] = useState<ProRequiredDetail | null>(null)
  const [pending, setPending] = useState(false)
  const [feedback, setFeedback] = useState<{ tone: FeedbackTone; message: string } | null>(null)

  useEffect(() => {
    const onEvent = (e: Event) => {
      const ce = e as CustomEvent<ProRequiredDetail>
      setDetail(ce.detail)
      setFeedback(null)
      setOpen(true)
    }
    window.addEventListener('depsilo:pro-required', onEvent)
    return () => window.removeEventListener('depsilo:pro-required', onEvent)
  }, [])

  if (!detail) return null

  const trialAvailable = detail.trial_available

  const onStartTrial = async () => {
    setPending(true)
    setFeedback(null)
    try {
      await licenseApi.activateTrial()
      setFeedback({ tone: 'ok', message: t('license.paywall.trial_activated_toast') })
      setTimeout(() => setOpen(false), 1800)
    } catch {
      setFeedback({ tone: 'error', message: t('license.paywall.trial_error') })
    } finally {
      setPending(false)
    }
  }

  const previewFeatures = [
    t('license.features.pro.f1'),
    t('license.features.pro.f2'),
    t('license.features.pro.f3'),
    t('license.features.pro.f4'),
  ]

  return (
    <ModalV2 open={open} onClose={() => setOpen(false)} title={t('license.paywall.title')}>
      {/* Brand mark + body */}
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start', marginBottom: 14 }}>
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 32,
            height: 32,
            flexShrink: 0,
            borderRadius: 8,
            background: 'var(--brand-soft)',
            border: '0.5px solid var(--brand-border)',
            color: 'var(--brand)',
          }}
        >
          <span className="icon" style={{ fontSize: 18 }}>workspace_premium</span>
        </span>
        <p style={{ margin: 0, fontSize: 13, lineHeight: 1.55, color: 'var(--text-muted)', flex: 1 }}>
          {t('license.paywall.body')}
        </p>
      </div>

      {/* Pro feature preview */}
      <div
        style={{
          background: 'var(--bg-soft)',
          border: '0.5px solid var(--border)',
          borderRadius: 8,
          padding: '10px 14px',
          marginBottom: 14,
        }}
      >
        <div
          className="eyebrow"
          style={{ marginBottom: 8 }}
        >
          {t('license.paywall.features_preview_title')}
        </div>
        <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 6 }}>
          {previewFeatures.map((f, i) => (
            <li
              key={i}
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: 8,
                fontSize: 13,
                lineHeight: 1.4,
                color: 'var(--text)',
              }}
            >
              <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true" style={{ marginTop: 4, flexShrink: 0, color: 'var(--brand)' }}>
                <path d="M2 6.2L4.8 9L10 3.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span>{f}</span>
            </li>
          ))}
        </ul>
      </div>

      {/* Feedback banner */}
      {feedback && (
        <div
          role="alert"
          style={{
            marginBottom: 14,
            padding: '8px 12px',
            borderRadius: 6,
            fontSize: 13,
            lineHeight: 1.45,
            color: feedback.tone === 'ok' ? 'var(--ok-text)' : 'var(--danger-text)',
            background: feedback.tone === 'ok' ? 'var(--ok-fill)' : 'var(--danger-fill)',
            border: `0.5px solid ${feedback.tone === 'ok' ? 'var(--ok-border)' : 'var(--danger-border)'}`,
          }}
        >
          {feedback.message}
        </div>
      )}

      {/* Footer */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 8,
          paddingTop: 14,
          borderTop: '0.5px solid var(--border)',
        }}
      >
        <ButtonV2 variant="ghost" size="sm" onClick={() => setOpen(false)}>
          {t('license.paywall.dismiss')}
        </ButtonV2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {trialAvailable ? (
            <>
              <ButtonV2
                variant="secondary"
                size="sm"
                onClick={() => {
                  setOpen(false)
                  nav('/admin/license')
                }}
              >
                {t('license.paywall.learn_more')}
              </ButtonV2>
              <ButtonV2 size="sm" onClick={onStartTrial} disabled={pending}>
                {t('license.paywall.start_trial')}
              </ButtonV2>
            </>
          ) : (
            <>
              <ButtonV2
                variant="secondary"
                size="sm"
                onClick={() => {
                  setOpen(false)
                  nav('/admin/license')
                }}
              >
                {t('license.paywall.view_status')}
              </ButtonV2>
              <ButtonV2
                size="sm"
                onClick={() => window.open(detail.upgrade ?? 'https://depsilo.com/#pricing', '_blank')}
              >
                {t('license.paywall.buy_pro')}
              </ButtonV2>
            </>
          )}
        </div>
      </div>
    </ModalV2>
  )
}
