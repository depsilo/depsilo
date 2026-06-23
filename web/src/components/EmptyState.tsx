import { type ReactNode } from 'react'
import Icon from './Icon'

// Friendly empty state used inline on the page (no Card wrapper).
// Icon + title + optional hint; optional CTA underneath.

interface EmptyStateProps {
  icon?: string
  title: string
  hint?: string
  action?: ReactNode
  /** Minimum vertical box so the section doesn't collapse to nothing. */
  minHeight?: number
}

export default function EmptyState({
  icon = 'monitoring',
  title,
  hint,
  action,
  minHeight = 160,
}: EmptyStateProps) {
  return (
    <div
      className="flex flex-col items-center justify-center text-center py-8"
      style={{ minHeight, color: 'var(--text-soft)' }}
    >
      <Icon name={icon} size="lg" />
      <p className="text-[13px] font-[500] mt-3" style={{ color: 'var(--text-muted)' }}>
        {title}
      </p>
      {hint && (
        <p className="text-[12px] mt-1 max-w-[36ch]" style={{ color: 'var(--text-soft)' }}>
          {hint}
        </p>
      )}
      {action && <div className="mt-3">{action}</div>}
    </div>
  )
}
