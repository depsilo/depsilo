import type { ButtonHTMLAttributes } from 'react'

import { Button as UiButton } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
type ButtonSize = 'sm' | 'md'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

/**
 * Depsilo's button vocabulary, mapped onto the shadcn primitive.
 *
 * `spacing` rather than `gap-*` on the icon slot keeps label+icon pairs from
 * shifting when a caller swaps the label for a spinner.
 */
const VARIANT: Record<ButtonVariant, 'default' | 'outline' | 'ghost' | 'destructive'> = {
  primary: 'default',
  secondary: 'outline',
  ghost: 'ghost',
  danger: 'destructive',
}

const SIZE: Record<ButtonSize, 'sm' | 'default'> = {
  sm: 'sm',
  md: 'default',
}

export default function Button({
  variant = 'primary',
  size = 'md',
  className,
  ...rest
}: ButtonProps) {
  return (
    <UiButton
      {...rest}
      variant={VARIANT[variant]}
      size={SIZE[size]}
      className={cn('text-[13px] font-medium', className)}
    />
  )
}
