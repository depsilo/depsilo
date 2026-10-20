import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import Tooltip from '@/components/Tooltip'
import type { DashboardPeriod } from '@/lib/adminApi.types'
import { formatEstimatedDuration, formatPercentRatio, formatSignedPercent, hitRateValue, latencyComparison, periodChange, timeSavedMs } from '@/lib/dashboardOverview'
import { formatBytes } from '@/lib/utils'
import { HINT_TOOLTIP_DELAY_MS } from './MetricTile'

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
      hint: `${t('overview.responseComparisonBasis')} ${t('overview.latencyHit')}: ${t('overview.samples', { count: latency.hitSamples.toLocaleString() })}; ${t('overview.latencyMiss')}: ${t('overview.samples', { count: latency.missSamples.toLocaleString() })}`, hintLabel: t('overview.responseInfoLabel'),
    },
  ]

  return (
    <section data-dashboard-benefits aria-labelledby="overview-benefits-title" aria-busy={pending || undefined} className="dashboard-benefits dash-card min-w-0">
      <h2 id="overview-benefits-title" className="px-5 pt-4 text-[16px] font-semibold">{t('overview.benefitsTitle')}</h2>
      <div className="grid min-w-0 grid-cols-1 gap-5 p-5 pt-2 sm:grid-cols-3 sm:gap-6">
        {metrics.map(metric => (
          <div key={metric.id} data-benefit={metric.id} className="min-w-0">
            <div className="flex min-h-[40px] items-center justify-between gap-1">
              <p className="text-[14px] text-[var(--dash-muted)]">{metric.label}</p>
              <Tooltip delay={HINT_TOOLTIP_DELAY_MS} content={<span className="block max-w-[300px] leading-relaxed">{metric.hint}</span>}>
                <button type="button" aria-label={metric.hintLabel} className="dash-focus grid size-[40px] shrink-0 place-items-center rounded-md text-[var(--dash-muted)] hover:bg-[var(--dash-soft)]"><Icon name="info" size="sm" /></button>
              </Tooltip>
            </div>
            {pending ? <span aria-hidden className="my-1 block h-8 w-28 animate-pulse rounded bg-[var(--dash-soft)]" /> : (
              <p className={`font-mono text-[30px] font-semibold leading-tight tabular-nums ${metric.id === 'saved' && (period?.hit_bytes ?? 0) > 0 ? 'text-[var(--dash-ok)]' : 'text-[var(--dash-ink)]'}`}>{metric.value}</p>
            )}
            <p className="mt-2 text-[13px] leading-relaxed text-[var(--dash-muted)]">{pending ? t('overview.loadingStatistics') : metric.detail}</p>
            {!pending && metric.id === 'hit' && change !== null && <p className="mt-1 text-[13px] text-[var(--dash-muted)]">{t('overview.changeVsPrevious')} <span className="font-mono tabular-nums">{formatSignedPercent(change)}</span></p>}
            {!pending && metric.id === 'latency' && hasSamples && (
              <div className="mt-2 space-y-1 text-[12px] text-[var(--dash-muted)]">
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
