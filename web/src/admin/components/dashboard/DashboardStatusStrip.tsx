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
  icon: 'check_circle' | 'cached' | 'history' | 'speed'
  label: string
  title: string
  detail: string
  toneColor: string
  toneSoft: string
  loading?: boolean
}

function StatusCell({ icon, label, title, detail, toneColor, toneSoft, loading = false }: StatusCellProps) {
  return (
    <div className="flex min-w-0 items-center gap-3 px-5 py-4">
      <span
        aria-hidden="true"
        className="grid size-10 shrink-0 place-items-center rounded-full"
        style={{ background: toneSoft, color: toneColor }}
      >
        <Icon name={icon} size="md" />
      </span>
      <div className="min-w-0">
        <p className="text-[13px] font-medium" style={{ color: 'var(--dash-muted)' }}>{label}</p>
        {loading ? (
          <span aria-hidden="true" className="mt-1 block h-5 w-28 animate-pulse rounded bg-[var(--dash-soft)]" />
        ) : (
          <p data-status-value className="mt-0.5 truncate text-[18px] font-semibold leading-tight" style={{ color: 'var(--dash-ink)' }}>
            {title}
          </p>
        )}
        <p className="mt-0.5 truncate text-[13px]" style={{ color: 'var(--dash-muted)' }} title={detail}>{detail}</p>
      </div>
    </div>
  )
}

interface DashboardStatusStripProps {
  now?: NowResponse
  nowPending: boolean
  nowStale: boolean
  nowError: boolean
  status: ServiceStatusModel
  onOpenProblems: () => void
  onRefresh: () => void
}

export default function DashboardStatusStrip({
  now,
  nowPending,
  nowStale,
  nowError,
  status,
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
        icon="check_circle"
        label={t('overview.serviceStatusLabel')}
        title={statusTitle}
        detail={status.problems.length > 0
          ? t('overview.problemCount', { count: status.problems.length })
          : t('overview.allNominal')}
        toneColor={tone.color}
        toneSoft={tone.soft}
        loading={nowPending}
      />
      <StatusCell
        icon="speed"
        label={t('overview.currentActivity')}
        title={activityTitle}
        detail={activityDetail}
        toneColor={hasActivity ? 'var(--dash-accent)' : 'var(--dash-muted)'}
        toneSoft={hasActivity ? 'var(--dash-accent-soft)' : 'var(--dash-soft)'}
        loading={nowPending}
      />
      <StatusCell
        icon="history"
        label={t('overview.recentActivity')}
        title={lastTitle}
        detail={lastDetail}
        toneColor="var(--dash-muted)"
        toneSoft="var(--dash-soft)"
        loading={nowPending}
      />
      <StatusCell
        icon="cached"
        label={t('overview.uptimeLabel')}
        title={uptimeTitle}
        detail={uptimeDetail}
        toneColor="var(--dash-muted)"
        toneSoft="var(--dash-soft)"
        loading={nowPending}
      />
    </>
  )

  return (
    <section
      data-dashboard-status-strip
      data-query-key="now"
      aria-label={t('overview.serviceStatusLabel')}
      aria-busy={nowPending || undefined}
      className="dash-card flex min-w-0 flex-col gap-3 py-1 lg:flex-row lg:items-center"
    >
      <div className="grid min-w-0 flex-1 grid-cols-1 divide-y divide-[var(--dash-border)] sm:grid-cols-2 sm:divide-y-0 lg:grid-cols-4 lg:divide-x">
        {cells}
      </div>
      {status.problems.length > 0 && !nowPending && (
        <div className="flex shrink-0 items-center px-5 pb-3 lg:pb-0 lg:pr-5">
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={onOpenProblems}>
            <Icon name="warning" size="sm" />
            {t('overview.viewProblems')}
          </ButtonV2>
        </div>
      )}
      {nowStale && (
        <div
          role="status"
          className="flex flex-wrap items-center gap-2 px-5 pb-3 text-[13px] lg:pb-0 lg:pr-5"
          style={{ color: 'var(--dash-warn)' }}
        >
          <span>{t('now.staleData')}</span>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={onRefresh}>
            {t('now.refresh')}
          </ButtonV2>
        </div>
      )}
    </section>
  )
}
