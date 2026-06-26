import { type ButtonHTMLAttributes } from 'react'

interface ButtonV2Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  size?: 'sm' | 'md'
}

// Compact admin-grade button.
// - primary: filled brand
// - secondary: bordered (paired with brand text on default, or any tone via parent style override)
// - ghost: bare, hover lift
// - danger: outlined danger
//
// Active state uses scale(0.96) — the canonical "feels right" press value.
// Transitions list the exact properties we touch (background / color /
// border-color / transform) rather than `transition-all` which silently
// animates any future property change too.
export default function ButtonV2({
  variant = 'primary',
  size = 'md',
  className = '',
  children,
  disabled,
  ...rest
}: ButtonV2Props) {
  const base =
    'inline-flex items-center justify-center gap-1.5 font-[500] cursor-pointer transition-[background,color,border-color,filter,transform] duration-150 disabled:opacity-50 disabled:pointer-events-none active:scale-[0.96] stripe-focus-ring'

  const sizes = {
    sm: 'text-[12px] px-2.5 py-1 rounded-[5px]',
    md: 'text-[13px] px-3 py-1.5 rounded-[5px]',
  }

  const variants: Record<string, string> = {
    primary: 'text-white hover:brightness-110',
    secondary: 'hover:bg-[var(--brand-soft)]',
    ghost: 'bg-transparent hover:bg-[var(--bg-hover)]',
    danger: 'hover:bg-[var(--danger-fill)]',
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
        ...(isSecondary
          ? { border: '0.5px solid var(--brand-border)', color: 'var(--brand-text)', background: 'transparent' }
          : {}),
        ...(isDanger
          ? { border: '0.5px solid var(--danger-border)', color: 'var(--danger-text)', background: 'transparent' }
          : {}),
        ...(!isPrimary && !isSecondary && !isDanger
          ? { color: 'var(--text-soft)' }
          : {}),
      }}
      {...rest}
    >
      {children}
    </button>
  )
}
