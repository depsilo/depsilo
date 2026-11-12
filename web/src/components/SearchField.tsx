import type { ComponentProps } from 'react'

import Icon from '@/components/Icon'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

interface SearchFieldProps extends Omit<ComponentProps<'input'>, 'className'> {
  /** Layout classes for the framed field, such as width limits. */
  className?: string
}

/**
 * The search box used by every Admin filter bar: a leading glyph inside the
 * same 40px frame, border, and focus ring as a standard Input. The frame owns
 * the focus ring so the glyph sits inside it, rather than the bare input
 * drawing a ring that excludes the icon or no ring at all.
 */
export default function SearchField({ className, ...props }: SearchFieldProps) {
  return (
    <div
      data-slot="search-field"
      className={cn(
        'flex h-10 min-w-0 flex-1 items-center gap-2 rounded-md border border-input bg-transparent px-3 transition-colors',
        'focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 dark:bg-input/30',
        className,
      )}
    >
      <Icon name="search" size="sm" className="shrink-0 text-muted-foreground" />
      <Input
        {...props}
        className="h-full min-w-0 flex-1 rounded-none border-0 bg-transparent px-0 shadow-none focus-visible:outline-none! focus-visible:ring-0 dark:bg-transparent"
      />
    </div>
  )
}
