import './StatusBand.css'

import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import StatusDot from '@/components/StatusDot'
import { LANGUAGES } from '@/lib/ecosystemData'
import { formatUptimeSeconds } from '@/lib/dashboardOverview'
import { upstreamStatus } from '@/lib/upstreamStatus'
import { formatBytes } from '@/lib/utils'
import { serviceHealth, usePortalStats, type PortalStats } from '@/portal/portalStats'

interface Props {
  /** Opens an ecosystem's configuration, e.g. from a failing mirror chip. */
  onOpenEcosystem: (id: string) => void
}

/** The last hour, five minutes a bar: hits in green over all requests. */
function LastHour({ stats, loading }: { stats?: PortalStats; loading: boolean }) {
  const { t, i18n } = useTranslation()
  const points = stats?.series?.points.slice(-12) ?? []
  const total = points.reduce((sum, point) => sum + point.requests, 0)

  if (loading || !stats || total === 0) {
    // An idle hour is a fact, not an empty chart: it is said in words.
    return (
      <div className="status-band-hour" data-status-band-hour="idle">
        <span className="status-band-label">{t('quickstart.status.lastHour')}</span>
        <span className="status-band-hour-idle">
          {loading ? <span className="status-band-skeleton" /> : stats ? t('quickstart.status.noRequests') : '—'}
        </span>
      </div>
    )
  }

  const peak = Math.max(1, ...points.map(point => point.requests))
  return (
    <div className="status-band-hour" data-status-band-hour="active" title={t('quickstart.status.lastHourHint')}>
      <span className="status-band-label">
        {t('quickstart.status.lastHourRequests', { total: total.toLocaleString(i18n.language) })}
      </span>
      <svg viewBox="0 0 120 24" preserveAspectRatio="none" aria-hidden="true">
        {points.map((point, index) => {
          const x = index * 10 + 1
          const height = (point.requests / peak) * 22
          const hitHeight = (point.hits / peak) * 22
          return (
            <g key={point.time_unix}>
              <rect x={x} y={24 - Math.max(height, 1)} width={8} height={Math.max(height, 1)} rx={1} fill={point.requests ? 'var(--status-band-miss)' : 'var(--border)'} />
              {point.hits > 0 && <rect x={x} y={24 - hitHeight} width={8} height={hitHeight} rx={1} fill="var(--status-band-hit)" />}
            </g>
          )
        })}
      </svg>
    </div>
  )
}

function Figure({ label, value, detail, loading, optional = false }: {
  label: string
  value: string
  detail?: string
  loading: boolean
  optional?: boolean
}) {
  return (
    <div className="status-band-cell" data-optional={optional ? 'true' : undefined}>
      <span className="status-band-label">{label}</span>
      <span className="status-band-value">
        {loading ? <span className="status-band-skeleton" /> : value}
        {!loading && detail && <small>{detail}</small>}
      </span>
    </div>
  )
}

/**
 * One row of live service figures above Quick Start: is Depsilo serving,
 * how much it served today, what the cache saved, and which upstream mirrors
 * are failing. A failing mirror opens its ecosystem's configuration below.
 */
export default function StatusBand({ onOpenEcosystem }: Props) {
  const { t, i18n } = useTranslation()
  const { data: stats, isPending, isError, refetch } = usePortalStats()
  const unavailable = isError && !stats
  const health = isPending || unavailable ? 'unknown' : serviceHealth(stats)
  const upstreams = stats?.upstreams ?? []
  const failing = upstreams.filter(upstream => upstreamStatus(upstream) === 'failed')
  const number = (value: number) => value.toLocaleString(i18n.language)
  const percent = (rate: number) => `${(rate * 100).toFixed(1)}%`
  const headline = isPending
    ? t('quickstart.status.loading')
    : unavailable
      ? t('quickstart.status.unavailable')
      : health === 'healthy'
        ? t('quickstart.status.serving')
        : health === 'failed'
          ? t('quickstart.status.offline')
          : t('quickstart.status.unknown')

  return (
    <section className="status-band" aria-label={t('quickstart.status.label')} aria-busy={isPending}>
      <div className="status-band-state" data-tone={health}>
        <StatusDot status={health} size={8} live={health === 'healthy' && !isError} />
        <div className="status-band-state-text">
          <span className="status-band-state-head" role="status">{headline}</span>
          {unavailable ? (
            <button type="button" className="status-band-retry stripe-focus-ring" onClick={() => void refetch()}>
              <Icon name="refresh" size="sm" />
              {t('quickstart.status.retry')}
            </button>
          ) : (
            <span className="status-band-label">
              {stats?.service.uptime_seconds != null
                ? t('quickstart.status.uptime', { duration: formatUptimeSeconds(stats.service.uptime_seconds, t) })
                : ' '}
              {stats && isError ? ` · ${t('quickstart.status.refreshFailed')}` : ''}
            </span>
          )}
        </div>
      </div>
      <Figure
        label={t('quickstart.status.requestsToday')}
        loading={isPending}
        value={stats?.today ? number(stats.today.total_requests) : '—'}
      />
      <Figure
        label={t('quickstart.status.hitRateToday')}
        loading={isPending}
        value={stats?.today ? percent(stats.today.hit_rate) : '—'}
        detail={stats?.week ? t('quickstart.status.hitRateWeek', { rate: percent(stats.week.hit_rate) }) : undefined}
      />
      <Figure
        label={t('quickstart.status.servedToday')}
        loading={isPending}
        value={stats?.today ? formatBytes(stats.today.bytes_served) : '—'}
        optional
      />
      <Figure
        label={t('quickstart.status.savedWeek')}
        loading={isPending}
        value={stats?.week ? formatBytes(stats.week.bytes_saved) : '—'}
      />
      <Figure
        label={t('quickstart.status.cacheSize')}
        loading={isPending}
        value={stats?.cache ? formatBytes(stats.cache.total_size_bytes) : '—'}
        detail={stats?.cache ? t('quickstart.status.cacheFiles', { files: number(stats.cache.total_files) }) : undefined}
        optional
      />
      <div className="status-band-cell status-band-upstreams" data-tone={stats ? (failing.length ? 'warn' : 'ok') : undefined}>
        <span className="status-band-label">
          {stats && upstreams.length > 0
            ? t('quickstart.status.upstreamsTotal', { count: upstreams.length })
            : t('quickstart.status.upstreams')}
        </span>
        <span className="status-band-value">
          {isPending ? <span className="status-band-skeleton" /> : !stats ? '—' : (
            <>
              <Icon name={failing.length ? 'warning' : 'check_circle'} size="sm" />
              {failing.length
                ? t('quickstart.status.upstreamsFailing', { count: failing.length })
                : t('quickstart.status.upstreamsHealthy')}
              {failing.slice(0, 2).map(upstream => {
                const language = LANGUAGES.find(item => item.iconAdapter === upstream.adapter)
                const label = language ? `${language.name} · ${upstream.name}` : upstream.name
                return language ? (
                  <button
                    key={upstream.id}
                    type="button"
                    className="status-band-failing"
                    onClick={() => onOpenEcosystem(language.id)}
                    aria-label={t('quickstart.status.openFailing', { mirror: label, name: language.name })}
                  >
                    {label}
                  </button>
                ) : (
                  <span key={upstream.id} className="status-band-failing">{label}</span>
                )
              })}
              {failing.length > 2 && <small>+{failing.length - 2}</small>}
            </>
          )}
        </span>
      </div>
      <LastHour stats={stats} loading={isPending} />
    </section>
  )
}
