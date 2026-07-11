import { forwardRef, type ButtonHTMLAttributes } from 'react'
import Icon from './Icon'
import TooltipV2 from './Tooltip'

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: string
  label: string
  tone?: 'neutral' | 'danger'
  loading?: boolean
}

export default forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { icon, label, tone = 'neutral', loading = false, disabled, className = '', style, ...rest },
  ref,
) {
  return (
    <TooltipV2 content={label}>
      <button
        {...rest}
        ref={ref}
        type={rest.type ?? 'button'}
        data-icon-button
        aria-label={label}
        aria-busy={loading || undefined}
        disabled={disabled || loading}
        className={`inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-[6px] bg-transparent stripe-focus-ring disabled:opacity-50 disabled:pointer-events-none ${className}`}
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
      </button>
    </TooltipV2>
  )
})
