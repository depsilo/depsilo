import { Alert, AlertDescription } from '@/components/ui/alert'
import { Field, FieldLabel } from '@/components/ui/field'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { licenseApi } from '@/lib/api'
import type { EntitlementStatus } from '@/lib/api'
import { proAccessUrl } from '@/lib/buy'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import Icon, { type IconName } from '@/components/Icon'

import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import AdminPage from '@/admin/components/AdminPage'
import { usePrincipal } from '@/hooks/usePrincipal'
import { getApiError } from '@/lib/apiError'

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
  icon: IconName
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
    <div className="rounded-md p-5" style={{ background: t.bg }}>
      <div className="flex items-center gap-3 mb-4">
        <span
          className="flex items-center justify-center w-10 h-10 rounded-md"
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
  const { canWrite } = usePrincipal()

  const statusQuery = useQuery({
    queryKey: ['license', 'status'],
    queryFn: async ({ signal }) => {
      const res = await licenseApi.status({ signal })
      return res.data as EntitlementStatus
    },
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
    retry: false,
  })

  const status = statusQuery.data

  const activateTrial = useMutation({
    mutationFn: async () => {
      const res = await licenseApi.activateTrial()
      return res.data as EntitlementStatus
    },
    onSuccess: (nextStatus) => {
      qc.setQueryData<EntitlementStatus>(['license', 'status'], nextStatus)
    },
  })

  const setKey = useMutation({
    mutationFn: async (key: string) => {
      const res = await licenseApi.setKey(key)
      return res.data as EntitlementStatus
    },
    onSuccess: (s) => {
      qc.setQueryData<EntitlementStatus>(['license', 'status'], s)
      if (s.source === 'paid') {
        setKeyInput('')
        setReplacingKey(false)
      }
    },
  })

  const clearKey = useMutation({
    mutationFn: async () => {
      const res = await licenseApi.clearKey()
      return res.data as EntitlementStatus
    },
    onSuccess: (nextStatus) => {
      qc.setQueryData<EntitlementStatus>(['license', 'status'], nextStatus)
      setRemoveOpen(false)
    },
  })

  const revalidate = useMutation({
    mutationFn: licenseApi.revalidate,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['license', 'status'] }),
  })

  const [keyInput, setKeyInput] = useState('')
  const [removeOpen, setRemoveOpen] = useState(false)
  const [keyExpanded, setKeyExpanded] = useState<boolean | null>(null)
  const [replacingKey, setReplacingKey] = useState(false)

  if (statusQuery.isPending) {
    return (
      <AdminPage width="readable" description={t('license.subtitle')}>
        <div aria-busy="true" className="py-6 text-[14px]" style={{ color: 'var(--text-soft)' }}>
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      </AdminPage>
    )
  }

  if (statusQuery.isError && !status) {
    const normalized = getApiError(statusQuery.error)
    return (
      <AdminPage width="readable" description={t('license.subtitle')}>
        <QueryErrorState message={normalized.status === 403 ? t('common.permissionDenied') : normalized.message} onRetry={() => { void statusQuery.refetch() }} />
      </AdminPage>
    )
  }

  if (!status) return <AdminPage width="readable" description={t('license.subtitle')}>{null}</AdminPage>

  const source = status.source
  const trialUsed = status.trial_used
  const keySectionOpen = keyExpanded ?? (source === 'none' && !trialUsed)
  const keyEditorOpen = canWrite && (source !== 'paid' || replacingKey)

  const formatDate = (iso?: string) =>
    iso ? new Date(iso).toLocaleDateString(i18n.language === 'zh' ? 'zh-CN' : 'en-US') : ''

  const formatRelative = (iso?: string) =>
    iso ? new Date(iso).toLocaleString(i18n.language === 'zh' ? 'zh-CN' : 'en-US') : ''

  return (
    <AdminPage width="readable" description={t('license.subtitle')}>
    <div className="space-y-10">
      {statusQuery.isRefetchError && (
        <Alert variant={"warning"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-3"><span>{t('now.staleData')}</span><Button type="button" variant="outline" size="sm" onClick={() => { void statusQuery.refetch() }}>{t('now.refresh')}</Button></div></AlertDescription></Alert>
      )}
      {/* ── State panel ────────────────────────────── */}
      {/* Commercial terms are intentionally absent until the product model
          is settled. Pro CTAs open a neutral access enquiry. */}
      {source === 'none' && !trialUsed && (
        <StatePanel
          tone="brand"
          icon="workspace_premium"
          title={t('license.trial.start_button')}
          description={t('license.trial.start_explainer')}
        >
          <div className="flex flex-wrap gap-2">
            {canWrite && <Button
              onClick={() => activateTrial.mutate()}
              aria-busy={activateTrial.isPending || undefined}
              disabled={activateTrial.isPending}
            >
              {t('license.trial.start_button')}
            </Button>}
            <Button variant="outline" onClick={() => window.open(proAccessUrl())}>
              {t('license.buy_lifetime')}
            </Button>
          </div>
          {activateTrial.isError && (
            <div className="mt-3">
              <Alert variant={"destructive"}><AlertDescription>{getApiError(activateTrial.error).message}</AlertDescription></Alert>
            </div>
          )}
        </StatePanel>
      )}

      {source === 'trial' && (
        <StatePanel
          tone="ok"
          icon="verified"
          title={`${t('license.status.pro')} · ${t('license.status.trial')}`}
          description={`${t('license.trial.days_left', { count: status.days_left })} · ${t('license.trial.expires_at', { date: formatDate(status.expires_at) })}`}
        >
          <Button onClick={() => window.open(proAccessUrl())}>
            {t('license.buy_lifetime')}
          </Button>
        </StatePanel>
      )}

      {source === 'none' && trialUsed && (
        <StatePanel
          tone="danger"
          icon="warning"
          title={t('license.trial.expired_message', { date: formatDate(status.expires_at) })}
        >
          <Button onClick={() => window.open(proAccessUrl())}>
            {t('license.buy_lifetime')}
          </Button>
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
          {canWrite && <Button
            variant="outline"
            onClick={() => revalidate.mutate()}
            disabled={revalidate.isPending}
            className="mt-4"
          >
            <Icon name="refresh" size="sm" />
            {t('license.revalidate')}
          </Button>}
          {revalidate.isError && (
            <div className="mt-3">
              <Alert variant={"destructive"}><AlertDescription>{getApiError(revalidate.error).message}</AlertDescription></Alert>
            </div>
          )}
        </StatePanel>
      )}

      {/* ── License key entry (collapsible) ────────── */}
      <section>
        <Button variant="ghost"
          type="button"
          className="w-full flex items-center justify-between bg-transparent cursor-pointer pb-2 disabled:cursor-not-allowed disabled:opacity-60 stripe-focus-ring"
          style={{ borderBottom: '1px solid var(--border)' }}
          aria-controls="license-key-content"
          aria-expanded={keySectionOpen}
          disabled={setKey.isPending}
          onClick={() => setKeyExpanded(!keySectionOpen)}
        >
          <span className="text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
            {t('license.key.title')}
          </span>
          <Icon name={keySectionOpen ? 'expand_less' : 'expand_more'} size="sm" style={{ color: 'var(--text-soft)' }} />
        </Button>

        {keySectionOpen && (
          <div id="license-key-content" className="mt-4 space-y-3">
            {keyEditorOpen && (
              <form
                className="space-y-3"
                onSubmit={(event) => {
                  event.preventDefault()
                  const key = keyInput.trim()
                  if (key && !setKey.isPending) setKey.mutate(key)
                }}
              >
                <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
                    <Field disabled={setKey.isPending}>
                      <FieldLabel>{t('license.key.title')}</FieldLabel>
                      <Input className="font-mono"
                        placeholder={t('license.key.placeholder')}
                        value={keyInput}
                        onChange={(event) => {
                          setKeyInput(event.target.value)
                          if (setKey.isError || setKey.data) setKey.reset()
                        }}
                        disabled={setKey.isPending}
                      />
                    </Field>
                  <Button
                    type="submit"
                    aria-busy={setKey.isPending || undefined}
                    disabled={setKey.isPending || keyInput.trim() === ''}
                  >
                    {replacingKey ? t('license.key.save_button') : t('license.key.activate_button')}
                  </Button>
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
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => revalidate.mutate()}
                        disabled={revalidate.isPending}
                      >
                        {t('license.key.try_revalidate')}
                      </Button>
                    </div>
                  </div>
                )}
                {setKey.isError && <Alert variant={"destructive"}><AlertDescription>{getApiError(setKey.error).message}</AlertDescription></Alert>}
                {replacingKey && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setReplacingKey(false)
                      setKeyInput('')
                      setKey.reset()
                    }}
                    disabled={setKey.isPending}
                  >
                    {t('cancel')}
                  </Button>
                )}
              </form>
            )}
            {status.license_key_masked && (
              <div className="flex flex-wrap gap-2">
                {source === 'paid' && canWrite && !replacingKey && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      setKey.reset()
                      setKeyInput('')
                      setReplacingKey(true)
                      setKeyExpanded(true)
                    }}
                  >
                    {t('license.key.change_button')}
                  </Button>
                )}
                {canWrite && !replacingKey && <Button
                  variant="destructive"
                  onClick={() => {
                    clearKey.reset()
                    setRemoveOpen(true)
                  }}
                >
                  {t('license.key.remove_button')}
                </Button>}
              </div>
            )}
          </div>
        )}
      </section>

      {/* ── Feature comparison ─────────────────────── */}
      <section>
        <SectionHeader title={t('license.features.heading')} />
        <div className="grid grid-cols-1 gap-8 md:grid-cols-2">
          <div>
            <p className="mb-3 whitespace-nowrap text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
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
            <p className="mb-3 whitespace-nowrap text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
              {t('license.status.pro')}
            </p>
            <ul className="space-y-2">
              {[1, 2].map((i) => (
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
        <Dialog open={removeOpen} onOpenChange={(nextOpen) => {
          if (!nextOpen) (() => {
            if (!clearKey.isPending) setRemoveOpen(false)
          })()
        }} modal>
          <DialogContent style={{ maxWidth: 440 }} finalFocus={true}>
            <DialogTitle className="pr-10">{t('license.key.remove_confirm_title')}</DialogTitle>
            <p className="text-[14px] mb-5" style={{ color: 'var(--text-soft)' }}>
              {t('license.key.remove_confirm_body')}
            </p>
            {clearKey.isError && (
              <div className="mb-4">
                <Alert variant={"destructive"}><AlertDescription>{getApiError(clearKey.error).message}</AlertDescription></Alert>
              </div>
            )}
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={() => setRemoveOpen(false)} disabled={clearKey.isPending}>
                {t('license.paywall.dismiss')}
              </Button>
              <Button
                variant="destructive"
                aria-busy={clearKey.isPending || undefined}
                onClick={() => {
                  clearKey.mutate()
                }}
                disabled={clearKey.isPending || !canWrite}
              >
                {t('license.key.remove_button')}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
    </div>
    </AdminPage>
  )
}
