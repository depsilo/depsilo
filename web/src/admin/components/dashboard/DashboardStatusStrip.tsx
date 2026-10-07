import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import type { NowResponse } from '@/lib/adminApi.types'
import {
  formatRelativeSeconds,
  formatUptimeSeconds,
  type ServiceStatusModel,
  type ServiceHealth,
} from '@/lib/dashboardOverview'

const HEALTH_TONE: Record<ServiceHealth, { color: string; soft: string; key: string }> = {
  healthy: { color: 'var(--dash-ok)', soft: 'var(--dash-ok-soft)', key: 'overview.statusHealthy' },
  partial: { color: 'var(--dash-warn)', soft: 'var(--dash-warn-soft)', key: 'overview.statusPartial' },
  unavailable: { color: 'var(--dash-danger)', soft: 'var(--dash-danger-soft)', key: 'overview.statusUnavailable' },
  unknown: { color: 'var(--dash-warn)', soft: 'var(--dash-warn-soft)', key: 'overview.statusUnknown' },
}

interface StatusCellProps {
  icon: 'check_circle' | 'warning' | 'cached' | 'history' | 'speed' | 'monitoring'
  label: string
  title: string
  detail: string
  toneColor: string
  toneSoft: string
  iconSize?: 'md' | 'lg'
  loading?: boolean
  /** When set, the whole cell becomes the action instead of a detached button. */
  onActivate?: () => void
  /** Inline affordance appended to the detail line, e.g. "查看问题". */
  actionLabel?: string
}

function StatusCell({
  icon,
  label,
  title,
  detail,
  toneColor,
  toneSoft,
  iconSize = 'md',
  loading = false,
  onActivate,
  actionLabel,
}: StatusCellProps) {
  const body = (
    <>
      <span
        aria-hidden="true"
        className={`grid shrink-0 place-items-center rounded-full ${iconSize === 'lg' ? 'size-12' : 'size-9'}`}
        style={{ background: toneSoft, color: toneColor }}
      >
        <Icon name={icon} size={iconSize === 'lg' ? 'lg' : 'md'} />
      </span>
      <div className="min-w-0">
        <p className="text-[13px] font-medium" style={{ color: 'var(--dash-muted)' }}>{label}</p>
        {loading ? (
          <span aria-hidden="true" className="mt-1 block h-5 w-28 animate-pulse rounded bg-[var(--dash-soft)]" />
        ) : (
          <p data-status-value className="mt-0.5 text-[16px] font-semibold leading-tight" style={{ color: 'var(--dash-ink)' }}>
            {title}
          </p>
        )}
        {/* Wraps instead of truncating: with the problems button on a 1280
            row the cell narrows and the guidance sentence used to clip. */}
        <p className="mt-0.5 text-[13px] leading-tight" style={{ color: 'var(--dash-muted)' }} title={detail}>
          {detail}
          {actionLabel && (
            <>
              <span aria-hidden="true"> · </span>
              <span className="inline-flex items-center gap-0.5 font-medium" style={{ color: 'var(--dash-accent)' }}>
                {actionLabel}
                <Icon name="chevron_right" size="sm" />
              </span>
            </>
          )}
        </p>
      </div>
    </>
  )

  if (onActivate) {
    return (
      <button
        type="button"
        onClick={onActivate}
        className="dash-focus flex min-w-0 w-full items-center gap-3 px-5 py-4 text-left transition-colors duration-150 hover:bg-[var(--dash-soft)]"
      >
        {body}
      </button>
    )
  }
  return <div className="flex min-w-0 items-center gap-3 px-5 py-4">{body}</div>
}

interface DashboardStatusStripProps {
  now?: NowResponse
  nowPending: boolean
  nowStale: boolean
  nowError: boolean
  status: ServiceStatusModel
  /** Resource cells rendered as the card's second, divided row. */
  resources?: ReactNode
  onOpenProblems: () => void
  onRefresh: () => void
}

export default function DashboardStatusStrip({
  now,
  nowPending,
  nowStale,
  nowError,
  status,
  resources,
  onOpenProblems,
  onRefresh,
}: DashboardStatusStripProps) {
  const { t } = useTranslation()
  const nowAvailable = Boolean(now) && !nowError
  const tone = HEALTH_TONE[nowStale ? 'unknown' : status.health]
  const statusTitle = nowStale
    ? t('overview.statusStale')
    : nowError
      ? t('overview.statusUnknown')
      : t(tone.key)

  const measured = now?.rate.measured === true
  const hasActivity = nowAvailable && now?.rate.has_data === true
  const activityTitle = !nowAvailable
    ? t('overview.activityUnknown')
    : hasActivity
      ? t('overview.activityServing')
      : t('overview.activityIdle')
  const activityDetail = !nowAvailable
    ? t('overview.activityUnknownHint')
    : hasActivity
      ? t('overview.serviceReqRate', { value: (now?.rate.service_requests_per_sec ?? 0).toFixed(2) })
      : measured
        ? t('overview.activityIdleHint')
        : t('overview.activityUnknownHint')

  const last = now?.last_activity
  const lastTitle = last ? formatRelativeSeconds(last.seconds_ago, t) : t('overview.noActivity')
  const lastDetail = last
    ? `${last.adapter_type}${last.package_name ? ` · ${last.package_name}` : ''}`
    : t('overview.noActivityHint')

  const uptimeTitle = now ? formatUptimeSeconds(now.uptime_seconds, t) : '—'
  const uptimeDetail = now ? t('overview.uptimeHint') : t('overview.uptimeUnavailable')

  const cells = (
    <>
      <StatusCell
        icon={status.health === 'healthy' ? 'check_circle' : 'warning'}
        label={t('overview.serviceStatusLabel')}
        title={statusTitle}
        detail={status.problems.length > 0
          ? t('overview.problemCount', { count: status.problems.length })
          : t('overview.allNominal')}
        toneColor={tone.color}
        toneSoft={tone.soft}
        iconSize="lg"
        loading={nowPending}
        onActivate={status.problems.length > 0 && !nowPending ? onOpenProblems : undefined}
        actionLabel={status.problems.length > 0 && !nowPending ? t('overview.viewProblems') : undefined}
      />
      <StatusCell
        icon="speed"
        label={t('overview.currentActivity')}
        title={activityTitle}
        detail={activityDetail}
        toneColor="var(--dash-memory)"
        toneSoft="var(--dash-memory-soft)"
        loading={nowPending}
      />
      <StatusCell
        icon="history"
        label={t('overview.recentActivity')}
        title={lastTitle}
        detail={lastDetail}
        toneColor="var(--dash-memory)"
        toneSoft="var(--dash-memory-soft)"
        loading={nowPending}
      />
      <StatusCell
        icon="monitoring"
        label={t('overview.uptimeLabel')}
        title={uptimeTitle}
        detail={uptimeDetail}
        toneColor="var(--dash-memory)"
        toneSoft="var(--dash-memory-soft)"
        loading={nowPending}
      />
    </>
  )

  return (
    <section
      data-dashboard-status-strip
      data-query-key="now"
      aria-labelledby="overview-runtime-title"
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex min-w-0 items-center border-b px-5 py-4" style={{ borderColor: 'var(--dash-border)' }}>
        <h2 id="overview-runtime-title" className="text-[20px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.runtimeStatusLabel')}
        </h2>
      </header>
      <div
        aria-busy={nowPending || undefined}
        className="flex min-w-0 flex-col gap-3 py-1 xl:flex-row xl:items-center"
      >
        <div className="grid min-w-0 flex-1 grid-cols-1 divide-y divide-[var(--dash-border)] sm:grid-cols-2 sm:divide-y-0 xl:grid-cols-4 xl:divide-x">
          {cells}
        </div>
        {nowStale && (
          <div
            role="status"
            className="flex flex-wrap items-center gap-2 px-5 pb-3 text-[13px] xl:pb-0 xl:pr-5"
            style={{ color: 'var(--dash-warn)' }}
          >
            <span>{t('now.staleData')}</span>
            <ButtonV2 type="button" variant="secondary" size="sm" onClick={onRefresh}>
              {t('now.refresh')}
            </ButtonV2>
          </div>
        )}
      </div>
      {resources && (
        <div className="min-w-0 border-t" style={{ borderColor: 'var(--dash-border)' }}>
          {resources}
        </div>
      )}
    </section>
  )
}
