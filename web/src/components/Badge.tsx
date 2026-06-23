import { type ReactNode } from 'react'

type BadgeV2Variant = 'default' | 'success' | 'error' | 'warning' | 'pro' | 'ecosystem'

interface BadgeV2Props {
  variant?: BadgeV2Variant
  children: ReactNode
  className?: string
}

// Borderless tinted chip. Modern Linear / Vercel style: bg + fg, no outline.
// `pro` is the only variant with a touch of decoration — an aurora-gradient
// background (very low alpha so the brand-text stays readable on top).
const variantStyles: Record<BadgeV2Variant, { bg: string; color: string }> = {
  default:   { bg: 'var(--brand-soft)',  color: 'var(--brand-text)' },
  success:   { bg: 'var(--ok-fill)',     color: 'var(--ok-text)' },
  error:     { bg: 'var(--danger-fill)', color: 'var(--danger-text)' },
  warning:   { bg: 'var(--warn-fill)',   color: 'var(--warn-text)' },
  pro:       { bg: 'var(--grad-aurora)', color: 'var(--brand-text)' },
  ecosystem: { bg: 'var(--brand-soft)',  color: 'var(--brand-text)' },
}

export default function BadgeV2({
  variant = 'default',
  children,
  className = '',
}: BadgeV2Props) {
  const s = variantStyles[variant]

  return (
    <span
      className={`inline-flex items-center px-[7px] py-[2px] ${className}`}
      style={{
        background: s.bg,
        color: s.color,
        borderRadius: 'var(--r-tag)',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '1.4',
        letterSpacing: '0.005em',
      }}
    >
      {children}
    </span>
  )
}
