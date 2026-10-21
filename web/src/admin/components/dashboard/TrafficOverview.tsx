import { useTranslation } from 'react-i18next'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'

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
    <Card render={<section />}
      data-dashboard-traffic
      aria-labelledby="overview-traffic-title"
      className="gap-0 p-0"
    >
      <CardHeader className="min-w-0 items-baseline gap-x-5 gap-y-1 py-2">
        <CardTitle id="overview-traffic-title" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.trafficTitle')}
        </CardTitle>
        <p className="text-[12px] leading-relaxed" style={{ color: 'var(--dash-muted)' }}>{t('overview.flowScope')}</p>
      </CardHeader>
      <div className="min-w-0 px-5 pb-2">
        <RequestFlow {...props} />
      </div>
    </Card>
  )
}
