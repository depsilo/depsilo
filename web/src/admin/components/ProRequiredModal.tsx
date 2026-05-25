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

export default function ProRequiredModal() {
  const { t } = useTranslation()
  const nav = useNavigate()
  const [open, setOpen] = useState(false)
  const [detail, setDetail] = useState<ProRequiredDetail | null>(null)
  const [pending, setPending] = useState(false)
  const [activatedMsg, setActivatedMsg] = useState<string | null>(null)

  useEffect(() => {
    const onEvent = (e: Event) => {
      const ce = e as CustomEvent<ProRequiredDetail>
      setDetail(ce.detail)
      setActivatedMsg(null)
      setOpen(true)
    }
    window.addEventListener('depsilo:pro-required', onEvent)
    return () => window.removeEventListener('depsilo:pro-required', onEvent)
  }, [])

  if (!detail) return null

  const trialAvailable = detail.trial_available

  const onStartTrial = async () => {
    setPending(true)
    try {
      await licenseApi.activateTrial()
      setActivatedMsg(t('license.paywall.trial_activated_toast'))
      setTimeout(() => setOpen(false), 1800)
    } catch {
      setActivatedMsg(t('license.paywall.title'))
    } finally {
      setPending(false)
    }
  }

  return (
    <ModalV2 open={open} onClose={() => setOpen(false)} title={t('license.paywall.title')}>
      <p className="text-sm mb-4" style={{ color: 'var(--text-soft)' }}>
        {t('license.paywall.body')}
      </p>
      {activatedMsg && (
        <div className="text-sm mb-4 p-3 rounded-[4px]" style={{ color: '#059669', background: '#ecfdf5' }}>
          {activatedMsg}
        </div>
      )}
      <div className="flex gap-2 justify-end">
        {trialAvailable ? (
          <>
            <ButtonV2
              variant="secondary"
              onClick={() => {
                setOpen(false)
                nav('/admin/license')
              }}
            >
              {t('license.paywall.learn_more')}
            </ButtonV2>
            <ButtonV2 onClick={onStartTrial} disabled={pending}>
              {t('license.paywall.start_trial')}
            </ButtonV2>
          </>
        ) : (
          <>
            <ButtonV2
              variant="secondary"
              onClick={() => {
                setOpen(false)
                nav('/admin/license')
              }}
            >
              {t('license.paywall.view_status')}
            </ButtonV2>
            <ButtonV2
              onClick={() => window.open(detail.upgrade ?? 'https://depsilo.com/#pricing', '_blank')}
            >
              {t('license.paywall.buy_pro')}
            </ButtonV2>
          </>
        )}
        <ButtonV2 variant="ghost" onClick={() => setOpen(false)}>
          {t('license.paywall.dismiss')}
        </ButtonV2>
      </div>
    </ModalV2>
  )
}
