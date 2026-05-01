import type { ReactNode } from 'react'

interface MetricCardV2Props {
  label: string
  value: string
  icon?: ReactNode
  change?: number | null  // percentage change vs yesterday
  sparkline?: ReactNode
}

export default function MetricCardV2({ label, value, icon, change, sparkline }: MetricCardV2Props) {
  return (
    <div
      className="rounded-[5px] p-5"
      style={{
        position: 'relative',
        background: 'var(--bg-card)',
        border: '1px solid var(--border)',
      }}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="eyebrow">
          {label}
        </span>
        {icon && <span style={{ color: 'var(--text-subtle)' }}>{icon}</span>}
      </div>
      <div className="flex items-center">
        <p
          className="tabular-nums"
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 32,
            fontWeight: 600,
            letterSpacing: '-0.04em',
            color: 'var(--text)',
            lineHeight: 1.1,
          }}
        >
          {value}
        </p>
        {typeof change === 'number' && (
          <span
            className="tabular-nums ml-2"
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              color: change >= 0 ? 'var(--ok-text)' : 'var(--danger-text)',
            }}
          >
            {change >= 0 ? '+' : ''}{change.toFixed(1)}%
          </span>
        )}
      </div>
      {sparkline && (
        <div style={{ position: 'absolute', bottom: 0, right: 0, opacity: 0.7 }}>
          {sparkline}
        </div>
      )}
    </div>
  )
}
