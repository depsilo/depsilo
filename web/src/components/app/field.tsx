import type { ReactNode } from 'react'

import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

interface FieldProps {
  /** Id of the control this field labels. The message id derives from it. */
  controlId: string
  label?: string
  hint?: string
  error?: string
  children: ReactNode
  className?: string
}

/**
 * One label + control + message composition for the whole product.
 *
 * `error` and `hint` share a single message slot: an error replaces the hint
 * rather than stacking two lines under the control, and the slot is announced
 * assertively only when it reports a failure.
 */
export default function Field({ controlId, label, hint, error, children, className }: FieldProps) {
  const messageId = hint || error ? `${controlId}-description` : undefined

  if (!label && !messageId) return <>{children}</>

  return (
    <div className={cn('flex min-w-0 flex-col gap-1.5', className)}>
      {label && (
        <Label htmlFor={controlId} className="text-body font-normal text-muted-foreground">
          {label}
        </Label>
      )}
      {children}
      {messageId && (
        <p
          id={messageId}
          role={error ? 'alert' : undefined}
          className={cn('text-label', error ? 'text-destructive' : 'text-muted-foreground')}
        >
          {error || hint}
        </p>
      )}
    </div>
  )
}
