import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { getAdminRouteHref } from '@/admin/routes'
import Button from '@/components/Button'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import { adminApi } from '@/lib/api'
import type { OnboardingEvent, OnboardingStatus, OnboardingStatusResponse } from '@/lib/adminApi.types'
import { LANGUAGES, ONBOARDING_LANGUAGES } from '@/lib/ecosystemData'
import {
  advanceOnboardingSession,
  ONBOARDING_GATE_QUERY_KEY,
  onboardingOutcomeKind,
  ONBOARDING_SESSION_KEY,
  parseOnboardingSession,
  startOnboardingSession,
  type OnboardingSession,
} from '@/lib/onboarding'
import { renderManagerTemplate, resolveServiceOrigin } from '@/lib/packageManagerConfig'
import {
  readSessionStorage,
  removeSessionStorage,
  writeSessionStorage,
} from '@/lib/storage'
import CodeBlock from '@/portal/components/CodeBlock'

const pollIntervalMs = 2_500
const troubleshootingDelayMs = 30_000

function eventLabel(event: OnboardingEvent) {
  return [event.package_name, event.version].filter(Boolean).join('@')
}

function eventEcosystemLabel(event: OnboardingEvent) {
  return operatorEcosystems.find(ecosystem => ecosystem.id === event.ecosystem)?.label ?? event.ecosystem
}

function restoredOnboardingSession(): OnboardingSession | null {
  const navigation = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
  let loadedPath = ''
  try {
    loadedPath = navigation?.name ? new URL(navigation.name, window.location.href).pathname : ''
  } catch {
    loadedPath = ''
  }
  if (navigation?.type !== 'reload' || loadedPath !== getAdminRouteHref('connect')) {
    removeSessionStorage(ONBOARDING_SESSION_KEY)
    return null
  }
  return parseOnboardingSession(readSessionStorage(ONBOARDING_SESSION_KEY))
}

export default function ConnectProject() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [showAll, setShowAll] = useState(false)
  const [languageId, setLanguageId] = useState(ONBOARDING_LANGUAGES[0]?.id ?? LANGUAGES[0]?.id ?? '')
  const language = LANGUAGES.find(candidate => candidate.id === languageId) ?? LANGUAGES[0]
  const [managerId, setManagerId] = useState(language?.managers[0]?.id ?? '')
  const manager = language?.managers.find(candidate => candidate.id === managerId) ?? language?.managers[0]
  // Reloads resume the same baseline. Every new/manual route entry starts a
  // fresh cursor so a historical HIT can never complete a new walkthrough.
  const [session, setSession] = useState<OnboardingSession | null>(restoredOnboardingSession)
  const [durableStatus, setDurableStatus] = useState<OnboardingStatus | null>(() => (
    queryClient.getQueryData<OnboardingStatusResponse>(ONBOARDING_GATE_QUERY_KEY)?.status ?? null
  ))
  const sessionRef = useRef(session)
  const completionStartedRef = useRef(Boolean(session?.firstRequest))
  const [initialError, setInitialError] = useState<unknown>(null)
  const [pollError, setPollError] = useState(false)
  const [savingChoice, setSavingChoice] = useState(false)
  const [choiceError, setChoiceError] = useState(false)
  const [showTroubleshooting, setShowTroubleshooting] = useState(Boolean(session?.firstRequest))
  const [announcement, setAnnouncement] = useState('')

  const endpoint = resolveServiceOrigin()
  const visibleLanguages = showAll ? LANGUAGES : ONBOARDING_LANGUAGES
  const firstRequest = session?.firstRequest
  const firstCacheHit = session?.firstCacheHit
  const sessionActive = Boolean(session && !firstCacheHit)

  useEffect(() => {
    setManagerId(language?.managers[0]?.id ?? '')
  }, [language?.id, language?.managers])

  const toggleAllEcosystems = () => {
    setShowAll(current => {
      if (current && !ONBOARDING_LANGUAGES.some(candidate => candidate.id === languageId)) {
        setLanguageId(ONBOARDING_LANGUAGES[0]?.id ?? LANGUAGES[0]?.id ?? '')
      }
      return !current
    })
  }

  useEffect(() => {
    sessionRef.current = session
    if (session) writeSessionStorage(ONBOARDING_SESSION_KEY, JSON.stringify(session))
  }, [session])

  useEffect(() => () => {
    window.setTimeout(() => {
      if (window.location.pathname !== getAdminRouteHref('connect')) {
        removeSessionStorage(ONBOARDING_SESSION_KEY)
      }
    }, 0)
  }, [])

  const recordTerminalStatus = useCallback(async (status: 'completed' | 'skipped') => {
    await adminApi.updateOnboardingStatus(status)
    setDurableStatus(status)
    queryClient.setQueryData(
      ONBOARDING_GATE_QUERY_KEY,
      (previous: OnboardingStatusResponse | undefined) => previous ? { ...previous, status } : previous,
    )
  }, [queryClient])

  const acceptResponse = useCallback((response: OnboardingStatusResponse) => {
    const current = sessionRef.current
    if (!current) return
    const next = advanceOnboardingSession(current, response)
    const receivedFirstRequest = !current.firstRequest && Boolean(next.firstRequest)
    const receivedFirstHit = !current.firstCacheHit && Boolean(next.firstCacheHit)
    sessionRef.current = next
    setSession(next)
    setPollError(false)

    if (receivedFirstRequest) {
      setAnnouncement(t('onboarding.requestReceivedAnnouncement'))
      if (!completionStartedRef.current) {
        completionStartedRef.current = true
        void recordTerminalStatus('completed').catch(() => setChoiceError(true))
      }
    }
    if (receivedFirstHit) setAnnouncement(t('onboarding.cacheHitAnnouncement'))
  }, [recordTerminalStatus, t])

  const startSession = useCallback(async (signal?: AbortSignal) => {
    setInitialError(null)
    try {
      const response = await adminApi.getOnboardingStatus({}, { signal })
      const next = startOnboardingSession(response.data)
      setDurableStatus(response.data.status)
      sessionRef.current = next
      setSession(next)
    } catch (error) {
      if (!signal?.aborted) setInitialError(error)
    }
  }, [])

  useEffect(() => {
    if (session) return
    const controller = new AbortController()
    void startSession(controller.signal)
    return () => controller.abort()
  }, [session, startSession])

  useEffect(() => {
    if (!sessionActive) return
    const controller = new AbortController()
    let timer: number | undefined
    let running = false

    const schedule = (delay = pollIntervalMs) => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => { void poll() }, delay)
    }
    const poll = async () => {
      if (controller.signal.aborted || running) return
      if (document.hidden) {
        return
      }
      const current = sessionRef.current
      if (!current || current.firstCacheHit) return
      running = true
      try {
        const response = await adminApi.getOnboardingStatus({
          after_id: current.afterId,
          started_at: current.startedAt,
        }, { signal: controller.signal })
        acceptResponse(response.data)
        if (!response.data.events.some(event => event.outcome === 'hit')) {
          schedule(response.data.has_more ? 0 : pollIntervalMs)
        }
      } catch {
        if (!controller.signal.aborted) {
          setPollError(true)
          schedule()
        }
      } finally {
        running = false
      }
    }
    const handleVisibility = () => {
      if (!document.hidden) {
        window.clearTimeout(timer)
        void poll()
      }
    }

    document.addEventListener('visibilitychange', handleVisibility)
    schedule(0)
    return () => {
      controller.abort()
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [acceptResponse, sessionActive])

  useEffect(() => {
    if (firstRequest) return
    const timer = window.setTimeout(() => setShowTroubleshooting(true), troubleshootingDelayMs)
    return () => window.clearTimeout(timer)
  }, [firstRequest])

  const openDashboard = (status: 'completed' | 'skipped') => {
    // This in-memory value keeps a stale not_started gate from trapping the
    // operator when the optional onboarding API is temporarily unavailable.
    // The durable server value is still rechecked in a later browser session.
    queryClient.setQueryData<OnboardingStatusResponse>(
      ONBOARDING_GATE_QUERY_KEY,
      previous => previous
        ? { ...previous, status }
        : {
            status,
            started_at: new Date().toISOString(),
            next_after_id: 0,
            events: [],
            has_more: false,
          },
    )
    removeSessionStorage(ONBOARDING_SESSION_KEY)
    navigate(getAdminRouteHref('dashboard'), { replace: true })
  }

  const finish = async () => {
    setSavingChoice(true)
    setChoiceError(false)
    const status = firstRequest ? 'completed' : 'skipped'
    try {
      if (durableStatus === 'not_started' || durableStatus === null) {
        await recordTerminalStatus(status)
      }
    } catch {
      // Onboarding is optional. A failed preference write must never block
      // access to the operational Dashboard.
    } finally {
      setSavingChoice(false)
    }
    openDashboard(status)
  }

  const requestKind = firstRequest ? onboardingOutcomeKind(firstRequest) : null
  const configuration = manager?.configure ?? manager?.persistent
  const config = useMemo(() => configuration ? renderManagerTemplate(configuration.body, endpoint) : '', [configuration, endpoint])
  const testCommand = useMemo(() => manager?.test ? renderManagerTemplate(manager.test.body, endpoint) : null, [endpoint, manager])

  if (initialError && !session) {
    return (
      <AdminPage
        width="readable"
        title={t('onboarding.title')}
        actions={(
          <Button className="whitespace-nowrap" onClick={() => openDashboard('skipped')}>
            {t('onboarding.goDashboard')}
          </Button>
        )}
      >
        <QueryErrorState message={t('onboarding.loadError')} onRetry={() => { void startSession() }} />
      </AdminPage>
    )
  }

  return (
    <AdminPage
      width="readable"
      title={t('onboarding.title')}
      description={t('onboarding.subtitle')}
      actions={(
        <Button className="whitespace-nowrap" variant="ghost" onClick={() => { void finish() }} disabled={savingChoice}>
          {t('onboarding.goDashboard')}
        </Button>
      )}
    >
      <ol aria-label={t('onboarding.progressLabel')} className="mb-7 grid grid-cols-3 border-y border-[var(--border)] py-3">
        {(['account', 'connect', 'verify'] as const).map((step, index) => {
          const complete = step === 'account' || (step === 'connect' && Boolean(firstRequest)) || (step === 'verify' && Boolean(firstCacheHit))
          const current = (step === 'connect' && !firstRequest) || (step === 'verify' && Boolean(firstRequest) && !firstCacheHit)
          return (
            <li key={step} aria-current={current ? 'step' : undefined} className="flex min-w-0 items-center gap-2 text-[12px] text-[var(--text-muted)]">
              <span className={`grid size-6 shrink-0 place-items-center rounded-full font-mono text-[11px] ${complete || current ? 'bg-[var(--brand-soft)] text-[var(--brand-text)]' : 'bg-[var(--bg-soft)]'}`}>
                {complete ? <Icon name="check" size="sm" /> : index + 1}
              </span>
              <span className="truncate">{t(`onboarding.steps.${step}`)}</span>
            </li>
          )
        })}
      </ol>

      <section aria-labelledby="onboarding-ecosystem-title">
        <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
          <div>
            <h2 id="onboarding-ecosystem-title" className="text-[16px] font-[660] text-[var(--text)]">{t('onboarding.chooseEcosystem')}</h2>
            <p className="mt-1 text-[13px] text-[var(--text-muted)]">{t('onboarding.chooseEcosystemHint')}</p>
          </div>
          <button type="button" className="stripe-focus-ring min-h-10 rounded-[5px] px-2 text-[12px] font-[600] text-[var(--brand-text)] hover:bg-[var(--bg-hover)]" onClick={toggleAllEcosystems} aria-expanded={showAll}>
            {showAll ? t('onboarding.showFeatured') : t('onboarding.viewAll')}
          </button>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
          {visibleLanguages.map(candidate => (
            <button
              key={candidate.id}
              type="button"
              aria-pressed={candidate.id === language?.id}
              onClick={() => setLanguageId(candidate.id)}
              className="stripe-focus-ring flex min-h-12 min-w-0 items-center gap-2 rounded-[7px] border px-3 text-left text-[13px] font-[600] transition-[background,border-color,color] duration-150"
              style={{
                background: candidate.id === language?.id ? 'var(--brand-soft)' : 'var(--bg-card)',
                borderColor: candidate.id === language?.id ? 'var(--brand-border)' : 'var(--border)',
                color: candidate.id === language?.id ? 'var(--brand-text)' : 'var(--text)',
              }}
            >
              <EcosystemIcon type={candidate.iconAdapter} size={18} useColor />
              <span className="truncate">{candidate.name}</span>
            </button>
          ))}
        </div>
      </section>

      {language && manager && (
        <section aria-labelledby="onboarding-manager-title" className="mt-7 border-t border-[var(--border)] pt-6">
          <h2 id="onboarding-manager-title" className="text-[16px] font-[660] text-[var(--text)]">{t('onboarding.chooseManager', { ecosystem: language.name })}</h2>
          <div role="group" aria-label={t('onboarding.managerLabel')} className="mt-3 flex max-w-full gap-1 overflow-x-auto rounded-[7px] bg-[var(--bg-soft)] p-1">
            {language.managers.map(candidate => (
              <button key={candidate.id} type="button" aria-pressed={candidate.id === manager.id} onClick={() => setManagerId(candidate.id)} className="stripe-focus-ring min-h-10 shrink-0 rounded-[5px] px-3 text-[13px] font-[600]" style={{ background: candidate.id === manager.id ? 'var(--bg-card)' : 'transparent', color: candidate.id === manager.id ? 'var(--brand-text)' : 'var(--text-muted)', boxShadow: candidate.id === manager.id ? 'var(--shadow-surface)' : 'none' }}>
                {candidate.name}
              </button>
            ))}
          </div>

          <div className="mt-6 space-y-6">
            <div className="min-w-0">
              <h3 className="text-[14px] font-[650] text-[var(--text)]">{t('onboarding.configureManager', { manager: manager.name })}</h3>
              <p className="mb-3 mt-1 break-words text-[12px] text-[var(--text-muted)]">
                {manager.configure ? t('onboarding.configCommand') : t('onboarding.configFile', { file: manager.persistent.file })}
              </p>
              <CodeBlock filename={manager.configure ? undefined : manager.persistent.file} code={config} language={configuration.lang} copyName={t('onboarding.configuration')} tone="ink" />
            </div>
            <div className="min-w-0">
              <h3 className="text-[14px] font-[650] text-[var(--text)]">{t('onboarding.testIt')}</h3>
              <p className={`${testCommand ? 'mb-3' : 'mb-0'} mt-1 text-[12px] leading-5 text-[var(--text-muted)]`}>
                {testCommand ? t('onboarding.testHint') : t('onboarding.normalDependencyCommand')}
              </p>
              {testCommand && manager.test && (
                <CodeBlock code={testCommand} language={manager.test.lang} copyName={t('onboarding.testCommand')} />
              )}
            </div>
          </div>
        </section>
      )}

      <section aria-label={t('onboarding.steps.verify')} className="mt-7 border-t border-[var(--border)] pt-6">
        <div className="rounded-[8px] border border-[var(--border-strong)] bg-[var(--bg-card)] p-4 sm:p-5">
          {!session ? (
            <div role="status" aria-busy="true" className="flex items-center gap-3 text-[13px] text-[var(--text-muted)]">
              <Icon name="progress_activity" size="sm" className="animate-spin motion-reduce:animate-none" />
              {t('onboarding.preparing')}
            </div>
          ) : firstCacheHit ? (
            <div>
              <div className="flex items-center gap-2 text-[var(--ok-text)]"><Icon name="check_circle" /><h2 id="onboarding-verify-title" className="text-[17px] font-[680]">{t('onboarding.firstCacheHit')}</h2></div>
              <p className="mt-2 font-mono text-[13px] text-[var(--text)]">{eventLabel(firstCacheHit)}</p>
              <p className="mt-1 text-[13px] text-[var(--text-muted)]">{t('onboarding.servedFromCache')}</p>
              <p className="mt-4 text-[13px] font-[600] text-[var(--text)]">{t('onboarding.ready')}</p>
            </div>
          ) : firstRequest ? (
            <div>
              <div className="flex items-center gap-2 text-[var(--ok-text)]"><Icon name="check_circle" /><h2 id="onboarding-verify-title" className="text-[17px] font-[680]">{t('onboarding.firstRequest')}</h2></div>
              <dl className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-[13px]">
                <dt className="text-[var(--text-subtle)]">{t('onboarding.package')}</dt><dd className="min-w-0 truncate font-mono text-[var(--text)]">{eventLabel(firstRequest)}</dd>
                <dt className="text-[var(--text-subtle)]">{t('onboarding.ecosystem')}</dt><dd className="text-[var(--text)]">{eventEcosystemLabel(firstRequest)}</dd>
                <dt className="text-[var(--text-subtle)]">{t('onboarding.result')}</dt><dd className="text-[var(--text)]">{t(`onboarding.outcomes.${requestKind}`)}</dd>
              </dl>
              <p className="mt-4 text-[13px] text-[var(--text-muted)]">{requestKind === 'blocked' ? t('onboarding.blockedHint') : requestKind === 'error' ? t('onboarding.errorHint') : testCommand ? t('onboarding.runAgain') : t('onboarding.runAgainNormal')}</p>
              {requestKind === 'error' && (
                <Link to={getAdminRouteHref('accessLogs')} className="stripe-focus-ring mt-3 inline-flex min-h-10 items-center rounded-[5px] px-2 text-[12px] font-[650] text-[var(--brand-text)] no-underline hover:bg-[var(--bg-hover)]">
                  {t('onboarding.viewAccessLogs')}
                </Link>
              )}
            </div>
          ) : (
            <div>
              <div className="flex items-center gap-3">
                <span aria-hidden="true" className="size-2 rounded-full bg-[var(--brand)] animate-pulse motion-reduce:animate-none" />
                <h2 id="onboarding-verify-title" className="text-[17px] font-[680] text-[var(--text)]">{t('onboarding.waitingTitle')}</h2>
              </div>
              <p className="mt-2 text-[13px] text-[var(--text-muted)]">{testCommand ? t('onboarding.waitingHint') : t('onboarding.waitingNormalHint')}</p>
              {showTroubleshooting && <p className="mt-4 border-t border-[var(--border)] pt-4 text-[12px] text-[var(--text-muted)]">{t('onboarding.nothingYet')}</p>}
            </div>
          )}
          {pollError && <p role="status" className="mt-3 text-[12px] text-[var(--warn-text)]">{t('onboarding.pollError')}</p>}
        </div>
      </section>

      <p className="sr-only" aria-live="polite" aria-atomic="true">{announcement}</p>
      {choiceError && <p role="alert" className="mt-4 text-[12px] text-[var(--danger-text)]">{t('onboarding.saveError')}</p>}
      <div className="mt-5 flex justify-end">
        <Button onClick={() => { void finish() }} disabled={savingChoice}>
          {savingChoice ? t('saving') : firstCacheHit ? t('onboarding.goDashboard') : t('onboarding.continueDashboard')}
          <Icon name="arrow_forward" size="sm" />
        </Button>
      </div>
    </AdminPage>
  )
}
