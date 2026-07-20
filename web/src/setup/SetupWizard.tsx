import { useState, useCallback, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import ButtonV2 from '../components/Button'
import InputV2 from '../components/Input'
import Icon from '../components/Icon'
import EcosystemIcon, { type EcosystemType } from '../components/EcosystemIcon'
import { ecosystemDefaults, type UpstreamDefault } from '../setup/defaults'
import { setupApi } from '../lib/api'
import axios from 'axios'

interface SetupWizardProps {
  tokenRequired?: boolean
}

type SetupPhase = 'editing' | 'saving' | 'restarting' | 'failed' | 'ready'

const reconnectTimeoutMs = 30_000
const reconnectInitialDelayMs = 1_500
const reconnectIntervalMs = 1_000
const reconnectAttemptTimeoutMs = 2_500

function abortableDelay(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException('Aborted', 'AbortError'))
      return
    }
    const handleAbort = () => {
      window.clearTimeout(timer)
      reject(new DOMException('Aborted', 'AbortError'))
    }
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', handleAbort)
      resolve()
    }, ms)
    signal.addEventListener('abort', handleAbort, { once: true })
  })
}

async function probeHealth(target: URL, signal: AbortSignal) {
  const attempt = new AbortController()
  const cancelAttempt = () => attempt.abort()
  signal.addEventListener('abort', cancelAttempt, { once: true })
  const timeout = window.setTimeout(cancelAttempt, reconnectAttemptTimeoutMs)
  try {
    const response = await fetch(target, {
      cache: 'no-store',
      mode: 'cors',
      signal: attempt.signal,
    })
    return response.ok
  } catch {
    if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
    return false
  } finally {
    window.clearTimeout(timeout)
    signal.removeEventListener('abort', cancelAttempt)
  }
}

async function waitForReconnect(reconnectURL: string, signal: AbortSignal) {
  const deadline = Date.now() + reconnectTimeoutMs
  const healthURL = new URL('/health', reconnectURL)
  await abortableDelay(reconnectInitialDelayMs, signal)
  while (Date.now() < deadline) {
    if (await probeHealth(healthURL, signal)) return
    const remaining = deadline - Date.now()
    if (remaining > 0) {
      await abortableDelay(Math.min(reconnectIntervalMs, remaining), signal)
    }
  }
  throw new Error('SETUP_RECONNECT_TIMEOUT')
}

function resolveReconnectURL(reportedURL: string) {
  const reported = new URL(reportedURL, window.location.href)
  const target = new URL(window.location.origin)
  target.port = reported.port
  target.pathname = '/'
  target.search = ''
  target.hash = ''
  return target.toString()
}

function strongEnoughPassword(username: string, password: string) {
  const characters = Array.from(password).length
  const classes = [
    /\p{Ll}/u.test(password),
    /\p{Lu}/u.test(password),
    /\p{N}/u.test(password),
    /[^\p{L}\p{N}]/u.test(password),
  ].filter(Boolean).length
  return characters >= 12 && new TextEncoder().encode(password).length <= 72 &&
    (characters >= 20 || classes >= 3) &&
    !password.toLocaleLowerCase().includes(username.toLocaleLowerCase())
}

export default function SetupWizard({ tokenRequired = false }: SetupWizardProps) {
  const { t } = useTranslation()

  const [step, setStep] = useState(1)
  const [port, setPort] = useState(23333)
  const [storagePath, setStoragePath] = useState('./data/cache')
  const [adminUsername, setAdminUsername] = useState('admin')
  const [adminPassword, setAdminPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [bootstrapToken, setBootstrapToken] = useState('')
  const [selectedEcosystems, setSelectedEcosystems] = useState<Set<string>>(
    () => new Set(ecosystemDefaults.map((e) => e.key))
  )
  const [upstreams, setUpstreams] = useState<Record<string, UpstreamDefault[]>>(() => {
    const map: Record<string, UpstreamDefault[]> = {}
    for (const eco of ecosystemDefaults) {
      map[eco.key] = eco.upstreams.map((u) => ({ ...u }))
    }
    return map
  })
  const [expandedEcosystem, setExpandedEcosystem] = useState<string | null>(null)
  const [phase, setPhase] = useState<SetupPhase>('editing')
  const [submitError, setSubmitError] = useState('')
  const [reconnectURL, setReconnectURL] = useState('')
  const reconnectAbortRef = useRef<AbortController | null>(null)
  const redirectTimerRef = useRef<number | null>(null)

  const totalSteps = 5
  const submitting = phase === 'saving'
  const restarting = phase === 'restarting' || phase === 'ready'

  useEffect(() => () => {
    reconnectAbortRef.current?.abort()
    if (redirectTimerRef.current !== null) window.clearTimeout(redirectTimerRef.current)
  }, [])

  const toggleEcosystem = useCallback((key: string) => {
    setSelectedEcosystems((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const updateUpstream = useCallback(
    (ecoKey: string, index: number, field: keyof UpstreamDefault, value: string | number) => {
      setUpstreams((prev) => {
        const list = [...(prev[ecoKey] || [])]
        list[index] = { ...list[index], [field]: value }
        return { ...prev, [ecoKey]: list }
      })
    },
    []
  )

  const addUpstream = useCallback((ecoKey: string) => {
    setUpstreams((prev) => {
      const list = [...(prev[ecoKey] || [])]
      const priority = list.length > 0 ? Math.max(...list.map((u) => u.priority)) + 1 : 1
      list.push({ name: '', url: '', priority })
      return { ...prev, [ecoKey]: list }
    })
  }, [])

  const removeUpstream = useCallback((ecoKey: string, index: number) => {
    setUpstreams((prev) => {
      const list = [...(prev[ecoKey] || [])]
      list.splice(index, 1)
      return { ...prev, [ecoKey]: list }
    })
  }, [])

  const monitorReconnect = async (target: string) => {
    reconnectAbortRef.current?.abort()
    const controller = new AbortController()
    reconnectAbortRef.current = controller
    setPhase('restarting')
    setSubmitError('')
    try {
      await waitForReconnect(target, controller.signal)
      if (controller.signal.aborted) return
      setPhase('ready')
      redirectTimerRef.current = window.setTimeout(() => window.location.replace(target), 300)
    } catch (error) {
      if (controller.signal.aborted) return
      setPhase('failed')
      setSubmitError(error instanceof Error && error.message === 'SETUP_RECONNECT_TIMEOUT'
        ? t('setup.restart_timeout')
        : t('setup.restart_failed'))
    } finally {
      if (reconnectAbortRef.current === controller) reconnectAbortRef.current = null
    }
  }

  const handleSubmit = async () => {
    reconnectAbortRef.current?.abort()
    setPhase('saving')
    setSubmitError('')
    setReconnectURL('')
    try {
      const ecosystems: Record<string, { enabled: boolean; upstreams: UpstreamDefault[] }> = {}
      for (const eco of ecosystemDefaults) {
        const enabled = selectedEcosystems.has(eco.key)
        ecosystems[eco.key] = {
          enabled,
          upstreams: enabled ? upstreams[eco.key] || [] : [],
        }
      }

      const response = await setupApi.complete(
        {
          server: { port },
          storage: { path: storagePath },
          admin: { username: adminUsername, password: adminPassword },
          ecosystems,
        },
        bootstrapToken.trim() || undefined
      )

      // Keep the browser-visible scheme and hostname. A reverse proxy may
      // rewrite Host/TLS before the request reaches Go; the response supplies
      // the newly configured port, which is the only part that must change.
      const target = resolveReconnectURL(response.data.reconnect_url)
      setReconnectURL(target)
      if (response.data.restart_strategy === 'supervisor_required') {
        setPhase('failed')
        setSubmitError(t('setup.supervisor_required'))
        return
      }
      void monitorReconnect(target)
    } catch (error) {
      setPhase('failed')
      if (axios.isAxiosError(error)) {
        setSubmitError(error.response?.data?.message || t('setup.save_failed'))
      } else {
        setSubmitError(t('setup.save_failed'))
      }
    }
  }

  const canNext = () => {
    if (step === 2) {
      const usernameValid = /^[\p{L}\p{N}][\p{L}\p{N}._-]{2,63}$/u.test(adminUsername)
      return port > 0 && storagePath.trim() !== '' && usernameValid &&
        strongEnoughPassword(adminUsername, adminPassword) &&
        adminPassword === confirmPassword && (!tokenRequired || bootstrapToken.trim() !== '')
    }
    if (step === 3) return selectedEcosystems.size > 0
    return true
  }

  // Step 1: Welcome
  const renderWelcome = () => (
    <div className="text-center py-8">
      <div
        className="text-[48px] font-[700] mb-2"
        style={{ color: 'var(--text)' }}
      >
        DepSilo
      </div>
      <p
        className="text-[16px] mb-8 max-w-md mx-auto"
        style={{ color: 'var(--text-soft)' }}
      >
        {t('setup.welcome_description')}
      </p>
      <ButtonV2 onClick={() => setStep(2)}>
        <Icon name="arrow_forward" size="sm" />
        {t('setup.get_started')}
      </ButtonV2>
    </div>
  )

  // Step 2: Basic Settings
  const renderBasicSettings = () => (
    <div className="space-y-6">
      <h2 className="text-[20px] font-[600]" style={{ color: 'var(--text)' }}>
        {t('setup.basic_settings')}
      </h2>
      <InputV2
        label={t('setup.port')}
        type="number"
        value={port}
        min={1}
        max={65535}
        onChange={(e) => setPort(Number(e.target.value))}
      />
      <InputV2
        label={t('setup.storage_path')}
        value={storagePath}
        mono
        onChange={(e) => setStoragePath(e.target.value)}
      />
      <div className="pt-2" style={{ borderTop: '1px solid var(--border)' }}>
        <h3 className="text-[15px] font-[600] mb-3" style={{ color: 'var(--text)' }}>
          {t('setup.admin_account')}
        </h3>
        <div className="space-y-4">
          <InputV2
            label={t('setup.admin_username')}
            autoComplete="username"
            value={adminUsername}
            onChange={(e) => setAdminUsername(e.target.value)}
          />
          <InputV2
            label={t('setup.admin_password')}
            hint={t('setup.admin_password_hint')}
            type="password"
            autoComplete="new-password"
            value={adminPassword}
            onChange={(e) => setAdminPassword(e.target.value)}
          />
          <InputV2
            label={t('setup.confirm_password')}
            error={confirmPassword && adminPassword !== confirmPassword ? t('setup.password_mismatch') : undefined}
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
          />
          {tokenRequired && (
            <InputV2
              label={t('setup.bootstrap_token')}
              hint={t('setup.bootstrap_token_hint')}
              type="password"
              autoComplete="off"
              mono
              value={bootstrapToken}
              onChange={(e) => setBootstrapToken(e.target.value)}
            />
          )}
        </div>
      </div>
    </div>
  )

  // Step 3: Select Ecosystems
  const renderSelectEcosystems = () => (
    <div>
      <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
        {t('setup.select_ecosystems')}
      </h2>
      <p className="text-[14px] mb-4" style={{ color: 'var(--text-soft)' }}>
        {t('setup.select_ecosystems_hint')}
      </p>
      <div className="grid grid-cols-4 gap-3">
        {ecosystemDefaults.map((eco) => {
          const checked = selectedEcosystems.has(eco.key)
          return (
            <button
              key={eco.key}
              type="button"
              className="flex items-center gap-2 rounded-[4px] px-3 py-2.5 text-left cursor-pointer transition-[background,border-color,transform] duration-150 active:scale-[0.96]"
              style={{
                border: `1px solid ${checked ? 'var(--brand)' : 'var(--border)'}`,
                background: checked ? 'var(--brand-soft)' : 'var(--bg-card)',
                color: 'var(--text)',
              }}
              onClick={() => toggleEcosystem(eco.key)}
            >
              <input
                type="checkbox"
                checked={checked}
                readOnly
                className="accent-[var(--brand)] pointer-events-none"
              />
              <EcosystemIcon type={eco.key as EcosystemType} size={16} />
              <span className="text-[13px] font-[400] truncate">{eco.label}</span>
            </button>
          )
        })}
      </div>
    </div>
  )

  // Step 4: Configure Upstreams
  const renderConfigureUpstreams = () => {
    const selected = ecosystemDefaults.filter((e) => selectedEcosystems.has(e.key))
    return (
      <div>
        <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
          {t('setup.configure_upstreams')}
        </h2>
        <div className="space-y-2 max-h-[400px] overflow-y-auto pr-1">
          {selected.map((eco) => {
            const expanded = expandedEcosystem === eco.key
            const ecoUpstreams = upstreams[eco.key] || []
            return (
              <div
                key={eco.key}
                className="rounded-[4px]"
                style={{ border: '1px solid var(--border)' }}
              >
                <button
                  type="button"
                  className="w-full flex items-center gap-2 px-4 py-3 cursor-pointer"
                  style={{ background: 'var(--bg-card)', color: 'var(--text)' }}
                  onClick={() => setExpandedEcosystem(expanded ? null : eco.key)}
                >
                  <Icon name={expanded ? 'expand_more' : 'chevron_right'} size="sm" />
                  <EcosystemIcon type={eco.key as EcosystemType} size={16} />
                  <span className="text-[14px] font-[500] flex-1 text-left">{eco.label}</span>
                  <span className="text-[12px]" style={{ color: 'var(--text-muted)' }}>
                    {ecoUpstreams.length} {t('setup.upstreams_count')}
                  </span>
                </button>
                {expanded && (
                  <div className="px-4 pb-4 space-y-3" style={{ borderTop: '1px solid var(--border)' }}>
                    {ecoUpstreams.map((upstream, idx) => (
                      <div key={idx} className="flex items-end gap-2 mt-3">
                        <div className="flex-1 min-w-0">
                          <InputV2
                            label={t('setup.upstream_name')}
                            value={upstream.name}
                            onChange={(e) => updateUpstream(eco.key, idx, 'name', e.target.value)}
                          />
                        </div>
                        <div className="flex-[2] min-w-0">
                          <InputV2
                            label={t('setup.upstream_url')}
                            value={upstream.url}
                            mono
                            onChange={(e) => updateUpstream(eco.key, idx, 'url', e.target.value)}
                          />
                        </div>
                        <div className="w-16">
                          <InputV2
                            label={t('setup.priority')}
                            type="number"
                            value={upstream.priority}
                            min={1}
                            onChange={(e) =>
                              updateUpstream(eco.key, idx, 'priority', Number(e.target.value))
                            }
                          />
                        </div>
                        <ButtonV2
                          variant="danger"
                          size="sm"
                          aria-label={t('setup.remove_upstream', { name: upstream.name || `${eco.label} ${idx + 1}` })}
                          onClick={() => removeUpstream(eco.key, idx)}
                        >
                          <Icon name="delete" size="sm" />
                        </ButtonV2>
                      </div>
                    ))}
                    <ButtonV2 variant="ghost" size="sm" onClick={() => addUpstream(eco.key)}>
                      <Icon name="add" size="sm" />
                      {t('setup.add_upstream')}
                    </ButtonV2>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  // Step 5: Complete
  const renderComplete = () => {
    if (restarting) {
      return (
        <div className="text-center py-12">
          <div
            className="text-[20px] font-[600] mb-4"
            style={{ color: 'var(--text)' }}
          >
            {phase === 'ready' ? t('setup.ready') : t('setup.restarting')}
          </div>
          <p className="text-[14px]" style={{ color: 'var(--text-soft)' }}>
            {phase === 'ready' ? t('setup.ready_hint') : t('setup.restarting_hint', { url: reconnectURL })}
          </p>
        </div>
      )
    }

    if (phase === 'failed') {
      return (
        <div className="text-center py-10">
          <h2 className="text-[20px] font-[600] mb-3" style={{ color: 'var(--danger-text)' }}>
              {reconnectURL
                ? t('setup.restart_failed_title')
                : t('setup.save_failed_title')}
          </h2>
          <p role="alert" className="text-[14px] mb-4" style={{ color: 'var(--text-soft)' }}>
            {submitError}
          </p>
          {reconnectURL && (
            <p className="font-mono text-[12px] mb-5 break-all" style={{ color: 'var(--text-muted)' }}>
              {t('setup.reconnect_target', { url: reconnectURL })}
            </p>
          )}
          <div className="flex justify-center gap-3">
            {reconnectURL ? (
              <ButtonV2 onClick={() => { void monitorReconnect(reconnectURL) }}>
                <Icon name="refresh" size="sm" />
                {t('setup.retry_connection')}
              </ButtonV2>
            ) : (
              <ButtonV2 onClick={() => setPhase('editing')}>
                <Icon name="arrow_back" size="sm" />
                {t('setup.return_to_settings')}
              </ButtonV2>
            )}
          </div>
        </div>
      )
    }

    const selectedList = ecosystemDefaults.filter((e) => selectedEcosystems.has(e.key))

    return (
      <div>
        <h2 className="text-[20px] font-[600] mb-4" style={{ color: 'var(--text)' }}>
          {t('setup.complete')}
        </h2>
        <div className="space-y-3 mb-6">
          <div className="flex justify-between text-[14px]" style={{ color: 'var(--text-soft)' }}>
            <span>{t('setup.port')}</span>
            <span className="font-mono" style={{ color: 'var(--text)' }}>
              {port}
            </span>
          </div>
          <div className="flex justify-between text-[14px]" style={{ color: 'var(--text-soft)' }}>
            <span>{t('setup.admin_username')}</span>
            <span style={{ color: 'var(--text)' }}>{adminUsername}</span>
          </div>
          <div className="flex justify-between text-[14px]" style={{ color: 'var(--text-soft)' }}>
            <span>{t('setup.storage_path')}</span>
            <span className="font-mono" style={{ color: 'var(--text)' }}>
              {storagePath}
            </span>
          </div>
          <div
            className="text-[14px] pt-2"
            style={{ color: 'var(--text-soft)', borderTop: '1px solid var(--border)' }}
          >
            <span>{t('setup.enabled_ecosystems')}</span>
          </div>
          <div className="flex flex-wrap gap-2">
            {selectedList.map((eco) => (
              <span
                key={eco.key}
                className="inline-flex items-center gap-1.5 text-[13px] px-2.5 py-1 rounded-full"
                style={{ background: 'var(--brand-soft)', color: 'var(--brand)' }}
              >
                <EcosystemIcon type={eco.key as EcosystemType} size={14} />
                {eco.label}
              </span>
            ))}
          </div>
        </div>
        {submitError && phase === 'editing' && (
          <p role="alert" className="mb-3 text-[13px] text-[var(--danger-text)]">
            {submitError}
          </p>
        )}
        <ButtonV2 onClick={handleSubmit} disabled={submitting} className="w-full">
          {submitting ? t('setup.saving') : t('setup.save_and_start')}
        </ButtonV2>
      </div>
    )
  }

  const renderStep = () => {
    switch (step) {
      case 1:
        return renderWelcome()
      case 2:
        return renderBasicSettings()
      case 3:
        return renderSelectEcosystems()
      case 4:
        return renderConfigureUpstreams()
      case 5:
        return renderComplete()
      default:
        return null
    }
  }

  return (
    <div
      className="min-h-screen flex items-center justify-center p-4"
      style={{ background: 'var(--bg-page)' }}
    >
      <div className="w-full max-w-[720px]">
        {/* Progress indicator */}
        {step > 1 && phase === 'editing' && (
          <div className="flex items-center justify-center gap-2 mb-6">
            {Array.from({ length: totalSteps }, (_, i) => i + 1).map((s) => (
              <div key={s} className="flex items-center gap-2">
                <div
                  className="w-7 h-7 rounded-full flex items-center justify-center text-[12px] font-[500]"
                  style={{
                    background: s <= step ? 'var(--brand)' : 'var(--bg-soft)',
                    color: s <= step ? 'white' : 'var(--text-muted)',
                    border: s <= step ? 'none' : '1px solid var(--border)',
                  }}
                >
                  {s}
                </div>
                {s < totalSteps && (
                  <div
                    className="w-8 h-[2px]"
                    style={{
                      background: s < step ? 'var(--brand)' : 'var(--border)',
                    }}
                  />
                )}
              </div>
            ))}
            <span className="ml-3 text-[13px]" style={{ color: 'var(--text-muted)' }}>
              {t('setup.step_of', { current: step, total: totalSteps })}
            </span>
          </div>
        )}

        <div
          className="p-5"
          style={{
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border)',
            borderRadius: 'var(--r-card)',
            boxShadow: 'var(--shadow-card)',
          }}
        >
          {renderStep()}

          {/* Navigation buttons */}
          {step > 1 && step < 5 && phase === 'editing' && (
            <div className="flex justify-between mt-6 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
              <ButtonV2 variant="ghost" onClick={() => setStep(step - 1)}>
                <Icon name="arrow_back" size="sm" />
                {t('setup.prev')}
              </ButtonV2>
              <ButtonV2 onClick={() => setStep(step + 1)} disabled={!canNext()}>
                {t('setup.next')}
                <Icon name="arrow_forward" size="sm" />
              </ButtonV2>
            </div>
          )}
          {step === 5 && phase === 'editing' && (
            <div className="mt-4 pt-4" style={{ borderTop: '1px solid var(--border)' }}>
              <ButtonV2 variant="ghost" onClick={() => setStep(4)}>
                <Icon name="arrow_back" size="sm" />
                {t('setup.prev')}
              </ButtonV2>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
