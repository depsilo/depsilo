import type { CSSProperties } from 'react'

// Bare KPI primitive — label + tabular value (+ optional delta).
// Use it inline on the page (no Card / Box wrapper); group with a parent grid.

export type MetricChangeIntent = 'neutral' | 'higher-is-better' | 'lower-is-better'

interface MetricProps {
  label: string
  value: string
  change?: number | null
  /**
   * Deltas are neutral unless the domain supplies a direction. This avoids
   * presenting higher latency as success or lower traffic as failure.
   */
  changeIntent?: MetricChangeIntent
  valueTone?: 'default' | 'ok'
  /** Override default 40 (top-row KPI) — pass 28 for "secondary" metric rows. */
  size?: CSSProperties['fontSize']
  /** Operational summaries scan left-to-right; legacy report grids remain centered. */
  align?: 'start' | 'center'
}

export default function Metric({
  label,
  value,
  change,
  changeIntent = 'neutral',
  valueTone = 'default',
  size = 40,
  align = 'center',
}: MetricProps) {
  const changeTone = typeof change !== 'number' || change === 0 || changeIntent === 'neutral'
    ? 'neutral'
    : (change > 0) === (changeIntent === 'higher-is-better')
      ? 'positive'
      : 'negative'

  return (
    <div
      className={`flex flex-col ${align === 'start' ? 'items-start text-left' : 'items-center text-center'}`}
      data-metric-label={label}
    >
      <span
        className="text-[11px] font-[600]"
        style={{ color: 'var(--text-subtle)' }}
      >
        {label}
      </span>
      <span
        data-metric-value
        className="mt-2 whitespace-nowrap font-mono tabular-nums"
        style={{
          fontSize: size,
          lineHeight: 1.05,
          fontWeight: 600,
          color: valueTone === 'ok' ? 'var(--ok-text)' : 'var(--text)',
        }}
      >
        {value}
      </span>
      {typeof change === 'number' && (
        <span
          data-metric-change
          data-change-intent={changeIntent}
          data-change-tone={changeTone}
          className="text-[11px] font-mono tabular-nums mt-1.5"
          style={{
            color: changeTone === 'positive'
              ? 'var(--ok-text)'
              : changeTone === 'negative'
                ? 'var(--danger-text)'
                : 'var(--text-soft)',
          }}
        >
          {change >= 0 ? '+' : ''}{change.toFixed(1)}%
        </span>
      )}
    </div>
  )
}
