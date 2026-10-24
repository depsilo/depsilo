import { Field, FieldLabel } from '@/components/ui/field'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import Icon from '@/components/Icon'
import { Input } from '@/components/ui/input'
import Logo from '@/components/Logo'
import { authApi } from '@/lib/api'
import { authenticationApi } from '@/lib/authenticationApi'
import { loginDestination } from '@/lib/adminLoginDestination'
import { identityAuthorizationURL, identityErrorKey } from '@/lib/identityAuth'
import { getApiError } from '@/lib/apiError'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import type { IdentityProvider } from '@/lib/adminApi.types'
import { writeLocalStorage } from '@/lib/storage'
import { clearOIDCTrial } from '@/lib/oidcTrialContext'
import { ldapBrowserCredentialsAllowed } from '@/lib/ldapBrowserOrigin'

export default function Login() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [directory, setDirectory] = useState<IdentityProvider | null>(null)
  const [directoryUsername, setDirectoryUsername] = useState('')
  const [directoryPassword, setDirectoryPassword] = useState('')
  const [directoryError, setDirectoryError] = useState('')
  const directoryAllowed = ldapBrowserCredentialsAllowed(window.location.origin)
  const [oidcLoading, setOIDCLoading] = useState<string | null>(null)
  const providers = useQuery({ queryKey: ['auth', 'providers'], queryFn: async ({ signal }) => (await authenticationApi.providers({ signal })).data.providers, retry: false })
  const [error, setError] = useState('')
  const [sourceSearch, setSourceSearch] = useState('')
  const [oidcError, setOIDCError] = useState<{ source: string; key: string } | null>(null)
  const visibleProviders = providers.data?.filter(provider => provider.name.toLocaleLowerCase().includes(sourceSearch.trim().toLocaleLowerCase())) ?? []
  const loginRequestRef = useRef<AbortController | null>(null)

  useEffect(() => () => {
    loginRequestRef.current?.abort()
    loginRequestRef.current = null
  }, [])

  const cancelLoginForPortalNavigation = (event: React.MouseEvent<HTMLAnchorElement>) => {
    // Modified clicks keep the Login page open while opening a second tab, so
    // only cancel when this browsing context is actually leaving the route.
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    loginRequestRef.current?.abort()
    loginRequestRef.current = null
  }

  const handleOIDC = async (providerId: string) => {
    if (loading || oidcLoading) return
    setError('')
    setOIDCError(null)
    setOIDCLoading(providerId)
    clearOIDCTrial()
    const controller = new AbortController()
    loginRequestRef.current = controller
    const submittedFrom = `${window.location.pathname}${window.location.search}${window.location.hash}`
    try {
      const { data } = await authenticationApi.startOIDC(loginDestination(location.state, location.search), providerId, { signal: controller.signal })
      const currentLocation = `${window.location.pathname}${window.location.search}${window.location.hash}`
      if (!controller.signal.aborted && currentLocation === submittedFrom) window.location.assign(identityAuthorizationURL(data.authorization_url))
    } catch (failure: unknown) {
      if (!controller.signal.aborted) setOIDCError({ source: providerId, key: identityErrorKey(failure) })
    } finally {
      if (loginRequestRef.current === controller) {
        loginRequestRef.current = null
        setOIDCLoading(null)
      }
    }
  }

  async function handleDirectory(event: React.FormEvent) {
    event.preventDefault()
    if (!ldapBrowserCredentialsAllowed(window.location.origin)) {
      setDirectoryPassword(''); setDirectoryError('ldap.errors.https')
      return
    }
    if (!directory || loading || oidcLoading || !directoryUsername.trim() || !directoryPassword) return
    clearOIDCTrial()
    setDirectoryError(''); setOIDCLoading(directory.id)
    const proof = directoryPassword
    setDirectoryPassword('')
    const controller = new AbortController()
    loginRequestRef.current = controller
    const submittedFrom = `${window.location.pathname}${window.location.search}${window.location.hash}`
    try {
      const { data } = await authenticationApi.loginLDAP({ provider_id: directory.id, username: directoryUsername.trim(), password: proof, return_to: loginDestination(location.state, location.search) }, { signal: controller.signal })
      if (controller.signal.aborted || `${window.location.pathname}${window.location.search}${window.location.hash}` !== submittedFrom) return
      if (data.mode === 'choice') { navigate('/admin/auth/complete', { replace: true }); return }
      if (!writeLocalStorage('token', data.token)) { setDirectoryError('login.storageUnavailable'); return }
      queryClient.clear()
      navigate(loginDestination(location.state, location.search), { replace: true })
    } catch (failure) { if (!controller.signal.aborted) setDirectoryError(identityErrorKey(failure)) }
    finally { if (loginRequestRef.current === controller) { loginRequestRef.current = null; setOIDCLoading(null) } }
  }

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (loading || oidcLoading) return
    setError('')
    setOIDCError(null)
    setLoading(true)
    clearOIDCTrial()
    const controller = new AbortController()
    loginRequestRef.current = controller
    const submittedFrom = `${window.location.pathname}${window.location.search}${window.location.hash}`
    try {
      const response = await authApi.login(
        { username: username.trim(), password },
        { signal: controller.signal },
      )
      const currentLocation = `${window.location.pathname}${window.location.search}${window.location.hash}`
      // React Router updates history before a concurrent route transition has
      // necessarily unmounted Login. Ignore a response that wins that narrow
      // cleanup race instead of storing its token and pulling the user back.
      if (controller.signal.aborted || currentLocation !== submittedFrom) return
      if (!writeLocalStorage('token', response.data.token)) {
        setError(t('login.storageUnavailable'))
        return
      }
      queryClient.clear()
      navigate(loginDestination(location.state, location.search), { replace: true })
    } catch (requestError: unknown) {
      if (controller.signal.aborted) return
      const status = getApiError(requestError).status
      setError(status === 429
        ? t('login.rateLimited')
        : status === 401 || status === 403
          ? t('login.failed')
          : t('login.unavailable'))
    } finally {
      if (loginRequestRef.current === controller) {
        loginRequestRef.current = null
        setLoading(false)
      }
    }
  }

  return (
    <main className="grid min-h-screen place-items-center bg-[var(--bg-page)] px-4 py-16 sm:px-6">
      <div className="w-full max-w-[420px]">
        <div className="mb-12 flex items-center justify-between gap-4">
          <Link
            to="/"
            aria-label="Depsilo"
            onClick={cancelLoginForPortalNavigation}
            className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-2 rounded-md text-[var(--text)] no-underline transition-opacity duration-150 hover:opacity-75"
          >
            <Logo size={28} />
            <span className="font-display text-[16px] font-[700]">Depsilo</span>
          </Link>
          <Link
            to="/"
            onClick={cancelLoginForPortalNavigation}
            className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-1.5 rounded-md px-2 text-[12px] font-[550] text-[var(--text-muted)] no-underline transition-colors duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)]"
          >
            <Icon name="arrow_back" size="sm" />
            {t('login.backToPortal')}
          </Link>
        </div>

        <header className="mb-8">
          <h1 className="m-0 font-display text-[30px] font-[650] leading-[1.1] text-[var(--text)]">
            {t('login.title')}
          </h1>
          <p className="mt-2 max-w-[46ch] text-[14px] leading-6 text-[var(--text-muted)]">
            {t('login.subtitle')}
          </p>
        </header>

        <form onSubmit={handleSubmit} className="space-y-5" aria-busy={loading || undefined} aria-labelledby="local-sign-in">
          <h2 id="local-sign-in" className="text-base font-semibold">{t('login.localSignIn')}</h2>
          <Field>
            <FieldLabel>{t('login.username')}</FieldLabel>
            <Input
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              placeholder={t('login.usernamePlaceholder')}
              autoComplete="username"
              autoFocus
              required
            />
          </Field>
          <Field>
            <FieldLabel>{t('login.password')}</FieldLabel>
            <Input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder={t('login.passwordPlaceholder')}
              autoComplete="current-password"
              required
            />
          </Field>

          {error && (
            <p
              role="alert"
              className="rounded-md border border-[var(--danger-border)] bg-[var(--danger-fill)] px-3 py-2.5 text-[13px] leading-5 text-[var(--danger-text)]"
            >
              {error}
            </p>
          )}

          <Button
            type="submit"
            className="min-h-10 w-full"
            aria-busy={loading || undefined}
            disabled={loading || Boolean(oidcLoading) || !username.trim() || !password}
          >
            {loading ? t('login.submitting') : t('login.submit')}
          </Button>
        </form>
        {(providers.isPending || providers.isError || Boolean(providers.data?.length)) && <section aria-labelledby="organization-sign-in" className="mt-7 space-y-3 border-t border-border pt-6">
          <h2 id="organization-sign-in" className="text-base font-semibold">{t('login.organizationSignIn')}</h2>
          <p className="text-sm leading-6 text-muted-foreground">{t('login.existingAccountHint')}</p>
          {providers.isPending ? <p role="status" className="text-sm text-muted-foreground">{t('account.loadingProviders')}</p>
            : providers.isError ? <div className="text-sm text-muted-foreground"><p>{t('account.providersUnavailable')}</p><Button className="mt-2" variant="outline" size="sm" onClick={() => { void providers.refetch() }}>{t('now.refresh')}</Button></div>
              : <>
                {(providers.data?.length ?? 0) > 4 && <Input aria-label={t('login.searchSources')} placeholder={t('login.searchSources')} value={sourceSearch} onChange={event => setSourceSearch(event.target.value)} disabled={loading || Boolean(oidcLoading)} />}
                <div className="space-y-3">
                  {visibleProviders.map(provider => <div key={provider.id} className="space-y-2">
                    <Button type="button" variant="outline" className="min-h-10 w-full whitespace-normal break-words" disabled={loading || Boolean(oidcLoading)} aria-busy={oidcLoading === provider.id} onClick={() => { if (provider.protocol === 'ldap') { setDirectory(provider); setDirectoryUsername(''); setDirectoryPassword(''); setDirectoryError('') } else void handleOIDC(provider.id) }}><Icon name="key" size="sm" />{t(oidcLoading === provider.id ? 'account.redirecting' : 'account.signInWith', { name: provider.name })}</Button>
                    {oidcError?.source === provider.id && <Alert variant="destructive"><AlertDescription>{t(oidcError.key)}</AlertDescription></Alert>}
                  </div>)}
                  {visibleProviders.length === 0 && <p role="status" className="text-sm text-muted-foreground">{t('login.noMatchingSources')}</p>}
                </div>
              </>}
        </section>}
      </div>
      <Dialog open={directory !== null} onOpenChange={open => { if (!open && !oidcLoading) { setDirectory(null); setDirectoryPassword(''); setDirectoryError('') } }}><DialogContent className="max-w-md" closeDisabled={Boolean(oidcLoading)} finalFocus><DialogTitle>{t('account.signInWith', { name: directory?.name })}</DialogTitle><DialogDescription>{t('ldap.signInHint')}</DialogDescription>{!directoryAllowed && <Alert variant="warning"><AlertDescription>{t('ldap.errors.https')}</AlertDescription></Alert>}<form className="space-y-4" onSubmit={event => { void handleDirectory(event) }}><Field><FieldLabel htmlFor="directory-username">{t('ldap.username')}</FieldLabel><Input id="directory-username" autoComplete="username" value={directoryUsername} onChange={event => setDirectoryUsername(event.target.value)} disabled={Boolean(oidcLoading) || !directoryAllowed} required /></Field><Field><FieldLabel htmlFor="directory-password">{t('ldap.password')}</FieldLabel><Input id="directory-password" type="password" autoComplete="current-password" value={directoryPassword} onChange={event => setDirectoryPassword(event.target.value)} disabled={Boolean(oidcLoading) || !directoryAllowed} required /></Field>{directoryError && directoryAllowed && <Alert variant="destructive"><AlertDescription>{t(directoryError)}</AlertDescription></Alert>}<div className="flex flex-wrap justify-end gap-2"><Button type="button" variant="outline" disabled={Boolean(oidcLoading)} onClick={() => { setDirectory(null); setDirectoryPassword('') }}>{t('cancel')}</Button><Button type="submit" disabled={Boolean(oidcLoading) || !directoryAllowed || !directoryUsername.trim() || !directoryPassword}>{t(oidcLoading ? 'account.processing' : 'login.submit')}</Button></div></form></DialogContent></Dialog>
    </main>
  )
}
