import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useBeforeUnload, useBlocker } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import AuthenticationImpact from './AuthenticationImpact'
import Icon from '@/components/Icon'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authenticationApi } from '@/lib/authenticationApi'
import { getApiError } from '@/lib/apiError'
import { identityAuthorizationURL } from '@/lib/identityAuth'
import { rememberOIDCTrial } from '@/lib/oidcTrialContext'
import type { AuthenticationCheck, AuthenticationField, AuthenticationSettingsResponse, OIDCAuthenticationSourceState } from '@/lib/adminApi.types'
import { authenticationCreateSettings, authenticationDraft, authenticationMayResetSessions, authenticationPatch, authenticationRuntimeStatus, authenticationValidationKey, rebaseAuthenticationDraft, type AuthenticationDraft } from '@/lib/authenticationSettings'

interface EditorProps {
  source: OIDCAuthenticationSourceState
  creating: boolean
  configWritable: boolean
  stale: boolean
  fetching: boolean
  refresh: () => void
  onBack: () => void
  onSaved: (source: OIDCAuthenticationSourceState) => void
  onDeleted: () => void
}

export default function AuthenticationEditor({ source, creating, configWritable, stale, fetching, refresh, onBack, onSaved, onDeleted }: EditorProps) {
  const { t, i18n } = useTranslation()
  const { canManageCredentials, principal } = usePrincipal()
  const state = useMemo<AuthenticationSettingsResponse>(() => ({ ...source, configured: source.configured!, effective: source.effective ?? { ...source.configured!, enabled: false } }), [source])
  const [id, setID] = useState(source.id)
  const [draft, setDraft] = useState(() => authenticationDraft(state))
  const [review, setReview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState('')
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [checked, setChecked] = useState<AuthenticationCheck | null>(null)
  const [checking, setChecking] = useState(false)
  const [copyStatus, setCopyStatus] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const previousState = useRef(state)
  const allowNavigation = useRef(false)
  useEffect(() => () => { controller.current?.abort() }, [])
  useEffect(() => {
    if (previousState.current === state) return
    const before = previousState.current
    previousState.current = state
    setDraft(current => rebaseAuthenticationDraft(current, before, state))
    setChecked(null)
  }, [state])
  const canTrial = principal?.auth_method === 'jwt' && principal.login_method === 'password'
  const callback = `${window.location.origin}/api/v1/auth/oidc/${source.legacy ? '' : `${id}/`}callback`
  const patch = authenticationPatch(draft, state)
  const changed = Object.keys(patch) as AuthenticationField[]
  const dirty = changed.length > 0 || id !== source.id
  const readOnly = !canManageCredentials || !configWritable || !state.config_writable || stale || fetching || busy || checking
  const disabled = (field: AuthenticationField) => readOnly || Boolean(state.overrides[field])
  const blocker = useBlocker(({ currentLocation, nextLocation }) => !allowNavigation.current && (dirty || busy) && nextLocation.pathname !== '/admin/login' && (currentLocation.pathname !== nextLocation.pathname || currentLocation.search !== nextLocation.search))
  useBeforeUnload(useCallback(event => { if ((dirty || busy) && !allowNavigation.current) { event.preventDefault(); event.returnValue = '' } }, [dirty, busy]))
  useEffect(() => {
    if (blocker.state === 'blocked' && !dirty && !busy && !error) blocker.proceed()
  }, [blocker, dirty, busy, error])
  const mayResetSessions = authenticationMayResetSessions(source, patch)
  const impactNeeded = !creating && (deleting || review && mayResetSessions)
  const impact = useQuery({ queryKey: ['admin', 'authentication', 'impact', source.id], queryFn: async ({ signal }) => (await authenticationApi.sourceImpact(source.id, { signal })).data, enabled: impactNeeded, staleTime: 0, retry: false })
  const impactReady = !impactNeeded || Boolean(impact.data && !impact.isFetching && !impact.isError)
  const change = <K extends keyof AuthenticationDraft>(field: K, value: AuthenticationDraft[K]) => {
    setDraft(current => ({ ...current, [field]: value }))
    setSaved(false); setError(''); setChecked(null)
    setFieldErrors(current => { const next = { ...current }; delete next[field]; return next })
  }
  const showFieldError = (field: string) => fieldErrors[field] && <p id={`error-${field}`} role="alert" className="text-sm leading-5 text-destructive">{t(authenticationValidationKey(fieldErrors[field]))}</p>
  const overrideHint = (field: AuthenticationField) => state.overrides[field] ? <FieldDescription className="break-all">{t(field === 'client_secret' ? 'authentication.secretEnvOwned' : 'authentication.envOwned', { variable: state.overrides[field] })}</FieldDescription> : null
  const textField = (field: 'display_name' | 'issuer' | 'client_id' | 'redirect_url' | 'groups_claim' | 'session_ttl', hint?: string) => <Field key={field}>
    <FieldLabel htmlFor={`oidc-${field}`}>{t(`authentication.fields.${field}`)}</FieldLabel>
    <Input id={`oidc-${field}`} value={draft[field]} onChange={event => change(field, event.target.value)} disabled={disabled(field)} required={field === 'display_name' || draft.enabled} aria-invalid={Boolean(fieldErrors[field])} aria-describedby={fieldErrors[field] ? `error-${field}` : undefined} maxLength={field === 'issuer' || field === 'redirect_url' ? 1024 : field === 'display_name' ? 80 : 255} autoComplete="off" spellCheck={false} placeholder={field === 'display_name' ? t('authentication.nameExample') : undefined} />
    {showFieldError(field)}{overrideHint(field) ?? (hint && <FieldDescription>{hint}</FieldDescription>)}
  </Field>
  const envelope = () => ({ id, protocol: 'oidc' as const, create: creating, settings: creating ? authenticationCreateSettings(draft) : authenticationPatch(draft, state) })
  function failureDetails(failure: unknown, fallback: string) {
    const { code, fieldErrors: errors } = getApiError(failure)
    setFieldErrors(errors ?? {})
    if (errors?.id || errors?.allow_insecure_loopback) setAdvancedOpen(true)
    setError(code === 'INVALID_SETTING' ? (fallback === 'authentication.checkError' ? 'authentication.checkInvalid' : 'authentication.invalid') : code === 'AUTH_SOURCE_CONFLICT' ? 'authentication.createInvalid' : fallback)
  }
  async function copyCallback() {
    try { await navigator.clipboard.writeText(draft.redirect_url); setCopyStatus('authentication.callbackCopied') }
    catch { setCopyStatus('authentication.copyFailed') }
  }
  async function save() {
    if ((!dirty && !creating) || readOnly || !impactReady) return
    setBusy(true); setError('')
    const abort = new AbortController(); controller.current = abort
    const payload = envelope()
    // Secrets stay in component memory/the request, never the query or mutation caches.
    setDraft(current => ({ ...current, client_secret: '' }))
    try {
      const { data } = creating
        ? await authenticationApi.createSource({ ...payload.settings, id, protocol: 'oidc', display_name: draft.display_name.trim() }, { signal: abort.signal })
        : await authenticationApi.updateSource(id, payload.settings, { signal: abort.signal })
      if (abort.signal.aborted) return
      if (data.protocol !== 'oidc') throw new Error('Unexpected authentication protocol')
      const next = { ...data, configured: data.configured!, effective: data.effective ?? { ...data.configured!, enabled: false } }
      previousState.current = next
      setDraft(authenticationDraft(next)); setReview(false); setSaved(true); setFieldErrors({}); setChecked(null)
      if (creating) allowNavigation.current = true
      onSaved(data)
    } catch (failure) {
      if (!abort.signal.aborted) { failureDetails(failure, 'authentication.saveError'); setReview(false) }
    } finally { if (!abort.signal.aborted) setBusy(false) }
  }
  async function check() {
    if (readOnly) return
    setChecking(true); setError(''); setFieldErrors({}); setChecked(null)
    const abort = new AbortController(); controller.current = abort
    try {
      const { data } = await authenticationApi.checkSource(envelope(), { signal: abort.signal })
      if (!abort.signal.aborted) setChecked(data)
    } catch (failure) { if (!abort.signal.aborted) failureDetails(failure, 'authentication.checkError') }
    finally { if (!abort.signal.aborted) setChecking(false) }
  }
  async function trial() {
    if (readOnly || dirty || creating || !canTrial) return
    setBusy(true); setError('')
    const abort = new AbortController(); controller.current = abort
    try {
      const { data } = await authenticationApi.trialSource({ id, protocol: 'oidc', create: false, settings: {} }, { signal: abort.signal })
      if (!abort.signal.aborted) { const destination = identityAuthorizationURL(data.authorization_url); rememberOIDCTrial(id); allowNavigation.current = true; window.location.assign(destination) }
    } catch (failure) { if (!abort.signal.aborted) { failureDetails(failure, 'authentication.trialError'); setBusy(false) } }
  }
  async function remove() {
    if (readOnly || !impactReady || Object.keys(source.overrides).length) return
    setBusy(true); setError('')
    const abort = new AbortController(); controller.current = abort
    try { await authenticationApi.deleteSource(source.id, { signal: abort.signal }); if (!abort.signal.aborted) { allowNavigation.current = true; onDeleted() } }
    catch { if (!abort.signal.aborted) setError('authentication.deleteError') }
    finally { if (!abort.signal.aborted) setBusy(false) }
  }
  function displayValue(field: AuthenticationField, value: unknown) {
    if (field === 'client_secret') return t('authentication.secretChanged')
    if (typeof value === 'boolean') return t(value ? 'authentication.enabled' : 'authentication.disabled')
    if (Array.isArray(value)) return value.join(', ') || '—'
    return typeof value === 'string' && value ? value : '—'
  }
  const impactPanel = <AuthenticationImpact data={impact.isError ? undefined : impact.data} pending={impact.isFetching} retry={() => { void impact.refetch() }} />

  return <div className="max-w-6xl space-y-5">
    <Button variant="ghost" disabled={busy} onClick={onBack}><Icon name="arrow_back" size="sm" />{t('authentication.backToSources')}</Button>
    <section aria-labelledby="authentication-status" className="flex flex-wrap items-start justify-between gap-4 rounded-lg border border-border bg-card p-5">
      <div className="min-w-0 flex-1 basis-64"><div className="flex flex-wrap items-center gap-2"><h2 id="authentication-status" className="break-all text-base font-semibold">{creating ? t('authentication.addSource') : source.effective?.display_name || source.configured?.display_name || source.id}</h2><Badge variant="outline">OIDC</Badge>{!creating && <Badge variant={source.effective?.enabled ? 'success' : 'secondary'}>{t('authentication.running')}: {t(`authentication.${authenticationRuntimeStatus(source)}`)}</Badge>}</div>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">{creating ? t('authentication.createHint') : source.effective?.enabled ? t('authentication.activeProvider', { name: source.effective.display_name }) : t('authentication.disabledHint')}</p>
        {source.effective?.enabled && <p className="mt-1 break-all text-sm text-muted-foreground">{source.effective.issuer}</p>}
      </div>{!creating && <Button variant="outline" size="sm" disabled={fetching || busy || checking || dirty} onClick={refresh}><Icon name="refresh" size="sm" />{t('now.refresh')}</Button>}
    </section>
    {state.pending_restart && <Alert variant="warning"><AlertDescription>{t('authentication.pendingRestart')}{source.effective?.enabled && <span className="block">{t(source.sessions_will_reset === false ? 'authentication.sessionsPreserved' : 'authentication.sessionsReset')}</span>}</AlertDescription></Alert>}
    {saved && <Alert variant="success"><AlertDescription>{t('authentication.saved')}</AlertDescription></Alert>}
    {stale && <Alert variant="warning"><AlertDescription>{t('authentication.stale')} <Button variant="link" size="sm" disabled={fetching || busy} onClick={refresh}>{t('now.refresh')}</Button></AlertDescription></Alert>}
    {!canManageCredentials && <Alert variant="info"><AlertDescription>{t('authentication.localAdminRequired')}</AlertDescription></Alert>}
    {(!configWritable || !state.config_writable) && <Alert variant="warning"><AlertDescription>{t('authentication.readonlyConfig')}</AlertDescription></Alert>}
    {error && !deleting && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}
    <div className="grid min-w-0 gap-8 xl:grid-cols-[minmax(0,1fr)_280px]">
      <form className="min-w-0 space-y-6" onSubmit={event => { event.preventDefault(); if (!readOnly && (dirty || creating)) { setError(''); setReview(true) } }}>
        <section className="space-y-4" aria-labelledby="authentication-provider">
          <h2 id="authentication-provider" className="text-base font-semibold">{t('authentication.oidcSettings')}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{t('authentication.protocolHint')}</p>
          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            {textField('display_name')}{textField('client_id')}
            <div className="sm:col-span-2">{textField('issuer', t('authentication.issuerHint'))}</div>
            <div className="sm:col-span-2"><Field><FieldLabel htmlFor="oidc-secret">{t('authentication.fields.client_secret')}</FieldLabel><Input id="oidc-secret" type="password" value={draft.client_secret} onChange={event => change('client_secret', event.target.value)} disabled={disabled('client_secret')} aria-invalid={Boolean(fieldErrors.client_secret)} aria-describedby={fieldErrors.client_secret ? 'error-client_secret' : undefined} autoComplete="new-password" maxLength={4096} required={draft.enabled && !draft.client_secret_set} placeholder={t(draft.client_secret_set ? 'authentication.secretSaved' : 'authentication.secretEmpty')} />{showFieldError('client_secret')}{overrideHint('client_secret') ?? <FieldDescription>{t('authentication.secretHint')}</FieldDescription>}</Field></div>
            <div className="sm:col-span-2">{textField('redirect_url', t('authentication.callbackHint'))}<div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2 text-sm"><Button type="button" variant="outline" size="sm" disabled={!draft.redirect_url} onClick={() => { void copyCallback() }}><Icon name="content_copy" size="sm" />{t('authentication.copyCallback')}</Button><Button type="button" variant="link" size="sm" className="h-auto whitespace-normal p-0" disabled={disabled('redirect_url')} onClick={() => { change('redirect_url', callback); setCopyStatus('') }}>{t('authentication.useCurrentOrigin')}</Button><span role="status" className="text-muted-foreground">{copyStatus && t(copyStatus)}</span></div></div>
          </div>
        </section>
        <section className="space-y-4 border-t border-border pt-5" aria-labelledby="authentication-access"><h2 id="authentication-access" className="text-base font-semibold">{t('authentication.access')}</h2><p className="text-sm leading-6 text-muted-foreground">{t('authentication.groupsHint')}</p><div className="grid gap-4 sm:grid-cols-2">
          {(['admin_groups', 'readonly_groups'] as const).map(field => <Field key={field}><FieldLabel htmlFor={`oidc-${field}`}>{t(`authentication.fields.${field}`)}</FieldLabel><Textarea id={`oidc-${field}`} value={draft[field]} disabled={disabled(field)} onChange={event => change(field, event.target.value)} aria-invalid={Boolean(fieldErrors[field])} aria-describedby={fieldErrors[field] ? `error-${field}` : undefined} rows={3} maxLength={8192} spellCheck={false} />{showFieldError(field)}{overrideHint(field)}</Field>)}
          {textField('groups_claim', t('authentication.claimHint'))}{textField('session_ttl', t('authentication.ttlHint'))}
        </div></section>
        <details open={advancedOpen} onToggle={event => setAdvancedOpen(event.currentTarget.open)} className="rounded-md border border-border p-4 text-sm"><summary className="cursor-pointer font-medium focus-visible:outline-2 focus-visible:outline-ring">{t('authentication.advanced')}</summary><div className="mt-4 space-y-4"><Field><FieldLabel htmlFor="source-id">{t('authentication.sourceID')}</FieldLabel><Input id="source-id" value={id} onChange={event => { const next = event.target.value; if (draft.redirect_url === callback) change('redirect_url', `${window.location.origin}/api/v1/auth/oidc/${next}/callback`); setID(next); setChecked(null); setFieldErrors({}) }} required pattern="[a-z][a-z0-9-]{0,31}" maxLength={32} disabled={!creating || readOnly} autoComplete="off" spellCheck={false} aria-invalid={Boolean(fieldErrors.id)} />{showFieldError('id')}<FieldDescription>{t('authentication.idHint')}</FieldDescription></Field><Field orientation="horizontal"><Switch id="oidc-loopback" checked={draft.allow_insecure_loopback} disabled={disabled('allow_insecure_loopback')} onCheckedChange={value => change('allow_insecure_loopback', value)} /><FieldLabel htmlFor="oidc-loopback">{t('authentication.fields.allow_insecure_loopback')}</FieldLabel></Field><p className="text-sm leading-6 text-muted-foreground">{t('authentication.loopbackHint')}</p>{overrideHint('allow_insecure_loopback')}{showFieldError('allow_insecure_loopback')}</div></details>
        <section className="space-y-2 border-t border-border pt-4"><Field orientation="horizontal"><Switch id="oidc-enabled" checked={draft.enabled} disabled={disabled('enabled')} onCheckedChange={value => change('enabled', value)} /><FieldLabel htmlFor="oidc-enabled">{t('authentication.fields.enabled')}</FieldLabel></Field><p className="text-sm leading-6 text-muted-foreground">{t('authentication.enableHint')}</p>{overrideHint('enabled')}{showFieldError('enabled')}</section>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4"><p className="text-sm text-muted-foreground">{t(dirty ? 'authentication.unsaved' : 'authentication.restartHint')}</p><div className="flex flex-wrap gap-2"><Button type="button" variant="outline" disabled={!dirty || busy || checking} onClick={() => { setDraft(authenticationDraft(state)); setID(source.id); setError(''); setFieldErrors({}); setSaved(false); setChecked(null) }}>{t('authentication.discard')}</Button><Button type="submit" disabled={readOnly || (!dirty && !creating) || !draft.display_name.trim()}>{t('authentication.reviewSave')}</Button></div></div>
      </form>
      <aside className="min-w-0 space-y-5 text-sm leading-6" aria-label={t('authentication.guide')}>
        <section className="space-y-3 rounded-lg border border-border bg-card p-4"><h2 className="text-sm font-semibold">{t('authentication.verifyTitle')}</h2><p className="text-muted-foreground">{t('authentication.checkHint')}</p><Button variant="outline" className="w-full" disabled={readOnly} aria-busy={checking} onClick={() => { void check() }}>{t(checking ? 'authentication.checking' : 'authentication.checkConfiguration')}</Button>
          {checked && <div role="status" className="space-y-1 text-sm"><p className="font-medium text-success">{t('authentication.checkPassed')}</p><p className="text-muted-foreground">{t('authentication.checkTime', { time: new Date(checked.checked_at).toLocaleString(i18n.language) })}</p><p className="text-muted-foreground">{t('authentication.checkLimited')}</p></div>}
          <div className="space-y-3 border-t border-border pt-3"><p className="text-muted-foreground">{t('authentication.trialHint')}</p><Button variant="outline" className="w-full" disabled={readOnly || dirty || creating || !canTrial} onClick={() => { void trial() }}>{t('authentication.trialLogin')}</Button>{!canTrial ? <p className="text-muted-foreground">{t('authentication.trialLocalSession')}</p> : (dirty || creating) && <p className="text-muted-foreground">{t('authentication.saveBeforeTrial')}</p>}</div>
        </section>
        <section className="space-y-3 rounded-lg bg-muted/50 p-4"><h2 className="text-sm font-semibold">{t('authentication.guide')}</h2><ol className="list-decimal space-y-3 pl-5 text-muted-foreground"><li>{t('authentication.guideRegister')}</li><li>{t('authentication.guideGroups')}</li><li>{t('authentication.guideVerify')}</li></ol></section>
        <section className="space-y-2"><h2 className="text-sm font-semibold">{t('authentication.personalTitle')}</h2><p className="text-muted-foreground">{t('authentication.personalHint')}</p><Link className="inline-flex min-h-10 items-center gap-1 rounded-sm text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring" to={getAdminRouteHref('account')}>{t('account.title')}<Icon name="arrow_forward" size="sm" /></Link></section>
      </aside>
    </div>
    {!creating && <div className="space-y-2 border-t border-border pt-4"><div className="flex flex-wrap items-center justify-between gap-3"><p className="text-sm text-muted-foreground">{t('authentication.removeHint')}</p><Button variant="outline" disabled={readOnly || dirty || Object.keys(source.overrides).length > 0} onClick={() => { setError(''); setDeleting(true) }}>{t('authentication.removeSource')}</Button></div>{Object.keys(source.overrides).length > 0 && <p className="text-sm text-muted-foreground">{t('authentication.envDeleteBlocked')}</p>}</div>}
    <Dialog open={blocker.state === 'blocked'} onOpenChange={open => { if (!open && blocker.state === 'blocked') blocker.reset() }}><DialogContent className="max-w-md" finalFocus><DialogTitle>{t(busy ? 'authentication.savingLeaveTitle' : 'authentication.leaveTitle')}</DialogTitle><DialogDescription>{t(busy ? 'authentication.savingLeaveHint' : 'authentication.leaveHint')}</DialogDescription><div className="flex flex-wrap justify-end gap-2"><Button variant="outline" onClick={() => { if (blocker.state === 'blocked') blocker.reset() }}>{t('cancel')}</Button><Button variant="destructive" disabled={busy} onClick={() => { if (blocker.state === 'blocked') blocker.proceed() }}>{t('authentication.discardAndLeave')}</Button></div></DialogContent></Dialog>
    <Dialog open={deleting} onOpenChange={value => { if (!busy) setDeleting(value) }}><DialogContent className="max-w-lg" finalFocus closeDisabled={busy}><DialogTitle>{t('authentication.removeNamed', { name: source.configured?.display_name || source.id })}</DialogTitle><DialogDescription className="text-sm leading-6">{t('authentication.removeDescription')}</DialogDescription>{impactPanel}{error && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}<div className="flex flex-wrap justify-end gap-2"><Button variant="outline" disabled={busy} onClick={() => setDeleting(false)}>{t('cancel')}</Button><Button variant="destructive" disabled={readOnly || !impactReady} aria-busy={busy} onClick={() => { void remove() }}>{t(busy ? 'deleting' : 'authentication.confirmRemove')}</Button></div></DialogContent></Dialog>
    <Dialog open={review} onOpenChange={open => { if (!busy) setReview(open) }}><DialogContent className="max-w-xl" closeDisabled={busy} finalFocus><DialogTitle>{t('authentication.reviewTitle')}</DialogTitle><DialogDescription className="text-sm leading-6">{t('authentication.reviewHint')}{source.effective?.enabled && <span className="mt-2 block">{t(mayResetSessions ? 'authentication.sessionsMayReset' : 'authentication.sessionsPreserved')}</span>}</DialogDescription>
      <div className="space-y-3 text-sm">{creating && <p>{t('authentication.protocol')}: OIDC · <span className="break-all font-mono">{id}</span></p>}{changed.map(field => <div key={field} className="grid min-w-0 gap-1 border-b border-border pb-2"><span className="font-medium">{t(`authentication.fields.${field}`)}</span><div className="grid min-w-0 gap-1 sm:grid-cols-2"><span className="break-all text-muted-foreground">{t('authentication.before')}: {field === 'client_secret' ? t(state.configured.client_secret_set ? 'authentication.secretPresent' : 'authentication.secretAbsent') : displayValue(field, state.configured[field])}</span><span className="break-all">{t('authentication.after')}: {displayValue(field, patch[field])}</span></div></div>)}</div>
      {impactNeeded && impactPanel}<div className="flex flex-wrap justify-end gap-2"><Button variant="outline" disabled={busy} onClick={() => setReview(false)}>{t('cancel')}</Button><Button disabled={readOnly || !impactReady || (!dirty && !creating)} aria-busy={busy} onClick={() => { void save() }}>{t(busy ? 'saving' : 'authentication.confirmSave')}</Button></div>
    </DialogContent></Dialog>
  </div>
}
