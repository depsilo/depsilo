import { useTranslation } from 'react-i18next'

import type { DashboardPeriod, NowResponse, OriginCoverage } from '@/lib/adminApi.types'

import RequestFlow from './RequestFlow'

interface TrafficOverviewProps {
  now?: NowResponse
  nowPending: boolean
  period?: DashboardPeriod
  rangeStart?: string
  coverage?: OriginCoverage
}

/**
 * One request-path card replaces the four metric tiles: the clients →
 * Depsilo → upstream flow with its live rails, then the selected period's
 * outcome chips. The card header follows the page's one header grammar
 * (in-card h2 + bottom border); the period itself is stated once by the scope
 * row above the cards instead of repeating in each header.
 */
export default function TrafficOverview({
  now,
  nowPending,
  period,
  rangeStart,
  coverage,
}: TrafficOverviewProps) {
  const { t } = useTranslation()

  return (
    <section
      data-dashboard-traffic
      aria-labelledby="overview-traffic-title"
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex min-w-0 items-center justify-between gap-2 border-b px-5 py-4" style={{ borderColor: 'var(--dash-border)' }}>
        <h2 id="overview-traffic-title" className="text-[20px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.trafficTitle')}
        </h2>
      </header>
      <div className="min-w-0 px-5 py-4">
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
