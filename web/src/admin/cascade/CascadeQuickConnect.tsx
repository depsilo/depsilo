import { useId, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { getAdminRouteHref } from '@/admin/routes'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import CodeBlock from '@/portal/components/CodeBlock'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import type { AdminCascadeJoinResponse, AdminCascadePairingResponse } from '@/lib/adminApi.types'

interface CascadeQuickConnectProps {
  canWrite: boolean
  /** Called after pairing or joining changed the cascade state. */
  onChanged: () => void
}

function loopbackAddress(value: string): boolean {
  try {
    const host = new URL(value).hostname.replace(/^\[|\]$/g, '')
    return host === 'localhost' || host === '::1' || /^127\./.test(host)
  } catch {
    return false
  }
}

/**
 * Pairing in two pastes: the parent generates a code, the child pastes it.
 * Both sides take effect at once, without a restart.
 */
export default function CascadeQuickConnect({ canWrite, onChanged }: CascadeQuickConnectProps) {
  const { t } = useTranslation()
  const [tab, setTab] = useState('join')
  const [address, setAddress] = useState(() => window.location.origin)
  const [code, setCode] = useState('')
  const [routeAll, setRouteAll] = useState(true)
  const [pairing, setPairing] = useState<AdminCascadePairingResponse | null>(null)
  const [joined, setJoined] = useState<AdminCascadeJoinResponse | null>(null)
  const addressId = useId()
  const codeId = useId()
  const routeAllId = useId()

  const pairMutation = useMutation({
    mutationFn: async () => (await adminApi.createCascadePairing(address.trim())).data,
    onSuccess: data => {
      setPairing(data)
      if (data.enabled_now) onChanged()
    },
  })
  const joinMutation = useMutation({
    mutationFn: async () => (await adminApi.joinCascade({ code: code.trim(), route_all: routeAll })).data,
    onSuccess: data => {
      setJoined(data)
      setCode('')
      onChanged()
    },
  })

  return <section aria-labelledby="cascade-quick-heading" className="order-first min-w-0 xl:order-none" data-cascade-pairing>
    <h2 id="cascade-quick-heading" className="mb-2 text-base font-semibold">{t('cascade.quick.title')}</h2>
    <Tabs value={tab} onValueChange={value => setTab(String(value))}>
      <TabsList aria-label={t('cascade.quick.title')} variant="line">
        <TabsTrigger value="join">{t('cascade.pairing.tabJoin')}</TabsTrigger>
        <TabsTrigger value="parent">{t('cascade.pairing.tabParent')}</TabsTrigger>
        <TabsTrigger value="client">{t('cascade.quick.tabClient')}</TabsTrigger>
      </TabsList>

      <TabsContent value="join" className="space-y-3 pt-3 text-sm">
        <p className="leading-6 text-muted-foreground">{t('cascade.pairing.joinGuide')}</p>
        <form
          className="space-y-3"
          onSubmit={event => { event.preventDefault(); if (code.trim()) joinMutation.mutate() }}
        >
          <fieldset className="space-y-3" disabled={!canWrite || joinMutation.isPending}>
            <Field>
              <FieldLabel htmlFor={codeId}>{t('cascade.pairing.codeLabel')}</FieldLabel>
              <Textarea
                id={codeId}
                className="min-h-20 font-mono text-[12px]"
                value={code}
                spellCheck={false}
                autoComplete="off"
                placeholder="depsilo-pair-1.…"
                onChange={event => setCode(event.target.value)}
              />
            </Field>
            <Field orientation="horizontal">
              <Checkbox id={routeAllId} checked={routeAll} onCheckedChange={value => setRouteAll(Boolean(value))} />
              <FieldLabel htmlFor={routeAllId}>{t('cascade.pairing.routeAll')}</FieldLabel>
            </Field>
            <p className="text-xs leading-5 text-muted-foreground">{t('cascade.pairing.routeAllHint')}</p>
            <Button type="submit" disabled={!code.trim()} aria-busy={joinMutation.isPending || undefined}>
              {t(joinMutation.isPending ? 'cascade.pairing.joining' : 'cascade.pairing.join')}
            </Button>
          </fieldset>
        </form>
        {joinMutation.isError && (
          <Alert variant="destructive"><AlertDescription>{t('cascade.pairing.joinFailed', { reason: getApiError(joinMutation.error).message })}</AlertDescription></Alert>
        )}
        {joined && !joinMutation.isError && (
          <Alert variant="success" data-cascade-joined>
            <AlertDescription className="space-y-1">
              <p>{routeAll && !joined.restart_required
                ? t('cascade.pairing.joined', { url: joined.url, peer: joined.peer, count: joined.bound })
                : t('cascade.pairing.joinedNoRoute', { url: joined.url, peer: joined.peer })}</p>
              {joined.skipped.length > 0 && <p>{t('cascade.pairing.skipped', { list: joined.skipped.join(', ') })}</p>}
            </AlertDescription>
          </Alert>
        )}
        {joined?.insecure_http_allowed && <Alert variant="warning"><AlertDescription>{t('cascade.pairing.insecureEnabled')}</AlertDescription></Alert>}
        {joined?.restart_required && <Alert variant="warning"><AlertDescription>{t('cascade.pendingRestart')}</AlertDescription></Alert>}
      </TabsContent>

      <TabsContent value="parent" className="space-y-3 pt-3 text-sm">
        <p className="leading-6 text-muted-foreground">{t('cascade.pairing.parentGuide')}</p>
        <form
          className="space-y-3"
          onSubmit={event => { event.preventDefault(); pairMutation.mutate() }}
        >
          <fieldset className="space-y-3" disabled={!canWrite || pairMutation.isPending}>
            <Field>
              <FieldLabel htmlFor={addressId}>{t('cascade.pairing.addressLabel')}</FieldLabel>
              <Input id={addressId} className="font-mono" value={address} autoComplete="off" spellCheck={false} onChange={event => { setAddress(event.target.value); setPairing(null) }} />
              <FieldDescription>{t('cascade.pairing.addressHint')}</FieldDescription>
            </Field>
            {loopbackAddress(address) && <Alert variant="warning"><AlertDescription>{t('cascade.pairing.loopbackWarning')}</AlertDescription></Alert>}
            <Button type="submit" disabled={!address.trim()} aria-busy={pairMutation.isPending || undefined}>
              {t(pairMutation.isPending ? 'cascade.pairing.generating' : 'cascade.pairing.generate')}
            </Button>
          </fieldset>
        </form>
        {pairMutation.isError && (
          <Alert variant="destructive"><AlertDescription>{t('cascade.pairing.generateFailed', { reason: getApiError(pairMutation.error).message })}</AlertDescription></Alert>
        )}
        {pairing && (
          <div className="space-y-2" data-cascade-pairing-code>
            {pairing.enabled_now && <Alert variant="success"><AlertDescription>{t('cascade.pairing.enabledNow')}</AlertDescription></Alert>}
            {pairing.restart_required && <Alert variant="warning"><AlertDescription>{t('cascade.pendingRestart')}</AlertDescription></Alert>}
            <CodeBlock filename={t('cascade.pairing.codeLabel')} copyName={t('cascade.pairing.codeLabel')} code={pairing.code} />
            <p className="text-xs leading-5 text-muted-foreground">{t('cascade.pairing.codeSecret')}</p>
          </div>
        )}
      </TabsContent>

      <TabsContent value="client" className="space-y-3 pt-3 text-sm">
        <p className="leading-6 text-muted-foreground">{t('cascade.quick.clientGuide')}</p>
        <Link to={getAdminRouteHref('connect')} className="inline-flex min-h-10 items-center text-[var(--brand-text)] underline underline-offset-4">{t('cascade.quick.viewAllClients')}</Link>
      </TabsContent>
    </Tabs>
    <p className="mt-4 text-xs leading-5 text-muted-foreground">{t('cascade.pairing.cliHint')}</p>
    <p className="mt-2 text-xs leading-5 text-muted-foreground">{t('cascade.quick.scopeHint')}</p>
  </section>
}
