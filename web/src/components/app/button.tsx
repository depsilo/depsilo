import type { ButtonHTMLAttributes } from 'react'
import { Link, type LinkProps } from 'react-router'

import { Button as UiButton, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'destructive'
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
  destructive: 'destructive',
}

const SIZE: Record<ButtonSize, 'sm' | 'lg'> = {
  sm: 'sm',
  // `default` is 32px in the shadcn primitive. Depsilo's standard control is
  // 36px, which is the field height, so a button sitting beside a select or an
  // input in a filter bar lines up with it instead of sitting 4px short.
  md: 'lg',
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
      className={cn('text-body font-medium', className)}
    />
  )
}

interface LinkButtonProps extends Omit<LinkProps, 'className'> {
  variant?: ButtonVariant
  size?: ButtonSize
  className?: string
}

/**
 * A link that measures and reads exactly like a button.
 *
 * A navigation action is an anchor: it has an `href`, it can be middle-clicked
 * and copied, and it shows its target. It is not a `<button>` with an
 * `onClick`, and it is not a hand-rolled set of button classes either — the app
 * had seven of those, each with its own height and colour decision, which is
 * how a command ends up 4px shorter than the control beside it.
 */
export function LinkButton({
  variant = 'primary',
  size = 'md',
  className,
  ...rest
}: LinkButtonProps) {
  return (
    <Link
      {...rest}
      className={cn(
        buttonVariants({ variant: VARIANT[variant], size: SIZE[size] }),
        'text-body font-medium no-underline',
        className,
      )}
    />
  )
}
