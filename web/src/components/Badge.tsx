import { type ReactNode } from 'react'

type BadgeV2Variant = 'default' | 'success' | 'error' | 'warning' | 'pro' | 'ecosystem'

interface BadgeV2Props {
  variant?: BadgeV2Variant
  children: ReactNode
  className?: string
}

const variantStyles: Record<BadgeV2Variant, { bg: string; color: string; border: string }> = {
  default: { bg: 'var(--brand-soft)', color: 'var(--brand-text)', border: 'var(--brand-border)' },
  success: { bg: 'var(--ok-fill)', color: 'var(--ok-text)', border: 'var(--ok-border)' },
  error: { bg: 'var(--danger-fill)', color: 'var(--danger-text)', border: 'var(--danger-border)' },
  warning: { bg: 'var(--warn-fill)', color: 'var(--warn-text)', border: 'var(--warn-border)' },
  pro: { bg: 'var(--grad-aurora)', color: 'transparent', border: 'var(--brand-border)' },
  ecosystem: { bg: 'var(--brand-soft)', color: 'var(--brand-text)', border: 'var(--brand-border)' },
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
      className={`inline-flex items-center px-[7px] py-[2px] ${className}`}
      style={{
        background: s.bg,
        color: s.color,
        border: `0.5px solid ${s.border}`,
        borderRadius: 'var(--r-tag)',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '1.4',
        ...(isPro ? {
          WebkitBackgroundClip: 'text',
          WebkitTextFillColor: 'transparent',
          backgroundClip: 'text',
        } : {}),
      }}
    >
      {children}
    </span>
  )
}
