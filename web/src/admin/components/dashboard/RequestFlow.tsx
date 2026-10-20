import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import Icon, { type IconName } from '@/components/Icon'
import Logo from '@/components/Logo'
import TooltipV2 from '@/components/Tooltip'
import { getAdminRouteHref } from '@/admin/routes'
import type { DashboardPeriod, NowResponse, OriginCoverage } from '@/lib/adminApi.types'
import { buildRequestFlow, coverageDetail } from '@/lib/dashboardOverview'
import { formatBps, formatBytes } from '@/lib/utils'

import { HINT_TOOLTIP_DELAY_MS } from './MetricTile'
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
  const rateDetail = (direction: string, rate: number | null) => livePending
    ? t('loading')
    : liveUnavailable
      ? t('overview.flowRateUnavailable')
      : rate === null ? t('overview.notCollected') : direction
  const rateBadge = nowStale ? t('overview.flowRateStale') : t('overview.rateWindow')

  return (
    <div
      data-dashboard-request-flow
      role="group"
      aria-label={t('overview.flowAria')}
      className="@container flex min-w-0 flex-col gap-4"
    >
      <div className="dashboard-flow-grid grid min-w-0 grid-cols-1 items-center gap-3 @[760px]:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.15fr)] @[760px]:gap-4">
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
          label={t('overview.originFlow')}
          badge={rateBadge}
          value={flow.liveOriginBytesPerSec === null ? '—' : formatBps(flow.liveOriginBytesPerSec)}
          pending={livePending}
          detail={rateDetail(t('overview.directionOrigin'), flow.liveOriginBytesPerSec)}
        />

        <FlowNode
          testId="traffic-depsilo"
          mark={<Logo size={26} />}
          title="Depsilo"
          detail={t('overview.flowLocalCache')}
        />

        <FlowRail
          testId="traffic-service-flow"
          label={t('overview.serviceFlow')}
          badge={rateBadge}
          value={flow.liveServiceBytesPerSec === null ? '—' : formatBps(flow.liveServiceBytesPerSec)}
          pending={livePending}
          detail={rateDetail(t('overview.directionService'), flow.liveServiceBytesPerSec)}
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
        className="flex flex-wrap items-center gap-x-5 gap-y-1 border-t pt-3"
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
          delay={HINT_TOOLTIP_DELAY_MS}
          content={<span className="block max-w-[260px] leading-[1.5]">{t('overview.flowBlockedHint')}</span>}
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
  mark,
}: {
  testId: string
  title: string
  value?: string
  detail?: string
  info?: string
  infoLabel?: string
  pending?: boolean
  icon?: IconName
  mark?: ReactNode
}) {
  return (
    <div data-testid={testId} aria-busy={pending} className="flex min-w-0 items-center gap-3 @[760px]:flex-col @[760px]:items-start @[760px]:gap-2">
      <span aria-hidden className="grid size-8 shrink-0 place-items-center" style={{ color: 'var(--dash-muted)' }}>
        {mark ?? (icon && <Icon name={icon} size="md" />)}
      </span>
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex min-w-0 items-center gap-1">
          <p className="min-w-0 text-[14px] font-medium leading-snug" style={{ color: 'var(--dash-ink)' }}>{title}</p>
          {info && infoLabel && <InfoHint text={info} label={infoLabel} />}
        </div>
        {value !== undefined && (pending
          ? <span aria-hidden className="block h-[26px] w-20 animate-pulse rounded bg-[var(--dash-soft)]" />
          : <p className="font-mono text-[22px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</p>)}
        {detail && <p className="min-w-0 text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>}
      </div>
    </div>
  )
}

/** The same semantic order becomes a downward path below the container breakpoint. */
function FlowRail({ testId, label, badge, value, pending, detail }: {
  testId: string
  label: string
  badge: string
  value: string
  pending: boolean
  detail: string
}) {
  return (
    <div data-testid={testId} aria-busy={pending} className="flex min-w-0 items-center gap-3 pl-3 @[760px]:block @[760px]:pl-0">
      <div aria-hidden className="flex shrink-0 flex-col items-center @[760px]:hidden">
        <span className="h-6 w-px" style={{ background: 'var(--dash-border)' }} />
        <Icon name="expand_more" size="sm" style={{ color: 'var(--dash-muted)' }} />
      </div>
      <div className="flex min-w-0 flex-col gap-1">
        <span className="text-[13px] font-medium leading-snug" style={{ color: 'var(--dash-muted)' }}>{label}</span>
        {pending
          ? <span aria-hidden className="block h-[23px] w-24 animate-pulse rounded bg-[var(--dash-soft)]" />
          : <span className="font-mono text-[18px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</span>}
        <div aria-hidden className="hidden items-center gap-1 @[760px]:flex">
          <span className="h-px flex-1" style={{ background: 'var(--dash-border)' }} />
          <Icon name="arrow_forward" size="sm" style={{ color: 'var(--dash-muted)' }} />
        </div>
        <p className="text-[12px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
        <span className="text-[12px] leading-snug" style={{ color: 'var(--dash-muted)' }}>{badge}</span>
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

function InfoHint({ text, label }: { text: string; label: string }) {
  return (
    <TooltipV2 delay={HINT_TOOLTIP_DELAY_MS} content={<span className="block max-w-[240px] leading-[1.5]">{text}</span>}>
      <button
        type="button"
        aria-label={label}
        className="dash-focus grid size-[40px] shrink-0 place-items-center rounded-full transition-colors duration-150 hover:bg-[var(--dash-soft)]"
        style={{ color: 'var(--dash-muted)' }}
      >
        <Icon name="info" size="sm" />
      </button>
    </TooltipV2>
  )
}
