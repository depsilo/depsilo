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
  /**
   * `stacked` puts the label above the control — the default, and the right
   * answer for a dialog or a two-column form. `row` puts it beside the control
   * for settings lists, where the labels form a scannable column of their own.
   *
   * This lives here rather than in the surface that wants it because Field
   * owns the label. A page that repositions the label with a descendant
   * selector is styling another component's internals, and breaks silently
   * when those internals change.
   */
  layout?: 'stacked' | 'row'
}

/**
 * One label + control + message composition for the whole product.
 *
 * `error` and `hint` share a single message slot: an error replaces the hint
 * rather than stacking two lines under the control, and the slot is announced
 * assertively only when it reports a failure.
 */
export default function Field({
  controlId,
  label,
  hint,
  error,
  children,
  className,
  layout = 'stacked',
}: FieldProps) {
  const messageId = hint || error ? `${controlId}-description` : undefined
  const row = layout === 'row'

  if (!label && !messageId) return <>{children}</>

  return (
    <div
      className={cn(
        'min-w-0',
        row
          ? 'flex flex-col gap-1.5 sm:grid sm:grid-cols-[160px_minmax(0,1fr)] sm:items-start sm:gap-x-6 sm:gap-y-0'
          : 'flex flex-col gap-1.5',
        className,
      )}
    >
      {label && (
        <Label
          htmlFor={controlId}
          className={cn('text-label font-normal text-muted-foreground', row && 'sm:pt-2.5')}
        >
          {label}
        </Label>
      )}
      <div className={cn('flex min-w-0 flex-col gap-1.5', row && 'sm:col-start-2')}>
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
    </div>
  )
}
