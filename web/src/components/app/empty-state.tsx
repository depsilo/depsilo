import type { ReactNode } from 'react'

import Icon, { type IconName } from '@/components/app/icon'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  icon?: IconName
  title: string
  hint?: string
  action?: ReactNode
  tone?: 'neutral' | 'danger' | 'warn'
  /** Minimum box height so a section never collapses to nothing. */
  minHeight?: number
}

const TONE_CLASS = {
  neutral: 'text-muted-foreground',
  warn: 'text-warning',
  danger: 'text-destructive',
} as const

/**
 * Successful-empty state. It renders only once a response has proven the
 * collection is empty, never while a query is still pending.
 */
export default function EmptyState({
  icon = 'monitoring',
  title,
  hint,
  action,
  tone = 'neutral',
  minHeight = 160,
}: EmptyStateProps) {
  return (
    <div
      className={cn('flex flex-col items-center justify-center py-8 text-center', TONE_CLASS[tone])}
      style={{ minHeight }}
    >
      <Icon name={icon} size="lg" />
      <p className="mt-3 text-[13px] font-medium text-foreground">{title}</p>
      {hint && <p className="mt-1 max-w-[36ch] text-[12px] text-muted-foreground">{hint}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  )
}
