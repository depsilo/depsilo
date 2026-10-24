import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import AdminPage from '@/admin/components/AdminPage'
import AuthenticationEditor from '@/admin/components/AuthenticationEditor'
import LDAPAuthenticationEditor from '@/admin/components/LDAPAuthenticationEditor'
import { ldapConfiguredStatus, newLDAPAuthenticationSource } from '@/lib/ldapAuthentication'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authenticationApi } from '@/lib/authenticationApi'
import type { AuthenticationSourcesResponse, AuthenticationSourceState } from '@/lib/adminApi.types'
import { authenticationConfiguredStatus, authenticationRuntimeStatus, newAuthenticationSource } from '@/lib/authenticationSettings'

const queryKey = ['admin', 'authentication', 'sources'] as const

export default function Authentication() {
  const { t } = useTranslation()
  const { canManageCredentials } = usePrincipal()
  const queryClient = useQueryClient()
  const [params, setParams] = useSearchParams()
  const [chooseProtocol, setChooseProtocol] = useState(false)
  const [newSource, setNewSource] = useState<AuthenticationSourceState>(() => newAuthenticationSource(`source-${crypto.getRandomValues(new Uint32Array(1))[0].toString(36)}`, window.location.origin))
  const [restoring, setRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState(false)
  const query = useQuery({ queryKey, queryFn: async ({ signal }) => (await authenticationApi.sources({ signal })).data, retry: false, refetchOnWindowFocus: false })
  const sourceID = params.get('source')
  const creating = params.get('new') === '1'
  const selected = creating ? newSource : query.data?.sources.find(source => source.id === sourceID)
  const canCreate = canManageCredentials && query.data?.config_writable && !query.isFetching && !query.isRefetchError
  const back = () => setParams({})
  function updated(source: AuthenticationSourceState) {
    queryClient.setQueryData<AuthenticationSourcesResponse>(queryKey, current => current ? {
      ...current, sources: current.sources.some(item => item.id === source.id) ? current.sources.map(item => item.id === source.id ? source : item) : [...current.sources, source],
    } : { sources: [source], config_writable: true })
    if (creating) setParams({ source: source.id }, { replace: true })
  }
  async function restore(source: AuthenticationSourceState) {
    setRestoring(true); setRestoreError(false)
    try { updated((await authenticationApi.restoreSource(source.id)).data) }
    catch { setRestoreError(true) }
    finally { setRestoring(false) }
  }
  return <AdminPage description={t('authentication.subtitle')} actions={!sourceID && !creating && <>
    <Button variant="outline" size="sm" disabled={query.isFetching} onClick={() => { void query.refetch() }}><Icon name="refresh" size="sm" />{t('now.refresh')}</Button>
    {canManageCredentials && <Button size="sm" disabled={!canCreate} onClick={() => setChooseProtocol(true)}><Icon name="add" size="sm" />{t('authentication.addSource')}</Button>}
  </>}>
    {query.isPending ? <p role="status" className="py-8 text-sm text-muted-foreground">{t('loading')}</p>
      : !query.data ? <QueryErrorState message={t('authentication.loadError')} onRetry={() => query.refetch()} />
        : sourceID || creating ? selected?.configured ? selected.protocol === 'ldap' ? <LDAPAuthenticationEditor key={`${creating ? 'new' : 'saved'}-${selected.id}`} source={selected} creating={creating} configWritable={query.data.config_writable} stale={query.isRefetchError} fetching={query.isFetching} refresh={() => { void query.refetch() }} onBack={back} onSaved={updated} onDeleted={() => { back(); void queryClient.invalidateQueries({ queryKey }) }} /> : <AuthenticationEditor key={`${creating ? 'new' : 'saved'}-${selected.id}`} source={selected} creating={creating} configWritable={query.data.config_writable} stale={query.isRefetchError} fetching={query.isFetching} refresh={() => { void query.refetch() }} onBack={back} onSaved={updated} onDeleted={() => { back(); void queryClient.invalidateQueries({ queryKey }) }} />
          : <div className="max-w-3xl space-y-4"><Button variant="ghost" onClick={back}><Icon name="arrow_back" size="sm" />{t('authentication.backToSources')}</Button><Alert variant={selected ? 'warning' : 'info'}><AlertDescription>{t(selected ? 'authentication.pendingRemoval' : 'authentication.sourceMissing')}</AlertDescription></Alert>
            {selected?.effective && <div className="space-y-2 rounded-lg border border-border p-5"><h2 className="text-base font-semibold">{selected.effective.display_name}</h2><p className="break-all text-sm text-muted-foreground">{selected.protocol === 'ldap' ? selected.effective.ldap.url : selected.effective.issuer}</p><p className="text-sm">{t('authentication.running')}: {t(`authentication.${authenticationRuntimeStatus(selected)}`)}</p>
              {selected.can_restore && <Button variant="outline" disabled={!canCreate || restoring} onClick={() => { void restore(selected) }}>{t(restoring ? 'saving' : 'authentication.restoreSource')}</Button>}
              <p className="text-sm text-muted-foreground">{t(selected.can_restore ? 'authentication.restoreHint' : 'authentication.restoreUnavailable')}</p>
            </div>}{restoreError && <Alert variant="destructive"><AlertDescription>{t('authentication.restoreError')}</AlertDescription></Alert>}
          </div>
          : <div className="max-w-6xl space-y-5">
            {query.isRefetchError && <Alert variant="warning"><AlertDescription>{t('authentication.stale')}</AlertDescription></Alert>}
            {!canManageCredentials && <Alert variant="info"><AlertDescription>{t('authentication.localAdminRequired')}</AlertDescription></Alert>}
            {!query.data.config_writable && <Alert variant="warning"><AlertDescription>{t('authentication.readonlyConfig')}</AlertDescription></Alert>}
            <section aria-labelledby="authentication-sources" className="space-y-3">
              <h2 id="authentication-sources" className="text-base font-semibold">{t('authentication.sources')}</h2>
              <p className="text-sm leading-6 text-muted-foreground">{t('authentication.sourcesHint')}</p>
              {query.data.sources.length === 0 ? <div className="rounded-lg border border-border bg-card p-6"><Icon name="key" size="md" className="text-muted-foreground" /><h3 className="mt-3 text-base font-semibold">{t('authentication.emptyTitle')}</h3><p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t('authentication.emptyHint')}</p></div>
                : <div className="divide-y divide-border rounded-lg border border-border bg-card">{query.data.sources.map(source => {
                  const running = source.effective
                  const name = running?.display_name || source.configured?.display_name || source.id
                  return <div key={source.id} className="flex min-w-0 flex-wrap items-center justify-between gap-4 p-5">
                    <div className="min-w-0 flex-1 basis-64"><div className="flex flex-wrap items-center gap-2"><h3 className="break-all text-base font-semibold">{name}</h3><Badge variant="outline">{source.protocol.toUpperCase()}</Badge><Badge variant={running?.enabled ? 'success' : 'secondary'}>{t('authentication.running')}: {t(`authentication.${authenticationRuntimeStatus(source)}`)}</Badge></div>
                      {running && <p className="mt-2 break-all text-sm text-muted-foreground">{(source.protocol === 'ldap' ? source.effective?.ldap.url : source.effective?.issuer) || t('ldap.noEndpoint')}</p>}
                      {(!running || source.pending_restart) && <div className="mt-3 space-y-1 border-l-2 border-border pl-3 text-sm"><div className="flex flex-wrap items-center gap-2"><span>{t('authentication.savedConfig')}: {t(`authentication.${(source.protocol === 'ldap' ? ldapConfiguredStatus(source) : authenticationConfiguredStatus(source))}`)}</span>{source.pending_restart && <Badge variant="warning">{t('authentication.pendingBadge')}</Badge>}</div>{source.configured && <p className="break-all text-muted-foreground">{source.configured.display_name} · {(source.protocol === 'ldap' ? source.configured.ldap.url : source.configured.issuer) || t('ldap.noEndpoint')}</p>}</div>}
                    </div>
                    <Button variant="outline" aria-label={t('authentication.configureNamed', { name })} onClick={() => { setRestoreError(false); setParams({ source: source.id }) }}>{t(source.configured ? 'authentication.configure' : 'authentication.viewRemoval')}<Icon name="arrow_forward" size="sm" /></Button>
                  </div>
                })}</div>}
            </section>
            <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{t('authentication.listHelp')}</p>
          </div>}
    <Dialog open={chooseProtocol} onOpenChange={setChooseProtocol}><DialogContent className="max-w-lg" finalFocus><DialogTitle>{t('ldap.chooseProtocol')}</DialogTitle><DialogDescription>{t('ldap.protocolChoiceHint')}</DialogDescription><div className="grid gap-3">{(['oidc', 'ldap'] as const).map(protocol => <Button key={protocol} variant="outline" className="h-auto min-h-16 justify-start whitespace-normal px-4 py-3 text-left" onClick={() => { const id = `source-${crypto.getRandomValues(new Uint32Array(1))[0].toString(36)}`; setNewSource(protocol === 'ldap' ? newLDAPAuthenticationSource(id) : newAuthenticationSource(id, window.location.origin)); setChooseProtocol(false); setParams({ new: '1' }) }}><span className="space-y-1"><span className="block font-semibold">{protocol === 'ldap' ? 'LDAP / LDAPS' : 'OIDC'}</span><span className="block text-sm font-normal text-muted-foreground">{t(protocol === 'ldap' ? 'ldap.protocolLDAP' : 'ldap.protocolOIDC')}</span></span></Button>)}</div></DialogContent></Dialog>
  </AdminPage>
}
