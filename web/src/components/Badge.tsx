import { type ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'

type BadgeV2Variant = 'default' | 'neutral' | 'success' | 'error' | 'warning' | 'pro' | 'ecosystem'

interface BadgeV2Props {
  variant?: BadgeV2Variant
  children: ReactNode
  className?: string
}

// Tinted chip. Pro adds a subtle brand border to distinguish entitlement.
const VARIANT_MAP: Record<BadgeV2Variant, 'default' | 'neutral' | 'success' | 'warning' | 'destructive' | 'pro'> = {
  default: 'default',
  neutral: 'neutral',
  success: 'success',
  error: 'destructive',
  warning: 'warning',
  pro: 'pro',
  ecosystem: 'default',
}

export default function BadgeV2({
  variant = 'default',
  children,
  className = '',
}: BadgeV2Props) {
  return (
    <Badge variant={VARIANT_MAP[variant]} className={className}>
      {children}
    </Badge>
  )
}
