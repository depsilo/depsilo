import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeBlock from '@/portal/components/CodeBlock'
import type { AdminCascadeConfigState, AdminCascadeInfo } from '@/lib/adminApi.types'

interface CascadeQuickConnectProps { info: AdminCascadeInfo; config?: AdminCascadeConfigState }

/** Node peering guidance stays distinct from package-manager connection commands. */
export default function CascadeQuickConnect({ info, config }: CascadeQuickConnectProps) {
  const { t } = useTranslation()
  const [tab, setTab] = useState('client')
  const origin = window.location.origin
  const childSnippet = `[cascade]\nenabled = true\ntoken = "<${t('cascade.quick.sharedSecretPlaceholder')}>"\n\n[[cascade.peers]]\nname = "parent"\nurl = ${JSON.stringify(origin)}`
  return <section aria-labelledby="cascade-quick-heading" className="min-w-0">
    <h2 id="cascade-quick-heading" className="mb-2 text-base font-semibold">{t('cascade.quick.title')}</h2>
    <Tabs value={tab} onValueChange={value => setTab(String(value))}>
      <TabsList aria-label={t('cascade.quick.title')} variant="line">
        <TabsTrigger value="client">{t('cascade.quick.tabClient')}</TabsTrigger>
        <TabsTrigger value="parent">{t('cascade.quick.tabParent')}</TabsTrigger>
        <TabsTrigger value="child">{t('cascade.quick.tabChild')}</TabsTrigger>
      </TabsList>
      <TabsContent value="client" className="space-y-3 pt-3 text-sm">
        <p className="leading-6 text-muted-foreground">{t('cascade.quick.clientGuide')}</p>
        <Link to={getAdminRouteHref('connect')} className="inline-flex min-h-10 items-center text-primary underline underline-offset-4">{t('cascade.quick.viewAllClients')}</Link>
      </TabsContent>
      <TabsContent value="parent" className="space-y-3 pt-3 text-sm">
        {!info.enabled && <Alert variant="info"><AlertDescription>{t('cascade.quick.enableFirst')}</AlertDescription></Alert>}
        <p className="leading-6 text-muted-foreground">{t('cascade.quick.parentGuide')}</p>
        <CodeBlock filename="child config.toml" copyName={t('cascade.quick.copyChildConfig')} code={childSnippet} />
        <p className="leading-6 text-muted-foreground">{t('cascade.quick.bindingGuide')}</p>
      </TabsContent>
      <TabsContent value="child" className="space-y-3 pt-3 text-sm">
        <ol className="list-decimal space-y-3 pl-5 leading-6 text-muted-foreground"><li>{t('cascade.quick.addParentStep')}</li><li>{t('cascade.quick.restartStep')}</li><li>{t('cascade.quick.bindStep')}</li></ol>
        {config?.pending_restart && <Alert variant="warning"><AlertDescription>{t('cascade.pendingRestart')}</AlertDescription></Alert>}
        <Link to={getAdminRouteHref('upstreams')} className="inline-flex min-h-10 items-center text-primary underline underline-offset-4">{t('cascade.manageUpstreams')}</Link>
      </TabsContent>
    </Tabs>
    <p className="mt-4 text-xs leading-5 text-muted-foreground">{t('cascade.quick.scopeHint')}</p>
  </section>
}
