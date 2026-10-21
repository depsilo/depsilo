import { Skeleton } from '@/components/ui/skeleton'
import { useTranslation } from 'react-i18next'

import type { DashboardPeriod } from '@/lib/adminApi.types'
import { formatEstimatedDuration, formatPercentRatio, formatSignedPercent, hitRateValue, latencyComparison, periodChange, timeSavedMs } from '@/lib/dashboardOverview'
import { formatBytes } from '@/lib/utils'
import DashboardMetricHelp from './DashboardMetricHelp'

interface CacheBenefitsProps {
  period?: DashboardPeriod
  prev?: DashboardPeriod
  pending?: boolean
}

/** Cumulative savings and request averages keep separate sample semantics. */
export default function CacheBenefits({ period, prev, pending = false }: CacheBenefitsProps) {
  const { t } = useTranslation()
  const hitRate = hitRateValue(period)
  const change = periodChange(hitRate, hitRateValue(prev))
  const latency = latencyComparison(period)
  const savedTime = timeSavedMs(period)
  const hasSamples = Boolean(period && period.total_requests > 0)
  const metrics = [
    {
      id: 'saved', label: t('overview.savedBytesLabel'),
      value: period ? formatBytes(period.hit_bytes) : '—',
      detail: t('overview.estimatedFromHits'),
      hint: t('overview.hintSavedBytes'), hintLabel: t('overview.savedBytesInfoLabel'),
    },
    {
      id: 'hit', label: t('overview.hitRateLabel'), value: formatPercentRatio(hitRate),
      detail: hasSamples && period
        ? t('overview.hitRateDetail', { hits: period.hit_requests.toLocaleString(), total: period.total_requests.toLocaleString() })
        : t('overview.noSamples'),
      hint: t('overview.hintHitRate'), hintLabel: t('overview.hitRateInfoLabel'),
    },
    {
      id: 'latency', label: t('overview.averageResponse'),
      value: hasSamples && period ? `${period.avg_latency_ms.toFixed(0)} ms` : '—',
      detail: hasSamples ? t('overview.responseBasis') : t('overview.noSamples'),
      hint: t('overview.responseComparisonBasis'), hintLabel: t('overview.responseInfoLabel'),
    },
  ]

  return (
    <section data-dashboard-benefits aria-labelledby="overview-benefits-title" aria-busy={pending || undefined} className="dashboard-benefits min-w-0">
      <h2 id="overview-benefits-title" className="px-5 pt-3 text-[16px] font-semibold">{t('overview.benefitsTitle')}</h2>
      <div className="grid min-w-0 grid-cols-1 gap-4 px-5 pb-3 sm:grid-cols-3 sm:gap-6">
        {metrics.map(metric => (
          <div key={metric.id} data-benefit={metric.id} className="min-w-0">
            <div className="flex min-h-[40px] items-center text-[14px] text-[var(--dash-muted)]">
              <DashboardMetricHelp label={metric.label} ariaLabel={metric.hintLabel}>
                <p>{metric.hint}</p>
                {metric.id === 'latency' && (pending ? <p>{t('overview.loadingStatistics')}</p> : (
                  <dl className="dashboard-help-samples">
                    <div><dt>{t('overview.latencyHit')}</dt><dd>{latency.hitSamples > 0 ? t('overview.samples', { count: latency.hitSamples.toLocaleString() }) : t('overview.noSamples')}</dd></div>
                    <div><dt>{t('overview.latencyMiss')}</dt><dd>{latency.missSamples > 0 ? t('overview.samples', { count: latency.missSamples.toLocaleString() }) : t('overview.noSamples')}</dd></div>
                  </dl>
                ))}
              </DashboardMetricHelp>
            </div>
            {pending ? <Skeleton render={<span />} className="my-1 block h-8 w-28" /> : (
              <p className={`font-mono text-[30px] font-semibold leading-tight tabular-nums ${metric.id === 'saved' && (period?.hit_bytes ?? 0) > 0 ? 'text-[var(--dash-ok)]' : 'text-[var(--dash-ink)]'}`}>{metric.value}</p>
            )}
            <p className="mt-1 text-[13px] leading-relaxed text-[var(--dash-muted)]">{pending ? t('overview.loadingStatistics') : metric.detail}</p>
            {!pending && metric.id === 'hit' && change !== null && <p className="mt-1 text-[13px] text-[var(--dash-muted)]">{t('overview.changeVsPrevious')} <span className="font-mono tabular-nums">{formatSignedPercent(change)}</span></p>}
            {!pending && metric.id === 'latency' && hasSamples && (
              <div className="mt-1 space-y-1 text-[12px] text-[var(--dash-muted)]">
                <p>{t('overview.latencyHit')} <span className="font-mono tabular-nums text-[var(--dash-ok)]">{latency.hitMs === null ? '—' : `${latency.hitMs.toFixed(0)} ms`}</span> · {t('overview.latencyMiss')} <span className="font-mono tabular-nums">{latency.missMs === null ? '—' : `${latency.missMs.toFixed(0)} ms`}</span></p>
                {savedTime !== null && <p>{t('overview.timeSavedEstimate', { duration: formatEstimatedDuration(savedTime, t) })}</p>}
              </div>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}
