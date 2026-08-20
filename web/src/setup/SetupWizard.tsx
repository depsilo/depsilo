import axios from 'axios'
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import Button from '../components/Button'
import EcosystemIcon, { type EcosystemType } from '../components/EcosystemIcon'
import Icon from '../components/Icon'
import Input from '../components/Input'
import LangToggle from '../components/LangToggle'
import Logo from '../components/Logo'
import ThemeToggle from '../components/ThemeToggle'
import { adminApi, authApi, setupApi } from '../lib/api'
import { adminLoginURL } from '../lib/adminLoginDestination'
import { writeLocalStorage } from '../lib/storage'
import { ecosystemDefaults, type UpstreamDefault } from './defaults'

interface SetupWizardProps {
  tokenRequired?: boolean
}

type SetupPhase = 'editing' | 'saving' | 'restarting' | 'failed' | 'ready'
type PasswordIssue =
  | 'required'
  | 'too_short'
  | 'too_long'
  | 'control_character'
  | 'weak'
  | 'contains_username'
  | 'common'
  | null

const reconnectTimeoutMs = 30_000
const reconnectInitialDelayMs = 1_500
const reconnectIntervalMs = 1_000
const reconnectAttemptTimeoutMs = 2_500
const commonPasswords = new Set([
  'adminadminadmin',
  'password123!',
  'change-me-in-production',
  'qwerty123456',
])

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

const onboardingPath = '/admin/connect?new=1'

function usernameValid(username: string) {
  return /^[\p{L}\p{N}][\p{L}\p{N}._-]{2,63}$/u.test(username)
}

function passwordIssue(username: string, password: string): PasswordIssue {
  if (!password) return 'required'
  const characters = Array.from(password).length
  if (characters < 12) return 'too_short'
  if (new TextEncoder().encode(password).length > 72) return 'too_long'
  if (/\p{Cc}/u.test(password)) return 'control_character'

  const classes = [
    /\p{Ll}/u.test(password),
    /\p{Lu}/u.test(password),
    /\p{N}/u.test(password),
    /[^\p{L}\p{N}]/u.test(password),
  ].filter(Boolean).length
  if (characters < 20 && classes < 3) return 'weak'

  const normalizedPassword = password.toLocaleLowerCase()
  if (username && normalizedPassword.includes(username.toLocaleLowerCase())) {
    return 'contains_username'
  }
  if (commonPasswords.has(normalizedPassword)) return 'common'
  return null
}

function validHTTPURL(value: string) {
  try {
    const parsed = new URL(value)
    return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && Boolean(parsed.host)
  } catch {
    return false
  }
}

export default function SetupWizard({ tokenRequired = false }: SetupWizardProps) {
  const { t } = useTranslation()
  const [port, setPort] = useState(23333)
  const [storagePath, setStoragePath] = useState('./data/cache')
  const [adminUsername, setAdminUsername] = useState('admin')
  const [adminPassword, setAdminPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [bootstrapToken, setBootstrapToken] = useState('')
  const [selectedEcosystems, setSelectedEcosystems] = useState<Set<string>>(
    () => new Set(ecosystemDefaults.map((ecosystem) => ecosystem.key)),
  )
  const [upstreams, setUpstreams] = useState<Record<string, UpstreamDefault[]>>(() => {
    const defaults: Record<string, UpstreamDefault[]> = {}
    for (const ecosystem of ecosystemDefaults) {
      defaults[ecosystem.key] = ecosystem.upstreams.map((upstream) => ({ ...upstream }))
    }
    return defaults
  })
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [expandedEcosystem, setExpandedEcosystem] = useState<string | null>(null)
  const [attemptedSubmit, setAttemptedSubmit] = useState(false)
  const [phase, setPhase] = useState<SetupPhase>('editing')
  const [submitError, setSubmitError] = useState('')
  const [reconnectURL, setReconnectURL] = useState('')
  const formRef = useRef<HTMLFormElement>(null)
  const reconnectAbortRef = useRef<AbortController | null>(null)
  const redirectTimerRef = useRef<number | null>(null)

  const submitting = phase === 'saving'
  const reconnecting = phase === 'restarting' || phase === 'ready'
  const selectedList = ecosystemDefaults.filter((ecosystem) => selectedEcosystems.has(ecosystem.key))

  const usernameError = attemptedSubmit && !usernameValid(adminUsername)
    ? t('setup.admin_username_error')
    : undefined
  const currentPasswordIssue = passwordIssue(adminUsername, adminPassword)
  const passwordError = (attemptedSubmit || adminPassword.length > 0) && currentPasswordIssue
    ? t(`setup.password_${currentPasswordIssue}`)
    : undefined
  const confirmPasswordError = attemptedSubmit || confirmPassword.length > 0
    ? !confirmPassword
      ? t('setup.confirm_password_required')
      : adminPassword !== confirmPassword
        ? t('setup.password_mismatch')
        : undefined
    : undefined
  const bootstrapTokenError = attemptedSubmit && tokenRequired && !bootstrapToken.trim()
    ? t('setup.bootstrap_token_required')
    : undefined
  const portError = attemptedSubmit && (port < 1 || port > 65535)
    ? t('setup.port_error')
    : undefined
  const storageError = attemptedSubmit && !storagePath.trim()
    ? t('setup.storage_path_error')
    : undefined

  useEffect(() => () => {
    reconnectAbortRef.current?.abort()
    if (redirectTimerRef.current !== null) window.clearTimeout(redirectTimerRef.current)
  }, [])

  const toggleEcosystem = useCallback((key: string) => {
    setSelectedEcosystems((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  const updateUpstream = useCallback(
    (ecosystemKey: string, index: number, field: keyof UpstreamDefault, value: string | number) => {
      setUpstreams((previous) => {
        const list = [...(previous[ecosystemKey] || [])]
        list[index] = { ...list[index], [field]: value }
        return { ...previous, [ecosystemKey]: list }
      })
    },
    [],
  )

  const addUpstream = useCallback((ecosystemKey: string) => {
    setUpstreams((previous) => {
      const list = [...(previous[ecosystemKey] || [])]
      const priority = list.length > 0 ? Math.max(...list.map((upstream) => upstream.priority)) + 1 : 1
      list.push({ name: '', url: '', priority })
      return { ...previous, [ecosystemKey]: list }
    })
  }, [])

  const removeUpstream = useCallback((ecosystemKey: string, index: number) => {
    setUpstreams((previous) => {
      const list = [...(previous[ecosystemKey] || [])]
      list.splice(index, 1)
      return { ...previous, [ecosystemKey]: list }
    })
  }, [])

  function firstConfigurationIssue() {
    if (port < 1 || port > 65535) return { message: t('setup.port_error') }
    if (!storagePath.trim()) return { message: t('setup.storage_path_error') }
    if (selectedEcosystems.size === 0) return { message: t('setup.ecosystem_required') }

    for (const ecosystem of selectedList) {
      const sources = upstreams[ecosystem.key] || []
      if (sources.length === 0) {
        return {
          message: t('setup.upstream_required', { name: ecosystem.label }),
          ecosystemKey: ecosystem.key,
        }
      }
      for (const source of sources) {
        if (!source.name.trim() || !validHTTPURL(source.url.trim()) || source.priority < 1) {
          return {
            message: t('setup.upstream_invalid', { name: ecosystem.label }),
            ecosystemKey: ecosystem.key,
          }
        }
      }
    }
    return null
  }

  function focusFirstInvalidField() {
    window.requestAnimationFrame(() => {
      formRef.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
    })
  }

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
      let destination = adminLoginURL(target)
      if (new URL(target).origin === window.location.origin) {
        try {
          const response = await authApi.login(
            { username: adminUsername.trim(), password: adminPassword },
            { signal: controller.signal },
          )
          if (writeLocalStorage('token', response.data.token)) {
            let nextPath = onboardingPath
            try {
              const onboarding = await adminApi.getOnboardingStatus({}, { signal: controller.signal })
              if (onboarding.data.status !== 'not_started') nextPath = '/admin'
            } catch {
              // If the optional status lookup is unavailable, keep the safe
              // first-project destination; Admin itself remains fail-open.
            }
            destination = new URL(nextPath, target).toString()
          }
        } catch {
          // Setup succeeded. A failed convenience sign-in must not strand the
          // operator; the login page re-enters through the durable Admin gate.
        }
      }
      if (controller.signal.aborted) return
      redirectTimerRef.current = window.setTimeout(() => window.location.replace(destination), 300)
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

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setAttemptedSubmit(true)
    setSubmitError('')

    const configurationIssue = firstConfigurationIssue()
    const accountInvalid = !usernameValid(adminUsername) || currentPasswordIssue !== null ||
      adminPassword !== confirmPassword || !confirmPassword ||
      (tokenRequired && !bootstrapToken.trim())
    if (accountInvalid || configurationIssue) {
      if (configurationIssue) {
        setAdvancedOpen(true)
        if (configurationIssue.ecosystemKey) setExpandedEcosystem(configurationIssue.ecosystemKey)
      }
      setSubmitError(configurationIssue?.message || '')
      focusFirstInvalidField()
      return
    }

    reconnectAbortRef.current?.abort()
    setPhase('saving')
    setReconnectURL('')
    try {
      const ecosystems: Record<string, { enabled: boolean; upstreams: UpstreamDefault[] }> = {}
      for (const ecosystem of ecosystemDefaults) {
        const enabled = selectedEcosystems.has(ecosystem.key)
        ecosystems[ecosystem.key] = {
          enabled,
          upstreams: enabled ? upstreams[ecosystem.key] || [] : [],
        }
      }

      const response = await setupApi.complete(
        {
          server: { port },
          storage: { path: storagePath.trim() },
          admin: { username: adminUsername, password: adminPassword },
          ecosystems,
        },
        bootstrapToken.trim() || undefined,
      )

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

  function renderReconnectState() {
    if (reconnecting) {
      return (
        <section
          role="status"
          aria-live="polite"
          aria-busy={phase === 'restarting'}
          className="flex min-h-[300px] flex-col items-center justify-center px-5 py-12 text-center"
        >
          <span className="mb-5 grid h-11 w-11 place-items-center rounded-[10px] bg-[var(--brand-soft)] text-[var(--brand-text)]">
            <Icon name={phase === 'ready' ? 'check_circle' : 'sync'} className={phase === 'ready' ? '' : 'animate-spin'} />
          </span>
          <h1 className="text-[24px] font-[650] text-[var(--text)]">
            {phase === 'ready' ? t('setup.ready') : t('setup.restarting')}
          </h1>
          <p className="mt-2 max-w-[52ch] text-[13px] leading-6 text-[var(--text-muted)]">
            {phase === 'ready' ? t('setup.ready_hint') : t('setup.restarting_hint', { url: reconnectURL })}
          </p>
        </section>
      )
    }

    return (
      <section className="flex min-h-[300px] flex-col items-center justify-center px-5 py-12 text-center">
        <span className="mb-5 grid h-11 w-11 place-items-center rounded-[10px] bg-[var(--danger-fill)] text-[var(--danger-text)]">
          <Icon name="warning" />
        </span>
        <h1 className="text-[24px] font-[650] text-[var(--text)]">
          {t('setup.restart_failed_title')}
        </h1>
        <p role="alert" className="mt-2 max-w-[52ch] text-[13px] leading-6 text-[var(--text-muted)]">
          {submitError}
        </p>
        {reconnectURL && (
          <p className="mt-3 max-w-full break-all font-mono text-[12px] text-[var(--text-soft)]">
            {t('setup.reconnect_target', { url: reconnectURL })}
          </p>
        )}
        <Button className="mt-6" type="button" onClick={() => { void monitorReconnect(reconnectURL) }}>
          <Icon name="refresh" size="sm" />
          {t('setup.retry_connection')}
        </Button>
      </section>
    )
  }

  function renderAdvancedSettings() {
    return (
      <details
        open={advancedOpen}
        onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
        className="border-t border-[var(--border)]"
      >
        <summary className="stripe-focus-ring flex min-h-[52px] cursor-pointer list-none items-center gap-3 rounded-[6px] px-1 text-left [&::-webkit-details-marker]:hidden">
          <span className="grid h-8 w-8 shrink-0 place-items-center rounded-[6px] bg-[var(--bg-soft)] text-[var(--text-muted)]">
            <Icon name="tune" size="sm" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block text-[13px] font-[600] text-[var(--text)]">{t('setup.advanced_settings')}</span>
            <span className="mt-0.5 block truncate text-[12px] text-[var(--text-muted)]">
              {t('setup.advanced_summary', { port, count: selectedEcosystems.size })}
            </span>
          </span>
          <Icon
            name="expand_more"
            size="sm"
            className={`shrink-0 transition-transform duration-150 ${advancedOpen ? 'rotate-180' : ''}`}
          />
        </summary>

        <div className="pb-2 pt-5">
          <p className="mb-5 max-w-[68ch] text-[13px] leading-6 text-[var(--text-muted)]">
            {t('setup.advanced_hint')}
          </p>

          <section aria-labelledby="setup-runtime-heading">
            <h2 id="setup-runtime-heading" className="mb-3 text-[14px] font-[600] text-[var(--text)]">
              {t('setup.runtime_settings')}
            </h2>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Input
                label={t('setup.port')}
                error={portError}
                type="number"
                value={port}
                min={1}
                max={65535}
                disabled={submitting}
                onChange={(event) => setPort(Number(event.target.value))}
              />
              <Input
                label={t('setup.storage_path')}
                error={storageError}
                value={storagePath}
                disabled={submitting}
                mono
                onChange={(event) => setStoragePath(event.target.value)}
              />
            </div>
          </section>

          <section aria-labelledby="setup-ecosystems-heading" className="mt-7">
            <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
              <div>
                <h2 id="setup-ecosystems-heading" className="text-[14px] font-[600] text-[var(--text)]">
                  {t('setup.select_ecosystems')}
                </h2>
                <p className="mt-1 text-[12px] text-[var(--text-muted)]">{t('setup.select_ecosystems_hint')}</p>
              </div>
              <span className="font-mono text-[11px] text-[var(--text-soft)]">
                {t('setup.enabled_count', { count: selectedEcosystems.size })}
              </span>
            </div>
            <div className="grid grid-cols-1 gap-2 min-[380px]:grid-cols-2">
              {ecosystemDefaults.map((ecosystem) => {
                const selected = selectedEcosystems.has(ecosystem.key)
                return (
                  <button
                    key={ecosystem.key}
                    type="button"
                    aria-pressed={selected}
                    disabled={submitting}
                    className="stripe-focus-ring flex min-h-[48px] items-center gap-2 rounded-[6px] border px-2.5 text-left transition-[background,border-color,color,transform] duration-150 active:scale-[0.98]"
                    style={{
                      borderColor: selected ? 'var(--brand)' : 'var(--border)',
                      background: selected ? 'var(--brand-soft)' : 'var(--bg-card)',
                      color: selected ? 'var(--brand-text)' : 'var(--text)',
                    }}
                    onClick={() => toggleEcosystem(ecosystem.key)}
                  >
                    <EcosystemIcon type={ecosystem.key as EcosystemType} size={17} decorative />
                    <span className="min-w-0 flex-1 text-[12px] font-[500] leading-4">{ecosystem.label}</span>
                    <Icon name={selected ? 'check' : 'add'} size="sm" />
                  </button>
                )
              })}
            </div>
            {attemptedSubmit && selectedEcosystems.size === 0 && (
              <p role="alert" className="mt-2 text-[12px] text-[var(--danger-text)]">
                {t('setup.ecosystem_required')}
              </p>
            )}
          </section>

          <section aria-labelledby="setup-upstreams-heading" className="mt-7">
            <div className="mb-3">
              <h2 id="setup-upstreams-heading" className="text-[14px] font-[600] text-[var(--text)]">
                {t('setup.configure_upstreams')}
              </h2>
              <p className="mt-1 text-[12px] text-[var(--text-muted)]">{t('setup.configure_upstreams_hint')}</p>
            </div>
            <div className="space-y-2">
              {selectedList.map((ecosystem) => {
                const expanded = expandedEcosystem === ecosystem.key
                const ecosystemUpstreams = upstreams[ecosystem.key] || []
                const panelId = `setup-${ecosystem.key}-upstreams`
                return (
                  <div key={ecosystem.key} className="overflow-hidden rounded-[6px] border border-[var(--border)]">
                    <button
                      type="button"
                      aria-expanded={expanded}
                      aria-controls={panelId}
                      disabled={submitting}
                      className="stripe-focus-ring flex min-h-[44px] w-full items-center gap-2.5 px-3 text-left text-[var(--text)] transition-colors duration-150 hover:bg-[var(--bg-hover)]"
                      onClick={() => setExpandedEcosystem(expanded ? null : ecosystem.key)}
                    >
                      <EcosystemIcon type={ecosystem.key as EcosystemType} size={16} decorative />
                      <span className="min-w-0 flex-1 truncate text-[13px] font-[500]">{ecosystem.label}</span>
                      <span className="text-[11px] text-[var(--text-muted)]">
                        {t('setup.upstreams_count_value', { count: ecosystemUpstreams.length })}
                      </span>
                      <Icon name="expand_more" size="sm" className={`transition-transform duration-150 ${expanded ? 'rotate-180' : ''}`} />
                    </button>
                    {expanded && (
                      <div id={panelId} className="space-y-4 border-t border-[var(--border)] bg-[var(--bg-soft)] p-3 sm:p-4">
                        {ecosystemUpstreams.map((upstream, index) => (
                          <div
                            key={`${ecosystem.key}-${index}`}
                            className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,0.8fr)_minmax(0,1.5fr)_80px_40px] sm:items-end"
                          >
                            <Input
                              label={t('setup.upstream_name')}
                              value={upstream.name}
                              disabled={submitting}
                              onChange={(event) => updateUpstream(ecosystem.key, index, 'name', event.target.value)}
                            />
                            <Input
                              label={t('setup.upstream_url')}
                              value={upstream.url}
                              disabled={submitting}
                              mono
                              onChange={(event) => updateUpstream(ecosystem.key, index, 'url', event.target.value)}
                            />
                            <Input
                              label={t('setup.priority')}
                              type="number"
                              value={upstream.priority}
                              min={1}
                              disabled={submitting}
                              onChange={(event) => updateUpstream(ecosystem.key, index, 'priority', Number(event.target.value))}
                            />
                            <Button
                              type="button"
                              variant="danger"
                              size="sm"
                              className="min-h-[40px] min-w-[40px] px-2"
                              disabled={submitting}
                              aria-label={t('setup.remove_upstream', {
                                name: upstream.name || `${ecosystem.label} ${index + 1}`,
                              })}
                              onClick={() => removeUpstream(ecosystem.key, index)}
                            >
                              <Icon name="delete" size="sm" />
                            </Button>
                          </div>
                        ))}
                        <Button
                          type="button"
                          variant="secondary"
                          size="sm"
                          disabled={submitting}
                          onClick={() => addUpstream(ecosystem.key)}
                        >
                          <Icon name="add" size="sm" />
                          {t('setup.add_upstream')}
                        </Button>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </section>
        </div>
      </details>
    )
  }

  const reconnectFailure = phase === 'failed' && Boolean(reconnectURL)

  return (
    <main className="min-h-[100dvh] bg-[var(--bg-page)] px-4 py-5 sm:px-6 sm:py-8">
      <div className="mx-auto w-full max-w-[720px]">
        <header className="mb-5 flex min-h-[40px] items-center justify-between gap-4">
          <div className="flex items-center gap-2.5 text-[var(--text)]">
            <span className="text-[var(--brand-text)]"><Logo size={29} /></span>
            <span className="text-[16px] font-[650]">Depsilo</span>
          </div>
          <div className="flex items-center gap-1">
            <LangToggle />
            <ThemeToggle />
          </div>
        </header>

        <div
          data-setup-surface="single-page"
          className="overflow-hidden rounded-[var(--r-card)] border border-[var(--border)] bg-[var(--bg-card)] shadow-[var(--shadow-card)]"
        >
          {reconnecting || reconnectFailure ? renderReconnectState() : (
            <form ref={formRef} noValidate onSubmit={handleSubmit}>
              <div className="px-5 pb-6 pt-6 sm:px-8 sm:pb-8 sm:pt-8">
                <header className="mb-7">
                  <h1 className="text-balance text-[26px] font-[680] leading-tight text-[var(--text)] sm:text-[30px]">
                    {t('setup.title')}
                  </h1>
                  <p className="mt-2 max-w-[62ch] text-[13px] leading-6 text-[var(--text-muted)]">
                    {t('setup.description')}
                  </p>
                </header>

                <section aria-labelledby="setup-admin-heading" className="mb-7">
                  <h2 id="setup-admin-heading" className="text-[15px] font-[600] text-[var(--text)]">
                    {t('setup.admin_account')}
                  </h2>
                  <p className="mb-4 mt-1 text-[12px] leading-5 text-[var(--text-muted)]">
                    {t('setup.admin_account_hint')}
                  </p>
                  <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                    <Input
                      label={t('setup.admin_username')}
                      error={usernameError}
                      autoComplete="username"
                      value={adminUsername}
                      disabled={submitting}
                      onChange={(event) => setAdminUsername(event.target.value)}
                    />
                    {tokenRequired && (
                      <Input
                        label={t('setup.bootstrap_token')}
                        hint={bootstrapTokenError ? undefined : t('setup.bootstrap_token_hint')}
                        error={bootstrapTokenError}
                        type="password"
                        autoComplete="off"
                        mono
                        value={bootstrapToken}
                        disabled={submitting}
                        onChange={(event) => setBootstrapToken(event.target.value)}
                      />
                    )}
                    <Input
                      label={t('setup.admin_password')}
                      hint={passwordError ? undefined : t('setup.admin_password_hint')}
                      error={passwordError}
                      type="password"
                      autoComplete="new-password"
                      value={adminPassword}
                      disabled={submitting}
                      onChange={(event) => setAdminPassword(event.target.value)}
                    />
                    <Input
                      label={t('setup.confirm_password')}
                      error={confirmPasswordError}
                      type="password"
                      autoComplete="new-password"
                      value={confirmPassword}
                      disabled={submitting}
                      onChange={(event) => setConfirmPassword(event.target.value)}
                    />
                  </div>
                </section>

                {renderAdvancedSettings()}

                {submitError && !reconnectURL && (
                  <p role="alert" className="mt-5 rounded-[6px] bg-[var(--danger-fill)] px-3 py-2.5 text-[12px] leading-5 text-[var(--danger-text)]">
                    {submitError}
                  </p>
                )}

                <div className="mt-6 flex flex-col gap-3 border-t border-[var(--border)] pt-5 sm:flex-row sm:items-center sm:justify-between">
                  <p className="text-[12px] leading-5 text-[var(--text-muted)]">
                    {t('setup.submit_hint')}
                  </p>
                  <Button
                    type="submit"
                    disabled={submitting}
                    aria-busy={submitting || undefined}
                    className="min-h-[40px] w-full shrink-0 sm:w-auto"
                  >
                    {submitting ? (
                      <Icon name="sync" size="sm" className="animate-spin" />
                    ) : (
                      <Icon name="arrow_forward" size="sm" />
                    )}
                    {submitting ? t('setup.saving') : t('setup.save_and_start')}
                  </Button>
                </div>
              </div>
            </form>
          )}
        </div>
      </div>
    </main>
  )
}
