import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

interface SectionHeaderProps {
  title: string
  action?: ReactNode
  /** Quiet subtitle under the title, e.g. a range descriptor. */
  hint?: string
  /** Dense dashboards group sections by spacing instead of another rule. */
  divider?: boolean
}

export default function SectionHeader({ title, action, hint, divider = true }: SectionHeaderProps) {
  return (
    <header
      className={cn(
        'mb-4 flex flex-col gap-3 pb-2 sm:flex-row sm:items-end sm:justify-between',
        divider && 'border-b border-border',
      )}
    >
      <div className="min-w-0">
        <h2 className="text-body font-semibold text-foreground">{title}</h2>
        {hint && <p className="mt-1 text-meta text-muted-foreground">{hint}</p>}
      </div>
      {action && (
        <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:w-auto">{action}</div>
      )}
    </header>
  )
}
