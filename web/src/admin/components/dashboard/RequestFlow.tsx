import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import Icon, { type IconName } from '@/components/Icon'
import Logo from '@/components/Logo'
import TooltipV2 from '@/components/Tooltip'
import { getAdminRouteHref } from '@/admin/routes'
import type { DashboardPeriod, NowResponse, OriginCoverage } from '@/lib/adminApi.types'
import { buildRequestFlow, coverageDetail } from '@/lib/dashboardOverview'
import { formatBps, formatBytes } from '@/lib/utils'

import DashboardMetricHelp from './DashboardMetricHelp'
import { METRIC_TONE_PALETTE, type MetricTone } from './metricTone'

interface RequestFlowProps {
  now?: NowResponse
  nowPending: boolean
  nowError?: boolean
  nowStale?: boolean
  period?: DashboardPeriod
  periodPending?: boolean
  rangeStart?: string
  coverage?: OriginCoverage
}

/** Download bytes flow from upstream through Depsilo to the client. Nodes
 * carry selected-period totals; connectors carry the unchanged 60s mean rate. */
export default function RequestFlow({
  now,
  nowPending,
  nowError = false,
  nowStale = false,
  period,
  periodPending = false,
  rangeStart,
  coverage,
}: RequestFlowProps) {
  const { t } = useTranslation()
  const flow = buildRequestFlow({ now, period, coverage, rangeStart })
  const livePending = nowPending && !now
  const totalsPending = periodPending && !period
  const liveUnavailable = nowError && !now
  const coverageNote = coverageDetail(coverage, rangeStart, t)
  const originRequestsDetail = flow.originMeasured
    ? (flow.originRequests === null ? undefined : t('overview.originRequestsTotal', { count: flow.originRequests.toLocaleString() }))
    : t('overview.notCollected')
  const originDetail = totalsPending
    ? t('loading')
    : flow.originMeasured && coverageNote
      ? `${t('overview.originPartialShort')} · ${originRequestsDetail ?? ''}`
      : originRequestsDetail
  const rateDetail = (rate: number | null) => livePending
    ? t('loading')
    : liveUnavailable
      ? t('overview.flowRateUnavailable')
      : rate === null ? t('overview.notCollected') : undefined
  const rateBadge = nowStale ? t('overview.flowRateStale') : undefined

  return (
    <div
      data-dashboard-request-flow
      role="group"
      aria-label={t('overview.flowAria')}
      className="dashboard-request-flow flex min-w-0 flex-col gap-2"
    >
      <div className="dashboard-flow-grid grid min-w-0 items-start">
        <FlowNode
          testId="traffic-origin-total"
          icon="cloud_sync"
          title={t('overview.flowUpstream')}
          value={flow.originBytes === null ? '—' : formatBytes(flow.originBytes)}
          detail={originDetail}
          pending={totalsPending}
          info={t('overview.hintOriginTotal')}
          infoLabel={t('overview.originTotalInfoLabel')}
        />

        <FlowRail
          testId="traffic-origin-flow"
          label={t('overview.originRate')}
          badge={rateBadge}
          value={flow.liveOriginBytesPerSec === null ? '—' : formatBps(flow.liveOriginBytesPerSec)}
          pending={livePending}
          direction={t('overview.directionOrigin')}
          detail={rateDetail(flow.liveOriginBytesPerSec)}
        />

        <div data-testid="traffic-depsilo" className="flex min-w-0 flex-col gap-1">
          <div className="dashboard-flow-proxy-heading flex min-h-[40px] items-center gap-2">
            <span aria-hidden className="dashboard-flow-mobile-mark grid size-7 shrink-0 place-items-center"><Logo size={26} /></span>
            <p className="text-[14px] font-medium leading-snug" style={{ color: 'var(--dash-ink)' }}>Depsilo</p>
          </div>
          <p className="dashboard-flow-proxy-detail min-h-[28px] pt-1 text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{t('overview.flowLocalCache')}</p>
          {/* The proxy mark shares the rails' final row, so the download path
              remains connected without moving the endpoint value baselines. */}
          <div data-flow-axis aria-hidden className="dashboard-flow-axis justify-center"><Logo size={26} /></div>
        </div>

        <FlowRail
          testId="traffic-service-flow"
          label={t('overview.clientDeliveryRate')}
          badge={rateBadge}
          value={flow.liveServiceBytesPerSec === null ? '—' : formatBps(flow.liveServiceBytesPerSec)}
          pending={livePending}
          direction={t('overview.directionService')}
          detail={rateDetail(flow.liveServiceBytesPerSec)}
        />

        <FlowNode
          testId="traffic-served-total"
          icon="computer"
          title={t('overview.flowClients')}
          value={flow.servedBytes === null ? '—' : formatBytes(flow.servedBytes)}
          pending={totalsPending}
          detail={totalsPending ? t('loading') : flow.servedRequests === null
            ? undefined
            : t('overview.servedRequests', { count: flow.servedRequests.toLocaleString() })}
        />
      </div>

      <div
        data-dashboard-flow-outcomes
        role="group"
        aria-label={t('overview.flowOutcomesLabel')}
        className="flex flex-wrap items-center gap-x-5 gap-y-1 border-t pt-1"
        style={{ borderColor: 'var(--dash-border)' }}
      >
        <OutcomeChip
          tone="ok"
          label={t('overview.outcomeHit')}
          value={flow.hitRequests === null ? '—' : flow.hitRequests.toLocaleString()}
          pending={totalsPending}
        />
        <OutcomeChip
          tone="accent"
          label={t('overview.outcomeMiss')}
          value={flow.missRequests === null ? '—' : flow.missRequests.toLocaleString()}
          pending={totalsPending}
        />
        <TooltipV2
          content={<span className="block max-w-[260px] text-[14px] leading-[1.65]">{t('overview.flowBlockedHint')}</span>}
        >
          <Link
            to={getAdminRouteHref('security')}
            aria-label={t('overview.flowBlockedLinkLabel')}
            className="dash-focus inline-flex min-h-[40px] min-w-0 items-center gap-2 rounded-md px-1.5 text-[13px] no-underline transition-colors duration-150 hover:bg-[var(--dash-soft)]"
          >
            <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: 'var(--dash-muted)' }} />
            <span style={{ color: 'var(--dash-muted)' }}>{t('overview.outcomeBlocked')}</span>
            <Icon name="chevron_right" size="sm" style={{ color: 'var(--dash-muted)' }} />
          </Link>
        </TooltipV2>
      </div>
    </div>
  )
}

function FlowNode({
  testId,
  title,
  value,
  detail,
  info,
  infoLabel,
  pending = false,
  icon,
}: {
  testId: string
  title: string
  value?: string
  detail?: string
  info?: string
  infoLabel?: string
  pending?: boolean
  icon?: IconName
}) {
  return (
    <div data-testid={testId} aria-busy={pending} className="flex min-w-0 flex-col gap-1">
      <div className="flex min-h-[40px] min-w-0 items-center gap-2">
        <span aria-hidden className="grid size-7 shrink-0 place-items-center" style={{ color: 'var(--dash-muted)' }}>
          {icon && <Icon name={icon} size="md" />}
        </span>
        <div className="min-w-0 text-[14px] font-medium leading-snug" style={{ color: 'var(--dash-ink)' }}>
          {info && infoLabel ? <DashboardMetricHelp label={title} ariaLabel={infoLabel}><p>{info}</p></DashboardMetricHelp> : <p>{title}</p>}
        </div>
      </div>
      <div className="flex min-h-[28px] min-w-0 items-baseline">
        {value !== undefined ? (pending
          ? <span aria-hidden className="block h-[26px] w-20 animate-pulse rounded bg-[var(--dash-soft)]" />
          : <p className="font-mono text-[22px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</p>)
          : <p className="pt-1 text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>}
      </div>
      {value !== undefined && detail && <p className="min-w-0 text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>}
    </div>
  )
}

/** The same semantic order becomes a downward path below the container breakpoint. */
function FlowRail({ testId, label, badge, value, pending, direction, detail }: {
  testId: string
  label: string
  badge?: string
  value: string
  pending: boolean
  direction: string
  detail?: string
}) {
  return (
    <div data-testid={testId} aria-busy={pending} className="dashboard-flow-rail min-w-0 items-center gap-3">
      <div aria-hidden className="dashboard-flow-mobile-mark flex shrink-0 flex-col items-center">
        <span className="h-6 w-px" style={{ background: 'var(--dash-border)' }} />
        <Icon name="expand_more" size="sm" style={{ color: 'var(--dash-muted)' }} />
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <span className="dashboard-flow-rate-label flex items-center text-[13px] font-medium leading-snug" style={{ color: 'var(--dash-muted)' }}>{label}</span>
        <div className="flex min-h-[28px] items-baseline">
          {pending
            ? <span aria-hidden className="block h-[23px] w-24 animate-pulse rounded bg-[var(--dash-soft)]" />
            : <span className="font-mono text-[18px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</span>}
        </div>
        <div data-flow-axis aria-hidden className="dashboard-flow-axis gap-1">
          <span className="h-px flex-1" style={{ background: 'var(--dash-border)' }} />
          <Icon name="arrow_forward" size="sm" style={{ color: 'var(--dash-muted)' }} />
        </div>
        <span className="sr-only">{direction}</span>
        {detail && <p className="text-[12px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>}
        {badge && <span className="text-[12px] leading-snug" style={{ color: 'var(--dash-muted)' }}>{badge}</span>}
      </div>
    </div>
  )
}

function OutcomeChip({ tone, label, value, pending }: { tone: MetricTone; label: string; value: string; pending: boolean }) {
  return (
    <p aria-busy={pending} className="inline-flex min-h-8 min-w-0 items-center gap-2 text-[13px]">
      <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: METRIC_TONE_PALETTE[tone].strong }} />
      <span style={{ color: 'var(--dash-muted)' }}>{label}</span>
      {pending
        ? <span aria-hidden className="block h-5 w-10 animate-pulse rounded bg-[var(--dash-soft)]" />
        : <span className="font-mono text-[15px] font-semibold tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</span>}
    </p>
  )
}
