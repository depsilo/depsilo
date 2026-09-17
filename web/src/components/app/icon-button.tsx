import { forwardRef } from 'react'

import IconButtonControl, { type IconButtonControlProps } from '@/components/app/icon-button-control'
import Tooltip from '@/components/app/tooltip'

/**
 * Icon action with a floating tooltip. Compact shell controls that must not
 * pay for the popup machinery can use `IconButtonControl` and a native
 * `title` instead.
 */
export default forwardRef<HTMLButtonElement, IconButtonControlProps>(function IconButton(props, ref) {
  return (
    <Tooltip content={props.label}>
      <IconButtonControl {...props} ref={ref} />
    </Tooltip>
  )
})
