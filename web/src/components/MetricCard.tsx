interface MetricCardV2Props {
  label: string
  value: string
  icon?: React.ReactNode
  change?: number | null  // percentage change vs yesterday
}

export default function MetricCardV2({ label, value, icon, change }: MetricCardV2Props) {
  return (
    <div
      className="rounded-[5px] p-5"
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        boxShadow: 'var(--shadow-soft)',
      }}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="text-[13px] font-[400] uppercase tracking-wider" style={{ color: 'var(--body)' }}>
          {label}
        </span>
        {icon && <span style={{ color: 'var(--body)' }}>{icon}</span>}
      </div>
      <div className="flex items-center">
        <p className="text-[26px] font-[300] tracking-tight font-mono tabular-nums" style={{ color: 'var(--heading)' }}>
          {value}
        </p>
        {typeof change === 'number' && (
          <span className="text-[11px] font-mono tabular-nums ml-2" style={{ color: change >= 0 ? '#3bd671' : 'var(--error)' }}>
            {change >= 0 ? '+' : ''}{change.toFixed(1)}%
          </span>
        )}
      </div>
    </div>
  )
}
