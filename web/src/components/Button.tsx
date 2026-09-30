import { type ButtonHTMLAttributes } from 'react'
import { Button } from '@/components/ui/button'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'

interface ButtonV2Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: 'sm' | 'md'
}

// Compact admin-grade button.
// - primary: filled command green
// - secondary: bordered (paired with brand text on default, or any tone via parent style override)
// - ghost: bare, hover lift
// - danger: outlined danger
//
// Thin app-facing name for the shadcn Button variant set. Call sites keep the
// Instrument vocabulary; the shadcn component owns the class composition.
const VARIANT_MAP: Record<ButtonVariant, 'default' | 'outline' | 'ghost' | 'destructive'> = {
  primary: 'default',
  secondary: 'outline',
  ghost: 'ghost',
  danger: 'destructive',
}

export default function ButtonV2({
  variant = 'primary',
  size = 'md',
  className = '',
  children,
  disabled,
  ...rest
}: ButtonV2Props) {
  return (
    <Button
      variant={VARIANT_MAP[variant]}
      size={size === 'sm' ? 'sm' : 'default'}
      className={className}
      disabled={disabled}
      {...rest}
    >
      {children}
    </Button>
  )
}
