import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type NoticeTone = 'success' | 'warning' | 'danger' | 'info'

interface NoticeProps {
  tone: NoticeTone
  title?: string
  children: ReactNode
  className?: string
}

const TONE_CLASS: Record<NoticeTone, string> = {
  success: 'border-success/35 bg-success/10 text-success',
  warning: 'border-warning/35 bg-warning/10 text-warning',
  danger: 'border-destructive/35 bg-destructive/10 text-destructive',
  info: 'border-primary/35 bg-primary/10 text-primary',
}

/**
 * Inline state message. Warning and danger stay distinct: a degraded or
 * partially completed operation is not the same result as a failure.
 */
export default function Notice({ tone, title, children, className }: NoticeProps) {
  return (
    <div
      role={tone === 'danger' ? 'alert' : undefined}
      className={cn('rounded-md border px-3 py-2.5 text-[13px] leading-5', TONE_CLASS[tone], className)}
    >
      {title && <p className="mb-1 font-semibold">{title}</p>}
      <div>{children}</div>
    </div>
  )
}
