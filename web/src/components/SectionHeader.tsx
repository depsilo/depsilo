import { type ReactNode } from 'react'

// Section divider for the no-card layout: confident heading + thin border-bottom
// + optional right-aligned action (link, filter chips, etc.).

interface SectionHeaderProps {
  title: string
  action?: ReactNode
  /** Subtle subtitle under the title (e.g. range descriptor). */
  hint?: string
}

export default function SectionHeader({ title, action, hint }: SectionHeaderProps) {
  return (
    <header
      className="flex items-baseline justify-between pb-2 mb-4"
      style={{ borderBottom: '1px solid var(--border)' }}
    >
      <div className="flex items-baseline gap-3 min-w-0">
        <h2
          className="text-[13px] font-[600] tracking-[-0.005em] shrink-0"
          style={{ color: 'var(--text)' }}
        >
          {title}
        </h2>
        {hint && (
          <span className="text-[11px] truncate" style={{ color: 'var(--text-soft)' }}>
            {hint}
          </span>
        )}
      </div>
      {action}
    </header>
  )
}
