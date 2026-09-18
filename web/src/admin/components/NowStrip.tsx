import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import ButtonV2 from '@/components/app/button'
import QueryErrorState from '@/components/app/error-state'
import type { NowResponse } from '@/lib/adminApi.types'
import { statsApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { cn } from '@/lib/utils'

/**
 * The request flowline: three stages on one axis, with a signal travelling
 * along it. Stacked below `md`, in a row from `md` up, and the axis turns with
 * the stages.
 *
 * This used to be a component-local `<style>` block of eight classes plus two
 * keyframes. It is Tailwind now, and the travelling signal is a named token
 * animation, so the global reduced-motion switch reaches it like every other.
 */
const FLOW_TRACK =
  'relative grid min-w-0 grid-cols-1 py-2 ' +
  "before:absolute before:top-7 before:bottom-7 before:left-[29px] before:w-px before:bg-input before:content-[''] " +
  'md:grid-cols-3 md:gap-6 md:p-0 ' +
  'md:before:top-[5px] md:before:right-[16.666%] md:before:bottom-auto ' +
  'md:before:left-[16.666%] md:before:h-px md:before:w-auto'

const FLOW_BEAT =
  'absolute top-7 left-7 z-0 h-7 w-[3px] animate-flow-y rounded-full bg-primary ' +
  'md:top-1 md:left-[16.666%] md:h-[3px] md:w-9 md:animate-flow-x'

const FLOW_STAGE =
  'relative z-[1] grid min-h-14 min-w-0 grid-cols-[20px_minmax(0,1fr)_auto] grid-rows-[auto_auto] ' +
  'items-center gap-x-3 px-5 py-1.5 ' +
  'md:flex md:min-h-28 md:flex-col md:items-center md:justify-start md:px-2 md:py-0 md:text-center'

const FLOW_NODE =
  'col-start-1 row-span-2 ml-1 size-[11px] rounded-full border-[3px] border-card shadow-[0_0_0_1px_var(--input)] ' +
  'md:mb-[13px] md:ml-0 md:shrink-0'

const FLOW_TITLE = 'col-start-2 row-start-1 self-end md:self-auto'
const FLOW_DETAIL = 'col-start-2 row-start-2 self-start md:self-auto'
const FLOW_VALUE = 'col-start-3 row-span-2 self-center justify-self-end md:self-auto'

function statusColor(status: NowResponse['status']): string {
  if (status === 'healthy') return 'var(--success)'
  if (status === 'degraded') return 'var(--warning)'
  return 'var(--destructive)'
}

function formatRelative(seconds: number, t: TFunction): string {
  if (seconds < 5) return t('now.justNow')
  if (seconds < 60) return t('now.secondsAgo', { count: seconds })
  if (seconds < 3600) return t('now.minutesAgo', { count: Math.floor(seconds / 60) })
  if (seconds < 86400) return t('now.hoursAgo', { count: Math.floor(seconds / 3600) })
  return t('now.daysAgo', { count: Math.floor(seconds / 86400) })
}

function formatUptime(seconds: number, t: TFunction): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  if (days > 0) return t('now.uptimeDH', { days, hours })
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours > 0) return t('now.uptimeHM', { hours, minutes })
  return t('now.uptimeMin', { minutes })
}

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
  const nodeClass = tone === 'ok' ? 'bg-success' : tone === 'warning' ? 'bg-warning' : 'bg-foreground'
  const valueClass = tone === 'ok' ? 'text-success' : tone === 'warning' ? 'text-warning' : 'text-foreground'

  return (
    <div className={FLOW_STAGE}>
      <span aria-hidden="true" className={`${FLOW_NODE} ${nodeClass}`} />
      <p className={`${FLOW_TITLE} text-label font-semibold text-muted-foreground`}>{title}</p>
      {loading ? (
        <div aria-hidden="true" className={`${FLOW_VALUE} h-7 w-20 animate-pulse rounded bg-muted`} />
      ) : action ? (
        <div className={FLOW_VALUE}>{action}</div>
      ) : (
          <p className={`${FLOW_VALUE} font-mono text-metric-sm font-semibold leading-none tabular-nums md:text-metric ${valueClass}`}>
          {value ?? '—'}
        </p>
      )}
      <p className={`${FLOW_DETAIL} mt-0.5 text-meta text-muted-foreground md:mt-1.5`}>{detail}</p>
    </div>
  )
}

interface NowStripProps {
  variant?: 'card' | 'compact'
  cacheHitRate?: number
  cacheDataPending?: boolean
}

export default function NowStrip({
  variant = 'card',
  cacheHitRate,
  cacheDataPending = false,
}: NowStripProps) {
  const { t } = useTranslation()
  const query = useQuery<NowResponse>({
    queryKey: ['admin', 'now'],
    queryFn: async ({ signal }) => {
      const response = await statsApi.getNow({ signal })
      return response.data
    },
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
    staleTime: 4_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  const data = query.data
  const isEmpty = Boolean(data && !data.last_activity && data.rate.requests_per_min === 0)
  const hasInitialError = query.isError && !data
  const hasStaleData = query.isRefetchError && Boolean(data)
  const statusLabel = hasInitialError
    ? t('now.statusUnavailable')
    : query.isPending && !data
      ? t('loading')
      : data?.status === 'healthy'
        ? t(isEmpty ? 'now.statusReady' : 'now.statusHealthy')
        : data?.status === 'degraded'
          ? t('now.statusDegraded')
          : t('now.statusDown')
  const dotColor = hasInitialError
    ? 'var(--warning)'
    : query.isPending && !data
      ? 'var(--muted-foreground)'
      : statusColor(data?.status ?? 'down')

  if (variant === 'compact') {
    const compactLabel = hasStaleData ? t('now.staleData') : statusLabel
    const compactDotColor = hasStaleData ? 'var(--warning)' : dotColor
    const errorMessage = hasInitialError ? getApiError(query.error).message : undefined
    const accessibleLabel = errorMessage ? `${compactLabel}: ${errorMessage}` : compactLabel

    return (
      <div
        data-admin-service-status
        role="status"
        aria-busy={query.isPending || undefined}
        aria-label={accessibleLabel}
        title={accessibleLabel}
        className="inline-flex h-10 min-w-0 items-center gap-2 whitespace-nowrap text-meta text-muted-foreground"
      >
          <span
          aria-hidden
          style={{
            width: 8,
            height: 8,
            flexShrink: 0,
            borderRadius: '50%',
            background: compactDotColor,
          }}
        />
        <span className="hidden sm:inline">{compactLabel}</span>
      </div>
    )
  }

  // No upstreams is *unknown*, not healthy. Rendering `0 / 0` in the success
  // colour would claim a clean bill of health for something never observed.
  const upstreamsKnown = Boolean(data && data.upstreams.total > 0)
  const upstreamsAllHealthy = Boolean(
    data && data.upstreams.total > 0 && data.upstreams.healthy >= data.upstreams.total,
  )
  const upstreamTone = !upstreamsKnown ? 'default' : upstreamsAllHealthy ? 'ok' : 'warning'
  const upstreamValue = data ? `${data.upstreams.healthy}/${data.upstreams.total}` : undefined
  const hitRateValue = typeof cacheHitRate === 'number' ? `${(cacheHitRate * 100).toFixed(1)}%` : undefined

  return (
    <section
      data-query-key="now"
      aria-labelledby="request-flow-title"
      aria-describedby="request-flow-description"
      aria-busy={query.isPending || undefined}
      className="flex h-full min-w-0 flex-col overflow-hidden border-b border-border bg-card"
    >
      <header className="flex min-h-12 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2">
        <div className="min-w-0">
          <h2 id="request-flow-title" className="text-body font-semibold text-foreground">
            {t('dashboard.requestPath')}
          </h2>
          <p id="request-flow-description" className="sr-only">
            {t('dashboard.requestPathHint')}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2" role="status" aria-live="polite">
          <span
            aria-hidden
            style={{
              width: 8,
              height: 8,
              borderRadius: '50%',
              background: hasStaleData ? 'var(--warning)' : dotColor,
            }}
          />
          <span className="text-meta font-semibold text-muted-foreground">
            {hasStaleData ? t('now.staleData') : statusLabel}
          </span>
        </div>
      </header>

      {hasStaleData && (
        <div className="flex flex-wrap items-center justify-between gap-2 bg-warning/10 px-4 py-2 text-meta text-warning">
          <span title={query.dataUpdatedAt ? new Date(query.dataUpdatedAt).toLocaleString() : undefined}>
            {t('now.staleData')}
          </span>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>
            {t('now.refresh')}
          </ButtonV2>
        </div>
      )}

      {hasInitialError ? (
        <div className="p-4">
          <QueryErrorState
            message={getApiError(query.error).status === 403
              ? t('common.permissionDenied')
              : getApiError(query.error).message}
            onRetry={() => { void query.refetch() }}
          />
        </div>
      ) : (
        <>
          <div
            role="group"
            aria-label={t('dashboard.flowlineDescription')}
            className={FLOW_TRACK}
          >
            {!hasStaleData && data && <span className={FLOW_BEAT} aria-hidden />}
            <FlowStage
              title={t('dashboard.clientIngress')}
              value={data ? String(data.rate.requests_per_min ?? 0) : undefined}
              detail={t('now.reqPerMin')}
              loading={query.isPending && !data}
            />
            <FlowStage
              title={t('dashboard.depsiloCache')}
              value={hitRateValue}
              detail={t('dashboard.cacheHitRate')}
              loading={cacheDataPending}
              tone="ok"
            />
            <FlowStage
              title={t('dashboard.upstreamStage')}
              detail={t('dashboard.upstreamHealth')}
              loading={query.isPending && !data}
              tone={upstreamTone}
              action={data ? (
                <Link
                  to={getAdminRouteHref('upstreams')}
                  className={cn(
                    'inline-flex min-h-10 items-center rounded-md px-2 font-mono text-metric-sm font-semibold leading-none tabular-nums no-underline hover:bg-accent md:text-metric',
                    !upstreamsKnown
                      ? 'text-muted-foreground'
                      : upstreamsAllHealthy ? 'text-success' : 'text-warning',
                  )}
                  aria-label={t('now.viewUpstreams', {
                    healthy: data.upstreams.healthy,
                    total: data.upstreams.total,
                  })}
                >
                  {upstreamValue}
                </Link>
              ) : undefined}
            />
          </div>

          <footer className="mt-auto flex min-h-10 min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-t border-border px-4 py-2 text-meta text-muted-foreground">
            {data?.last_activity ? (
              <span className="min-w-0 flex-1 truncate">
                {t('now.lastActivity')}{' '}
                {formatRelative(data.last_activity.seconds_ago, t)} ·{' '}
                <span className="text-muted-foreground">{data.last_activity.adapter_type}</span>
                {data.last_activity.package_name && (
                  <> · <span className="font-mono">{data.last_activity.package_name}</span></>
                )}
              </span>
            ) : (
              <span className="min-w-0 flex-1">
                {isEmpty ? t('now.emptyHint') : t('dashboard.flowlineDescription')}
              </span>
            )}
            {data && (
              <span className="ml-auto shrink-0 font-mono tabular-nums">
                {t('now.uptime')} {formatUptime(data.uptime_seconds, t)}
              </span>
            )}
          </footer>
        </>
      )}
    </section>
  )
}
