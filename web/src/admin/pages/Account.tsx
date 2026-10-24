import { useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import AdminPage from '@/admin/components/AdminPage'
import { getAdminRouteHref } from '@/admin/routes'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authenticationApi } from '@/lib/authenticationApi'
import type { LoginIdentitiesResponse, LoginIdentity } from '@/lib/adminApi.types'
import { identityAuthorizationURL, identityErrorKey } from '@/lib/identityAuth'
import { clearOIDCTrial } from '@/lib/oidcTrialContext'
import { ldapBrowserCredentialsAllowed } from '@/lib/ldapBrowserOrigin'
import { formatDate, formatTime } from '@/lib/utils'
import QueryErrorState from '@/components/QueryErrorState'
import Icon from '@/components/Icon'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

type AccountProvider = NonNullable<LoginIdentitiesResponse['providers']>[number]
type ProofAction = { kind: 'link'; provider: AccountProvider } | { kind: 'unlink'; identity: LoginIdentity }

export default function Account() {
  const { t } = useTranslation()
  const location = useLocation()
  const queryClient = useQueryClient()
  const { principal } = usePrincipal()
  const apiTokenSession = principal?.auth_method === 'api_token'
  const [action, setAction] = useState<ProofAction | null>(null)
  const [password, setPassword] = useState('')
  const [directoryUsername, setDirectoryUsername] = useState('')
  const [directoryPassword, setDirectoryPassword] = useState('')
  const directoryBlocked = action?.kind === 'link' && action.provider.protocol === 'ldap' && !ldapBrowserCredentialsAllowed(window.location.origin)
  const [linked, setLinked] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [unlinked, setUnlinked] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => { request.current?.abort() }, [])
  const identities = useQuery({
    queryKey: ['auth', 'identities'],
    queryFn: async ({ signal }) => (await authenticationApi.identities({ signal })).data,
    enabled: Boolean(principal && !apiTokenSession),
    retry: false,
  })
  const data = identities.data
  // The array is authoritative when present, including an empty array. Keep
  // compatibility with instances that still expose the single-provider DTO.
  const providers: AccountProvider[] = data?.providers ?? (data?.provider ? [{ ...data.provider, can_link: data.can_link }] : [])
  const boundSources = new Set(data?.identities.map(identity => identity.provider))
  const unboundProviders = providers.filter(provider => !boundSources.has(provider.id))
  const availableProviders = unboundProviders.filter(provider => provider.enabled)
  const disabledProviders = unboundProviders.filter(provider => !provider.enabled)
  const localProofAvailable = data?.permission_source === 'local' && data.password_enabled
  const identityName = (identity: LoginIdentity) => identity.provider_matches === false ? t('account.previousProviderNamed', { id: identity.provider }) : identity.provider_name

  function openAction(next: ProofAction) {
    setPassword('')
    setDirectoryUsername(''); setDirectoryPassword('')
    setError('')
    setAction(next)
  }
  function closeAction() {
    if (busy) return
    setAction(null)
    setPassword('')
    setDirectoryUsername(''); setDirectoryPassword('')
    setError('')
  }
  async function submitProof(event: React.FormEvent) {
    event.preventDefault()
    if (action?.kind === 'link' && action.provider.protocol === 'ldap' && !ldapBrowserCredentialsAllowed(window.location.origin)) {
      setPassword(''); setDirectoryPassword(''); setError('ldap.errors.https')
      return
    }
    if (!action || !password || busy) return
    setBusy(true)
    setError('')
    const controller = new AbortController()
    request.current = controller
    const submittedFrom = window.location.pathname
    const proof = password
    const directoryProof = directoryPassword
    setPassword(''); setDirectoryPassword('')
    try {
      // Password proofs never enter Query's mutation cache or browser storage.
      if (action.kind === 'link') {
        clearOIDCTrial()
        if (action.provider.protocol === 'ldap') {
          await authenticationApi.linkLDAP({ provider_id: action.provider.id, password: proof, username: directoryUsername.trim(), directory_password: directoryProof }, { signal: controller.signal })
          if (controller.signal.aborted || window.location.pathname !== submittedFrom) return
          setAction(null); setLinked(true); setUnlinked(false)
          await queryClient.invalidateQueries({ queryKey: ['auth', 'identities'] })
          return
        }
        const { data: response } = await authenticationApi.linkIdentity(proof, action.provider.id, { signal: controller.signal })
        if (controller.signal.aborted || window.location.pathname !== submittedFrom) return
        window.location.assign(identityAuthorizationURL(response.authorization_url))
      } else {
        await authenticationApi.unlinkIdentity(action.identity.id, proof, { signal: controller.signal })
        if (controller.signal.aborted || window.location.pathname !== submittedFrom) return
        setAction(null)
        setUnlinked(true); setLinked(false)
        await queryClient.invalidateQueries({ queryKey: ['auth', 'identities'] })
        // Unlinking may revoke this very session when it used the removed identity.
        await queryClient.invalidateQueries({ queryKey: ['auth', 'principal'] })
      }
    } catch (failure: unknown) {
      if (!controller.signal.aborted) setError(identityErrorKey(failure))
    } finally {
      setBusy(false)
    }
  }

  function providerOption(provider: AccountProvider) {
    return <div key={provider.id} className="p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="flex min-w-0 items-center gap-2 text-[15px] font-semibold"><Icon name="link" size="sm" className="shrink-0 text-muted-foreground" /><span className="break-all">{provider.name}</span></h3>
        {provider.enabled && localProofAvailable && data?.can_link && provider.can_link
          ? <Button className="h-auto min-h-10 max-w-full whitespace-normal break-words py-2" disabled={identities.isRefetchError} onClick={() => openAction({ kind: 'link', provider })}>{t('account.linkProvider', { name: provider.name })}</Button>
          : <Badge variant="secondary">{t(provider.enabled ? 'account.notBound' : 'account.notEnabled')}</Badge>}
      </div>
      {!provider.enabled && <p className="mt-2 text-sm leading-6 text-muted-foreground">{t('account.sourceDisabledHint')}</p>}
      <details className="group mt-2 text-[13px] leading-6 text-muted-foreground">
        <summary aria-label={t('account.detailsFor', { name: provider.name })} className="stripe-focus-ring flex min-h-10 cursor-pointer items-center gap-2 rounded-md">{t('account.methodDetails')}<Icon name="expand_more" size="sm" className="group-open:rotate-180" /></summary>
        <div className="space-y-2 pt-1">
          <p className="break-all">{t('account.sourceId', { id: provider.id })} · {provider.protocol?.toUpperCase() ?? 'OIDC'}</p>
          {provider.enabled && Boolean(provider.session_ttl_seconds) && <p>{t('account.sessionMinutes', { count: (provider.session_ttl_seconds ?? 0) / 60 })}</p>}
        </div>
      </details>
    </div>
  }

  return <AdminPage description={t('account.subtitle')} actions={!apiTokenSession && <Button variant="outline" size="sm" disabled={identities.isFetching} onClick={() => { void identities.refetch() }}><Icon name="refresh" size="sm" />{t('now.refresh')}</Button>}>
    <div className="min-w-0 max-w-3xl space-y-6">
      <section className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-card p-5">
        <div className="min-w-0"><p className="break-all text-base font-semibold">{principal?.username}</p><p className="mt-1 text-sm text-muted-foreground">{t(principal?.permission_source === 'oidc' || principal?.permission_source === 'ldap' ? 'account.managedAuthority' : 'account.localAuthority')}</p></div>
        <Badge variant="secondary">{t(principal?.role === 'admin' ? 'users.admin' : 'users.readonly')}</Badge>
      </section>
      {apiTokenSession ? <Alert variant="info"><AlertDescription>{t('account.apiTokenSession')}</AlertDescription></Alert> : identities.isPending ? <p role="status" className="py-8 text-sm text-muted-foreground">{t('loading')}</p> : identities.isError && !data ? <QueryErrorState message={t('account.loadError')} onRetry={() => identities.refetch()} /> : data && <>
        {identities.isRefetchError && <Alert variant="warning"><AlertDescription>{t('account.stale')}</AlertDescription></Alert>}
        {(location.state?.identityLinked || linked) && !unlinked && <Alert variant="success"><AlertDescription>{t('account.linked')}</AlertDescription></Alert>}
        {unlinked && <Alert variant="success"><AlertDescription>{t('account.unlinked')}</AlertDescription></Alert>}
        <section aria-labelledby="account-methods" className="space-y-4">
          <h2 id="account-methods" className="text-base font-semibold">{t('account.methods')}</h2>
          <div className="divide-y divide-border rounded-lg border border-border bg-card">
            <div className="p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h3 className="flex items-center gap-2 text-[15px] font-semibold"><Icon name="key" size="sm" className="text-muted-foreground" />{t('account.password')}</h3>
                <div className="flex flex-wrap gap-2">
                  {principal?.login_method === 'password' && <Badge variant="outline">{t('account.currentSession')}</Badge>}
                  <Badge variant={data.password_enabled ? 'success' : 'secondary'}>{t(data.password_enabled ? 'account.available' : 'account.notConfigured')}</Badge>
                </div>
              </div>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{t(data.password_enabled ? 'account.passwordHint' : 'account.passwordUnavailable')}</p>
            </div>
            {data.identities.map(identity => {
              const provider = providers.find(source => source.id === identity.provider)
              const loginAvailable = identity.provider_matches !== false && (identity.login_available ?? Boolean(provider?.enabled))
              const current = data.current_identity_id === identity.id && (!data.current_provider_id || data.current_provider_id === identity.provider)
              return <div key={identity.id} className="p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h3 className="flex min-w-0 items-center gap-2 text-[15px] font-semibold"><Icon name="link" size="sm" className="shrink-0 text-muted-foreground" /><span className="break-all">{identityName(identity)}</span></h3>
                  <div className="flex flex-wrap gap-2">
                    {current && <Badge variant="outline">{t('account.currentSession')}</Badge>}
                    <Badge variant={loginAvailable ? 'success' : 'warning'}>{t(loginAvailable ? 'account.bound' : 'account.boundUnavailable')}</Badge>
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0 flex-1 basis-48">
                    <p className="break-all text-sm leading-6">{identity.display_name}</p>
                    {identity.provider_matches === false
                      ? <p className="mt-2 text-sm leading-6 text-muted-foreground">{t('account.boundProviderChanged')}</p>
                      : !loginAvailable && <p className="mt-2 text-sm leading-6 text-muted-foreground">{t(provider?.enabled === false ? 'account.boundProviderDisabled' : 'account.boundProviderUnknown')}</p>}
                    {!identity.can_unlink && <p className="mt-2 text-sm leading-6 text-muted-foreground">{t('account.managedHint')}</p>}
                  </div>
                  {localProofAvailable && identity.can_unlink && <Button variant="outline" aria-label={identity.provider_matches === false ? t('account.unlinkTitle', { name: identityName(identity) }) : undefined} disabled={identities.isRefetchError} onClick={() => openAction({ kind: 'unlink', identity })}>{t('account.unlink')}</Button>}
                </div>
                <details className="group mt-2 text-[13px] leading-6 text-muted-foreground">
                  <summary aria-label={t('account.detailsFor', { name: identityName(identity) })} className="stripe-focus-ring flex min-h-10 cursor-pointer items-center gap-2 rounded-md">{t('account.methodDetails')}<Icon name="expand_more" size="sm" className="group-open:rotate-180" /></summary>
                  <div className="space-y-2 pt-1">
                    <p className="break-all">{t('account.sourceId', { id: identity.provider })}</p>
                    {loginAvailable && Boolean(provider?.session_ttl_seconds) && <p>{t('account.sessionMinutes', { count: (provider?.session_ttl_seconds ?? 0) / 60 })}</p>}
                    <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
                      <div><dt>{t('account.boundAt')}</dt><dd className="mt-1 tabular-nums">{formatDate(identity.created_at)} {formatTime(identity.created_at, 'time')}</dd></div>
                      <div><dt>{t('users.lastLogin')}</dt><dd className="mt-1 tabular-nums">{identity.last_login_at && !identity.last_login_at.startsWith('0001-') ? `${formatDate(identity.last_login_at)} ${formatTime(identity.last_login_at, 'time')}` : t('account.neverSignedIn')}</dd></div>
                    </dl>
                  </div>
                </details>
              </div>
            })}
          </div>
        </section>
        {availableProviders.length > 0 && (localProofAvailable ? <section aria-labelledby="account-available" className="space-y-3">
          <h2 id="account-available" className="text-base font-semibold">{t('account.availableMethods')}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{t('account.linkAvailableHint')}</p>
          <div className="divide-y divide-border rounded-lg border border-border bg-card">{availableProviders.map(providerOption)}</div>
        </section> : <p className="text-sm leading-6 text-muted-foreground">{t('account.managedLinkHint')}</p>)}
        {disabledProviders.length > 0 && <details className="group rounded-lg border border-border bg-card">
          <summary className="stripe-focus-ring flex min-h-12 cursor-pointer items-center justify-between gap-3 rounded-lg px-5 py-3 text-sm text-muted-foreground">{t('account.disabledSources', { count: disabledProviders.length })}<Icon name="expand_more" size="sm" className="group-open:rotate-180" /></summary>
          <div className="divide-y divide-border border-t border-border">{disabledProviders.map(providerOption)}</div>
        </details>}
        {providers.length === 0 && data.identities.length === 0 && <section className="space-y-2 rounded-lg border border-border bg-card p-5">
          <h2 className="text-base font-semibold">{t('account.externalLogin')}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{t('account.providerDisabledHint')}</p>
        </section>}
        {providers.every(provider => !provider.enabled) && (principal?.role === 'admin'
          ? <Button variant="outline" nativeButton={false} role="link" render={<Link to={getAdminRouteHref('authentication')} />}>{t('account.manageAuthentication')}<Icon name="arrow_forward" size="sm" /></Button>
          : <p className="text-sm leading-5 text-muted-foreground">{t('account.contactAdmin')}</p>)}
        {(providers.some(provider => provider.enabled) || data.identities.length > 0) && <details className="group text-[13px] leading-6 text-muted-foreground">
          <summary className="stripe-focus-ring flex min-h-10 cursor-pointer items-center gap-2 rounded-md font-medium text-foreground">{t('account.bindingDetails')}<Icon name="expand_more" size="sm" className="group-open:rotate-180" /></summary>
          <div className="space-y-2 pt-2"><p>{t('account.bindingScope')}</p><p>{t('account.logoutScope')}</p></div>
        </details>}
      </>}
    </div>
    <Dialog open={action !== null} onOpenChange={open => { if (!open) closeAction() }} modal>
      <DialogContent className="max-w-md" finalFocus closeDisabled={busy}>
        <DialogTitle className="break-words pr-10">{t(action?.kind === 'unlink' ? 'account.unlinkTitle' : 'account.linkTitle', { name: action?.kind === 'unlink' ? identityName(action.identity) : action?.provider.name })}</DialogTitle>
        <DialogDescription className="text-sm leading-6">{t(action?.kind === 'unlink' ? 'account.unlinkHint' : action?.provider.protocol === 'ldap' ? 'ldap.linkHint' : 'account.linkHint')}</DialogDescription>
        {action?.kind === 'unlink' && data?.current_identity_id === action.identity.id && <Alert variant="warning"><AlertDescription>{t('account.unlinkCurrentSession')}</AlertDescription></Alert>}
        {directoryBlocked && <Alert variant="warning"><AlertDescription>{t('ldap.errors.https')}</AlertDescription></Alert>}
        <form className="space-y-5" onSubmit={event => { void submitProof(event) }}>
          <Field><FieldLabel htmlFor="account-proof">{t('account.currentPassword')}</FieldLabel><Input id="account-proof" type="password" autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} required disabled={busy || directoryBlocked} /></Field>
          {action?.kind === 'link' && action.provider.protocol === 'ldap' && <><Field><FieldLabel htmlFor="account-directory-username">{t('ldap.username')}</FieldLabel><Input id="account-directory-username" value={directoryUsername} onChange={event => setDirectoryUsername(event.target.value)} autoComplete="off" required disabled={busy || directoryBlocked} /></Field><Field><FieldLabel htmlFor="account-directory-password">{t('ldap.password')}</FieldLabel><Input id="account-directory-password" type="password" value={directoryPassword} onChange={event => setDirectoryPassword(event.target.value)} autoComplete="new-password" required disabled={busy || directoryBlocked} /></Field></>}
          {error && !directoryBlocked && <Alert variant="destructive"><AlertDescription>{t(error)}</AlertDescription></Alert>}
          <div className="flex flex-wrap justify-end gap-2"><Button type="button" variant="outline" disabled={busy} onClick={closeAction}>{t('cancel')}</Button><Button type="submit" variant={action?.kind === 'unlink' ? 'destructive' : 'default'} disabled={busy || directoryBlocked || !password || (action?.kind === 'link' && action.provider.protocol === 'ldap' && (!directoryUsername.trim() || !directoryPassword))} aria-busy={busy}>{t(busy ? 'account.processing' : action?.kind === 'unlink' ? 'account.confirmUnlink' : action?.provider.protocol === 'ldap' ? 'ldap.confirmLink' : 'account.continueToProvider')}</Button></div>
        </form>
      </DialogContent>
    </Dialog>
  </AdminPage>
}
