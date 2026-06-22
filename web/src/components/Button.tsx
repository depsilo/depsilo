import { type ButtonHTMLAttributes } from 'react'

interface ButtonV2Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
}

export default function ButtonV2({
  variant = 'primary',
  size = 'md',
  className = '',
  children,
  disabled,
  ...rest
}: ButtonV2Props) {
  const base = 'inline-flex items-center justify-center gap-2 font-[400] cursor-pointer transition-all duration-150 disabled:opacity-50 disabled:pointer-events-none stripe-focus-ring'

  const sizes = {
    sm: 'text-[14px] px-3 py-1.5 rounded-[4px]',
    md: 'text-[16px] px-4 py-2 rounded-[4px]',
  }

  const variants: Record<string, string> = {
    primary: 'text-white hover:brightness-95 active:scale-[0.98]',
    secondary: 'bg-transparent hover:bg-[var(--brand-soft)] active:scale-[0.98]',
    ghost: 'bg-transparent border-none hover:bg-surface-container active:scale-[0.98]',
    danger: 'bg-transparent hover:bg-error-container active:scale-[0.98]',
  }

  const isPrimary = variant === 'primary'
  const isSecondary = variant === 'secondary'
  const isDanger = variant === 'danger'

  return (
    <button
      className={`${base} ${sizes[size]} ${variants[variant]} ${className}`}
      disabled={disabled}
      style={{
        ...(isPrimary ? { background: 'var(--brand)', color: 'white' } : {}),
        ...(isSecondary ? { border: '0.5px solid var(--brand-border)', color: 'var(--brand)' } : {}),
        ...(isDanger ? { border: '1px solid var(--danger)', color: 'var(--danger)' } : {}),
        ...(!isPrimary && !isSecondary && !isDanger ? { color: 'var(--brand)' } : {}),
      }}
      {...rest}
    >
      {children}
    </button>
  )
}
