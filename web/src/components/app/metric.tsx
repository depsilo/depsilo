import type { CSSProperties } from 'react'

import { cn } from '@/lib/utils'

export type MetricChangeIntent = 'neutral' | 'higher-is-better' | 'lower-is-better'

interface MetricProps {
  label: string
  value: string
  change?: number | null
  /**
   * Deltas stay neutral unless the domain supplies a direction, so higher
   * latency is never presented as success or lower traffic as failure.
   */
  changeIntent?: MetricChangeIntent
  valueTone?: 'default' | 'success'
  /** Override the default 40px KPI size; pass 28 for secondary metric rows. */
  size?: CSSProperties['fontSize']
  /** Operational summaries scan left-to-right; report grids stay centred. */
  align?: 'start' | 'center'
}

/**
 * Bare KPI: label + tabular value + optional delta. It is deliberately not a
 * card — callers group metrics with a parent grid or data rail.
 */
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
      className={cn('flex flex-col', align === 'start' ? 'items-start text-left' : 'items-center text-center')}
      data-metric-label={label}
    >
      <span className="text-[11px] font-semibold text-muted-foreground">{label}</span>
      <span
        data-metric-value
        className={cn(
          'mt-2 font-mono tabular-nums leading-none font-semibold',
          valueTone === 'success' ? 'text-success' : 'text-foreground',
        )}
        style={{ fontSize: size }}
      >
        {value}
      </span>
      {typeof change === 'number' && (
        <span
          data-metric-change
          data-change-intent={changeIntent}
          data-change-tone={changeTone}
          className={cn(
            'mt-1.5 font-mono text-[11px] tabular-nums',
            changeTone === 'positive' && 'text-success',
            changeTone === 'negative' && 'text-destructive',
            changeTone === 'neutral' && 'text-muted-foreground',
          )}
        >
          {change >= 0 ? '+' : ''}{change.toFixed(1)}%
        </span>
      )}
    </div>
  )
}
