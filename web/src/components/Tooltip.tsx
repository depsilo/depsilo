import type { ReactElement, ReactNode } from 'react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

interface TooltipV2Props {
  content: ReactNode
  children: ReactElement
}

export default function TooltipV2({ content, children }: TooltipV2Props) {
  return (
    <Tooltip
      onOpenChange={(_open, details) => {
        if (details.reason === 'escape-key') details.allowPropagation()
      }}
    >
      <TooltipTrigger render={children} delay={350} />
      <TooltipContent>{content}</TooltipContent>
    </Tooltip>
  )
}
