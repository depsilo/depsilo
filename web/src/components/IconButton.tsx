import { forwardRef } from 'react'
import IconButtonControl, { type IconButtonControlProps } from './IconButtonControl'
import TooltipV2 from './Tooltip'

export default forwardRef<HTMLButtonElement, IconButtonControlProps>(function IconButton(props, ref) {
  return (
    <TooltipV2 content={props.label}>
      <IconButtonControl {...props} ref={ref} />
    </TooltipV2>
  )
})
