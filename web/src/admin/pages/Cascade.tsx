import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import CascadeConfigForm from '@/admin/cascade/CascadeConfigForm'
import CascadeQuickConnect from '@/admin/cascade/CascadeQuickConnect'
import CascadeTopology from '@/admin/cascade/CascadeTopology'
import AdminPage from '@/admin/components/AdminPage'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { getAdminRouteHref } from '@/admin/routes'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import InlineNotice from '@/components/InlineNotice'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import SelectV2 from '@/components/Select'
import TableViewport from '@/components/TableViewport'
import { useAppToast } from '@/components/Toast'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import type { AdminUpstream, UpstreamMutationRequest } from '@/lib/adminApi.types'

const navigateLinkClass = 'stripe-focus-ring inline-flex min-h-10 items-center rounded-sm px-2 text-[12px] font-[600] no-underline text-[var(--brand-text)] hover:bg-[var(--bg-hover)]'
const tableHeadClass = 'px-3 py-2 text-left font-mono text-[10px] font-[600] uppercase first:pl-0'
const tableCellClass = 'px-3 py-3 align-top first:pl-0'

function truncateMiddle(value: string, max = 48): string {
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
  const showStale = Boolean(info && (infoQuery.isRefetchError || upstreamsQuery.isRefetchError))
  const loadError = getApiError(infoQuery.error)

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

  return (
    <AdminPage
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
      <div className="space-y-5">
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
            {(!canWrite || (config && !config.config_writable) || config?.pending_restart || showStale) && (
              <div className="space-y-2">
                {!canWrite && <InlineNotice tone="info">{t('cascade.readonlyPrincipal')}</InlineNotice>}
                {canWrite && config && !config.config_writable && (
                  <InlineNotice tone="warning">{t('cascade.configReadOnly')}</InlineNotice>
                )}
                {config?.pending_restart && (
                  <InlineNotice tone="warning">{t('cascade.pendingRestart')}</InlineNotice>
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
              </div>
            )}

            <CascadeTopology
              info={info}
              boundUpstreams={boundUpstreams.length}
              unresolved={unresolved.length}
            />

            <div className="grid grid-cols-1 gap-x-8 gap-y-6 xl:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]">
              <section aria-labelledby="cascade-config-heading">
                <SectionHeader title={t('cascade.configSectionTitle')} divider={false} />
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

              <CascadeQuickConnect info={info} config={config} />
            </div>

            {boundUpstreams.length > 0 && (
            <section aria-labelledby="cascade-upstreams-heading">
              <SectionHeader
                title={t('cascade.upstreamsTitle')}
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
              <TableViewport label={t('cascade.upstreamsTitle')} minWidth={760}>
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
                              <span title={upstream.url}>{truncateMiddle(upstream.url)}</span>
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
            </section>
            )}
          </>
        ) : null}
      </div>
    </AdminPage>
  )
}
