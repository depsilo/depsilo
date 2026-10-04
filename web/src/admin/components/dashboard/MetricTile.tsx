import type { ReactNode } from 'react'

import Icon from '@/components/Icon'

export type MetricTone = 'default' | 'ok' | 'warn' | 'danger' | 'accent'

const TONE_COLOR: Record<MetricTone, string> = {
  default: 'var(--dash-ink)',
  ok: 'var(--dash-ok)',
  warn: 'var(--dash-warn)',
  danger: 'var(--dash-danger)',
  accent: 'var(--dash-accent)',
}

export interface MetricProgress {
  /** 0..1; values are clamped for the bar while the numeric value stays exact. */
  ratio: number
  label?: string
  tone?: MetricTone
}

interface MetricTileProps {
  label: string
  value: string
  unit?: string
  detail?: ReactNode
  tone?: MetricTone
  /** Real progress (memory limit / cache quota / disk). Omit when no honest denominator exists. */
  progress?: MetricProgress | null
  /** Real sampled series; omitted when nothing has been sampled yet. */
  series?: number[]
  seriesTone?: MetricTone
  loading?: boolean
  onInfo?: () => void
  infoLabel?: string
  testId?: string
}

function Sparkline({ series, tone }: { series: number[]; tone: MetricTone }) {
  if (series.length < 2) return null
  const max = Math.max(...series)
  const min = Math.min(...series)
  const span = max - min || 1
  const width = 96
  const height = 30
  const step = width / (series.length - 1)
  const points = series
    .map((value, index) => `${(index * step).toFixed(1)},${(height - ((value - min) / span) * (height - 4) - 2).toFixed(1)}`)
    .join(' ')

  return (
    <svg
      aria-hidden="true"
      viewBox={`0 0 ${width} ${height}`}
      width={width}
      height={height}
      className="shrink-0"
      preserveAspectRatio="none"
    >
      <polyline
        points={points}
        fill="none"
        stroke={TONE_COLOR[tone]}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
        opacity={0.85}
      />
    </svg>
  )
}

/**
 * Overview metric tile: label, a large tabular value with a de-emphasized unit,
 * an explicit measurement basis, and a real progress bar and/or sampled series.
 * No decorative icon block — the value is the subject.
 */
export default function MetricTile({
  label,
  value,
  unit,
  detail,
  tone = 'default',
  progress = null,
  series,
  seriesTone = 'accent',
  loading = false,
  onInfo,
  infoLabel,
  testId,
}: MetricTileProps) {
  const ratio = progress ? Math.min(1, Math.max(0, progress.ratio)) : null
  const progressTone = progress?.tone ?? tone

  return (
    <div
      data-dashboard-metric
      data-testid={testId}
      className="dash-card flex min-w-0 flex-col gap-2 p-5"
    >
      <div className="flex min-w-0 items-center justify-between gap-2">
        <p className="min-w-0 truncate text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>
          {label}
        </p>
        {onInfo && (
          <button
            type="button"
            onClick={onInfo}
            aria-label={infoLabel}
            className="dash-focus grid size-7 shrink-0 place-items-center rounded-full transition-colors duration-150 hover:bg-[var(--dash-soft)]"
            style={{ color: 'var(--dash-muted)' }}
          >
            <Icon name="info" size="sm" />
          </button>
        )}
      </div>

      <div className="flex min-w-0 items-end justify-between gap-3">
        <div className="flex min-w-0 items-baseline gap-1.5">
          {loading ? (
            <span aria-hidden="true" className="block h-9 w-24 animate-pulse rounded bg-[var(--dash-soft)]" />
          ) : (
            <>
              <span
                className="font-mono text-[32px] font-semibold leading-none tabular-nums"
                style={{ color: TONE_COLOR[tone] }}
                title={unit ? `${value} ${unit}` : value}
              >
                {value}
              </span>
              {unit && (
                <span className="text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>{unit}</span>
              )}
            </>
          )}
        </div>
        {series && series.length >= 2 && <Sparkline series={series} tone={seriesTone} />}
      </div>

      {ratio !== null && (
        <div
          className="h-1.5 w-full overflow-hidden rounded-full"
          style={{ background: 'var(--dash-soft)' }}
          aria-hidden="true"
          data-progress-label={progress?.label}
        >
          <div
            className="h-full rounded-full transition-[width] duration-300"
            style={{ width: `${ratio * 100}%`, background: TONE_COLOR[progressTone] }}
          />
        </div>
      )}

      {detail && (
        <p className="min-w-0 text-[14px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
      )}
    </div>
  )
}
