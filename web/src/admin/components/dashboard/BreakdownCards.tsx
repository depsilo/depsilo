import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import type { DashboardBreakdownResponse } from '@/lib/adminApi.types'
import { formatPercentRatio, rangeLabelKey } from '@/lib/dashboardOverview'
import { formatBytes, formatLatency } from '@/lib/utils'

import { ecosystemLabel, useNumberLocale } from './cockpitFormat'
import { CardNote, CockpitCard, EcoName, QueryBody, RankBar, ScrollTable, SeriesLegend, SplitBar } from './cockpitParts'

type BreakdownQuery = UseQueryResult<DashboardBreakdownResponse>

const PACKAGE_ROWS = 8
const ECOSYSTEM_ROWS = 6
const UPSTREAM_ROWS = 4

/** The range a breakdown payload describes, which can lag the selector while a new range loads. */
function useRangeChip(query: BreakdownQuery): string | undefined {
  const { t } = useTranslation()
  return query.data ? t(rangeLabelKey(query.data.range.key)) : undefined
}

/** The packages that moved the most bytes in the period. */
export function TopPackagesCard({ query }: { query: BreakdownQuery }) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const dayGrained = query.data?.top_packages_grain === 'utc_day'

  return (
    <CockpitCard
      id="packages"
      title={t('overview.topPackagesTitle')}
      chip={useRangeChip(query)}
      help={dayGrained ? { label: t('overview.trendMeasurementInfo'), content: <p>{t('overview.topPackagesDayGrain')}</p> } : undefined}
      busy={query.isPending}
      data-dashboard-packages=""
    >
      <QueryBody query={query} unavailable={t('overview.breakdownUnavailable')} rows={PACKAGE_ROWS}>
        {data => data.top_packages.length === 0 ? <CardNote>{t('overview.breakdownEmpty')}</CardNote> : (
          <div className="dash-card-body">
            <ScrollTable label={t('overview.topPackagesTitle')}>
            <table className="dash-table">
              <thead>
                <tr>
                  <th scope="col">{t('overview.colPackageName')}</th>
                  <th scope="col">{t('overview.colEcosystem')}</th>
                  <th scope="col">{t('overview.colRequests')}</th>
                  <th scope="col">{t('overview.colDownloaded')}</th>
                  <th scope="col">{t('overview.colCacheShare')}</th>
                </tr>
              </thead>
              <tbody>
                {data.top_packages.slice(0, PACKAGE_ROWS).map(pkg => {
                  const share = pkg.total_bytes > 0 ? pkg.hit_bytes / pkg.total_bytes : null
                  return (
                    <tr key={`${pkg.ecosystem}:${pkg.package_name}`}>
                      <td><span className="dash-pkg" title={pkg.package_name}>{pkg.package_name}</span></td>
                      <td className="is-mute">{ecosystemLabel(pkg.ecosystem)}</td>
                      <td>{pkg.requests.toLocaleString(locale)}</td>
                      <td>{formatBytes(pkg.total_bytes)}</td>
                      <td>
                        <SplitBar hit={pkg.hit_bytes} miss={pkg.total_bytes - pkg.hit_bytes} label={t('overview.cacheShareOf', { name: pkg.package_name })} />
                        {formatPercentRatio(share, 0)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            </ScrollTable>
          </div>
        )}
      </QueryBody>
    </CockpitCard>
  )
}

/** Each ecosystem's traffic, cache share, and how much slower a miss is than a hit. */
export function EcosystemsCard({ query }: { query: BreakdownQuery }) {
  const { t } = useTranslation()
  const locale = useNumberLocale()

  return (
    <CockpitCard
      id="ecosystems"
      title={t('overview.ecosystemsTitle')}
      chip={useRangeChip(query)}
      aside={<SeriesLegend hit={t('overview.legendCache')} miss={t('overview.legendUpstream')} />}
      busy={query.isPending}
      data-dashboard-ecosystems=""
    >
      <QueryBody query={query} unavailable={t('overview.breakdownUnavailable')} rows={4}>
        {data => data.by_ecosystem.length === 0 ? <CardNote>{t('overview.breakdownEmpty')}</CardNote> : (
          <div className="dash-card-body">
            <ScrollTable label={t('overview.ecosystemsTitle')}>
            <table className="dash-table">
              <thead>
                <tr>
                  <th scope="col">{t('overview.colEcosystem')}</th>
                  <th scope="col">{t('overview.colRequests')}</th>
                  <th scope="col">{t('overview.colDownloaded')}</th>
                  <th scope="col">{t('overview.colCacheShare')}</th>
                  <th scope="col">{t('overview.colHitMissLatency')}</th>
                </tr>
              </thead>
              <tbody>
                {data.by_ecosystem.slice(0, ECOSYSTEM_ROWS).map(eco => {
                  const bytes = eco.hit_bytes + eco.miss_bytes
                  return (
                    <tr key={eco.ecosystem} data-ecosystem={eco.ecosystem}>
                      <td><EcoName id={eco.ecosystem} /></td>
                      <td>{(eco.hit_requests + eco.miss_requests).toLocaleString(locale)}</td>
                      <td>{formatBytes(bytes)}</td>
                      <td>
                        <SplitBar hit={eco.hit_bytes} miss={eco.miss_bytes} label={t('overview.cacheShareOf', { name: ecosystemLabel(eco.ecosystem) })} />
                        {formatPercentRatio(bytes > 0 ? eco.hit_bytes / bytes : null, 0)}
                      </td>
                      <td className="is-mute">
                        {eco.hit_requests > 0 ? Math.round(eco.avg_hit_latency_ms).toLocaleString(locale) : '—'}
                        {' / '}
                        {eco.miss_requests > 0 ? Math.round(eco.avg_miss_latency_ms).toLocaleString(locale) : '—'}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            </ScrollTable>
            {data.by_ecosystem.length > ECOSYSTEM_ROWS && (
              <p className="dash-sub mt-1">{t('overview.moreEcosystems', { count: data.by_ecosystem.length - ECOSYSTEM_ROWS })}</p>
            )}
          </div>
        )}
      </QueryBody>
    </CockpitCard>
  )
}

/**
 * What each Upstream actually transferred to Depsilo in the period: measured
 * origin exchanges, the same quantity as the origin traffic tile. Before
 * origin metering covered the period, the figures are labelled partial or
 * withheld rather than shown as measured.
 */
export function OriginByUpstream({ query, originMeasured, originPartial }: { query: BreakdownQuery; originMeasured: boolean; originPartial: boolean }) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const chip = useRangeChip(query)

  return (
    <div data-dashboard-origin-upstreams>
      <h3 className="dash-subhead px-3">
        {t('overview.originByUpstreamTitle')}{chip && ` · ${chip}`}{originMeasured && originPartial && ` · ${t('overview.originPartialShort')}`}
      </h3>
      {!originMeasured ? <CardNote>{t('overview.originNotCollected')}</CardNote> : (
        <QueryBody query={query} unavailable={t('overview.breakdownUnavailable')} rows={3}>
          {data => {
            const rows = data.by_upstream.filter(row => row.upstream_requests > 0)
            const widest = Math.max(1, ...rows.map(row => row.upstream_bytes))
            return rows.length === 0 ? <CardNote>{t('overview.originByUpstreamEmpty')}</CardNote> : (
              <div className="dash-card-body pt-0">
                <ScrollTable label={t('overview.originByUpstreamTitle')}>
                  <table className="dash-table">
                    <thead>
                      <tr>
                        <th scope="col">{t('overview.colUpstream')}</th>
                        <th scope="col">{t('overview.colOriginBytes')}</th>
                        <th scope="col">{t('overview.colOriginRequests')}</th>
                        <th scope="col">{t('overview.colMissLatency')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {rows.slice(0, UPSTREAM_ROWS).map(row => (
                        <tr key={`${row.ecosystem}:${row.upstream}`} data-upstream={`${row.ecosystem}/${row.upstream}`}>
                          <td>{row.upstream} <span className="is-mute">{ecosystemLabel(row.ecosystem)}</span></td>
                          <td><RankBar ratio={row.upstream_bytes / widest} kind="miss" />{formatBytes(row.upstream_bytes)}</td>
                          <td>{row.upstream_requests.toLocaleString(locale)}</td>
                          <td>{row.miss_requests > 0 ? formatLatency(row.avg_miss_latency_ms) : '—'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </ScrollTable>
              </div>
            )
          }}
        </QueryBody>
      )}
    </div>
  )
}
