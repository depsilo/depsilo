import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
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
import TableViewport from '@/components/TableViewport'
import { useTransientState } from '@/hooks/useTransientFlag'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { copyText } from '@/lib/clipboard'

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

  const info = infoQuery.data
  const upstreams = useMemo(() => upstreamsQuery.data?.items ?? [], [upstreamsQuery.data])
  const boundUpstreams = useMemo(
    () => upstreams.filter(upstream => (upstream.via ?? '').trim() !== ''),
    [upstreams],
  )
  const peerNames = useMemo(
    () => new Set((info?.peers ?? []).map(peer => peer.name)),
    [info],
  )
  const boundByPeer = useMemo(() => {
    const counts = new Map<string, number>()
    for (const upstream of boundUpstreams) {
      const via = upstream.via ?? ''
      counts.set(via, (counts.get(via) ?? 0) + 1)
    }
    return counts
  }, [boundUpstreams])
  const unresolved = useMemo(
    () => boundUpstreams.filter(upstream => !peerNames.has(upstream.via ?? '')),
    [boundUpstreams, peerNames],
  )

  const roleKey = !info?.enabled
    ? 'cascade.roleDisabled'
    : (info.peers.length > 0 ? 'cascade.roleBoth' : 'cascade.roleParent')
  const loadError = getApiError(infoQuery.error)
  const showStale = Boolean(info && (infoQuery.isRefetchError || upstreamsQuery.isRefetchError))

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

            {info.enabled && (
              <>
                <section aria-labelledby="cascade-peers-heading">
                  <SectionHeader title={t('cascade.peersTitle')} hint={t('cascade.peersHint')} />
                  <span id="cascade-peers-heading" className="sr-only">{t('cascade.peersTitle')}</span>
                  {info.peers.length === 0 ? (
                    <EmptyState
                      icon="hub"
                      title={t('cascade.peersEmptyTitle')}
                      hint={t('cascade.peersEmptyHint')}
                      minHeight={200}
                    />
                  ) : (
                    <TableViewport label={t('cascade.peersTitle')} minWidth={720}>
                      <table className="w-full text-[12px]">
                        <thead>
                          <tr style={{ borderBottom: '1px solid var(--border)' }}>
                            {[t('cascade.peerName'), t('cascade.peerURL'), t('cascade.peerCredentials'), t('cascade.peerBoundCount')].map(heading => (
                              <th key={heading} scope="col" className={tableHeadClass} style={{ color: 'var(--text-subtle)' }}>
                                {heading}
                              </th>
                            ))}
                          </tr>
                        </thead>
                        <tbody>
                          {info.peers.map(peer => (
                            <tr
                              key={peer.name}
                              data-cascade-peer={peer.name}
                              style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                            >
                              <td className={tableCellClass}>
                                <span className="font-mono font-[600]" style={{ color: 'var(--text)' }}>{peer.name}</span>
                              </td>
                              <td className={`${tableCellClass} font-mono`} style={{ color: 'var(--text-muted)' }}>
                                <span title={peer.url}>{truncateMiddle(peer.url)}</span>
                              </td>
                              <td className={tableCellClass}>
                                <BadgeV2 variant={peer.forward_credentials ? 'warning' : 'neutral'}>
                                  {t(peer.forward_credentials ? 'cascade.credentialsForwarded' : 'cascade.credentialsLocal')}
                                </BadgeV2>
                              </td>
                              <td className={`${tableCellClass} font-mono tabular-nums`} style={{ color: 'var(--text)' }}>
                                {boundByPeer.get(peer.name) ?? 0}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </TableViewport>
                  )}
                </section>

                <section aria-labelledby="cascade-upstreams-heading">
                  <SectionHeader
                    title={t('cascade.upstreamsTitle')}
                    hint={t('cascade.upstreamsHint')}
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
                                  {missing ? (
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
              </>
            )}

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
