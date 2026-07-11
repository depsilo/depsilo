import { Tooltip } from '@base-ui/react/tooltip'
import type { ReactElement, ReactNode } from 'react'

interface TooltipV2Props {
  content: ReactNode
  children: ReactElement
}

export default function TooltipV2({ content, children }: TooltipV2Props) {
  return (
    <Tooltip.Root
      onOpenChange={(_open, details) => {
        if (details.reason === 'escape-key') details.allowPropagation()
      }}
    >
      <Tooltip.Trigger render={children} delay={350} />
      <Tooltip.Portal>
        <Tooltip.Positioner sideOffset={8} className="app-tooltip-positioner">
          <Tooltip.Popup role="tooltip" className="app-tooltip-popup">{content}</Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  )
}
