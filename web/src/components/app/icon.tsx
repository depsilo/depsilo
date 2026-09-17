import type { LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * Renders a Lucide icon in the product's icon box.
 *
 * The icon *family* is Lucide everywhere: call sites import the glyph they
 * need from `lucide-react` directly. This helper exists only for the places
 * where the glyph is chosen at runtime — a tone map, a prop, or a ternary —
 * because a JSX element name cannot be an expression. It owns the shared
 * inline box (fixed size, no baseline shift, never shrinks in a flex row) so
 * those dynamic sites cannot drift from the static ones.
 */
export default function Icon({
  icon: Glyph,
  size = 'md',
  className,
}: {
  icon: LucideIcon
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  return (
    <Glyph
      aria-hidden="true"
      focusable="false"
      className={cn('icon', size === 'sm' && 'icon-sm', size === 'lg' && 'icon-lg', className)}
    />
  )
}

export type { LucideIcon }
