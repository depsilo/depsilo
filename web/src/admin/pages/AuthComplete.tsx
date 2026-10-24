import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { authenticationApi } from '@/lib/authenticationApi'
import type { OIDCCompleteResponse } from '@/lib/adminApi.types'
import { safeAdminDestination } from '@/lib/adminLoginDestination'
import { identityErrorKey } from '@/lib/identityAuth'
import { clearOIDCTrial, readOIDCTrial, rememberOIDCTrial } from '@/lib/oidcTrialContext'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'
import { ADMIN_LOGIN_PATH, getAdminRouteHref } from '@/admin/routes'
import { formatDate, formatTime } from '@/lib/utils'
import Logo from '@/components/Logo'
import Icon from '@/components/Icon'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

type PendingCompletion = Extract<OIDCCompleteResponse, { mode: 'choice' | 'test' }>

export default function AuthComplete() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const request = useRef<ReturnType<typeof authenticationApi.completeOIDC> | null>(null)
  const actionRequest = useRef<AbortController | null>(null)
  const [error, setError] = useState('')
  const [result, setResult] = useState<PendingCompletion | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState<'link' | 'create' | 'cancel' | null>(null)
  const [actionError, setActionError] = useState('')
  const [trialContext, setTrialContext] = useState(readOIDCTrial)
  const trialRecovery = trialContext ? `${getAdminRouteHref('authentication')}?source=${encodeURIComponent(trialContext.sourceID)}` : null
  const finishLogin = useCallback((data: Extract<OIDCCompleteResponse, { mode: 'login' }>) => {
    if (!data.token || !writeLocalStorage('token', data.token)) {
      setError('login.storageUnavailable')
      return
    }
    queryClient.clear()
    navigate(safeAdminDestination(data.return_to) ?? getAdminRouteHref('dashboard'), { replace: true })
  }, [navigate, queryClient])

  useEffect(() => {
    let active = true
    // StrictMode replays effects. Exchange the callback once per mount. The
    // server may restore an unexpired choice without creating a user/session.
    // A trial is tied to its initiating administrator. Ordinary completion is
    // still public; the backend only requires this existing token for trials.
    request.current ??= authenticationApi.completeOIDC(readLocalStorage('token'))
    void request.current.then(({ data }) => {
      if (!active) return
      if (data.mode === 'login') { clearOIDCTrial(); finishLogin(data) }
      else if (data.mode === 'link' && data.result === 'linked') {
        clearOIDCTrial()
        void queryClient.invalidateQueries({ queryKey: ['auth', 'identities'] })
        navigate(getAdminRouteHref('account'), { replace: true, state: { identityLinked: true } })
      } else if (data.mode === 'choice' || data.mode === 'test') {
        if (data.mode === 'test') rememberOIDCTrial(data.provider.id)
        else { clearOIDCTrial(); setTrialContext(null) }
        setResult(data)
      }
      else setError('account.errors.failed')
    }).catch((failure: unknown) => { if (active) setError(identityErrorKey(failure)) })
    return () => { active = false }
  }, [finishLogin, navigate, queryClient])
  useEffect(() => () => { actionRequest.current?.abort() }, [])

  async function resolve(action: 'link' | 'create' | 'cancel') {
    if (busy || result?.mode !== 'choice') return
    if (action === 'link' && (!username.trim() || !password)) return
    setBusy(action)
    setActionError('')
    const controller = new AbortController()
    actionRequest.current = controller
    const submittedFrom = window.location.pathname
    const proof = password
    setPassword('')
    try {
      // These explicit actions are the only paths that can consume the pending
      // identity. Password proof never enters Query caches or browser storage.
      if (action === 'cancel') {
        await authenticationApi.cancelOIDC({ signal: controller.signal })
        if (!controller.signal.aborted && window.location.pathname === submittedFrom) navigate(ADMIN_LOGIN_PATH, { replace: true })
      } else {
        const payload = action === 'link' ? { action, username: username.trim(), password: proof } as const : { action } as const
        const { data } = await authenticationApi.resolveOIDC(payload, { signal: controller.signal })
        if (!controller.signal.aborted && window.location.pathname === submittedFrom) {
          if (data.mode === 'login') { clearOIDCTrial(); finishLogin(data) }
          else setActionError('account.errors.failed')
        }
      }
    } catch (failure: unknown) {
      if (!controller.signal.aborted) {
        const key = identityErrorKey(failure)
        if (key === 'account.errors.expired') setError(key)
        else setActionError(key)
      }
    } finally {
      if (!controller.signal.aborted) setBusy(null)
    }
  }

  if (result?.mode === 'test' && !error) return <main className="grid min-h-screen place-items-center bg-background px-4 py-12">
    <section className="w-full max-w-lg space-y-5">
      <Logo size={32} />
      <h1 className="text-[26px] font-semibold">{t('account.testTitle')}</h1>
      <p className="break-words text-base font-medium">{result.provider.name}</p>
      <Alert variant={result.outcome === 'success' ? 'success' : result.outcome === 'denied' || result.code === 'AUTHENTICATION_NOT_COMPLETED' ? 'warning' : 'destructive'}><AlertDescription>{t(result.outcome === 'success' ? 'account.testSuccess' : result.outcome === 'denied' ? 'account.testDenied' : result.code === 'AUTHENTICATION_NOT_COMPLETED' ? 'account.testNotCompleted' : 'account.testFailed')}</AlertDescription></Alert>
      <dl className="space-y-3 rounded-lg border border-border bg-card p-5 text-sm">
        <div><dt className="text-muted-foreground">{t('account.testIdentity')}</dt><dd className="mt-1 break-words">{result.authenticated ? result.display_name || t('account.testAuthenticated') : t('account.testNotAuthenticated')}</dd></div>
        <div><dt className="text-muted-foreground">{t('account.testRole')}</dt><dd className="mt-1">{result.mapped_role ? t(result.mapped_role === 'admin' ? 'users.admin' : 'users.readonly') : t('account.testNoRole')}</dd></div>
        <div><dt className="text-muted-foreground">{t('account.testTime')}</dt><dd className="mt-1 tabular-nums">{formatDate(result.checked_at)} {formatTime(result.checked_at, 'time')}</dd></div>
      </dl>
      <p className="text-sm leading-6 text-muted-foreground">{t('account.testSessionUnchanged')}</p>
      <Button nativeButton={false} role="link" render={<Link to={safeAdminDestination(result.return_to) ?? getAdminRouteHref('authentication')} onClick={clearOIDCTrial} />}><Icon name="arrow_back" size="sm" />{t('account.backToSources')}</Button>
    </section>
  </main>

  if (result?.mode === 'choice' && !error) return <main className="grid min-h-screen place-items-center bg-background px-4 py-10 sm:px-6">
    <section className="w-full max-w-lg space-y-6">
      <Logo size={32} />
      <header className="space-y-2"><h1 className="text-[26px] font-semibold">{t('account.chooseAccount')}</h1><p className="text-sm leading-6 text-muted-foreground">{t('account.choiceHint')}</p></header>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-4">
        <div className="min-w-0"><p className="break-words font-medium">{result.display_name || result.provider.name}</p><p className="mt-1 break-words text-sm text-muted-foreground">{result.provider.name}</p></div>
        <Badge variant="success">{t('account.identityVerified')}</Badge>
      </div>
      <section aria-labelledby="existing-account" className="space-y-3">
        <h2 id="existing-account" className="text-base font-semibold">{t('account.useExistingAccount')}</h2>
        <p className="text-sm leading-6 text-muted-foreground">{t('account.existingAccountChoiceHint')}</p>
        <form className="space-y-4" onSubmit={event => { event.preventDefault(); void resolve('link') }}>
          <Field><FieldLabel htmlFor="choice-username">{t('login.username')}</FieldLabel><Input id="choice-username" autoComplete="username" value={username} onChange={event => setUsername(event.target.value)} required disabled={Boolean(busy)} /></Field>
          <Field><FieldLabel htmlFor="choice-password">{t('account.currentPassword')}</FieldLabel><Input id="choice-password" type="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} required disabled={Boolean(busy)} /></Field>
          <Button type="submit" className="min-h-10 w-full whitespace-normal" disabled={Boolean(busy) || !username.trim() || !password} aria-busy={busy === 'link'}>{t(busy === 'link' ? 'account.processing' : 'account.linkAndSignIn')}</Button>
        </form>
      </section>
      <details className="group border-t border-border pt-3">
        <summary className="stripe-focus-ring flex min-h-10 cursor-pointer items-center justify-between gap-2 rounded-md text-sm font-medium">{t('account.needNewAccount')}<Icon name="expand_more" size="sm" className="group-open:rotate-180" /></summary>
        <div className="space-y-3 pt-3">
          <p className="text-sm leading-6 text-muted-foreground">{t('account.newAccountChoiceHint', { name: result.provider.name, role: t(result.role === 'admin' ? 'users.admin' : 'users.readonly') })}</p>
          <Button variant="outline" className="min-h-10 w-full whitespace-normal" disabled={Boolean(busy)} aria-busy={busy === 'create'} onClick={() => { void resolve('create') }}>{t(busy === 'create' ? 'account.processing' : 'account.createAndSignIn')}</Button>
        </div>
      </details>
      {actionError && <Alert variant="destructive"><AlertDescription>{t(actionError)}</AlertDescription></Alert>}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
        <p className="text-xs leading-5 text-muted-foreground">{t('account.choiceExpires', { time: formatTime(new Date(result.expires_at * 1000).toISOString(), 'time') })}</p>
        <Button variant="ghost" disabled={Boolean(busy)} onClick={() => { void resolve('cancel') }}>{t('account.cancelChoice')}</Button>
      </div>
    </section>
  </main>

  return <main className="grid min-h-screen place-items-center bg-background px-4 py-12">
    <section className="w-full max-w-md space-y-6">
      <Logo size={32} />
      <h1 className="text-[26px] font-semibold">{t(error ? 'account.completeFailed' : 'account.completing')}</h1>
      {error ? <>
        <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>
        {trialRecovery && <p className="text-sm leading-6 text-muted-foreground">{t('account.testRecovery')}</p>}
        <div className="flex flex-wrap gap-4">
          {trialRecovery ? <Link className="stripe-focus-ring inline-flex min-h-10 items-center rounded-md text-primary underline underline-offset-4" to={trialRecovery} onClick={clearOIDCTrial}>{t('account.backToSources')}</Link> : <>
          <Link className="stripe-focus-ring inline-flex min-h-10 items-center rounded-md text-primary underline underline-offset-4" to={ADMIN_LOGIN_PATH}>{t('account.retryLogin')}</Link>
          <Link className="stripe-focus-ring inline-flex min-h-10 items-center rounded-md text-primary underline underline-offset-4" to={getAdminRouteHref('account')}>{t('account.backToAccount')}</Link>
          </>}
        </div>
      </> : <p role="status" className="text-sm text-muted-foreground">{t('account.completingHint')}</p>}
    </section>
  </main>
}
