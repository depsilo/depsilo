import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { getAdminRouteHref } from '@/admin/routes'
import type { AdminUpstream, DashboardBreakdownResponse } from '@/lib/adminApi.types'
import type { ServiceStatusModel } from '@/lib/dashboardOverview'
import { groupUpstreamFleet } from '@/lib/overviewCockpit'

import { OriginByUpstream } from './BreakdownCards'
import { ecosystemLabel, useNumberLocale } from './cockpitFormat'
import { CardNote, CockpitCard, EcoName, MoreLink } from './cockpitParts'

const ECOSYSTEM_ORDER = operatorEcosystems.map(ecosystem => ecosystem.id)
const STATES = ['healthy', 'slow', 'unavailable'] as const

interface UpstreamsCardProps {
  status: ServiceStatusModel
  /** Whether an Upstream health payload has ever loaded. */
  available: boolean
  stale: boolean
  pending: boolean
  upstreamList?: AdminUpstream[]
  breakdown: UseQueryResult<DashboardBreakdownResponse>
  originMeasured: boolean
  originPartial: boolean
}

/**
 * Every Upstream at a glance, grouped by ecosystem, and where the period's
 * cache misses actually went. Health here is the snapshot behind the service
 * status, so the card and the status never disagree.
 */
export default function UpstreamsCard({ status, available, stale, pending, upstreamList, breakdown, originMeasured, originPartial }: UpstreamsCardProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const fleet = groupUpstreamFleet(status.upstreams, ECOSYSTEM_ORDER, upstreamList)
  const total = status.upstreams.length
  const usable = status.upstreams.filter(upstream => upstream.state !== 'unavailable').length

  return (
    <CockpitCard
      id="upstreams"
      title={t('overview.upstreamHealthTitle')}
      chip={available && total > 0 ? t('overview.usableCount', { usable, total }) : undefined}
      help={{ label: t('overview.trendMeasurementInfo'), content: <p>{t('overview.upstreamObservationBasis')}</p> }}
      aside={<MoreLink to={getAdminRouteHref('upstreams')}>{t('overview.linkUpstreams')}</MoreLink>}
      busy={pending}
      data-dashboard-upstreams=""
    >
      {stale && <CardNote tone="warning" role="status">{t('overview.upstreamStatusStale')}</CardNote>}
      {!available ? (
        <CardNote>{t(pending ? 'overview.checkingHealth' : 'overview.upstreamStatusUnavailable')}</CardNote>
      ) : fleet.length === 0 ? (
        <CardNote>{t('overview.fleetEmpty')}</CardNote>
      ) : (
        <>
          <ul className="dash-fleet" data-upstream-fleet>
            {fleet.map(group => {
              const down = group.upstreams.filter(upstream => upstream.state === 'unavailable').length
              const slow = group.upstreams.filter(upstream => upstream.state === 'slow').length
              return (
                <li key={group.ecosystem} data-ecosystem={group.ecosystem}>
                  <EcoName id={group.ecosystem} />
                  <span className="dash-fleet-marks" aria-hidden>
                    {group.upstreams.map(upstream => (
                      <i
                        key={upstream.key}
                        className="dash-mark-state"
                        data-state={upstream.state}
                        title={`${upstream.name} · ${t(`overview.upstreamState.${upstream.state}`)} · ${Math.round(upstream.latencyMs).toLocaleString(locale)} ms`}
                      />
                    ))}
                  </span>
                  <span className="is-num" aria-hidden>{group.primaryLatencyMs === null ? '—' : Math.round(group.primaryLatencyMs).toLocaleString(locale)}</span>
                  <span className="sr-only">
                    {t('overview.fleetSummary', { ecosystem: ecosystemLabel(group.ecosystem), total: group.upstreams.length, unavailable: down, slow })}
                  </span>
                </li>
              )
            })}
          </ul>
          <p className="dash-fleet-key" aria-hidden>
            {STATES.map(state => <span key={state}><i className="dash-mark-state" data-state={state} />{t(`overview.upstreamState.${state}`)}</span>)}
            <span>{t('overview.fleetLatencyNote')}</span>
          </p>
        </>
      )}
      <OriginByUpstream query={breakdown} originMeasured={originMeasured} originPartial={originPartial} />
    </CockpitCard>
  )
}
