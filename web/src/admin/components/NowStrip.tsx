// NowStrip is the live liveness signal at the top of the dashboard.
//
// Polls /api/v1/now every 5s. Shows a status dot with a subtle breathing
// animation (the load-bearing "service is alive" cue), rolling rate +
// hit-rate + avg-latency, an upstream rollup, a 30-min request sparkline,
// and a last-activity relative timestamp. When access_logs is empty it
// switches to an onboarding hint that nudges new users toward configuring
// their first client.
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Link } from 'react-router-dom'

import { getAdminRouteHref } from '@/admin/routes'
import ButtonV2 from '@/components/Button'
import QueryErrorState from '@/components/QueryErrorState'
import { statsApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { formatBps } from '@/lib/utils'

interface SparklinePoint {
  t: number
  requests: number
  hits: number
}

interface LastActivity {
  seconds_ago: number
  adapter_type: string
  hit: boolean
  package_name?: string
}

interface NowData {
  status: 'healthy' | 'degraded' | 'down'
  uptime_seconds: number
  now_unix: number
  version: string
  last_activity?: LastActivity
  rate: {
    requests_per_min: number
    egress_bps: number
    ingress_bps: number
    has_data: boolean
  }
  upstreams: {
    total: number
    healthy: number
  }
  sparkline: SparklinePoint[]
}

const breathing = `
@keyframes nowPulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50%      { opacity: 0.55; transform: scale(0.85); }
}
.now-pulse { animation: nowPulse 1.6s ease-in-out infinite; }
@media (prefers-reduced-motion: reduce) {
  .now-pulse { animation: none; }
}
`

function statusColor(s: NowData['status']): string {
  if (s === 'healthy') return 'var(--ok)'
  if (s === 'degraded') return 'var(--warn-text)'
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
  const mins = Math.floor((seconds % 3600) / 60)
  if (hours > 0) return t('now.uptimeHM', { hours, minutes: mins })
  return t('now.uptimeMin', { minutes: mins })
}

function Sparkline({ points }: { points: SparklinePoint[] }) {
  if (points.length === 0) return null
  const W = 120
  const H = 28
  const max = Math.max(1, ...points.map(p => p.requests))
  const step = W / Math.max(1, points.length - 1)
  const path = points
    .map((p, i) => {
      const x = i * step
      const y = H - (p.requests / max) * H
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  const areaPath = `${path} L${W.toFixed(1)},${H} L0,${H} Z`
  return (
    <svg width={W} height={H} aria-hidden style={{ flexShrink: 0 }}>
      <defs>
        <linearGradient id="nowSparkGrad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="var(--brand)" stopOpacity={0.35} />
          <stop offset="100%" stopColor="var(--brand)" stopOpacity={0} />
        </linearGradient>
      </defs>
      <path d={areaPath} fill="url(#nowSparkGrad)" stroke="none" />
      <path d={path} fill="none" stroke="var(--brand)" strokeWidth={1.4} />
    </svg>
  )
}

const cellStyle: React.CSSProperties = {
  display: 'inline-flex',
  alignItems: 'baseline',
  gap: 5,
  fontSize: 12,
  color: 'var(--text-muted)',
  whiteSpace: 'nowrap',
}
const valueStyle: React.CSSProperties = {
  fontSize: 13,
  fontWeight: 600,
  color: 'var(--text)',
  // Tabular numerals so the 5-second poll doesn't shift req/min, MB/s,
  // and upstream-count cells horizontally as their digit widths change.
  fontVariantNumeric: 'tabular-nums',
}
// Small visual nudge for the inline ↑/↓ arrows: pure baseline alignment
// makes the glyphs sit too low against the digit caps next to them.
const arrowStyle: React.CSSProperties = {
  ...valueStyle,
  display: 'inline-block',
  transform: 'translateY(-1px)',
  fontWeight: 700,
}
const sepStyle: React.CSSProperties = {
  color: 'var(--border-strong)',
  fontSize: 11,
  flexShrink: 0,
}

interface NowStripProps {
  /**
   * 'card' (default) — original full-page-card chrome with border + radius.
   * 'compact'        — status-only shell signal. Detailed traffic and retry
   *                    controls remain in the Dashboard card.
   */
  variant?: 'card' | 'compact'
}

export default function NowStrip({ variant = 'card' }: NowStripProps) {
  const { t } = useTranslation()
  const {
    data,
    error,
    isPending,
    isError,
    isRefetchError,
    dataUpdatedAt,
    refetch,
  } = useQuery<NowData>({
    queryKey: ['admin', 'now'],
    queryFn: async () => {
      const res = await statsApi.getNow()
      return res.data as NowData
    },
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
    staleTime: 4000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })

  // Empty / onboarding state: no traffic ever recorded.
  const isEmpty = Boolean(data && !data.last_activity && data.rate.requests_per_min === 0)
  const hasInitialError = isError && !data
  const hasStaleData = isRefetchError && Boolean(data)
  const statusLabel = hasInitialError
    ? t('now.statusUnavailable')
    : isPending && !data
      ? t('loading')
      : data?.status === 'healthy'
        ? t(isEmpty ? 'now.statusReady' : 'now.statusHealthy')
        : data?.status === 'degraded'
          ? t('now.statusDegraded')
          : t('now.statusDown')
  const dotColor = hasInitialError
    ? 'var(--warn-text)'
    : isPending && !data
      ? 'var(--text-subtle)'
      : statusColor(data?.status ?? 'down')

  if (variant === 'compact') {
    const compactLabel = hasStaleData ? t('now.staleData') : statusLabel
    const compactDotColor = hasStaleData ? 'var(--warn-text)' : dotColor
    const errorMessage = hasInitialError ? getApiError(error).message : undefined
    const accessibleLabel = errorMessage ? `${compactLabel}: ${errorMessage}` : compactLabel

    return (
      <div
        data-admin-service-status
        role="status"
        aria-busy={isPending || undefined}
        aria-label={accessibleLabel}
        title={accessibleLabel}
        className="inline-flex h-10 min-w-0 items-center gap-2 whitespace-nowrap text-[11px] text-[var(--text-soft)]"
      >
        <style>{breathing}</style>
        <span
          className={!hasInitialError && !(isPending && !data) && !hasStaleData ? 'now-pulse' : undefined}
          aria-hidden
          style={{
            display: 'inline-block',
            width: 8,
            height: 8,
            flexShrink: 0,
            borderRadius: '50%',
            background: compactDotColor,
            boxShadow: `0 0 0 3px ${compactDotColor}22`,
          }}
        />
        <span className="hidden sm:inline">{compactLabel}</span>
      </div>
    )
  }

  const containerStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 14,
    padding: '10px 14px',
    background: 'var(--bg-card)',
    border: '0.5px solid var(--border)',
    borderRadius: 'var(--r-card)',
    flexWrap: 'wrap',
  }

  return (
    <div data-query-key="now" aria-busy={isPending || undefined} style={containerStyle}>
      <style>{breathing}</style>

      {hasStaleData && (
        <span
          className="inline-flex shrink-0 items-center gap-2 text-[11px]"
          style={{ color: 'var(--warn-text)' }}
          title={dataUpdatedAt ? new Date(dataUpdatedAt).toLocaleString() : undefined}
        >
          {t('now.staleData')}
          <ButtonV2
            type="button"
            variant="secondary"
            size="sm"
            className="min-h-10 shrink-0"
            onClick={() => { void refetch() }}
          >
            {t('now.refresh')}
          </ButtonV2>
        </span>
      )}

      {/* Status dot + label — always visible */}
      <span
        role="status"
        aria-live="polite"
        aria-atomic="true"
        style={{ display: 'inline-flex', alignItems: 'center', gap: 8, flexShrink: 0 }}
      >
        <span
          className={!hasInitialError && !(isPending && !data) ? 'now-pulse' : undefined}
          aria-hidden
          style={{
            display: 'inline-block',
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: dotColor,
            boxShadow: `0 0 0 3px ${dotColor}22`,
          }}
        />
        <span style={{ ...valueStyle, fontWeight: 500 }}>{statusLabel}</span>
      </span>

      {hasInitialError ? (
        <div className="min-w-0 flex-1">
          <QueryErrorState message={getApiError(error).status === 403 ? t('common.permissionDenied') : getApiError(error).message} onRetry={() => { void refetch() }} />
        </div>
      ) : isEmpty ? (
        // Onboarding hint — replaces the metrics row when no traffic exists.
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {t('now.emptyHint')}
        </span>
      ) : data ? (
        <>
          {/* No-data state used to render "—" placeholders; per user
              request 2026-06-29 we now show the actual zero so an
              idle instance reads as "0 req/min · 0 B/s up · 0 B/s
              down" rather than three dashes the eye reads as
              "broken / loading". rate.has_data on the wire is kept
              for any downstream caller that wants to distinguish
              never-tracked from genuinely-zero, but the NowStrip
              treats them the same. */}
          <span className="inline-flex items-center gap-3">
            <span style={sepStyle}>·</span>
            <span style={cellStyle}>
              <span style={valueStyle}>{data.rate.requests_per_min ?? 0}</span>
              {t('now.reqPerMin')}
            </span>
          </span>

          <span className="inline-flex items-center gap-3">
            <span style={sepStyle}>·</span>
            <span style={cellStyle}>
              <span style={{ ...arrowStyle, color: 'var(--ok-text)' }} aria-hidden>↑</span>
              <span style={valueStyle}>{formatBps(data.rate.egress_bps ?? 0)}</span>
              {t('now.egress')}
            </span>
          </span>

          <span className="inline-flex items-center gap-3">
            <span style={sepStyle}>·</span>
            <span style={cellStyle}>
              <span style={{ ...arrowStyle, color: 'var(--text-muted)' }} aria-hidden>↓</span>
              <span style={valueStyle}>{formatBps(data.rate.ingress_bps ?? 0)}</span>
              {t('now.ingress')}
            </span>
          </span>

          <span className="inline-flex items-center gap-3">
            <span style={sepStyle}>·</span>
            <Link
              to={getAdminRouteHref('upstreams')}
              className="stripe-focus-ring -my-1 inline-flex min-h-[40px] items-center gap-1 rounded-[4px] px-1 no-underline hover:bg-[var(--bg-hover)]"
              style={cellStyle}
              aria-label={t('now.viewUpstreams', {
                healthy: data.upstreams.healthy,
                total: data.upstreams.total,
              })}
            >
              <span style={valueStyle}>{data.upstreams.healthy}/{data.upstreams.total}</span>
              {t('now.upstreams')}
              <span aria-hidden>→</span>
            </Link>
          </span>

          <span style={{ flex: 1 }} />

          <Sparkline points={data.sparkline ?? []} />
        </>
      ) : (
        // Initial fetch in flight — render the skeleton at the same height
        // so the page doesn't jump when data arrives.
        <span style={{ height: 28, flex: 1 }} aria-hidden />
      )}

      {!isEmpty && data?.last_activity && (
        <div
          className="flex w-full min-w-0 flex-wrap items-start gap-x-3 gap-y-1 text-[11px] text-[var(--text-subtle)]"
        >
          <span className="min-w-0 flex-1 break-words">
            {t('now.lastActivity')}{' '}
            {formatRelative(data.last_activity.seconds_ago, t)} ·{' '}
            <span style={{ color: 'var(--text-muted)' }}>{data.last_activity.adapter_type}</span>
            {data.last_activity.package_name && (
              <>
                {' '}
                <span className="mono">{data.last_activity.package_name}</span>
              </>
            )}{' '}
            <span style={{ color: data.last_activity.hit ? 'var(--ok-text)' : 'var(--text-subtle)' }}>
              ({data.last_activity.hit ? t('now.cacheHit') : t('now.cacheMiss')})
            </span>
          </span>
          <span className="ml-auto shrink-0">
            {t('now.uptime')} {formatUptime(data.uptime_seconds, t)}
          </span>
        </div>
      )}
    </div>
  )
}
