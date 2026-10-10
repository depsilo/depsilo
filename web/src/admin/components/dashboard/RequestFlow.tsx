import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import Icon, { type IconName } from '@/components/Icon'
import Logo from '@/components/Logo'
import TooltipV2 from '@/components/Tooltip'
import { getAdminRouteHref } from '@/admin/routes'
import type { DashboardPeriod, NowResponse, OriginCoverage } from '@/lib/adminApi.types'
import { buildRequestFlow, coverageDetail, formatPercentRatio } from '@/lib/dashboardOverview'
import { formatBps, formatBytes } from '@/lib/utils'

import { HINT_TOOLTIP_DELAY_MS } from './MetricTile'
import { METRIC_TONE_PALETTE, type MetricTone } from './metricTone'

interface RequestFlowProps {
  now?: NowResponse
  nowPending: boolean
  period?: DashboardPeriod
  rangeStart?: string
  coverage?: OriginCoverage
}

/**
 * The Overview's request path: who is asking, what Depsilo did with the
 * request, and where the origin traffic went. Live rails carry the 60s byte
 * rates; the nodes carry the selected period's totals. Every value keeps its
 * honest state (未采集 / — / 平台不支持) and a measurement-basis hint.
 */
export default function RequestFlow({
  now,
  nowPending,
  period,
  rangeStart,
  coverage,
}: RequestFlowProps) {
  const { t } = useTranslation()
  const flow = buildRequestFlow({ now, period, coverage, rangeStart })
  const livePending = nowPending && !now
  const coverageNote = coverageDetail(coverage, rangeStart, t)
  const originRequestsDetail = flow.originMeasured
    ? t('overview.originRequestsTotal', { count: (flow.originRequests ?? 0).toLocaleString() })
    : t('overview.notCollected')
  const originDetail = flow.originMeasured && coverageNote
    ? `${t('overview.originPartialShort')} · ${originRequestsDetail}`
    : originRequestsDetail

  return (
    <div
      data-dashboard-request-flow
      role="group"
      aria-label={t('overview.flowAria')}
      className="flex min-w-0 flex-col gap-4"
    >
      <div className="dashboard-flow-grid grid min-w-0 grid-cols-1 items-center gap-3 lg:grid-cols-6 lg:gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(120px,0.7fr)_minmax(0,1fr)_minmax(120px,0.7fr)_minmax(0,1fr)]">
        <FlowNode
          testId="traffic-served-total"
          tone="memory"
          icon="computer"
          title={t('overview.flowClients')}
          value={flow.servedBytes === null ? '—' : formatBytes(flow.servedBytes)}
          detail={flow.servedRequests === null
            ? undefined
            : t('overview.servedRequests', { count: flow.servedRequests.toLocaleString() })}
        />

        <FlowRail
          testId="traffic-service-flow"
          label={t('overview.serviceFlow')}
          badge={t('overview.liveBadge')}
          value={flow.liveServiceBytesPerSec === null ? '—' : formatBps(flow.liveServiceBytesPerSec)}
          pending={livePending}
          detail={flow.liveServiceBytesPerSec === null ? t('overview.notCollected') : t('overview.directionService')}
        />

        <FlowNode
          testId="traffic-depsilo"
          mark={<Logo size={30} />}
          title="Depsilo"
          value={formatPercentRatio(flow.hitRate)}
          detail={t('overview.hitRateLabel')}
          info={t('overview.hintHitRate')}
          infoLabel={t('overview.hitRateInfoLabel')}
        />

        <FlowRail
          testId="traffic-origin-flow"
          label={t('overview.originFlow')}
          badge={t('overview.liveBadge')}
          value={flow.liveOriginBytesPerSec === null ? '—' : formatBps(flow.liveOriginBytesPerSec)}
          pending={livePending}
          detail={flow.liveOriginBytesPerSec === null ? t('overview.notCollected') : t('overview.directionOrigin')}
        />

        <FlowNode
          testId="traffic-origin-total"
          tone="origin"
          icon="cloud_sync"
          title={t('overview.flowUpstream')}
          value={flow.originBytes === null ? '—' : formatBytes(flow.originBytes)}
          detail={originDetail}
          info={t('overview.hintOriginTotal')}
          infoLabel={t('overview.originTotalInfoLabel')}
        />
      </div>

      <div
        data-dashboard-flow-outcomes
        role="group"
        aria-label={t('overview.flowOutcomesLabel')}
        className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t pt-3"
        style={{ borderColor: 'var(--dash-border)' }}
      >
        <OutcomeChip
          tone="cpu"
          label={t('overview.outcomeHit')}
          value={flow.hitRequests === null ? '—' : flow.hitRequests.toLocaleString()}
        />
        <OutcomeChip
          tone="origin"
          label={t('overview.outcomeMiss')}
          value={flow.missRequests === null ? '—' : flow.missRequests.toLocaleString()}
        />
        <TooltipV2
          delay={HINT_TOOLTIP_DELAY_MS}
          content={<span className="block max-w-[260px] leading-[1.5]">{t('overview.flowBlockedHint')}</span>}
        >
          <Link
            to={getAdminRouteHref('security')}
            aria-label={t('overview.flowBlockedLinkLabel')}
            className="dash-focus inline-flex min-h-8 min-w-0 items-center gap-2 rounded-md px-1.5 text-[13px] no-underline transition-colors duration-150 hover:bg-[var(--dash-soft)]"
          >
            <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: METRIC_TONE_PALETTE.danger.strong }} />
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
  tone,
  icon,
  mark,
}: {
  testId: string
  title: string
  value: string
  detail?: string
  /** Optional: only hints that add a real measurement basis get an affordance. */
  info?: string
  infoLabel?: string
  tone?: MetricTone
  icon?: IconName
  /** Custom leading mark (the brand mark for Depsilo); wins over icon/tone. */
  mark?: ReactNode
}) {
  return (
    <div data-testid={testId} className="flex min-w-0 items-start gap-3">
      {mark ?? (
        <span
          aria-hidden
          className="grid size-10 shrink-0 place-items-center rounded-lg"
          style={{
            background: METRIC_TONE_PALETTE[tone ?? 'default'].soft,
            color: METRIC_TONE_PALETTE[tone ?? 'default'].strong,
          }}
        >
          {icon && <Icon name={icon} size="md" />}
        </span>
      )}
      <div className="flex min-w-0 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-1.5">
          {/* Wraps instead of truncating: "开发者 / CI / Agent" cannot fit the
              node column at 1280, and the three audiences are all real. */}
          <p className="min-w-0 text-[15px] font-medium leading-tight" style={{ color: 'var(--dash-muted)' }}>{title}</p>
          {info && infoLabel && <InfoHint text={info} label={infoLabel} />}
        </div>
        <p className="font-mono text-[22px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</p>
        {detail && (
          <p className="min-w-0 text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
        )}
      </div>
    </div>
  )
}

/**
 * The connector between two nodes. Desktop draws the horizontal rail with its
 * label above and the measured direction below; narrow screens stack the same
 * facts under a short vertical connector.
 */
function FlowRail({
  testId,
  label,
  badge,
  value,
  pending,
  detail,
  info,
  infoLabel,
}: {
  testId: string
  label: string
  badge: string
  value: string
  pending: boolean
  /** Direction of the measured bytes, or the honest "not collected" label. */
  detail: string
  /** Optional: the live rate already states its own direction and window. */
  info?: string
  infoLabel?: string
}) {
  const valueNode = pending
    ? <span aria-hidden className="block h-6 w-24 animate-pulse rounded bg-[var(--dash-soft)]" />
    : (
      <span className="font-mono text-[18px] font-semibold leading-tight tabular-nums" style={{ color: 'var(--dash-ink)' }}>
        {value}
      </span>
    )

  const labelRow = (
    <div className="flex min-w-0 items-center gap-1.5">
      <span className="min-w-0 text-[13px] font-medium leading-tight" style={{ color: 'var(--dash-muted)' }}>{label}</span>
      <span className="shrink-0 rounded-full px-1.5 py-0.5 text-[12px] font-medium" style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}>
        {badge}
      </span>
      {info && infoLabel && <InfoHint text={info} label={infoLabel} />}
    </div>
  )

  return (
    <div data-testid={testId} className="min-w-0">
      {/* Desktop: label → value → rail → measured direction. */}
      <div className="hidden min-w-0 flex-col gap-1 lg:flex">
        {labelRow}
        {valueNode}
        <div aria-hidden className="flex items-center gap-1">
          <span className="h-px flex-1" style={{ background: 'var(--dash-border)' }} />
          <Icon name="arrow_forward" size="sm" style={{ color: 'var(--dash-muted)' }} />
        </div>
        <p className="text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
      </div>
      {/* Narrow screens: short vertical connector + the same facts. */}
      <div className="flex min-w-0 items-start gap-3 pl-4 lg:hidden">
        <div aria-hidden className="flex shrink-0 flex-col items-center pt-1">
          <span className="h-4 w-px" style={{ background: 'var(--dash-border)' }} />
          <Icon name="expand_more" size="sm" style={{ color: 'var(--dash-muted)' }} />
        </div>
        <div className="flex min-w-0 flex-col gap-0.5">
          {labelRow}
          {valueNode}
          <p className="text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
        </div>
      </div>
    </div>
  )
}

function OutcomeChip({ tone, label, value }: { tone: MetricTone; label: string; value: string }) {
  return (
    <p className="inline-flex min-h-8 min-w-0 items-center gap-2 text-[13px]">
      <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: METRIC_TONE_PALETTE[tone].strong }} />
      <span style={{ color: 'var(--dash-muted)' }}>{label}</span>
      <span className="font-mono text-[15px] font-semibold tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</span>
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
