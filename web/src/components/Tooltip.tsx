import { Tooltip as AriaTooltip, TooltipTrigger } from 'react-aria-components'
import type { ReactElement, ReactNode } from 'react'

interface TooltipV2Props {
  content: ReactNode
  children: ReactElement
}

export default function TooltipV2({ content, children }: TooltipV2Props) {
  return (
    // React Aria merges the trigger props into the first child through context,
    // so that child must be a React Aria component — IconButtonControl is one.
    // There is no positioner element; the tooltip itself is positioned and
    // carries the stacking order in CSS.
    <TooltipTrigger delay={350}>
      {children}
      <AriaTooltip offset={8} className="app-tooltip-popup">{content}</AriaTooltip>
    </TooltipTrigger>
  )
}
