import { useTranslation } from 'react-i18next'

import type { DashboardPeriod, DashboardRange, NowResponse, OriginCoverage } from '@/lib/adminApi.types'
import { coverageDetail, rangeLabelKey } from '@/lib/dashboardOverview'
import { formatBytes, formatBps } from '@/lib/utils'

import MetricTile from './MetricTile'

interface TrafficOverviewProps {
  now?: NowResponse
  nowPending: boolean
  period?: DashboardPeriod
  range: DashboardRange
  rangeStart?: string
  coverage?: OriginCoverage
}

export default function TrafficOverview({
  now,
  nowPending,
  period,
  range,
  rangeStart,
  coverage,
}: TrafficOverviewProps) {
  const { t } = useTranslation()
  const measured = now?.rate.measured === true
  const periodLabel = t(rangeLabelKey(range))
  const coverageNote = coverageDetail(coverage, rangeStart, t)
  const coverageFlag = coverageNote ? t('overview.originPartialShort') : undefined

  return (
    <section data-dashboard-traffic aria-labelledby="overview-traffic-title" className="flex min-w-0 flex-col gap-3">
      <h2 id="overview-traffic-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
        {t('overview.trafficTitle')}
      </h2>
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricTile
          testId="traffic-service-flow"
          label={t('overview.serviceFlow')}
          icon="hub"
          tone="memory"
          badge={t('overview.liveBadge')}
          value={measured ? formatBps(now?.rate.service_bytes_per_sec ?? 0) : '—'}
          detail={measured ? undefined : t('overview.notCollected')}
          loading={nowPending && !now}
          info={t('overview.hintServiceFlow')}
          infoLabel={t('overview.serviceFlowInfoLabel')}
        />
        <MetricTile
          testId="traffic-origin-flow"
          label={t('overview.originFlow')}
          icon="sync"
          tone="origin"
          badge={t('overview.liveBadge')}
          value={measured ? formatBps(now?.rate.origin_bytes_per_sec ?? 0) : '—'}
          detail={measured ? undefined : t('overview.notCollected')}
          loading={nowPending && !now}
          info={t('overview.hintOriginFlow')}
          infoLabel={t('overview.originFlowInfoLabel')}
        />
        <MetricTile
          testId="traffic-served-total"
          label={t('overview.servedTotal')}
          icon="download"
          tone="download"
          badge={periodLabel}
          value={period ? formatBytes(period.bytes_served) : '—'}
          info={t('overview.hintServedTotal')}
          infoLabel={t('overview.servedTotalInfoLabel')}
        />
        <MetricTile
          testId="traffic-origin-total"
          label={t('overview.originTotal')}
          icon="cloud_sync"
          tone="origin"
          badge={periodLabel}
          value={period ? formatBytes(period.upstream_bytes) : '—'}
          detail={coverageFlag}
          info={t('overview.hintOriginTotal')}
          infoLabel={t('overview.originTotalInfoLabel')}
        />
      </div>
    </section>
  )
}
