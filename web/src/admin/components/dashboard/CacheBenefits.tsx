import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import type { DashboardPeriod, DashboardRange, OriginCoverage } from '@/lib/adminApi.types'
import {
  coverageDetail,
  formatPercentRatio,
  formatSignedPercent,
  hitRateValue,
  latencyComparison,
  periodChange,
  rangeLabelKey,
} from '@/lib/dashboardOverview'
import { formatBytes } from '@/lib/utils'

import type { MetricTone } from './MetricTile'

export type BenefitInfoKind = 'hit-rate' | 'saved-bytes' | 'latency'

function BenefitBlock({
  label,
  value,
  unit,
  detail,
  change,
  changeIntent,
  icon,
  iconTone,
  onInfo,
  infoLabel,
  children,
}: {
  label: string
  value: string
  unit?: string
  detail: ReactNode
  change?: number | null
  changeIntent?: 'higher-is-better' | 'neutral'
  icon: 'donut_large' | 'bolt' | 'speed'
  iconTone: MetricTone
  onInfo: () => void
  infoLabel: string
  children?: ReactNode
}) {
  const changeTone = change === null || change === undefined || changeIntent === 'neutral'
    ? 'var(--dash-muted)'
    : (change >= 0 ? 'var(--dash-ok)' : 'var(--dash-warn)')
  return (
    <div className="flex min-w-0 flex-col gap-2 px-5 py-4">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <span
            aria-hidden="true"
            className="grid size-8 shrink-0 place-items-center rounded-lg"
            style={{ background: `var(--dash-${iconTone}-soft, var(--dash-soft))`, color: `var(--dash-${iconTone}, var(--dash-ink))` }}
          >
            <Icon name={icon} size="sm" />
          </span>
          <p className="min-w-0 truncate text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>{label}</p>
        </div>
        <button
          type="button"
          onClick={onInfo}
          aria-label={infoLabel}
          className="dash-focus grid size-7 shrink-0 place-items-center rounded-full hover:bg-[var(--dash-soft)]"
          style={{ color: 'var(--dash-muted)' }}
        >
          <Icon name="info" size="sm" />
        </button>
      </div>
      <div className="flex min-w-0 flex-wrap items-baseline gap-1.5">
        <span className="font-mono text-[30px] font-semibold leading-none tabular-nums" style={{ color: 'var(--dash-ink)' }}>{value}</span>
        {unit && <span className="text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>{unit}</span>}
        {change !== null && change !== undefined && (
          <span className="ml-1 font-mono text-[13px] tabular-nums" style={{ color: changeTone }}>
            {formatSignedPercent(change)}
          </span>
        )}
      </div>
      {children}
      <p className="text-[14px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
    </div>
  )
}

interface CacheBenefitsProps {
  period?: DashboardPeriod
  prev?: DashboardPeriod
  range: DashboardRange
  rangeStart?: string
  coverage?: OriginCoverage
  onInfo: (kind: BenefitInfoKind) => void
}

export default function CacheBenefits({
  period,
  prev,
  range,
  rangeStart,
  coverage,
  onInfo,
}: CacheBenefitsProps) {
  const { t } = useTranslation()
  const periodLabel = t(rangeLabelKey(range))

  const hitRate = hitRateValue(period)
  const prevHitRate = hitRateValue(prev)
  const hitRateChange = periodChange(hitRate, prevHitRate)
  const hitDetail = period && period.total_requests > 0
    ? t('overview.hitRateDetail', {
      hits: period.hit_requests.toLocaleString(),
      total: period.total_requests.toLocaleString(),
    })
    : t('overview.hitRateNoSample')

  const saved = period?.hit_bytes ?? 0
  const coverageNote = coverageDetail(coverage, rangeStart, t)

  const latency = latencyComparison(period)
  const latencyValue = latency.reductionPct !== null ? `${latency.reductionPct.toFixed(0)}%` : '—'
  const latencyDetail = latency.sufficient
    ? t('overview.latencyReductionDetail', {
      hit: latency.hitMs?.toFixed(0) ?? '—',
      miss: latency.missMs?.toFixed(0) ?? '—',
    })
    : t('overview.insufficientSamples')

  return (
    <section data-dashboard-benefits aria-labelledby="overview-benefits-title" className="dash-card flex min-w-0 flex-col">
      <header className="flex items-center justify-between gap-3 border-b px-5 py-4" style={{ borderColor: 'var(--dash-border)' }}>
        <h2 id="overview-benefits-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.benefitsTitle')}
        </h2>
        <span className="text-[13px]" style={{ color: 'var(--dash-muted)' }}>{periodLabel}</span>
      </header>
      <div className="grid min-w-0 grid-cols-1 divide-y divide-[var(--dash-border)] lg:grid-cols-3 lg:divide-x lg:divide-y-0">
        <BenefitBlock
          label={t('overview.hitRateLabel')}
          icon="donut_large"
          iconTone="cpu"
          value={formatPercentRatio(hitRate)}
          detail={hitDetail}
          change={hitRateChange}
          changeIntent="higher-is-better"
          onInfo={() => onInfo('hit-rate')}
          infoLabel={t('overview.hitRateInfoLabel')}
        />
        <BenefitBlock
          label={t('overview.savedBytesLabel')}
          icon="bolt"
          iconTone="origin"
          value={formatBytes(saved)}
          detail={`${t('overview.estimatedFromHits')}${coverageNote ? ` · ${coverageNote}` : ''}`}
          onInfo={() => onInfo('saved-bytes')}
          infoLabel={t('overview.savedBytesInfoLabel')}
        />
        <BenefitBlock
          label={t('overview.latencyLabel')}
          icon="speed"
          iconTone="memory"
          value={latencyValue}
          detail={latencyDetail}
          onInfo={() => onInfo('latency')}
          infoLabel={t('overview.latencyInfoLabel')}
        >
          <dl className="flex flex-wrap gap-x-5 gap-y-1 text-[13px]">
            <div className="flex items-baseline gap-1.5">
              <dt style={{ color: 'var(--dash-muted)' }}>{t('overview.latencyHit')}</dt>
              <dd className="font-mono tabular-nums" style={{ color: 'var(--dash-ink)' }}>
                {latency.hitMs !== null ? `${latency.hitMs.toFixed(0)} ms` : '—'}
                <span className="ml-1 text-[12px]" style={{ color: 'var(--dash-muted)' }}>
                  ({t('overview.samples', { count: latency.hitSamples })})
                </span>
              </dd>
            </div>
            <div className="flex items-baseline gap-1.5">
              <dt style={{ color: 'var(--dash-muted)' }}>{t('overview.latencyMiss')}</dt>
              <dd className="font-mono tabular-nums" style={{ color: 'var(--dash-ink)' }}>
                {latency.missMs !== null ? `${latency.missMs.toFixed(0)} ms` : '—'}
                <span className="ml-1 text-[12px]" style={{ color: 'var(--dash-muted)' }}>
                  ({t('overview.samples', { count: latency.missSamples })})
                </span>
              </dd>
            </div>
          </dl>
        </BenefitBlock>
      </div>
    </section>
  )
}
