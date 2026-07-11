// Bare KPI primitive — centered label + tabular value (+ optional delta).
// Use it inline on the page (no Card / Box wrapper); group with a parent grid.

interface MetricProps {
  label: string
  value: string
  change?: number | null
  valueTone?: 'default' | 'ok'
  /** Override default 40 (top-row KPI) — pass 28 for "secondary" metric rows. */
  size?: number
}

export default function Metric({
  label,
  value,
  change,
  valueTone = 'default',
  size = 40,
}: MetricProps) {
  return (
    <div className="flex flex-col items-center text-center">
      <span
        className="text-[10px] font-mono font-[600] uppercase"
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
          className="text-[11px] font-mono tabular-nums mt-1.5"
          style={{ color: change >= 0 ? 'var(--ok-text)' : 'var(--danger-text)' }}
        >
          {change >= 0 ? '+' : ''}{change.toFixed(1)}%
        </span>
      )}
    </div>
  )
}
