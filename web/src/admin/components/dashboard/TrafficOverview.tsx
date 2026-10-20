import { useTranslation } from 'react-i18next'

import type { DashboardPeriod, NowResponse, OriginCoverage } from '@/lib/adminApi.types'

import RequestFlow from './RequestFlow'

interface TrafficOverviewProps {
  now?: NowResponse
  nowPending: boolean
  nowError?: boolean
  nowStale?: boolean
  period?: DashboardPeriod
  periodPending?: boolean
  rangeStart?: string
  coverage?: OriginCoverage
}

export default function TrafficOverview(props: TrafficOverviewProps) {
  const { t } = useTranslation()

  return (
    <section
      data-dashboard-traffic
      aria-labelledby="overview-traffic-title"
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-5 gap-y-1 px-5 pt-4">
        <h2 id="overview-traffic-title" className="text-[16px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.trafficTitle')}
        </h2>
        <p className="text-[12px] leading-relaxed" style={{ color: 'var(--dash-muted)' }}>{t('overview.flowScope')}</p>
      </header>
      <div className="min-w-0 px-5 pb-3 pt-2">
        <RequestFlow {...props} />
      </div>
    </section>
  )
}
