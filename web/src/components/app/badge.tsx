import type { ReactNode } from 'react'

import { Badge as UiBadge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

export type BadgeTone =
  | 'default'
  | 'neutral'
  | 'success'
  | 'destructive'
  | 'warning'
  | 'info'
  | 'pro'
  | 'ecosystem'

interface BadgeProps {
  variant?: BadgeTone
  children: ReactNode
  className?: string
}

/**
 * Tints carry product meaning rather than decoration: success/warning/destructive map
 * onto cache, delivery, and policy outcomes, and `pro` marks an entitlement.
 * They live here instead of `components/ui` for that reason.
 */
const TONE_CLASS: Record<BadgeTone, string> = {
  default: 'border-transparent bg-accent text-accent-foreground',
  neutral: 'border-border bg-muted text-muted-foreground',
  success: 'border-transparent bg-success/10 text-success',
  warning: 'border-transparent bg-warning/10 text-warning',
  destructive: 'border-transparent bg-destructive/10 text-destructive',
  info: 'border-transparent bg-info/10 text-info',
  pro: 'border-primary/40 text-primary',
  ecosystem: 'border-transparent bg-accent text-accent-foreground',
}

export default function Badge({ variant = 'default', children, className }: BadgeProps) {
  return (
    <UiBadge
      variant="outline"
      className={cn('text-meta font-semibold tracking-normal', TONE_CLASS[variant], className)}
    >
      {children}
    </UiBadge>
  )
}
