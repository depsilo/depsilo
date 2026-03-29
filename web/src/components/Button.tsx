import { type ButtonHTMLAttributes } from 'react'

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost'
}

const variantStyles: Record<NonNullable<ButtonProps['variant']>, string> = {
  primary:
    'text-white font-medium hover:brightness-110 active:scale-[0.98]',
  secondary:
    'bg-transparent border border-outline-variant/15 text-on-surface hover:bg-surface-container',
  ghost:
    'bg-transparent text-primary-dim hover:bg-surface-container border-none',
}

export default function Button({
  variant = 'primary',
  className = '',
  children,
  disabled,
  ...rest
}: ButtonProps) {
  const base =
    'inline-flex items-center justify-center gap-2 rounded-[0.25rem] px-4 py-2 text-sm cursor-pointer transition-all disabled:opacity-50 disabled:pointer-events-none'

  const isPrimary = variant === 'primary'

  return (
    <button
      className={`${base} ${variantStyles[variant]} ${className}`}
      disabled={disabled}
      style={
        isPrimary
          ? {
              background:
                'linear-gradient(135deg, var(--primary), var(--primary-container))',
            }
          : undefined
      }
      {...rest}
    >
      {children}
    </button>
  )
}
