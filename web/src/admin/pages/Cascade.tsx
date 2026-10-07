import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import CascadeConfigForm from '@/admin/cascade/CascadeConfigForm'
import { getAdminRouteHref } from '@/admin/routes'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import EmptyState from '@/components/EmptyState'
import Icon from '@/components/Icon'
import IconButton from '@/components/IconButton'
import InlineNotice from '@/components/InlineNotice'
import Metric from '@/components/Metric'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import SelectV2 from '@/components/Select'
import TableViewport from '@/components/TableViewport'
import { useAppToast } from '@/components/Toast'
import { usePrincipal } from '@/hooks/usePrincipal'
import { useTransientState } from '@/hooks/useTransientFlag'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { copyText } from '@/lib/clipboard'
import type { AdminUpstream, UpstreamMutationRequest } from '@/lib/adminApi.types'

const navigateLinkClass = 'stripe-focus-ring inline-flex min-h-10 items-center rounded-sm px-2 text-[12px] font-[600] no-underline text-[var(--brand-text)] hover:bg-[var(--bg-hover)]'
const tableHeadClass = 'px-3 py-2 text-left font-mono text-[10px] font-[600] uppercase first:pl-0'
const tableCellClass = 'px-3 py-3 align-top first:pl-0'

function formatTTL(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '—'
  if (seconds % 86_400 === 0) return `${seconds / 86_400}d`
  if (seconds % 3_600 === 0) return `${seconds / 3_600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

function truncateMiddle(value: string, max = 56): string {
  const characters = Array.from(value)
  if (characters.length <= max) return value
  const half = Math.floor((max - 1) / 2)
  return `${characters.slice(0, half).join('')}…${characters.slice(-half).join('')}`
}

export default function Cascade() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const [copiedValue, showCopiedValue] = useTransientState<string | null>(null)

  const infoQuery = useQuery({
    queryKey: ['admin', 'cascade'],
    queryFn: async ({ signal }) => (await adminApi.getCascadeInfo({ signal })).data,
    retry: false,
    staleTime: 30_000,
  })
  const upstreamsQuery = useQuery({
    queryKey: ['admin', 'upstreams'],
    queryFn: async ({ signal }) => (await adminApi.listUpstreams({ signal })).data,
    retry: false,
  })
  const configQuery = useQuery({
    queryKey: ['admin', 'cascade-config'],
    queryFn: async ({ signal }) => (await adminApi.getCascadeConfig({ signal })).data,
    retry: false,
    staleTime: 30_000,
  })

  const info = infoQuery.data
  const config = configQuery.data
  const upstreams = useMemo(() => upstreamsQuery.data?.items ?? [], [upstreamsQuery.data])
  const boundUpstreams = useMemo(
    () => upstreams.filter(upstream => (upstream.via ?? '').trim() !== ''),
    [upstreams],
  )
  const peerNames = useMemo(
    () => new Set((info?.peers ?? []).map(peer => peer.name)),
    [info],
  )
  const unresolved = useMemo(
    () => boundUpstreams.filter(upstream => !peerNames.has(upstream.via ?? '')),
    [boundUpstreams, peerNames],
  )

  const roleKey = !info?.enabled
    ? 'cascade.roleDisabled'
    : (info.peers.length > 0 ? 'cascade.roleBoth' : 'cascade.roleParent')
  const loadError = getApiError(infoQuery.error)
  const showStale = Boolean(info && (infoQuery.isRefetchError || upstreamsQuery.isRefetchError))

  const rebindMutation = useMutation({
    mutationFn: ({ id, request }: { id: number; request: UpstreamMutationRequest }) =>
      adminApi.updateUpstream(id, request),
    onSuccess: ({ data }) => {
      toast.show({
        tone: 'success',
        message: t('cascade.rebindSaved', {
          name: data.name,
          target: data.via ? data.via : t('cascade.viaDirect'),
        }),
      })
      void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
      void queryClient.invalidateQueries({ queryKey: ['admin', 'cascade'] })
    },
    onError: (error) => {
      toast.show({
        tone: 'danger',
        message: t('cascade.rebindFailed', { reason: getApiError(error).message }),
      })
    },
  })

  function rebindUpstream(upstream: AdminUpstream, via: string) {
    rebindMutation.mutate({
      id: upstream.id,
      request: {
        adapter_type: upstream.adapter_type,
        name: upstream.name,
        url: upstream.url,
        proxy: upstream.proxy,
        priority: upstream.priority,
        probe_mode: upstream.probe_mode,
        probe_interval: upstream.probe_interval,
        via,
      },
    })
  }

  async function copyInstanceID(value: string) {
    if (await copyText(value)) showCopiedValue(value)
  }

  return (
    <AdminPage
      description={t('cascade.subtitle')}
      actions={(
        <ButtonV2
          type="button"
          variant="secondary"
          size="sm"
          aria-busy={infoQuery.isFetching || undefined}
          disabled={infoQuery.isFetching}
          onClick={() => {
            void infoQuery.refetch()
            void upstreamsQuery.refetch()
            void configQuery.refetch()
          }}
        >
          <Icon name={infoQuery.isFetching ? 'progress_activity' : 'refresh'} size="sm" />
          {t(infoQuery.isFetching ? 'cascade.refreshing' : 'cascade.refresh')}
        </ButtonV2>
      )}
    >
      <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {copiedValue ? t('cascade.copied') : ''}
      </span>
      <div className="space-y-12">
        {infoQuery.isPending ? (
          <div aria-busy="true" className="py-16 text-center text-[13px] text-[var(--text-soft)]">
            {t('loading')}
          </div>
        ) : infoQuery.isError && !info ? (
          <QueryErrorState
            message={loadError.status === 403 ? t('common.permissionDenied') : t('cascade.loadError')}
            onRetry={() => { void infoQuery.refetch() }}
          />
        ) : info ? (
          <>
            {(!canWrite || (config && !config.config_writable) || config?.pending_restart) && (
              <div className="space-y-3">
                {!canWrite && <InlineNotice tone="info">{t('cascade.readonlyPrincipal')}</InlineNotice>}
                {canWrite && config && !config.config_writable && (
                  <InlineNotice tone="warning">{t('cascade.configReadOnly')}</InlineNotice>
                )}
                {config?.pending_restart && (
                  <InlineNotice tone="warning">{t('cascade.pendingRestart')}</InlineNotice>
                )}
              </div>
            )}
            {showStale && (
              <StaleDataNotice
                message={t('cascade.stale')}
                refreshing={infoQuery.isFetching || upstreamsQuery.isFetching}
                onRefresh={() => {
                  void infoQuery.refetch()
                  void upstreamsQuery.refetch()
                }}
              />
            )}

            <section aria-labelledby="cascade-status-heading">
              <SectionHeader
                title={t('cascade.statusTitle')}
                hint={t('cascade.statusHint')}
                action={(
                  <BadgeV2 variant={info.enabled ? 'success' : 'warning'}>
                    {t(info.enabled ? 'cascade.enabled' : 'cascade.disabled')}
                  </BadgeV2>
                )}
              />
              <span id="cascade-status-heading" className="sr-only">{t('cascade.statusTitle')}</span>

              {!info.enabled ? (
                <div className="space-y-5">
                  <InlineNotice tone="info">{t('cascade.disabledBody')}</InlineNotice>
                  <div>
                    <p className="mb-2 text-[12px] font-[600]" style={{ color: 'var(--text)' }}>
                      {t('cascade.configExampleLabel')}
                    </p>
                    <pre
                      data-cascade-config-example
                      className="overflow-x-auto rounded-md border p-4 font-mono text-[12px] leading-6"
                      style={{ borderColor: 'var(--border)', background: 'var(--bg-soft)', color: 'var(--text-muted)' }}
                    >
{`[cascade]
enabled = true
token = "<shared-secret>"

[[cascade.peers]]
name = "home"
url  = "http://192.168.1.10:23333"
# allow_insecure_http = true   # plain HTTP outside loopback`}
                    </pre>
                  </div>
                </div>
              ) : (
                <div className="space-y-7">
                  <div className="grid grid-cols-2 gap-x-5 gap-y-7 lg:grid-cols-4">
                    <Metric label={t('cascade.roleLabel')} value={t(roleKey)} size={24} />
                    <Metric label={t('cascade.peersLabel')} value={String(info.peers.length)} size={30} />
                    <Metric label={t('cascade.boundLabel')} value={String(boundUpstreams.length)} size={30} />
                    <Metric
                      label={t('cascade.unresolvedLabel')}
                      value={String(unresolved.length)}
                      size={30}
                      valueTone={unresolved.length > 0 ? 'warn' : 'default'}
                    />
                  </div>

                  <dl className="grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.instanceId')}</dt>
                      <dd className="mt-1 flex min-w-0 items-center gap-1">
                        <span className="break-all font-mono text-[12px]" style={{ color: 'var(--text)' }}>
                          {info.instance_id}
                        </span>
                        <IconButton
                          icon={copiedValue === info.instance_id ? 'check' : 'content_copy'}
                          label={t('cascade.copyInstanceId')}
                          onClick={() => { void copyInstanceID(info.instance_id) }}
                        />
                      </dd>
                    </div>
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.relayPath')}</dt>
                      <dd className="mt-1 break-all font-mono text-[12px]" style={{ color: 'var(--text)' }}>
                        {info.relay_path}
                      </dd>
                    </div>
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.maxHops')}</dt>
                      <dd className="mt-1 font-mono text-[12px]" style={{ color: 'var(--text)' }}>{info.max_hops}</dd>
                    </div>
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.maxTTL')}</dt>
                      <dd className="mt-1 font-mono text-[12px]" style={{ color: 'var(--text)' }}>
                        {formatTTL(info.max_ttl_seconds)}
                      </dd>
                    </div>
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.plaintextHTTP')}</dt>
                      <dd className="mt-1 text-[12px]" style={{ color: 'var(--text)' }}>
                        {t(info.allow_insecure_http ? 'cascade.plaintextAllowed' : 'cascade.plaintextDenied')}
                      </dd>
                    </div>
                    <div className="min-w-0">
                      <dt className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.tokenLabel')}</dt>
                      <dd className="mt-1 text-[12px]" style={{ color: 'var(--text)' }}>{t('cascade.tokenNote')}</dd>
                    </div>
                  </dl>
                </div>
              )}
            </section>

            <section aria-labelledby="cascade-config-heading">
              <SectionHeader title={t('cascade.configSectionTitle')} hint={t('cascade.configSectionHint')} />
              <span id="cascade-config-heading" className="sr-only">{t('cascade.configSectionTitle')}</span>
              {configQuery.isPending ? (
                <div aria-busy="true" className="py-10 text-center text-[13px] text-[var(--text-soft)]">
                  {t('loading')}
                </div>
              ) : configQuery.isError && !config ? (
                <QueryErrorState
                  message={getApiError(configQuery.error).status === 403
                    ? t('common.permissionDenied')
                    : t('cascade.loadError')}
                  onRetry={() => { void configQuery.refetch() }}
                />
              ) : config ? (
                <CascadeConfigForm
                  state={config}
                  canWrite={canWrite}
                  onSaved={(result) => {
                    toast.show({
                      tone: 'success',
                      message: t(result.restart_required ? 'cascade.configSavedRestart' : 'cascade.configSaved'),
                    })
                    void queryClient.invalidateQueries({ queryKey: ['admin', 'cascade-config'] })
                    void queryClient.invalidateQueries({ queryKey: ['admin', 'cascade'] })
                    void queryClient.invalidateQueries({ queryKey: ['admin', 'upstreams'] })
                  }}
                />
              ) : null}
            </section>

            <section aria-labelledby="cascade-upstreams-heading">
              <SectionHeader
                title={t('cascade.upstreamsTitle')}
                hint={t(canWrite && info.enabled ? 'cascade.upstreamsQuickHint' : 'cascade.upstreamsHint')}
                action={(
                  <Link to={getAdminRouteHref('upstreams')} className={navigateLinkClass}>
                    {t('cascade.manageUpstreams')}
                  </Link>
                )}
              />
              <span id="cascade-upstreams-heading" className="sr-only">{t('cascade.upstreamsTitle')}</span>
              {unresolved.length > 0 && (
                <InlineNotice tone="warning">
                  {t('cascade.unresolvedBody', { count: unresolved.length })}
                </InlineNotice>
              )}
              {canWrite && !info.enabled && (
                <InlineNotice tone="info">{t('cascade.rebindRuntimeDisabled')}</InlineNotice>
              )}
              {boundUpstreams.length === 0 ? (
                <EmptyState
                  icon="cloud_sync"
                  title={t('cascade.upstreamsEmptyTitle')}
                  hint={t('cascade.upstreamsEmptyHint')}
                  minHeight={200}
                />
              ) : (
                <TableViewport label={t('cascade.upstreamsTitle')} minWidth={860}>
                  <table className="w-full text-[12px]">
                    <thead>
                      <tr style={{ borderBottom: '1px solid var(--border)' }}>
                        {[
                          t('cascade.upstreamEcosystem'),
                          t('cascade.upstreamName'),
                          t('cascade.upstreamURL'),
                          t('cascade.upstreamVia'),
                          t('cascade.upstreamPriority'),
                        ].map(heading => (
                          <th key={heading} scope="col" className={tableHeadClass} style={{ color: 'var(--text-subtle)' }}>
                            {heading}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {boundUpstreams.map(upstream => {
                        const via = upstream.via ?? ''
                        const missing = !peerNames.has(via)
                        return (
                          <tr
                            key={upstream.id}
                            data-cascade-upstream={upstream.id}
                            style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                          >
                            <td className={tableCellClass}>
                              <span className="font-mono uppercase" style={{ color: 'var(--text-muted)' }}>
                                {upstream.adapter_type}
                              </span>
                            </td>
                            <td className={`${tableCellClass} font-[500]`} style={{ color: 'var(--text)' }}>
                              {upstream.name}
                            </td>
                            <td className={`${tableCellClass} font-mono`} style={{ color: 'var(--text-muted)' }}>
                              <span title={upstream.url}>{truncateMiddle(upstream.url, 48)}</span>
                            </td>
                            <td className={tableCellClass}>
                              {canWrite && info.enabled ? (
                                <SelectV2
                                  aria-label={t('cascade.upstreamVia')}
                                  value={via}
                                  disabled={rebindMutation.isPending}
                                  onChange={event => rebindUpstream(upstream, event.target.value)}
                                >
                                  <option value="">{t('cascade.viaDirect')}</option>
                                  {info.peers.map(peer => (
                                    <option key={peer.name} value={peer.name}>{peer.name}</option>
                                  ))}
                                  {missing && <option value={via}>{t('cascade.viaMissing', { peer: via })}</option>}
                                </SelectV2>
                              ) : missing ? (
                                <BadgeV2 variant="warning">{t('cascade.viaMissing', { peer: via })}</BadgeV2>
                              ) : (
                                <span className="font-mono" style={{ color: 'var(--text)' }}>{via}</span>
                              )}
                            </td>
                            <td className={`${tableCellClass} font-mono tabular-nums`} style={{ color: 'var(--text)' }}>
                              {upstream.priority}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </TableViewport>
              )}
            </section>

            <section aria-labelledby="cascade-how-heading">
              <SectionHeader title={t('cascade.howTitle')} />
              <span id="cascade-how-heading" className="sr-only">{t('cascade.howTitle')}</span>
              <ul className="space-y-3 text-[12px] leading-6" style={{ color: 'var(--text-muted)' }}>
                <li className="flex gap-2"><Icon name="arrow_forward" size="sm" className="mt-1 shrink-0 text-[var(--brand-text)]" />{t('cascade.howBody1')}</li>
                <li className="flex gap-2"><Icon name="shield" size="sm" className="mt-1 shrink-0 text-[var(--brand-text)]" />{t('cascade.howBody2')}</li>
                <li className="flex gap-2"><Icon name="sync" size="sm" className="mt-1 shrink-0 text-[var(--brand-text)]" />{t('cascade.howBody3')}</li>
              </ul>
            </section>
          </>
        ) : null}
      </div>
    </AdminPage>
  )
}
