import { type ReactNode } from 'react'

type BadgeV2Variant = 'default' | 'success' | 'error' | 'warning' | 'pro' | 'ecosystem'

interface BadgeV2Props {
  variant?: BadgeV2Variant
  children: ReactNode
  className?: string
}

const variantStyles: Record<BadgeV2Variant, { bg: string; color: string; border: string }> = {
  default: { bg: 'var(--surface-container)', color: 'var(--body)', border: 'var(--border)' },
  success: { bg: 'rgba(21,190,83,0.2)', color: 'var(--success-text, #108c3d)', border: 'rgba(21,190,83,0.4)' },
  error: { bg: 'var(--error-container)', color: 'var(--error)', border: 'transparent' },
  warning: { bg: 'rgba(155,104,41,0.15)', color: 'var(--lemon)', border: 'transparent' },
  pro: { bg: 'linear-gradient(135deg, var(--stripe-purple), var(--magenta))', color: '#ffffff', border: 'transparent' },
  ecosystem: { bg: 'var(--surface-low)', color: 'var(--heading)', border: 'var(--border)' },
}

export default function BadgeV2({
  variant = 'default',
  children,
  className = '',
}: BadgeV2Props) {
  const s = variantStyles[variant]
  const isPro = variant === 'pro'

  return (
    <span
      className={`inline-flex items-center rounded-[4px] text-[10px] font-[300] px-1.5 py-[1px] ${className}`}
      style={{
        background: isPro ? undefined : s.bg,
        ...(isPro ? { backgroundImage: s.bg } : {}),
        color: s.color,
        border: s.border !== 'transparent' ? `1px solid ${s.border}` : 'none',
      }}
    >
      {children}
    </span>
  )
}
