import { type ReactNode } from 'react'

type BadgeVariant = 'pypi' | 'apt' | 'success' | 'error' | 'default'

interface BadgeProps {
  variant?: BadgeVariant
  children: ReactNode
  className?: string
}

const variantStyles: Record<BadgeVariant, string> = {
  pypi: 'bg-secondary-container/20 text-primary-dim',
  apt: 'bg-tertiary-container/20 text-tertiary',
  success: 'bg-success/15 text-success',
  error: 'bg-error/15 text-error',
  default: 'bg-surface-container text-on-surface-variant',
}

export default function Badge({
  variant = 'default',
  children,
  className = '',
}: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded-full text-[11px] font-bold tracking-widest uppercase px-2.5 py-0.5 ${variantStyles[variant]} ${className}`}
    >
      {children}
    </span>
  )
}
