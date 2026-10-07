import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import TooltipV2 from '@/components/Tooltip'
import type { DashboardPeriod, OriginCoverage } from '@/lib/adminApi.types'
import {
  coverageDetail,
  formatEstimatedDuration,
  formatPercentRatio,
  formatSignedPercent,
  hitRateValue,
  latencyComparison,
  periodChange,
  timeSavedMs,
} from '@/lib/dashboardOverview'
import { formatBytes } from '@/lib/utils'

import { HINT_TOOLTIP_DELAY_MS, type MetricTone } from './MetricTile'

// Only the three identity colours this card needs; the shared tile palette
// carries the rest.
const TONE: Partial<Record<MetricTone, { strong: string; soft: string }>> = {
  cpu: { strong: 'var(--dash-cpu)', soft: 'var(--dash-cpu-soft)' },
  origin: { strong: 'var(--dash-origin)', soft: 'var(--dash-origin-soft)' },
  memory: { strong: 'var(--dash-memory)', soft: 'var(--dash-memory-soft)' },
}

function Track({ children }: { children?: ReactNode }) {
  return (
    <div
      className="flex h-1.5 w-full overflow-hidden rounded-full"
      style={{ background: 'var(--dash-soft)' }}
      aria-hidden="true"
    >
      {children}
    </div>
  )
}

function Fill({ ratio, color }: { ratio: number; color: string }) {
  const percent = Math.min(1, Math.max(0, ratio)) * 100
  return <div className="h-full rounded-full transition-[width] duration-300" style={{ width: `${percent}%`, background: color }} />
}

function BenefitColumn({
  label,
  icon,
  tone,
  info,
  infoLabel,
  value,
  secondary,
  children,
  footnote,
}: {
  label: string
  icon: 'donut_large' | 'bolt' | 'speed'
  tone: MetricTone
  info: string
  infoLabel: string
  value: string
  secondary?: ReactNode
  children?: ReactNode
  footnote?: ReactNode
}) {
  const palette = TONE[tone] ?? TONE.memory!
  return (
    <div className="flex min-w-0 flex-col gap-3 px-5 py-4">
      <div className="flex min-w-0 items-center gap-3">
        <span
          aria-hidden="true"
          className="grid size-10 shrink-0 place-items-center rounded-lg"
          style={{ background: palette.soft, color: palette.strong }}
        >
          <Icon name={icon} size="md" />
        </span>
        {/* Wraps instead of truncating: the three benefit columns are narrow at
            1280 and "预计节省回源流量" clipped to "预计节省回…". */}
        <p className="min-w-0 text-[15px] font-medium leading-tight" style={{ color: 'var(--dash-muted)' }}>{label}</p>
        <TooltipV2 delay={HINT_TOOLTIP_DELAY_MS} content={<span className="block max-w-[240px] leading-[1.5]">{info}</span>}>
          <button
            type="button"
            aria-label={infoLabel}
            className="dash-focus ml-auto grid size-7 shrink-0 place-items-center rounded-full hover:bg-[var(--dash-soft)]"
            style={{ color: 'var(--dash-muted)' }}
          >
            <Icon name="info" size="sm" />
          </button>
        </TooltipV2>
      </div>

      <div className="flex min-h-9 min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1">
        <span className="font-mono text-[30px] font-semibold leading-none tabular-nums" style={{ color: 'var(--dash-ink)' }}>
          {value}
        </span>
        {secondary}
      </div>

      {children}

      {footnote && (
        <p className="text-[13px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{footnote}</p>
      )}
    </div>
  )
}

function LatencyRow({
  label,
  ms,
  max,
  color,
  samplesLabel,
}: {
  label: string
  ms: number | null
  max: number
  color: string
  samplesLabel: string
}) {
  const ratio = ms !== null && max > 0 ? ms / max : 0
  return (
    <div className="flex min-w-0 items-center gap-2 text-[13px]">
      <dt className="w-10 shrink-0 whitespace-nowrap" style={{ color: 'var(--dash-muted)' }}>{label}</dt>
      <dd className="min-w-0 flex-1">
        {ms !== null ? <Track><Fill ratio={ratio} color={color} /></Track> : <span className="block h-1.5" />}
      </dd>
      <dd className="w-16 shrink-0 text-right font-mono tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-ink)' }}>
        {ms !== null ? `${ms.toFixed(0)} ms` : '—'}
      </dd>
      <dd className="w-16 shrink-0 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-muted)' }} title={samplesLabel}>
        {samplesLabel}
      </dd>
    </div>
  )
}

interface CacheBenefitsProps {
  period?: DashboardPeriod
  prev?: DashboardPeriod
  rangeStart?: string
  coverage?: OriginCoverage
}

export default function CacheBenefits({
  period,
  prev,
  rangeStart,
  coverage,
}: CacheBenefitsProps) {
  const { t } = useTranslation()

  // ── Request hit rate ────────────────────────────────────────────────
  const hitRate = hitRateValue(period)
  const hitRateChange = periodChange(hitRate, hitRateValue(prev))
  const changeTone = hitRateChange === null
    ? 'var(--dash-muted)'
    : hitRateChange >= 0 ? 'var(--dash-ok)' : 'var(--dash-warn)'

  // ── Estimated origin traffic saved ──────────────────────────────────
  const savedBytes = period?.hit_bytes ?? 0
  const servedBytes = savedBytes + (period?.miss_bytes ?? 0)
  const cachedShare = servedBytes > 0 ? savedBytes / servedBytes : null
  const coverageNote = coverageDetail(coverage, rangeStart, t)
  const savedFootnote = cachedShare === null
    ? undefined
    : `${t('overview.savedShareOfServed', { percent: (cachedShare * 100).toFixed(1) })} · ${t('overview.estimatedShort')}${coverageNote ? ` · ${coverageNote}` : ''}`

  // ── Response performance ────────────────────────────────────────────
  const latency = latencyComparison(period)
  const maxLatency = Math.max(latency.hitMs ?? 0, latency.missMs ?? 0)
  const sampleLabel = (count: number) => t('overview.samplesShort', { count: count.toLocaleString() })
  // Same comparable-sample gate as the reduction percentage: the summed
  // latency difference stays an estimate of waiting avoided, not a measured
  // build-time saving.
  const savedTime = timeSavedMs(period)

  return (
    <section data-dashboard-benefits aria-labelledby="overview-benefits-title" className="dash-card flex min-w-0 flex-col">
      <header className="flex items-center justify-between gap-3 border-b px-5 py-4" style={{ borderColor: 'var(--dash-border)' }}>
        <h2 id="overview-benefits-title" className="text-[20px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.benefitsTitle')}
        </h2>
      </header>

      <div className="grid min-w-0 grid-cols-1 divide-y divide-[var(--dash-border)] lg:grid-cols-3 lg:divide-x lg:divide-y-0">
        <BenefitColumn
          label={t('overview.hitRateLabel')}
          icon="donut_large"
          tone="cpu"
          info={t('overview.hintHitRate')}
          infoLabel={t('overview.hitRateInfoLabel')}
          value={formatPercentRatio(hitRate)}
          secondary={hitRateChange !== null && (
            <span className="font-mono text-[13px] tabular-nums" style={{ color: changeTone }}>
              {formatSignedPercent(hitRateChange)}
            </span>
          )}
          footnote={period && period.total_requests > 0
            ? t('overview.hitRateDetail', {
              hits: period.hit_requests.toLocaleString(),
              total: period.total_requests.toLocaleString(),
            })
            : undefined}
        >
          {hitRate !== null && <Track><Fill ratio={hitRate} color="var(--dash-cpu)" /></Track>}
        </BenefitColumn>

        <BenefitColumn
          label={t('overview.savedBytesLabel')}
          icon="bolt"
          tone="origin"
          info={t('overview.hintSavedBytes')}
          infoLabel={t('overview.savedBytesInfoLabel')}
          value={cachedShare === null ? '—' : formatBytes(savedBytes)}
          footnote={savedFootnote}
        >
          {cachedShare !== null && <Track><Fill ratio={cachedShare} color="var(--dash-cpu)" /></Track>}
        </BenefitColumn>

        <BenefitColumn
          label={t('overview.latencyLabel')}
          icon="speed"
          tone="memory"
          info={t('overview.hintLatency')}
          infoLabel={t('overview.latencyInfoLabel')}
          value={latency.reductionPct !== null ? `−${latency.reductionPct.toFixed(0)}%` : '—'}
          secondary={latency.sufficient && (
            <span className="text-[13px]" style={{ color: 'var(--dash-muted)' }}>{t('overview.latencyVs')}</span>
          )}
          footnote={savedTime !== null
            ? t('overview.timeSavedEstimate', { duration: formatEstimatedDuration(savedTime, t) })
            : undefined}
        >
          <dl className="flex flex-col gap-2">
            <LatencyRow
              label={t('overview.latencyHit')}
              ms={latency.hitMs}
              max={maxLatency}
              color="var(--dash-memory)"
              samplesLabel={sampleLabel(latency.hitSamples)}
            />
            <LatencyRow
              label={t('overview.latencyMiss')}
              ms={latency.missMs}
              max={maxLatency}
              color="var(--dash-origin)"
              samplesLabel={sampleLabel(latency.missSamples)}
            />
          </dl>
        </BenefitColumn>
      </div>
    </section>
  )
}
