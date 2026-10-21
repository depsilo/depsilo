import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import Icon from '@/components/Icon'
import { getAdminRouteHref } from '@/admin/routes'
import type { NowResponse, ReadinessResponse } from '@/lib/adminApi.types'
import { formatUptimeSeconds, type ServiceProblem, type ServiceProblemCode, type ServiceStatusModel } from '@/lib/dashboardOverview'
import DashboardInfoDialog from './DashboardInfoDialog'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  status: ServiceStatusModel
  pending: boolean
  refreshing: boolean
  readiness?: ReadinessResponse
  now?: NowResponse
  policyStale: boolean
  upstreamsStale: boolean
  upstreamsUpdatedAt: number
  onRefresh: () => void
}

const upstreamCodes = new Set<ServiceProblemCode>(['upstreams', 'upstreams-slow'])

function problemTitle(problem: ServiceProblem, t: TFunction, policyStale: boolean): string {
  switch (problem.code) {
    case 'database': return t('overview.problemDatabase')
    case 'storage': return t('overview.problemStorage')
    case 'readiness-unavailable': return t('overview.problemReadinessUnavailable')
    case 'readiness-stale': return t('overview.problemReadinessStale')
    case 'upstream-status-unavailable': return t('overview.upstreamStatusUnavailable')
    case 'upstream-status-stale': return t('overview.upstreamStatusStale')
    case 'policy-status-unavailable': return t('overview.policyStatusUnavailable')
    case 'policy-status-stale': return t('overview.policyStatusStale')
    case 'policy': return t(policyStale ? 'overview.problemPolicyStale' : 'overview.problemPolicy')
    case 'cache': return t('overview.problemCache', { percent: (problem.percent ?? 0).toFixed(1) })
    case 'status-unavailable': return t('overview.problemUnavailable')
    default: return t('overview.problemDegraded')
  }
}

function problemEntry(code: ServiceProblemCode): { href: string; label: string; impact: string } | null {
  switch (code) {
    case 'database': case 'storage':
      return { href: getAdminRouteHref('settings'), label: 'overview.viewSettings', impact: 'overview.readinessImpact' }
    case 'degraded':
      return { href: getAdminRouteHref('upstreams'), label: 'dashboard.viewUpstreams', impact: 'overview.upstreamImpact' }
    case 'cache':
      return { href: getAdminRouteHref('cache'), label: 'dashboard.manageCache', impact: 'overview.cacheImpact' }
    case 'policy':
      return { href: getAdminRouteHref('rules'), label: 'policy.reviewRules', impact: 'overview.policyImpact' }
    default: return null
  }
}

/** One keyboard-accessible entry and one existing dialog for all health states. */
export default function DashboardServiceStatus({
  open, onOpenChange, status, pending, refreshing, readiness, now,
  policyStale, upstreamsStale, upstreamsUpdatedAt, onRefresh,
}: Props) {
  const { t, i18n } = useTranslation()
  const upstreamOnly = status.affectedUpstreamCount > 0
    && status.problems.every(problem => upstreamCodes.has(problem.code))
  const title = pending ? t('overview.checkingHealth')
    : upstreamOnly ? t('overview.upstreamsAttention', { count: status.affectedUpstreamCount })
    : t({
      healthy: 'overview.statusHealthy', partial: 'overview.serviceAttention',
      unavailable: 'overview.statusUnavailable', unknown: 'overview.statusUnknown',
    }[status.health])
  const color = pending || status.health === 'unknown' ? 'var(--text-soft)'
    : status.health === 'healthy' ? 'var(--ok-text)'
    : status.health === 'unavailable' ? 'var(--danger-text)' : 'var(--warn-text)'
  const otherProblems = status.problems.filter(problem => !upstreamCodes.has(problem.code))
  const upstreams = status.upstreamIssues.length > 0 ? status.upstreamIssues : status.upstreams
  const close = () => onOpenChange(false)

  return <>
    <button
      type="button"
      data-dashboard-status
      data-query-key="now"
      data-health={pending ? 'loading' : status.health}
      aria-haspopup="dialog"
      aria-expanded={open}
      aria-busy={pending || undefined}
      onClick={() => onOpenChange(true)}
      className="stripe-focus-ring inline-flex min-h-[40px] max-w-full items-center gap-2 rounded-md border px-3 text-[13px] font-medium hover:bg-[var(--bg-hover)]"
      style={{ background: 'var(--bg-card)', borderColor: 'var(--border)', color: 'var(--text)' }}
    >
      <Icon name={pending ? 'progress_activity' : status.health === 'healthy' ? 'check_circle' : status.health === 'unknown' ? 'help' : 'warning'} size="sm" style={{ color }} />
      <span className="min-w-0 text-left">{title}</span>
      <Icon name="expand_more" size="sm" className="shrink-0 text-[var(--text-soft)]" />
    </button>
    <DashboardInfoDialog
      open={open}
      onClose={close}
      title={t('overview.problemsTitle')}
      width={640}
      footer={<button type="button" onClick={onRefresh} aria-busy={refreshing || undefined} className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-2 rounded-md border border-[var(--border)] px-3 text-[14px] hover:bg-[var(--bg-hover)]"><Icon name="refresh" size="sm" />{t('overview.refreshStatus')}</button>}
    >
      <div role="status" className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-[14px] font-semibold" style={{ color }}>{title}</p>
        {now && <p className="text-[12px] text-[var(--text-soft)]">{t('overview.uptimeLabel')} {formatUptimeSeconds(now.uptime_seconds, t)}</p>}
      </div>
      <dl className="flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
        {(['database', 'storage'] as const).map(check => <div key={check} className="flex items-center gap-2">
          <dt className="text-[var(--text-soft)]">{t(`overview.healthChecks.${check}`)}</dt>
          <dd>{status.problems.some(problem => problem.code === 'readiness-stale') && `${t('overview.previousState')} · `}{t(readiness?.checks[check] === 'ready' ? 'overview.checkReady' : readiness?.checks[check] === 'unavailable' ? 'overview.checkUnavailable' : 'overview.checkUnknown')}</dd>
        </div>)}
      </dl>
      {otherProblems.length > 0 && <ul className="flex flex-col gap-3">
        {otherProblems.map(problem => {
          const entry = problemEntry(problem.code)
          return <li key={problem.code} className="rounded-md border border-[var(--border)] p-3">
            <h3 className="text-[14px] font-semibold">{problemTitle(problem, t, policyStale)}</h3>
            <p className="mt-1 text-[13px] leading-relaxed text-[var(--text-soft)]">{t(entry?.impact ?? 'overview.healthUnknownImpact')}</p>
            {entry && <Link to={entry.href} onClick={close} className="stripe-focus-ring mt-1 inline-flex min-h-[40px] items-center gap-1 rounded-md text-[13px] text-[var(--brand-text)]">{t(entry.label)}<Icon name="chevron_right" size="sm" /></Link>}
          </li>
        })}
      </ul>}
      {upstreams.length > 0 && <section aria-label={t('dashboard.viewUpstreams')} className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <h3 className="text-[14px] font-semibold">{status.affectedUpstreamCount > 0 ? t('overview.upstreamChecksSummary', { count: status.affectedUpstreamCount, checks: status.upstreamCheckCount }) : t('overview.upstreamChecksHealthy', { count: upstreams.length })}</h3>
          <Link to={getAdminRouteHref('upstreams')} onClick={close} className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-1 rounded-md text-[13px] text-[var(--brand-text)]">{t('dashboard.viewUpstreams')}<Icon name="chevron_right" size="sm" /></Link>
        </div>
        {upstreams.map(upstream => <article key={upstream.key} data-upstream-health-id={upstream.id} className="min-w-0 rounded-md border border-[var(--border)] p-3">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <h4 className="break-words text-[14px] font-semibold">{upstream.name}</h4>
              <p className="mt-0.5 text-[12px] text-[var(--text-soft)]">{upstream.adapter}{upstream.id > 0 && ` · #${upstream.id}`} · {t('overview.upstreamCheckCount', { count: upstream.checks.length })}</p>
            </div>
            <p className="text-[13px]" style={{ color: upstream.state === 'healthy' ? 'var(--ok-text)' : 'var(--warn-text)' }}>
              {upstreamsStale && `${t('overview.previousState')} · `}{t(`overview.upstreamState.${upstream.state}`)}
            </p>
          </div>
          <dl className="mt-3 grid gap-2 text-[13px]">
            <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-[var(--text-soft)]">{t('overview.upstreamHealthCheck')}</dt><dd>{t(upstream.healthy ? 'overview.checkAvailable' : 'overview.checkUnavailable')}</dd></div>
            {upstream.avg_latency_ms > 0 && <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-[var(--text-soft)]">{t('overview.upstreamLatencyCheck')}</dt><dd className="tabular-nums">{Math.round(upstream.avg_latency_ms).toLocaleString(i18n.language)} ms{upstream.checks.includes('slow') && ` · ${t('overview.upstreamState.slow')}`}</dd></div>}
          </dl>
        </article>)}
        {status.affectedUpstreamCount > 0 && <p className="text-[13px] leading-relaxed text-[var(--text-soft)]">{t('overview.upstreamImpact')}</p>}
        <div className="space-y-1 text-[12px] leading-relaxed text-[var(--text-soft)]">
          <p>{t('overview.upstreamObservationBasis')}</p>
          <p>{t('overview.upstreamTimingUnavailable')}</p>
          {upstreamsUpdatedAt > 0 && <p>{t('overview.statusDataUpdatedAt', { time: new Date(upstreamsUpdatedAt).toLocaleString(i18n.language) })}</p>}
        </div>
      </section>}
      {!pending && status.health === 'healthy' && upstreams.length === 0 && <p className="text-[13px] text-[var(--text-soft)]">{t('overview.noUpstreamChecks')}</p>}
    </DashboardInfoDialog>
  </>
}
