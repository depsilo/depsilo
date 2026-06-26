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

import { statsApi } from '@/lib/api'

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
    hit_rate: number
    avg_latency_ms: number
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

function formatRelative(seconds: number, // eslint-disable-next-line @typescript-eslint/no-explicit-any
t: (key: string, options?: any) => string): string {
  if (seconds < 5) return t('now.justNow')
  if (seconds < 60) return t('now.secondsAgo', { count: seconds })
  if (seconds < 3600) return t('now.minutesAgo', { count: Math.floor(seconds / 60) })
  if (seconds < 86400) return t('now.hoursAgo', { count: Math.floor(seconds / 3600) })
  return t('now.daysAgo', { count: Math.floor(seconds / 86400) })
}

function formatUptime(seconds: number, // eslint-disable-next-line @typescript-eslint/no-explicit-any
t: (key: string, options?: any) => string): string {
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
  letterSpacing: '-0.01em',
}
const sepStyle: React.CSSProperties = {
  color: 'var(--border-strong)',
  fontSize: 11,
  flexShrink: 0,
}

export default function NowStrip() {
  const { t } = useTranslation()
  const { data } = useQuery<NowData>({
    queryKey: ['admin', 'now'],
    queryFn: async () => {
      const res = await statsApi.getNow()
      return res.data as NowData
    },
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
    staleTime: 4000,
  })

  // Empty / onboarding state: no traffic ever recorded.
  const isEmpty = data && !data.last_activity && data.rate.requests_per_min === 0
  const status = data?.status ?? 'healthy'
  const statusLabel =
    status === 'healthy' ? t(isEmpty ? 'now.statusReady' : 'now.statusHealthy')
    : status === 'degraded' ? t('now.statusDegraded')
    : t('now.statusDown')

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 14,
        padding: '10px 14px',
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-card)',
        flexWrap: 'wrap',
      }}
    >
      <style>{breathing}</style>

      {/* Status dot + label — always visible */}
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
        <span
          className="now-pulse"
          aria-hidden
          style={{
            display: 'inline-block',
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: statusColor(status),
            boxShadow: `0 0 0 3px ${statusColor(status)}22`,
          }}
        />
        <span style={{ ...valueStyle, fontWeight: 500 }}>{statusLabel}</span>
      </span>

      {isEmpty ? (
        // Onboarding hint — replaces the metrics row when no traffic exists.
        <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {t('now.emptyHint')}
        </span>
      ) : data ? (
        <>
          <span style={sepStyle}>·</span>
          <span style={cellStyle}>
            <span style={valueStyle}>{data.rate.requests_per_min}</span>
            {t('now.reqPerMin')}
          </span>

          <span style={sepStyle}>·</span>
          <span style={cellStyle}>
            {t('now.hitRate')}
            <span style={valueStyle}>{Math.round(data.rate.hit_rate * 100)}%</span>
          </span>

          <span style={sepStyle}>·</span>
          <span style={cellStyle}>
            <span style={valueStyle}>{Math.round(data.rate.avg_latency_ms)}ms</span>
            {t('now.avgLatency')}
          </span>

          <span style={sepStyle}>·</span>
          <span style={cellStyle}>
            <span style={valueStyle}>{data.upstreams.healthy}/{data.upstreams.total}</span>
            {t('now.upstreams')}
          </span>

          <span style={{ flex: 1 }} />

          <Sparkline points={data.sparkline} />
        </>
      ) : (
        // Initial fetch in flight — render the skeleton at the same height
        // so the page doesn't jump when data arrives.
        <span style={{ height: 28, flex: 1 }} aria-hidden />
      )}

      {!isEmpty && data?.last_activity && (
        <div
          style={{
            width: '100%',
            fontSize: 11,
            color: 'var(--text-subtle)',
            display: 'flex',
            gap: 12,
          }}
        >
          <span>
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
          <span style={{ marginLeft: 'auto' }}>
            {t('now.uptime')} {formatUptime(data.uptime_seconds, t)}
          </span>
        </div>
      )}
    </div>
  )
}
