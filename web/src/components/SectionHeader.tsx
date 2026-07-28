import { type ReactNode } from 'react'

interface SectionHeaderProps {
  title: string
  action?: ReactNode
  /** Subtle subtitle under the title (e.g. range descriptor). */
  hint?: string
}

export default function SectionHeader({ title, action, hint }: SectionHeaderProps) {
  return (
    <header className="mb-4 flex flex-col gap-3 border-b border-[var(--border)] pb-2 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        <h2 className="text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
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
