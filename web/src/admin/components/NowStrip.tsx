import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import ButtonV2 from '@/components/app/button'
import Modal from '@/components/app/modal'
import QueryErrorState from '@/components/app/error-state'
import type { dashboardStatus } from '@/admin/dashboardStatus'
import type { NowResponse } from '@/lib/adminApi.types'
import { getApiError } from '@/lib/apiError'
import { cn } from '@/lib/utils'

/**
 * The request path is a compact three-stage instrument. Each stage owns one
 * operational fact and its supporting context; the two connectors are kept
 * deliberately quiet so the numbers carry the hierarchy.
 */
const FLOW_STAGE =
  'relative flex min-w-0 flex-col gap-1 px-3 py-2 ' +
  'md:h-full md:justify-between'

interface FlowStageProps {
  title: string
  value?: string
  detail: string
  loading?: boolean
  tone?: 'default' | 'ok' | 'warning'
  action?: ReactNode
}

function FlowStage({ title, value, detail, loading = false, tone = 'default', action }: FlowStageProps) {
  // The node is filled and the value is text, so they take the same tone
  // through different properties. One map carrying both would paint a
  // background behind the number.
  const valueClass = tone === 'ok' ? 'text-success' : tone === 'warning' ? 'text-warning' : 'text-foreground'

  return (
    <div className={FLOW_STAGE}>
      <div className="flex items-center justify-between gap-3">
        <p className="text-label font-semibold text-muted-foreground">{title}</p>
        <span aria-hidden="true" className={cn('size-2 rounded-full', tone === 'ok' ? 'bg-success' : tone === 'warning' ? 'bg-warning' : 'bg-foreground')} />
      </div>
      {loading ? (
        <div aria-hidden="true" className="h-8 w-24 animate-pulse rounded bg-muted" />
      ) : action ? (
        <div>{action}</div>
      ) : (
        <p className={`font-mono text-metric-sm font-semibold leading-none tabular-nums md:text-metric ${valueClass}`}>
          {value ?? '—'}
        </p>
      )}
      <div className="space-y-0.5">
        <p className="text-meta text-muted-foreground">{detail}</p>
      </div>
    </div>
  )
}

interface NowStripProps {
  query: UseQueryResult<NowResponse>
  upstreamHealth: ReturnType<typeof dashboardStatus>['upstreamHealth']
  snapshotStale: boolean
  cacheHitRate?: number
  hitCount?: number
  cacheDataPending?: boolean
}

export default function NowStrip({
  query,
  upstreamHealth,
  snapshotStale,
  cacheHitRate,
  hitCount,
  cacheDataPending = false,
}: NowStripProps) {
  const { t } = useTranslation()
  const data = query.data
  const hasInitialError = query.isError && !data
  const hasStaleData = query.isRefetchError && Boolean(data)
  const upstreamsKnown = Boolean(upstreamHealth && upstreamHealth.total > 0)
  const upstreamsAllHealthy = upstreamsKnown && upstreamHealth?.healthy === upstreamHealth?.total
  const upstreamTone = !upstreamsKnown || snapshotStale ? 'default' : upstreamsAllHealthy ? 'ok' : 'warning'
  const upstreamValue = upstreamsKnown ? `${upstreamHealth!.healthy} / ${upstreamHealth!.total}` : undefined
  const hitRateValue = typeof cacheHitRate === 'number' ? `${(cacheHitRate * 100).toFixed(1)}%` : undefined
  const requestValue = data ? data.rate.requests_per_min.toLocaleString() : undefined
  const requestDetail = t('dashboard.requestRateWindow')
  const [infoOpen, setInfoOpen] = useState(false)
  const cachePrimary = hitCount !== undefined ? hitCount.toLocaleString() : hitRateValue
  const cacheDetail = hitCount !== undefined ? t('dashboard.cacheHitsRange') : t('dashboard.cacheHitRate')

  return (
    <section
      data-query-key="now"
      aria-labelledby="request-flow-title"
      aria-describedby="request-flow-description"
      aria-busy={query.isPending || undefined}
      className="flex min-w-0 flex-col overflow-hidden border-t border-border"
    >
      <header className="flex min-h-10 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-0">
        <div className="min-w-0">
          <h2 id="request-flow-title" className="text-body font-semibold text-foreground">
            {t('dashboard.requestPath')}
          </h2>
          <p id="request-flow-description" className="sr-only">
            {t('dashboard.requestPathHint')}
          </p>
        </div>
        <ButtonV2 type="button" variant="ghost" size="sm" className="size-10 p-0 text-muted-foreground" onClick={() => setInfoOpen(true)} aria-haspopup="dialog">
          ⓘ <span className="sr-only">{t('dashboard.requestPathInfo')}</span>
        </ButtonV2>
      </header>

      {snapshotStale && <p role="status" className="px-4 py-1 text-meta text-warning">{t('dashboard.snapshotStale')}</p>}
      {hasStaleData && (
        <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 text-meta text-warning">
          <span title={query.dataUpdatedAt ? new Date(query.dataUpdatedAt).toLocaleString() : undefined}>
            {t('dashboard.liveStale')}
          </span>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>
            {t('now.refresh')}
          </ButtonV2>
        </div>
      )}

      <div
        role="group"
        aria-label={t('dashboard.flowlineDescription')}
        className="grid min-w-0 grid-cols-1 gap-2 px-4 py-2 md:grid-cols-[minmax(0,1fr)_16px_minmax(0,1.25fr)_64px_minmax(0,1fr)] md:items-stretch md:gap-2"
      >
        {hasInitialError ? (
          <div className={FLOW_STAGE}>
            <h3 className="text-label font-semibold">{t('dashboard.clientIngress')}</h3>
            <QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : t('dashboard.liveUnavailable')} onRetry={() => { void query.refetch() }} />
          </div>
        ) : <FlowStage
          title={t('dashboard.clientIngress')}
          value={requestValue}
          detail={requestDetail}
          loading={query.isPending && !data}
        />}
            <span aria-hidden="true" className="hidden self-center text-center text-lg text-muted-foreground md:block">→</span>
            <FlowStage
              title={t('dashboard.depsiloCache')}
              value={cachePrimary}
              detail={cacheDetail}
              loading={cacheDataPending}
            />
            <span className="self-center whitespace-nowrap text-center text-meta text-muted-foreground">{t('dashboard.onDemandShort')} <span aria-hidden>→</span></span>
            <FlowStage
              title={t('dashboard.upstreamStage')}
              detail={upstreamHealth ? upstreamsKnown
                ? upstreamHealth.failed > 0
                  ? [upstreamHealth.slow > 0 ? t('dashboard.upstreamSlow', { count: upstreamHealth.slow }) : null, t('dashboard.upstreamUnavailable', { count: upstreamHealth.failed })].filter(Boolean).join(' · ')
                  : upstreamHealth.slow > 0
                    ? t('dashboard.upstreamSlow', { count: upstreamHealth.slow })
                    : t('dashboard.upstreamHealthyLabel')
                : t('dashboard.noUpstreams') : t('dashboard.upstreamStatusUnknown')}
              loading={cacheDataPending}
              tone={upstreamTone}
              action={upstreamsKnown ? (
                <Link
                  to={getAdminRouteHref('upstreams')}
                  className={cn(
                    'inline-flex min-h-10 items-center rounded-md px-2 font-mono text-metric-sm font-semibold leading-none tabular-nums no-underline hover:bg-accent md:text-metric',
                    snapshotStale
                      ? 'text-muted-foreground'
                      : upstreamsAllHealthy ? 'text-success' : 'text-warning',
                  )}
                  aria-label={t('now.viewUpstreams', {
                    healthy: upstreamHealth!.healthy,
                    total: upstreamHealth!.total,
                  })}
                >
                  {upstreamValue}
                </Link>
              ) : undefined}
            />
      </div>

      <Modal open={infoOpen} onClose={() => setInfoOpen(false)} title={t('dashboard.requestPathInfo')} width={520}>
        <p className="text-sm leading-5 text-muted-foreground">{t('dashboard.requestPathHint')}</p>
      </Modal>
    </section>
  )
}
