import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import ButtonV2 from '@/components/Button'
import QueryErrorState from '@/components/QueryErrorState'
import type { NowResponse } from '@/lib/adminApi.types'
import { statsApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'

const flowMotion = `
@keyframes dependencyFlowX {
  0% { left: 16.666%; opacity: 0; }
  14%, 84% { opacity: 1; }
  100% { left: calc(83.333% - 36px); opacity: 0; }
}
@keyframes dependencyFlowY {
  0% { top: 28px; opacity: 0; }
  14%, 84% { opacity: 1; }
  100% { top: calc(100% - 56px); opacity: 0; }
}
.dependency-flow-track {
  position: relative;
  display: grid;
  grid-template-columns: 1fr;
  padding: 8px 0;
}
.dependency-flow-track::before {
  content: '';
  position: absolute;
  top: 28px;
  bottom: 28px;
  left: 29px;
  width: 1px;
  background: var(--border-strong);
}
.dependency-flow-beat {
  position: absolute;
  z-index: 0;
  top: 28px;
  left: 28px;
  width: 3px;
  height: 28px;
  border-radius: 999px;
  background: var(--brand);
  animation: dependencyFlowY 2.6s cubic-bezier(.2,.8,.2,1) infinite;
}
.dependency-flow-stage {
  position: relative;
  z-index: 1;
  display: grid;
  min-width: 0;
  min-height: 56px;
  grid-template-columns: 20px minmax(0, 1fr) auto;
  grid-template-rows: auto auto;
  align-items: center;
  column-gap: 12px;
  padding: 6px 20px;
}
.dependency-flow-node {
  grid-column: 1;
  grid-row: 1 / 3;
  width: 11px;
  height: 11px;
  margin-left: 4px;
  border: 3px solid var(--bg-card);
  border-radius: 999px;
  box-shadow: 0 0 0 1px var(--border-strong);
}
.dependency-flow-title {
  grid-column: 2;
  grid-row: 1;
  align-self: end;
}
.dependency-flow-detail {
  grid-column: 2;
  grid-row: 2;
  align-self: start;
}
.dependency-flow-value {
  grid-column: 3;
  grid-row: 1 / 3;
  align-self: center;
  justify-self: end;
}
@media (min-width: 768px) {
  .dependency-flow-track {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 24px;
    padding: 0;
  }
  .dependency-flow-track::before {
    top: 5px;
    right: 16.666%;
    bottom: auto;
    left: 16.666%;
    width: auto;
    height: 1px;
  }
  .dependency-flow-beat {
    top: 4px;
    left: 16.666%;
    width: 36px;
    height: 3px;
    animation-name: dependencyFlowX;
  }
  .dependency-flow-stage {
    display: flex;
    min-height: 112px;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    padding: 0 8px;
    text-align: center;
  }
  .dependency-flow-node {
    flex: 0 0 auto;
    margin: 0 0 13px;
  }
  .dependency-flow-title,
  .dependency-flow-detail,
  .dependency-flow-value {
    align-self: auto;
  }
}
@media (prefers-reduced-motion: reduce) {
  .dependency-flow-beat { animation: none; }
  .dependency-flow-beat { opacity: 1; }
}
`

function statusColor(status: NowResponse['status']): string {
  if (status === 'healthy') return 'var(--ok)'
  if (status === 'degraded') return 'var(--warn-text)'
  return 'var(--danger)'
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
  const color = tone === 'ok'
    ? 'var(--ok-text)'
    : tone === 'warning'
      ? 'var(--warn-text)'
      : 'var(--text)'

  return (
    <div className="dependency-flow-stage">
      <span aria-hidden="true" className="dependency-flow-node" style={{ background: color }} />
      <p className="dependency-flow-title text-[12px] font-[650] text-[var(--text-soft)]">{title}</p>
      {loading ? (
        <div aria-hidden="true" className="dependency-flow-value h-7 w-20 animate-pulse rounded bg-[var(--bg-soft)]" />
      ) : action ? (
        <div className="dependency-flow-value">{action}</div>
      ) : (
        <p className="dependency-flow-value font-mono text-[22px] font-[620] leading-none tabular-nums md:text-[27px]" style={{ color }}>
          {value ?? '—'}
        </p>
      )}
      <p className="dependency-flow-detail mt-0.5 text-[11px] text-[var(--text-subtle)] md:mt-1.5">{detail}</p>
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
    ? 'var(--warn-text)'
    : query.isPending && !data
      ? 'var(--text-subtle)'
      : statusColor(data?.status ?? 'down')

  if (variant === 'compact') {
    const compactLabel = hasStaleData ? t('now.staleData') : statusLabel
    const compactDotColor = hasStaleData ? 'var(--warn-text)' : dotColor
    const errorMessage = hasInitialError ? getApiError(query.error).message : undefined
    const accessibleLabel = errorMessage ? `${compactLabel}: ${errorMessage}` : compactLabel

    return (
      <div
        data-admin-service-status
        role="status"
        aria-busy={query.isPending || undefined}
        aria-label={accessibleLabel}
        title={accessibleLabel}
        className="inline-flex h-10 min-w-0 items-center gap-2 whitespace-nowrap text-[11px] text-[var(--text-soft)]"
      >
        <style>{flowMotion}</style>
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

  const upstreamTone = data && data.upstreams.healthy < data.upstreams.total ? 'warning' : 'ok'
  const upstreamValue = data ? `${data.upstreams.healthy}/${data.upstreams.total}` : undefined
  const hitRateValue = typeof cacheHitRate === 'number' ? `${(cacheHitRate * 100).toFixed(1)}%` : undefined

  return (
    <section
      data-query-key="now"
      aria-labelledby="dependency-flow-title"
      aria-describedby="dependency-flow-description"
      aria-busy={query.isPending || undefined}
      className="admin-primary-panel flex h-full min-w-0 flex-col overflow-hidden"
    >
      <style>{flowMotion}</style>
      <header className="flex min-h-12 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2">
        <div className="min-w-0">
          <h2 id="dependency-flow-title" className="text-[13px] font-[680] text-[var(--text)]">
            {t('dashboard.requestPath')}
          </h2>
          <p id="dependency-flow-description" className="sr-only">
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
              background: hasStaleData ? 'var(--warn-text)' : dotColor,
            }}
          />
          <span className="text-[11px] font-[650] text-[var(--text-soft)]">
            {hasStaleData ? t('now.staleData') : statusLabel}
          </span>
        </div>
      </header>

      {hasStaleData && (
        <div className="flex flex-wrap items-center justify-between gap-2 bg-[var(--warn-fill)] px-4 py-2 text-[11px] text-[var(--warn-text)]">
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
            className="dependency-flow-track min-w-0"
          >
            {!hasStaleData && data && <span className="dependency-flow-beat" aria-hidden />}
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
                  className="stripe-focus-ring inline-flex min-h-10 items-center rounded-[5px] px-2 font-mono text-[22px] font-[620] leading-none tabular-nums no-underline hover:bg-[var(--bg-hover)] md:text-[27px]"
                  style={{ color: upstreamTone === 'warning' ? 'var(--warn-text)' : 'var(--ok-text)' }}
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

          <footer className="mt-auto flex min-h-10 min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-t border-[var(--border-soft)] px-4 py-2 text-[11px] text-[var(--text-subtle)]">
            {data?.last_activity ? (
              <span className="min-w-0 flex-1 truncate">
                {t('now.lastActivity')}{' '}
                {formatRelative(data.last_activity.seconds_ago, t)} ·{' '}
                <span className="text-[var(--text-soft)]">{data.last_activity.adapter_type}</span>
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
