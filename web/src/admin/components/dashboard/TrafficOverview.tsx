import { useTranslation } from 'react-i18next'

import type { DashboardPeriod, DashboardRange, NowResponse, OriginCoverage } from '@/lib/adminApi.types'
import { rangeLabelKey } from '@/lib/dashboardOverview'

import RequestFlow from './RequestFlow'

interface TrafficOverviewProps {
  now?: NowResponse
  nowPending: boolean
  period?: DashboardPeriod
  range: DashboardRange
  rangeStart?: string
  coverage?: OriginCoverage
}

/**
 * One request-path card replaces the four metric tiles: the clients →
 * Depsilo → upstream flow with its live rails, then the selected period's
 * outcome chips. The section header carries the range so it is stated once.
 */
export default function TrafficOverview({
  now,
  nowPending,
  period,
  range,
  rangeStart,
  coverage,
}: TrafficOverviewProps) {
  const { t } = useTranslation()
  const periodLabel = t(rangeLabelKey(range))

  return (
    <section data-dashboard-traffic aria-labelledby="overview-traffic-title" className="flex min-w-0 flex-col gap-3">
      <header className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <h2 id="overview-traffic-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.trafficTitle')}
        </h2>
        {period && (
          <span className="text-[13px]" style={{ color: 'var(--dash-muted)' }}>{periodLabel}</span>
        )}
      </header>
      <div className="dash-card min-w-0 px-5 py-4">
        <RequestFlow
          now={now}
          nowPending={nowPending}
          period={period}
          rangeStart={rangeStart}
          coverage={coverage}
        />
      </div>
    </section>
  )
}
