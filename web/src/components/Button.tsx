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

  // Primary buttons follow the Ink + Copper-Halo treatment: the button
  // itself is a near-black inkwell, white text, with a copper-tinted
  // box-shadow that reads as ambient glow rather than chrome. This keeps
  // the brand color "alive" on the page without painting buttons with
  // it — and lets every primary button feel like premium glass instead
  // of solid plastic. Tokens use the brand CSS variable's hue so a
  // future re-palette propagates automatically.
  const primaryStyle: React.CSSProperties = isPrimary
    ? {
        background: 'var(--btn-primary-bg, oklch(0.18 0.02 250))',
        color: 'var(--btn-primary-fg, white)',
        boxShadow:
          'inset 0 1px 0 color-mix(in oklab, white 13%, transparent), 0 1px 0 oklch(0 0 0 / 0.4), 0 10px 28px color-mix(in oklab, var(--brand) 32%, transparent)',
      }
    : {}

  return (
    <button
      className={`${base} ${sizes[size]} ${variants[variant]} ${className}`}
      disabled={disabled}
      style={{
        ...primaryStyle,
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
