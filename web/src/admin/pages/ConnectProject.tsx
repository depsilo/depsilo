import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'
import AdminPage from '@/admin/components/AdminPage'
import DockerBootstrap from '@/admin/components/DockerBootstrap'
import { getAdminRouteHref } from '@/admin/routes'
import { useConnectionVerification } from '@/admin/useConnectionVerification'
import { Button } from '@/components/ui/button'
import Icon from '@/components/Icon'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { adminApi } from '@/lib/api'
import type { OnboardingStatusResponse } from '@/lib/adminApi.types'
import {
  CLIENT_CONNECTION_LANGUAGES, getClientConnection,
  type ClientConnection, type ConnectionShell, type PipScope,
} from '@/lib/clientConnection'
import {
  ONBOARDING_GATE_QUERY_KEY, ONBOARDING_SESSION_KEY, onboardingOutcomeKind,
  parseOnboardingSession, type OnboardingSession,
} from '@/lib/onboarding'
import { resolveServiceOrigin } from '@/lib/packageManagerConfig'
import { getReadiness } from '@/lib/readiness'
import { readSessionStorage, removeSessionStorage } from '@/lib/storage'
import CodeBlock from '@/portal/components/CodeBlock'
import './ConnectProject.css'

function restoreSession(): OnboardingSession | null {
  const navigation = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
  try {
    if (navigation?.type === 'reload' && new URL(navigation.name).pathname === getAdminRouteHref('connect')) {
      return parseOnboardingSession(readSessionStorage(ONBOARDING_SESSION_KEY))
    }
  } catch { /* Invalid navigation metadata starts a fresh observation. */ }
  removeSessionStorage(ONBOARDING_SESSION_KEY)
  return null
}

function ConnectionHealth({ ecosystem, onNavigate }: { ecosystem: string; onNavigate: (destination: 'dashboard' | 'upstreams') => void }) {
  const { t } = useTranslation()
  const ready = useQuery({ queryKey: ['admin', 'readiness'], queryFn: async ({ signal }) => (await getReadiness({ signal })).data, refetchInterval: 30_000, retry: false })
  const upstreams = useQuery({ queryKey: ['admin', 'connect-upstreams'], queryFn: async ({ signal }) => (await adminApi.listUpstreams({ signal })).data, refetchInterval: 30_000, retry: false })
  const affected = upstreams.data?.items.filter(upstream => upstream.adapter_type === ecosystem && upstream.healthy === false)
  return <>
    {ready.data?.status === 'not_ready' && <p role="alert" className="connection-notice connection-danger"><Icon name="warning" size="sm" /><span>
      {ready.isError && `${t('onboarding.lastKnownHealth')} `}
      {t('onboarding.proxyUnavailable')} <button type="button" onClick={() => onNavigate('dashboard')}>{t('onboarding.viewServiceStatus')}</button>
    </span></p>}
    {Boolean(affected?.length) && <p className="connection-notice connection-warning"><Icon name="warning" size="sm" /><span>
      {upstreams.isError && `${t('onboarding.lastKnownHealth')} `}
      {t('onboarding.upstreamAttention', { names: affected!.map(upstream => upstream.name).join(', ') })} <button type="button" onClick={() => onNavigate('upstreams')}>{t('onboarding.viewUpstreams')}</button>
    </span></p>}
    {(ready.isError || upstreams.isError) && <p className="connection-note">{t('onboarding.healthUnknown')} <button type="button" onClick={() => onNavigate('dashboard')}>{t('onboarding.viewServiceStatus')}</button></p>}
  </>
}

function ConnectionSelect<Value extends string>({ label, value, options, disabled, onValueChange }: {
  label: string; value: Value; options: { value: Value; label: string }[]
  disabled: boolean; onValueChange: (value: Value) => void
}) {
  const id = useId()
  return <div>
    <Label htmlFor={id} className="mb-1 block">{label}</Label>
    <Select value={value} items={options} disabled={disabled} onValueChange={next => {
      if (next !== null && next !== value) onValueChange(next)
    }}>
      <SelectTrigger id={id} className="min-h-10 min-w-0 text-foreground">
        <SelectValue className="min-w-0 whitespace-normal break-words" />
      </SelectTrigger>
      <SelectContent>
        {options.map(option => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
      </SelectContent>
    </Select>
  </div>
}

export default function ConnectProject() {
  const { t } = useTranslation()
  const [restored] = useState(restoreSession)
  const [selection, setSelection] = useState(() => ({
    languageId: restored?.languageId ?? 'python', managerId: restored?.managerId ?? 'pip',
    pipScope: (restored?.configurationScope === 'temporary' ? 'temporary' : restored?.configurationScope === 'venv' ? 'venv' : 'user') as PipScope,
    shell: (restored?.shell === 'powershell' ? 'powershell' : 'posix') as ConnectionShell,
  }))
  const [attempt, setAttempt] = useState(0)
  const endpoint = resolveServiceOrigin()
  const language = CLIENT_CONNECTION_LANGUAGES.find(language => language.id === selection.languageId)!
  const manager = language.managers.find(manager => manager.id === selection.managerId)!
  const connection = useMemo(() => getClientConnection(selection.languageId, selection.managerId, endpoint, selection)!, [selection, endpoint])
  const key = [selection.languageId, selection.managerId, connection.scope.id, connection.shell, attempt].join(':')
  const restoredMatches = restored?.languageId === selection.languageId && restored.managerId === selection.managerId &&
    restored.configurationScope === connection.scope.id && restored.shell === connection.shell &&
    restored.targetPackage === connection.test.targetPackage && restored.targetVersion === connection.test.targetVersion && attempt === 0
  const select = (next: typeof selection) => {
    removeSessionStorage(ONBOARDING_SESSION_KEY)
    setAttempt(value => value + 1)
    setSelection(next)
  }
  return <div className="connection-page">
    <ConnectionWorkspace observationId={key} connection={connection} restored={restoredMatches ? restored : null}
      managerName={manager.name} onRestart={() => { removeSessionStorage(ONBOARDING_SESSION_KEY); setAttempt(value => value + 1) }}
      selectors={disabled => <section className="connection-selector" aria-label={t('onboarding.chooseClient')}>
        <div className="connection-selector-fields">
          <ConnectionSelect label={t('onboarding.ecosystem')} value={selection.languageId} disabled={disabled}
            options={CLIENT_CONNECTION_LANGUAGES.map(language => ({ value: language.id, label: language.name }))} onValueChange={value => {
            const language = CLIENT_CONNECTION_LANGUAGES.find(language => language.id === value)!
            select({ ...selection, languageId: language.id, managerId: language.managers[0].id })
          }} />
          <ConnectionSelect label={t('onboarding.managerLabel')} value={selection.managerId} disabled={disabled}
            options={language.managers.map(manager => ({ value: manager.id, label: manager.name }))}
            onValueChange={value => select({ ...selection, managerId: value })} />
          {selection.managerId === 'pip' && <ConnectionSelect<PipScope> label={t('onboarding.configurationScope')} value={selection.pipScope} disabled={disabled}
            options={[{ value: 'temporary', label: t('onboarding.scopeTemporary') }, { value: 'user', label: t('onboarding.scopeUser') }, { value: 'venv', label: t('onboarding.scopeVenv') }]}
            onValueChange={value => select({ ...selection, pipScope: value })} />}
          {connection.shellOptions.length > 1 && <ConnectionSelect<ConnectionShell> label={t('onboarding.shell')} value={connection.shell} disabled={disabled}
            options={[{ value: 'posix', label: t('onboarding.shellPosix') }, { value: 'powershell', label: 'PowerShell' }]}
            onValueChange={value => select({ ...selection, shell: value })} />}
        </div>
        <p className="connection-origin"><span>{t('onboarding.instanceAddress')}</span><code>{endpoint}</code></p>
      </section>} />
  </div>
}

function ConnectionWorkspace({ connection, restored, managerName, selectors, onRestart, observationId }: {
  connection: ClientConnection; restored: OnboardingSession | null; managerName: string
  selectors: (disabled: boolean) => React.ReactNode; onRestart: () => void; observationId: string
}) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [leaving, setLeaving] = useState(false)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const verification = useConnectionVerification({
    ...connection.selection, configurationScope: connection.scope.id, shell: connection.shell,
    targetPackage: connection.test.targetPackage, targetVersion: connection.test.targetVersion,
  }, restored, observationId, leaving)
  const { session } = verification
  const completedRequest = useRef<string | null>(null)
  const save = useMutation({
    scope: { id: 'onboarding-preference' },
    mutationFn: (status: 'completed' | 'skipped') => adminApi.updateOnboardingStatus(status),
    onSuccess: response => queryClient.setQueryData<OnboardingStatusResponse>(ONBOARDING_GATE_QUERY_KEY,
      previous => previous ? { ...previous, status: response.data.status } : previous),
  })
  const complete = save.mutate
  const resetSave = save.reset
  useEffect(() => { resetSave() }, [observationId, resetSave])
  useEffect(() => {
    const request = session?.successfulDownload?.request_id
    const completion = request ? `${observationId}:${request}` : null
    if (!leaving && completion && completedRequest.current !== completion) {
      completedRequest.current = completion
      if (verification.data?.status !== 'completed') complete('completed')
    }
  }, [observationId, session?.successfulDownload?.request_id, verification.data?.status, complete, leaving])

  const leave = async (destination: 'dashboard' | 'upstreams' | 'accessLogs' = 'dashboard') => {
    if (leaving) return
    setLeaving(true)
    await verification.stop()
    const durable = queryClient.getQueryData<OnboardingStatusResponse>(ONBOARDING_GATE_QUERY_KEY)?.status ?? verification.data?.status
    let status = durable === 'completed' || session?.successfulDownload ? 'completed' as const : 'skipped' as const
    try {
      if (durable !== status) status = (await save.mutateAsync(status)).data.status
    } catch { /* Optional onboarding preferences never trap an operator. */ }
    if (!mounted.current) return
    queryClient.setQueryData<OnboardingStatusResponse>(ONBOARDING_GATE_QUERY_KEY, previous => previous
      ? { ...previous, status }
      : { status, started_at: new Date().toISOString(), next_after_id: 0, events: [], has_more: false })
    for (const area of ['overview', 'trends', 'now', 'dashboard']) {
      void queryClient.invalidateQueries({ queryKey: ['admin', area], refetchType: 'none' })
    }
    removeSessionStorage(ONBOARDING_SESSION_KEY)
    navigate(getAdminRouteHref(destination), { replace: true })
  }
  const configuration = connection.configuration?.command ?? connection.configuration
  const temporary = connection.scope.id === 'temporary'
  const latestRequest = session?.latestRequest
  const latestDelivery = session?.latestDelivery
  const latest = latestDelivery && (!latestRequest || Date.parse(latestDelivery.created_at) >= Date.parse(latestRequest.created_at))
    ? latestDelivery : latestRequest
  const outcome = latest ? onboardingOutcomeKind(latest) : null
  const deliveryFailed = latest === latestDelivery && latestDelivery && latestDelivery.dependency_kind === 'artifact' &&
    (latestDelivery.delivery_result === 'failed' || latestDelivery.delivery_result === 'cancelled' || latestDelivery.status_code >= 400)
  const failed = outcome === 'error' || deliveryFailed
  const blocked = outcome === 'blocked'
  const result = verification.isError ? 'verificationUnavailable'
    : !session ? 'preparing'
    : blocked ? 'requestBlocked'
    : failed ? 'requestFailed'
    : session.firstCacheHit ? 'cacheConfirmed'
    : session.successfulDownload ? 'artifactDelivered'
    : session.firstRequest ? 'relatedRequest'
    : 'waitingTitle'
  const success = Boolean(session?.successfulDownload)
  const resultEvent = blocked || failed ? latest : session?.firstCacheHit ?? session?.successfulDownload ?? latestRequest
  const tone = verification.isError || blocked ? 'warning' : failed ? 'danger' : success ? 'success' : 'neutral'
  return <AdminPage title={t('onboarding.title')} description={t('onboarding.subtitle')} actions={
    <Button variant="ghost" onClick={() => { void leave() }} disabled={leaving}><Icon name="arrow_back" size="sm" />{t('onboarding.backOverview')}</Button>
  }>
    <fieldset disabled={leaving}>{selectors(leaving)}
      <ConnectionHealth ecosystem={connection.selection.ecosystem} onNavigate={destination => { void leave(destination) }} />
    </fieldset>
    {connection.selection.ecosystem === 'docker' && <DockerBootstrap />}
    <div className="connection-workspace">
      <div className="connection-instructions">
        <section className="connection-section" aria-labelledby="connection-config-title">
          <header className="connection-section-heading"><span className="connection-number" aria-hidden="true">1</span><div>
            <h2 id="connection-config-title">{t(temporary ? 'onboarding.temporarySource' : 'onboarding.configureManager', { manager: managerName })}</h2>
            <p>{t('onboarding.runOnClient')} · {t(connection.scope.labelKey)}</p>
          </div></header>
          {configuration && <CodeBlock key={configuration.body} wrap filename={connection.configuration?.command ? t(connection.shell === 'powershell' ? 'onboarding.terminalPowerShell' : 'onboarding.terminal') : connection.configuration?.file}
            code={configuration.body} language={configuration.lang} copyLabel={t('onboarding.copyConfiguration')} />}
          <p className="connection-note">{temporary || connection.configuration?.command ? t(connection.scope.hintKey) : t('onboarding.saveConfigFile', { file: connection.configuration?.file })}</p>
          <details className="connection-details">
            <summary>{t('onboarding.inspectConfig')}</summary>
            <p>{t(temporary ? 'onboarding.temporarySourcePrecedence' : 'onboarding.configPrecedence')}</p>
            {connection.verifyConfig && <CodeBlock key={connection.verifyConfig.body} wrap filename={t('onboarding.terminal')} code={connection.verifyConfig.body} copyName={t('onboarding.inspectConfig')} />}
            {!connection.verifyConfig && connection.configuration && <p>{t('onboarding.inspectFile', { file: connection.configuration.file })}</p>}
            <p>{t('onboarding.nothingYet')}</p>
            <Link to="/" target="_blank" rel="noreferrer">{t('onboarding.connectionDocs')}<Icon name="arrow_forward" size="sm" /></Link>
          </details>
        </section>
        <section className="connection-section" aria-labelledby="connection-test-title">
          <header className="connection-section-heading"><span className="connection-number" aria-hidden="true">2</span><div>
            <h2 id="connection-test-title">{t(connection.test.purpose === 'metadata' ? 'onboarding.testMetadata' : 'onboarding.testDownload')}</h2>
            <p>{t(connection.test.hintKey)}</p>
          </div></header>
          {connection.test.command && <CodeBlock key={connection.test.command.body} wrap filename={t(connection.shell === 'powershell' ? 'onboarding.terminalPowerShell' : 'onboarding.terminal')}
            code={connection.test.command.body} language={connection.test.command.lang} copyLabel={t('onboarding.copyTest')}
            copyDisabled={!session || verification.isError} />}
          {!session && <p role="status" className="connection-note">{t(verification.isError ? 'onboarding.verificationUnavailable' : 'onboarding.preparing')}</p>}
          {connection.selection.ecosystem === 'docker' && <p className="connection-note">{t('onboarding.docker.clientNotes')}</p>}
          <details className="connection-details">
            <summary>{t('onboarding.cacheOptional')}</summary>
            {connection.test.hintKey === 'onboarding.testIsolatedArtifactHint' && <p>{t('onboarding.isolatedCacheRepeat')}</p>}
            <p>{t('onboarding.cacheConditions')}</p>
            {connection.test.purpose === 'metadata' && <p>{t('onboarding.metadataNext')}</p>}
            <Link to="/" target="_blank" rel="noreferrer">{t('onboarding.connectionDocs')}<Icon name="arrow_forward" size="sm" /></Link>
          </details>
        </section>
      </div>
      <aside className="connection-verification" aria-label={t('onboarding.verificationResult')}>
        <div className={`connection-result connection-result-${tone}`}>
          <p className="connection-eyebrow">{t('onboarding.verificationResult')}</p>
          <div className="connection-result-title" role="status" aria-live="polite" aria-atomic="true">
            <Icon name={verification.isError || blocked || failed ? 'warning' : success ? 'check_circle' : !session ? 'progress_activity' : 'radar'} size="sm" />
            <h2>{t(`onboarding.${result}`)}</h2>
          </div>
          <p className="connection-result-hint">{t(verification.isError ? 'onboarding.pollError' : blocked ? 'onboarding.blockedHint' : failed ? 'onboarding.errorHint' : session?.firstCacheHit ? 'onboarding.servedFromCache' : success ? 'onboarding.downloadConfirmedHint' : session?.firstRequest ? 'onboarding.requestOnlyHint' : 'onboarding.waitingHint')}</p>
          <ol className="connection-evidence">
            {([
              ['relatedRequest', Boolean(session?.firstRequest || success)],
              ['artifactDelivered', success],
              ['cacheConfirmed', Boolean(session?.firstCacheHit)],
            ] as const).map(([label, done]) => <li key={label} data-complete={done}>
              {done ? <Icon name="check_circle" size="sm" /> : <span className="connection-pending-dot" aria-hidden="true" />}<span>{t(`onboarding.${label}`)}</span><span className="sr-only">{t(done ? 'onboarding.confirmed' : 'onboarding.notConfirmed')}</span>
            </li>)}
          </ol>
          {resultEvent && <dl className="connection-event"><dt>{t('onboarding.package')}</dt><dd>{[resultEvent.package_name, resultEvent.version].filter(Boolean).join('@')}</dd>
            <dt>{t('onboarding.result')}</dt><dd>{t(`onboarding.outcomes.${onboardingOutcomeKind(resultEvent)}`)} · HTTP {resultEvent.status_code}</dd>
            <dt>{t('onboarding.requestStarted')}</dt><dd><time dateTime={resultEvent.created_at}>{new Date(resultEvent.created_at).toLocaleString(i18n.language)}</time></dd>
          </dl>}
          {resultEvent?.status_code === 206 && <p className="connection-note">{t('onboarding.partialDelivery')}</p>}
          <p className="connection-note">{t('onboarding.observationScope', { package: connection.test.targetPackage ?? managerName })}</p>
          {session && !session.deliveryAfterId && <p className="connection-note">{t('onboarding.deliveryUnsupported')}</p>}
          {connection.selection.ecosystem === 'docker' && <p className="connection-note">{t('onboarding.testDockerHint')}</p>}
          {success && !session?.firstCacheHit && <p className="connection-note">{t('onboarding.cacheNext')}</p>}
          {save.isError && <p role="alert" className="connection-note connection-warning">{t('onboarding.saveError')}</p>}
          <div className="connection-result-actions">
            {save.isError && success && <Button variant="outline" disabled={save.isPending || leaving} onClick={() => complete('completed')}>{t('common.retry')}</Button>}
            {verification.isError && <Button onClick={() => { void verification.refetch() }} disabled={verification.isFetching}>{t('onboarding.retryVerification')}</Button>}
            <Button variant={success ? "default" : 'ghost'} className={success ? 'connection-primary' : 'connection-return-unverified'} disabled={leaving} onClick={() => { void leave() }}>{t(success ? 'onboarding.viewOverview' : 'onboarding.returnUnverified')}<Icon name="arrow_forward" size="sm" /></Button>
            <Button variant="ghost" disabled={leaving} onClick={onRestart}>{t('onboarding.restartCheck')}</Button>
            {(session?.firstRequest || success) && <Button variant="ghost" disabled={leaving} onClick={() => { void leave('accessLogs') }}>{t('onboarding.viewAccessLogs')}</Button>}
          </div>
        </div>
      </aside>
    </div>
  </AdminPage>
}
