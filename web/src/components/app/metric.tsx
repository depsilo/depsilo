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
  /**
   * A KPI value is one of the two metric tokens, not a caller-supplied
   * font size. Callers used to pass a `clamp(...)`, which meant the largest
   * number on a page was the one piece of type outside the scale.
   */
  size?: 'default' | 'sm'
  /** Operational summaries scan left-to-right; report grids stay centred. */
  align?: 'start' | 'center'
  className?: string
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
  size = 'default',
  align = 'center',
  className,
}: MetricProps) {
  const changeTone = typeof change !== 'number' || change === 0 || changeIntent === 'neutral'
    ? 'neutral'
    : (change > 0) === (changeIntent === 'higher-is-better')
      ? 'positive'
      : 'negative'

  return (
    <div
      className={cn(
        'flex flex-col',
        align === 'start' ? 'items-start text-left' : 'items-center text-center',
        className,
      )}
      data-metric-label={label}
    >
      <span className="text-meta font-semibold text-muted-foreground">{label}</span>
      <span
        data-metric-value
        className={cn(
          'mt-2 font-mono font-semibold tabular-nums',
          size === 'sm' ? 'text-metric-sm' : 'text-metric-sm lg:text-metric',
          valueTone === 'success' ? 'text-success' : 'text-foreground',
        )}
      >
        {value}
      </span>
      {typeof change === 'number' && (
        <span
          data-metric-change
          data-change-intent={changeIntent}
          data-change-tone={changeTone}
          className={cn(
            'mt-1.5 font-mono text-meta tabular-nums',
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
