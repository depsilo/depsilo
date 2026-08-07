import { type ReactNode } from 'react'

interface SectionHeaderProps {
  title: string
  action?: ReactNode
  /** Subtle subtitle under the title (e.g. range descriptor). */
  hint?: string
  /** Keep dense dashboard sections grouped by spacing instead of another rule. */
  divider?: boolean
}

export default function SectionHeader({ title, action, hint, divider = true }: SectionHeaderProps) {
  return (
    <header className={`mb-4 flex flex-col gap-3 pb-2 sm:flex-row sm:items-end sm:justify-between ${divider ? 'border-b border-[var(--border)]' : ''}`}>
      <div className="min-w-0">
        <h2 className="text-[14px] font-[600]" style={{ color: 'var(--text)' }}>
          {title}
        </h2>
        {hint && (
          <p className="mt-1 text-[11px]" style={{ color: 'var(--text-soft)' }}>
            {hint}
          </p>
        )}
      </div>
      {action && <div className="flex min-w-0 w-full flex-wrap items-center gap-2 sm:w-auto">{action}</div>}
    </header>
  )
}
