import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import type { NowResponse } from '@/lib/adminApi.types'
import { formatRelativeSeconds, formatUptimeSeconds, type ServiceStatusModel } from '@/lib/dashboardOverview'

interface DashboardStatusStripProps {
  now?: NowResponse
  nowPending: boolean
  nowStale: boolean
  nowError: boolean
  healthPending?: boolean
  status: ServiceStatusModel
  onRefresh: () => void
  onOpenProblems: () => void
}

export default function DashboardStatusStrip({ now, nowPending, nowStale, nowError, healthPending, status, onRefresh, onOpenProblems }: DashboardStatusStripProps) {
  const { t } = useTranslation()
  const pending = healthPending ?? nowPending
  const tone = status.health === 'healthy' ? 'ok' : status.health === 'unavailable' ? 'danger' : 'warn'
  const title = pending ? t('overview.checkingHealth') : t({ healthy: 'overview.statusHealthy', partial: 'overview.statusPartial', unavailable: 'overview.statusUnavailable', unknown: 'overview.statusUnknown' }[status.health])
  const last = now?.last_activity
  return (
    <section data-dashboard-status-strip data-query-key="now" aria-label={t('overview.runtimeStatusLabel')} aria-busy={pending || undefined} className="dashboard-status-summary">
      <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1">
        <p className="flex items-center gap-2 text-[14px] font-semibold" style={{ color: pending ? 'var(--dash-muted)' : `var(--dash-${tone})` }}>
          <Icon name={pending ? 'progress_activity' : status.health === 'healthy' ? 'check_circle' : 'warning'} size="sm" />
          <span data-status-value>{title}</span>
        </p>
        {!pending && status.problems.length > 0 && (
          <button type="button" data-dashboard-status-issues aria-haspopup="dialog" onClick={onOpenProblems} className="dash-focus inline-flex min-h-[40px] items-center gap-1 rounded-md text-[13px] text-[var(--dash-muted)] hover:text-[var(--dash-accent)]">
            {t('overview.problemCount', { count: status.problems.length })}<span className="text-[var(--dash-accent)]">{t('overview.viewProblems')}</span><Icon name="chevron_right" size="sm" />
          </button>
        )}
        {!pending && status.problems.length === 0 && <span className="text-[13px] text-[var(--dash-muted)]">{t('overview.healthBasis')}</span>}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-[13px] text-[var(--dash-muted)]">
        <span className="inline-flex items-center gap-1.5" title={t('overview.uptimeHint')}><Icon name="history" size="sm" />{t('overview.uptimeLabel')} <span className="tabular-nums">{nowPending ? t('overview.collecting') : now ? formatUptimeSeconds(now.uptime_seconds, t) : '—'}</span></span>
        {last && !nowError && <span title={`${last.adapter_type} · ${last.package_name ?? ''}`}>{t('overview.recentActivity')} · {formatRelativeSeconds(last.seconds_ago, t)}</span>}
        {nowStale && <button type="button" onClick={onRefresh} className="dash-focus min-h-[40px] rounded-md text-[var(--dash-warn)]">{t('overview.statusStale')} · {t('overview.refresh')}</button>}
      </div>
    </section>
  )
}
