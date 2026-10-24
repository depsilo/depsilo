import { useCallback, useEffect, useRef, useState } from 'react'
import { useBeforeUnload, useBlocker } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { authenticationApi } from '@/lib/authenticationApi'
import { getApiError } from '@/lib/apiError'
import { authenticationMayResetSessions, authenticationValidationKey } from '@/lib/authenticationSettings'
import { ldapDraft, ldapFormSettings, ldapPatch, rebaseLDAPDraft, type LDAPDraft } from '@/lib/ldapAuthentication'
import { ldapBrowserCredentialsAllowed } from '@/lib/ldapBrowserOrigin'
import type { AuthenticationSourceState, LDAPAuthenticationSourceState, LDAPConfigurationCheck, OIDCCompleteResponse } from '@/lib/adminApi.types'
import { usePrincipal } from '@/hooks/usePrincipal'
import AuthenticationImpact from './AuthenticationImpact'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import Icon from '@/components/Icon'

interface Props {
  source: LDAPAuthenticationSourceState
  creating: boolean
  configWritable: boolean
  stale: boolean
  fetching: boolean
  refresh: () => void
  onBack: () => void
  onSaved: (source: AuthenticationSourceState) => void
  onDeleted: () => void
}

export default function LDAPAuthenticationEditor({ source, creating, configWritable, stale, fetching, refresh, onBack, onSaved, onDeleted }: Props) {
  const { t, i18n } = useTranslation()
  const { canManageCredentials, principal } = usePrincipal()
  const [draft, setDraft] = useState(() => ldapDraft(ldapFormSettings(source)))
  const [id, setID] = useState(source.id)
  const [busy, setBusy] = useState<'save' | 'check' | 'trial' | 'delete' | null>(null)
  const [error, setError] = useState('')
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [review, setReview] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [saved, setSaved] = useState(false)
  const [checked, setChecked] = useState<LDAPConfigurationCheck | null>(null)
  const [trial, setTrial] = useState<Extract<OIDCCompleteResponse, { mode: 'test' }> | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const previous = useRef(source)
  const controller = useRef<AbortController | null>(null)
  const allowNavigation = useRef(false)
  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => {
    if (source.configured && previous.current !== source) {
      const before = previous.current
      previous.current = source
      setDraft(current => rebaseLDAPDraft(current, ldapFormSettings(before), ldapFormSettings(source), source.overrides))
      setChecked(null); setTrial(null)
    }
  }, [source])
  const patch = ldapPatch(draft, source.configured!, false, source.overrides)
  const dirty = Object.keys(patch).length > 0 || id !== source.id
  const readOnly = !canManageCredentials || !configWritable || !source.config_writable || stale || fetching || Boolean(busy)
  const canTrial = principal?.auth_method === 'jwt' && principal.login_method === 'password'
  const directoryAllowed = ldapBrowserCredentialsAllowed(window.location.origin)
  const blocker = useBlocker(({ currentLocation, nextLocation }) => !allowNavigation.current && (dirty || Boolean(busy)) && nextLocation.pathname !== '/admin/login' && (currentLocation.pathname !== nextLocation.pathname || currentLocation.search !== nextLocation.search))
  useBeforeUnload(useCallback(event => { if ((dirty || busy) && !allowNavigation.current) { event.preventDefault(); event.returnValue = '' } }, [dirty, busy]))
  useEffect(() => { if (blocker.state === 'blocked' && !dirty && !busy && !error) blocker.proceed() }, [blocker, dirty, busy, error])
  const mayResetSessions = authenticationMayResetSessions(source, patch)
  const impactNeeded = !creating && (deleting || review && mayResetSessions)
  const impact = useQuery({ queryKey: ['admin', 'authentication', 'impact', source.id], queryFn: async ({ signal }) => (await authenticationApi.sourceImpact(source.id, { signal })).data, enabled: impactNeeded, retry: false })
  const impactReady = !impactNeeded || Boolean(impact.data && !impact.isError && !impact.isFetching)
  const impactPanel = <AuthenticationImpact data={impact.isError ? undefined : impact.data} pending={impact.isFetching} retry={() => { void impact.refetch() }} />
  const clearFeedback = (field: string) => { setError(''); setSaved(false); setChecked(null); setTrial(null); setFieldErrors(current => { const next = { ...current }; delete next[field]; return next }) }
  const change = <K extends Exclude<keyof LDAPDraft, 'ldap'>>(field: K, value: LDAPDraft[K]) => { setDraft(current => ({ ...current, [field]: value })); clearFeedback(field) }
  const changeDirectory = (field: Exclude<keyof LDAPDraft['ldap'], 'bind_password_set'>, value: string) => { setDraft(current => ({ ...current, ldap: { ...current.ldap, [field]: value } })); clearFeedback(`ldap.${field}`) }
  const fieldError = (field: string) => fieldErrors[field] && <p id={`ldap-error-${field}`} role="alert" className="text-sm text-destructive">{t(field === 'ldap.url' && fieldErrors[field] === 'INVALID_URL' ? 'ldap.errors.invalidURL' : authenticationValidationKey(fieldErrors[field]))}</p>
  const envelope = () => ({ id, protocol: 'ldap' as const, create: creating, settings: ldapPatch(draft, source.configured!, creating, source.overrides) })
  async function run(action: 'save' | 'check' | 'trial' | 'delete') {
    if (readOnly || ((action === 'save' || action === 'delete') && !impactReady)) return
    if (!ldapBrowserCredentialsAllowed(window.location.origin) && (action === 'check' || action === 'trial' || action === 'save' && envelope().settings.ldap?.bind_password !== undefined)) {
      setPassword(''); setDraft(current => ({ ...current, ldap: { ...current.ldap, bind_password: '' } })); setError('ldap.errors.https')
      if (action === 'save') setReview(false)
      return
    }
    if (action === 'trial' && (creating || dirty || !canTrial || !username.trim() || !password)) return
    setBusy(action); setError(''); setFieldErrors({})
    const abort = new AbortController(); controller.current = abort
    const payload = envelope()
    const proof = password
    if (action === 'trial') { setPassword(''); setTrial(null) }
    if (action === 'save') setDraft(current => ({ ...current, ldap: { ...current.ldap, bind_password: '' } }))
    try {
      if (action === 'check') {
        setChecked(null); setTrial(null)
        const response = await authenticationApi.checkLDAP(payload, { signal: abort.signal })
        if (!abort.signal.aborted) setChecked(response.data)
      } else if (action === 'trial') {
        const response = await authenticationApi.trialLDAP({ ...payload, settings: {}, username: username.trim(), password: proof }, { signal: abort.signal })
        if (!abort.signal.aborted) setTrial(response.data)
      } else if (action === 'delete') {
        await authenticationApi.deleteSource(source.id, { signal: abort.signal })
        if (!abort.signal.aborted) { allowNavigation.current = true; onDeleted() }
      } else {
        const response = creating
          ? await authenticationApi.createSource({ ...payload.settings, id, protocol: 'ldap', display_name: draft.display_name.trim() }, { signal: abort.signal })
          : await authenticationApi.updateSource(id, payload.settings, { signal: abort.signal })
        if (abort.signal.aborted) return
        if (response.data.protocol !== 'ldap' || !response.data.configured) throw new Error('Unexpected authentication source')
        previous.current = response.data
        setDraft(ldapDraft(ldapFormSettings(response.data))); setReview(false); setSaved(true); setChecked(null); setTrial(null)
        if (creating) allowNavigation.current = true
        onSaved(response.data)
      }
    } catch (failure) {
      if (!abort.signal.aborted) {
        const apiError = getApiError(failure)
        setFieldErrors(apiError.fieldErrors ?? {})
        setError(apiError.code === 'INVALID_SETTING' ? 'authentication.invalid' : `ldap.errors.${action}`)
        if (action === 'save') setReview(false)
      }
    } finally { if (!abort.signal.aborted) setBusy(null) }
  }
  const owned = (field: string) => source.overrides[field] || (field.startsWith('ldap.') ? source.overrides.ldap : undefined)
  const disabled = (field: string) => readOnly || Boolean(owned(field)) || field === 'ldap.bind_password' && !directoryAllowed
  const overrideHint = (field: string) => owned(field) && <FieldDescription className="break-all">{t(field === 'ldap.bind_password' ? 'authentication.secretEnvOwned' : 'authentication.envOwned', { variable: owned(field) })}</FieldDescription>
  const directoryField = (field: Exclude<keyof LDAPDraft['ldap'], 'bind_password_set'>, multiline = false) => <Field key={field}>
    <FieldLabel htmlFor={`ldap-${field}`}>{t(`ldap.fields.${field}`)}</FieldLabel>
    {multiline ? <Textarea id={`ldap-${field}`} className="field-sizing-fixed max-h-64 min-h-28 font-mono" rows={4} value={draft.ldap[field]} disabled={disabled(`ldap.${field}`)} onChange={event => changeDirectory(field, event.target.value)} spellCheck={false} />
      : <Input id={`ldap-${field}`} type={field === 'bind_password' ? 'password' : 'text'} value={draft.ldap[field]} disabled={disabled(`ldap.${field}`)} onChange={event => changeDirectory(field, event.target.value)} autoComplete={field === 'bind_password' ? 'new-password' : 'off'} aria-invalid={Boolean(fieldErrors[`ldap.${field}`])} aria-describedby={fieldErrors[`ldap.${field}`] ? `ldap-error-ldap.${field}` : undefined} maxLength={4096} required={draft.enabled && (field !== 'bind_password' || !draft.ldap.bind_password_set)} placeholder={field === 'bind_password' && draft.ldap.bind_password_set ? t('authentication.secretSaved') : undefined} />}
    {fieldError(`ldap.${field}`)}{overrideHint(`ldap.${field}`)}
    {['url', 'user_filter', 'subject_attribute', 'groups_attribute', 'ca_cert_pem', 'bind_password'].includes(field) && <FieldDescription>{t(`ldap.hints.${field}`)}</FieldDescription>}
  </Field>
  const savedSettings = source.configured!
  const reviewRows = Object.entries(envelope().settings).flatMap<{ key: string; label: string; before: unknown; next: unknown; secret: boolean }>(([field, value]) => field === 'ldap'
    ? Object.entries(value).map(([key, next]) => ({ key: `ldap.${key}`, label: t(`ldap.fields.${key}`), before: savedSettings.ldap[key as keyof typeof savedSettings.ldap], next, secret: key === 'bind_password' }))
    : [{ key: field, label: t(`authentication.fields.${field}`), before: savedSettings[field as keyof typeof savedSettings], next: value, secret: false }])
  const displayValue = (value: unknown) => typeof value === 'boolean' ? t(value ? 'authentication.enabled' : 'authentication.disabled') : Array.isArray(value) ? value.join('\n') || '—' : typeof value === 'string' && value ? value : '—'
  return <div className="max-w-6xl space-y-5">
    <Button variant="ghost" disabled={Boolean(busy)} onClick={onBack}><Icon name="arrow_back" size="sm" />{t('authentication.backToSources')}</Button>
    <section className="flex flex-wrap items-center justify-between gap-4 rounded-lg border border-border bg-card p-5"><div className="space-y-2"><div className="flex flex-wrap items-center gap-2"><h2 className="break-all text-base font-semibold">{creating ? t('authentication.addSource') : source.effective?.display_name || source.configured?.display_name || id}</h2><Badge variant="outline">LDAP</Badge>{!creating && <Badge variant={source.effective?.enabled ? 'success' : 'secondary'}>{t('authentication.running')}: {t(source.effective ? source.effective.enabled ? 'authentication.enabled' : 'authentication.disabled' : 'authentication.notLoaded')}</Badge>}</div><p className="text-sm text-muted-foreground">{t('ldap.description')}</p></div>{!creating && <Button variant="outline" disabled={Boolean(busy) || dirty || fetching} onClick={refresh}>{t('now.refresh')}</Button>}</section>
    {source.pending_restart && <Alert variant="warning"><AlertDescription>{t('authentication.pendingRestart')} {source.effective?.enabled && t(source.sessions_will_reset === false ? 'authentication.sessionsPreserved' : 'authentication.sessionsMayReset')}</AlertDescription></Alert>}
    {saved && <Alert variant="success"><AlertDescription>{t('authentication.saved')}</AlertDescription></Alert>}
    {stale && <Alert variant="warning"><AlertDescription>{t('authentication.stale')} <Button variant="link" onClick={refresh}>{t('now.refresh')}</Button></AlertDescription></Alert>}
    {!canManageCredentials && <Alert variant="info"><AlertDescription>{t('authentication.localAdminRequired')}</AlertDescription></Alert>}
    {(!configWritable || !source.config_writable) && <Alert variant="warning"><AlertDescription>{t('authentication.readonlyConfig')}</AlertDescription></Alert>}
    {!directoryAllowed && <Alert variant="warning"><AlertDescription>{t('ldap.errors.https')}</AlertDescription></Alert>}
    {error && !deleting && (directoryAllowed || error !== 'ldap.errors.https') && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}
    <div className="grid min-w-0 gap-8 xl:grid-cols-[minmax(0,1fr)_300px]">
      <form className="min-w-0 space-y-6" onSubmit={event => { event.preventDefault(); if (!readOnly) setReview(true) }}>
        <section className="space-y-4"><h2 className="text-base font-semibold">{t('ldap.connection')}</h2>
          <Field><FieldLabel htmlFor="ldap-display-name">{t('authentication.fields.display_name')}</FieldLabel><Input id="ldap-display-name" value={draft.display_name} onChange={event => change('display_name', event.target.value)} disabled={disabled('display_name')} required maxLength={80} />{fieldError('display_name')}{overrideHint('display_name')}</Field>
          {directoryField('url')}<div className="grid gap-4 sm:grid-cols-2">{directoryField('bind_dn')}{directoryField('bind_password')}</div>{directoryField('user_base_dn')}{directoryField('user_filter')}
        </section>
        <section className="space-y-4 border-t border-border pt-5"><h2 className="text-base font-semibold">{t('ldap.identityMapping')}</h2><div className="grid gap-4 sm:grid-cols-2">{directoryField('subject_attribute')}{directoryField('display_name_attribute')}</div>{directoryField('groups_attribute')}
          <div className="grid gap-4 sm:grid-cols-2">{(['admin_groups', 'readonly_groups'] as const).map(field => <Field key={field}><FieldLabel htmlFor={`ldap-${field}`}>{t(`authentication.fields.${field}`)}</FieldLabel><Textarea id={`ldap-${field}`} value={draft[field]} disabled={disabled(field)} onChange={event => change(field, event.target.value)} rows={3} />{fieldError(field)}{overrideHint(field)}</Field>)}</div><p className="text-sm text-muted-foreground">{t('ldap.hints.roles')}</p>
          <Field><FieldLabel htmlFor="ldap-ttl">{t('authentication.fields.session_ttl')}</FieldLabel><Input id="ldap-ttl" value={draft.session_ttl} disabled={disabled('session_ttl')} onChange={event => change('session_ttl', event.target.value)} required />{fieldError('session_ttl')}{overrideHint('session_ttl')}</Field>
        </section>
        <section className="space-y-4 border-t border-border pt-5"><h2 className="text-base font-semibold">{t('ldap.certificate')}</h2>{directoryField('ca_cert_pem', true)}
          <Field><FieldLabel htmlFor="ldap-source-id">{t('authentication.sourceID')}</FieldLabel><Input id="ldap-source-id" value={id} disabled={!creating || readOnly} pattern="[a-z][a-z0-9-]{0,31}" maxLength={32} required onChange={event => { setID(event.target.value); clearFeedback('id') }} />{fieldError('id')}<FieldDescription>{t('authentication.idHint')}</FieldDescription></Field>
        </section>
        <section className="space-y-2 border-t border-border pt-4"><Field orientation="horizontal"><Switch id="ldap-enabled" checked={draft.enabled} disabled={disabled('enabled')} onCheckedChange={value => change('enabled', value)} /><FieldLabel htmlFor="ldap-enabled">{t('authentication.fields.enabled')}</FieldLabel></Field><p className="text-sm text-muted-foreground">{t('authentication.enableHint')}</p>{overrideHint('enabled')}</section>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4"><p className="text-sm text-muted-foreground">{t(dirty ? 'authentication.unsaved' : 'authentication.restartHint')}</p><div className="flex flex-wrap gap-2"><Button type="button" variant="outline" disabled={!dirty || Boolean(busy)} onClick={() => { setDraft(ldapDraft(ldapFormSettings(source))); setID(source.id); setError(''); setFieldErrors({}); setChecked(null); setTrial(null) }}>{t('authentication.discard')}</Button><Button type="submit" disabled={readOnly || (!dirty && !creating) || !draft.display_name.trim()}>{t('authentication.reviewSave')}</Button></div></div>
      </form>
      <aside className="min-w-0 space-y-5 text-sm leading-6" aria-label={t('authentication.guide')}>
        <section className="space-y-3 rounded-lg border border-border bg-card p-4"><h2 className="text-sm font-semibold">{t('ldap.checkTitle')}</h2><p className="text-muted-foreground">{t('ldap.checkHint')}</p><Button className="w-full" variant="outline" disabled={readOnly || !directoryAllowed} onClick={() => { void run('check') }}>{t(busy === 'check' ? 'authentication.checking' : 'authentication.checkConfiguration')}</Button>{checked && <div role="status"><p className="font-medium text-success">{t('ldap.checkPassed')}</p><p className="text-muted-foreground">{new Date(checked.checked_at).toLocaleString(i18n.language)}</p></div>}</section>
        <form className="space-y-3 rounded-lg border border-border bg-card p-4" onSubmit={event => { event.preventDefault(); void run('trial') }}><h2 className="text-sm font-semibold">{t('authentication.trialLogin')}</h2><p className="text-muted-foreground">{t('ldap.trialHint')}</p>
          <Field><FieldLabel htmlFor="ldap-trial-username">{t('ldap.username')}</FieldLabel><Input id="ldap-trial-username" value={username} onChange={event => { setUsername(event.target.value); setTrial(null) }} disabled={readOnly || !directoryAllowed || !canTrial || creating || dirty} autoComplete="off" required /></Field>
          <Field><FieldLabel htmlFor="ldap-trial-password">{t('ldap.password')}</FieldLabel><Input id="ldap-trial-password" type="password" value={password} onChange={event => { setPassword(event.target.value); setTrial(null) }} disabled={readOnly || !directoryAllowed || !canTrial || creating || dirty} autoComplete="new-password" required /></Field>
          <Button type="submit" variant="outline" className="w-full" disabled={readOnly || !directoryAllowed || !canTrial || creating || dirty || !username.trim() || !password}>{t(busy === 'trial' ? 'account.processing' : 'ldap.testCredentials')}</Button>
          {!canTrial ? <p className="text-muted-foreground">{t('authentication.trialLocalSession')}</p> : (creating || dirty) && <p className="text-muted-foreground">{t('authentication.saveBeforeTrial')}</p>}
          {trial && <Alert variant={trial.outcome === 'success' ? 'success' : trial.outcome === 'denied' ? 'warning' : 'destructive'}><AlertDescription><p>{t(trial.outcome === 'success' ? 'account.testSuccess' : trial.outcome === 'denied' ? 'account.testDenied' : 'ldap.testFailed')}</p><p>{trial.authenticated ? trial.display_name || t('account.testAuthenticated') : t('account.testNotAuthenticated')}</p><p>{t('account.testRole')}: {trial.mapped_role ? t(`users.${trial.mapped_role}`) : t('account.testNoRole')}</p><p>{new Date(trial.checked_at).toLocaleString(i18n.language)}</p></AlertDescription></Alert>}
        </form>
      </aside>
    </div>
    {!creating && <div className="flex flex-wrap items-center justify-between gap-4 border-t border-border pt-5"><p className="text-sm text-muted-foreground">{t('authentication.removeHint')}</p><Button variant="outline" disabled={readOnly || dirty || Object.keys(source.overrides).length > 0} onClick={() => { setError(''); setDeleting(true) }}>{t('authentication.removeSource')}</Button></div>}
    <Dialog open={blocker.state === 'blocked'} onOpenChange={open => { if (!open && blocker.state === 'blocked') blocker.reset() }}><DialogContent className="max-w-md" finalFocus><DialogTitle>{t(busy ? 'authentication.savingLeaveTitle' : 'authentication.leaveTitle')}</DialogTitle><DialogDescription>{t(busy ? 'authentication.savingLeaveHint' : 'authentication.leaveHint')}</DialogDescription><div className="flex flex-wrap justify-end gap-2"><Button variant="outline" onClick={() => { if (blocker.state === 'blocked') blocker.reset() }}>{t('cancel')}</Button><Button variant="destructive" disabled={Boolean(busy)} onClick={() => { if (blocker.state === 'blocked') blocker.proceed() }}>{t('authentication.discardAndLeave')}</Button></div></DialogContent></Dialog>
    <Dialog open={review} onOpenChange={open => { if (!busy) setReview(open) }}><DialogContent className="max-w-xl" finalFocus closeDisabled={Boolean(busy)}><DialogTitle>{t('authentication.reviewTitle')}</DialogTitle><DialogDescription>{t('authentication.reviewHint')}</DialogDescription><div className="space-y-2 text-sm"><p className="break-all font-medium">{draft.display_name} · LDAP · {id}</p><p className="break-all text-muted-foreground">{draft.ldap.url}</p><p>{t('ldap.reviewSummary')}</p>{reviewRows.map(row => <div key={row.key} className="space-y-1 border-b border-border pb-2"><p className="font-medium">{row.label}</p><div className="grid min-w-0 gap-1 sm:grid-cols-2"><p className="whitespace-pre-wrap break-all text-muted-foreground">{t('authentication.before')}: {row.secret ? t(source.configured!.ldap.bind_password_set ? 'authentication.secretPresent' : 'authentication.secretAbsent') : displayValue(row.before)}</p><p className="whitespace-pre-wrap break-all">{t('authentication.after')}: {row.secret ? t('authentication.secretChanged') : displayValue(row.next)}</p></div></div>)}{source.effective?.enabled && <p>{t(mayResetSessions ? 'authentication.sessionsMayReset' : 'authentication.sessionsPreserved')}</p>}</div>{impactNeeded && impactPanel}<div className="flex flex-wrap justify-end gap-2"><Button variant="outline" disabled={Boolean(busy)} onClick={() => setReview(false)}>{t('cancel')}</Button><Button disabled={readOnly || !impactReady} onClick={() => { void run('save') }}>{t(busy === 'save' ? 'saving' : 'authentication.confirmSave')}</Button></div></DialogContent></Dialog>
    <Dialog open={deleting} onOpenChange={open => { if (!busy) setDeleting(open) }}><DialogContent className="max-w-lg" finalFocus closeDisabled={Boolean(busy)}><DialogTitle>{t('authentication.removeNamed', { name: source.configured?.display_name || id })}</DialogTitle><DialogDescription>{t('authentication.removeDescription')}</DialogDescription>{impactPanel}{error && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}<div className="flex flex-wrap justify-end gap-2"><Button variant="outline" disabled={Boolean(busy)} onClick={() => setDeleting(false)}>{t('cancel')}</Button><Button variant="destructive" disabled={readOnly || !impactReady} onClick={() => { void run('delete') }}>{t('authentication.confirmRemove')}</Button></div></DialogContent></Dialog>
  </div>
}
