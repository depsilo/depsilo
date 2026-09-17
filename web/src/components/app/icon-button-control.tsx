import { LoaderCircle } from 'lucide-react'
import { forwardRef, type ButtonHTMLAttributes } from 'react'

import { Button as UiButton } from '@/components/ui/button'
import Icon from '@/components/app/icon'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface IconButtonControlProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: LucideIcon
  label: string
  tone?: 'neutral' | 'danger'
  loading?: boolean
}

/**
 * Bare icon action: no floating tooltip. The target stays 40x40 in every
 * state — including pending — so a slow request cannot shrink the hit area,
 * and `data-icon-button` is what the accessibility contract measures.
 */
export const ICON_BUTTON_SIZE_CLASS = 'size-10'

export default forwardRef<HTMLButtonElement, IconButtonControlProps>(function IconButtonControl(
  { icon, label, tone = 'neutral', loading = false, disabled, className, ...rest },
  ref,
) {
  return (
    <UiButton
      {...rest}
      ref={ref}
      type={rest.type ?? 'button'}
      data-icon-button
      variant="ghost"
      size="icon"
      aria-label={label}
      aria-busy={loading || undefined}
      disabled={disabled || loading}
      className={cn(
        ICON_BUTTON_SIZE_CLASS,
        tone === 'danger' ? 'text-destructive hover:text-destructive' : 'text-muted-foreground',
        className,
      )}
    >
      <Icon
        icon={loading ? LoaderCircle : icon}
        size="sm"
        className={loading ? 'animate-spin motion-reduce:animate-none' : ''}
      />
    </UiButton>
  )
})
