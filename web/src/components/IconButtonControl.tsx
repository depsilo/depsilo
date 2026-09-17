import { forwardRef, type ButtonHTMLAttributes } from 'react'
import { Button } from 'react-aria-components'
import Icon, { type IconName } from './Icon'

export interface IconButtonControlProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: IconName
  label: string
  tone?: 'neutral' | 'danger'
  loading?: boolean
}

// Accessible icon-button rendering without an opinionated label surface.
// Callers that need the rich floating tooltip use IconButton; compact shell
// controls can use the native title adapter without loading popup machinery.
//
// React Aria's Button, not a plain <button>: React Aria hands a composed
// trigger's props to its child through context, and only React Aria components
// consume it. A plain element receives nothing, so TooltipTrigger would never
// open. `aria-busy` is gone with it — React Aria filters DOM props through a
// fixed allowlist and announces `isPending` through its own assertive message.
export default forwardRef<HTMLButtonElement, IconButtonControlProps>(function IconButtonControl(
  { icon, label, tone = 'neutral', loading = false, disabled, className = '', style, onClick, type, ...rest },
  ref,
) {
  return (
    <Button
      {...rest}
      ref={ref}
      // React Aria omits onClick from its Button type to steer callers to
      // onPress, but useButton forwards onClick into usePress, so call sites
      // keep a real currentTarget for focus restoration and stopPropagation.
      onClick={onClick as never}
      type={type ?? 'button'}
      isDisabled={disabled}
      isPending={loading}
      data-icon-button
      aria-label={label}
      className={`inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-[6px] bg-transparent stripe-focus-ring data-[disabled]:opacity-50 data-[disabled]:pointer-events-none data-[pending]:opacity-50 data-[pending]:pointer-events-none ${className}`}
      style={{
        width: 41,
        height: 41,
        minWidth: 41,
        minHeight: 41,
        color: tone === 'danger' ? 'var(--danger-text)' : 'var(--text-soft)',
        ...style,
      }}
    >
      <Icon
        name={loading ? 'progress_activity' : icon}
        size="sm"
        className={loading ? 'animate-spin' : ''}
      />
    </Button>
  )
})
