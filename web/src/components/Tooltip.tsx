import type { ReactElement, ReactNode } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

interface TooltipV2Props {
  content: ReactNode
  children: ReactElement
  /** Hover delay in ms before the tooltip opens. Defaults to the app-wide 350ms. */
  delay?: number
}

export default function TooltipV2({ content, children, delay = 350 }: TooltipV2Props) {
  return (
    <Tooltip
      onOpenChange={(_open, details) => {
        if (details.reason === 'escape-key') details.allowPropagation()
      }}
    >
      <TooltipTrigger render={children} delay={delay} />
      <TooltipContent>{content}</TooltipContent>
    </Tooltip>
  )
}
