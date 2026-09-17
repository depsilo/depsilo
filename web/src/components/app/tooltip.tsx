import type { ReactElement, ReactNode } from 'react'

import {
  Tooltip as TooltipRoot,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

interface TooltipProps {
  content: ReactNode
  /** A single element; the trigger merges its props into it. */
  children: ReactElement
}

/**
 * The product's tooltip: a short label attached to one trigger element.
 *
 * Escape closes the tooltip and is allowed to continue to whatever contains
 * it, so dismissing tooltip help never swallows the Escape that closes a
 * dialog around it.
 */
export default function Tooltip({ content, children }: TooltipProps) {
  return (
    <TooltipRoot
      onOpenChange={(_open, details) => {
        if (details.reason === 'escape-key') details.allowPropagation()
      }}
    >
      <TooltipTrigger render={children} delay={350} />
      <TooltipContent role="tooltip">{content}</TooltipContent>
    </TooltipRoot>
  )
}
