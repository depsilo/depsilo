import { useId, type InputHTMLAttributes } from 'react'
import { mergeDescriptionIds } from './fieldFeedback'

interface FeedbackProps {
  label?: string
  hint?: string
  error?: string
}

interface InputV2Props extends InputHTMLAttributes<HTMLInputElement>, FeedbackProps {
  mono?: boolean
}

export default function InputV2({
  className = '',
  mono,
  label,
  hint,
  error,
  onFocus,
  onBlur,
  style,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: InputV2Props) {
  const generatedId = useId()
  const controlId = rest.id ?? generatedId
  const descriptionId = hint || error ? `${controlId}-description` : undefined
  const composedDescriptionIds = mergeDescriptionIds(ariaDescribedBy, descriptionId)

  const input = (
    <input
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={composedDescriptionIds}
      className={`w-full rounded-[4px] px-3 py-2 text-[16px] md:text-[13px] transition-colors duration-150 stripe-focus-ring ${mono ? 'font-mono' : ''} ${className}`}
      style={{
        background: 'var(--bg-card)',
        border: '1px solid var(--border)',
        color: 'var(--text)',
        outline: 'none',
        ...style,
      }}
      onFocus={(event) => {
        event.currentTarget.style.borderColor = 'var(--brand)'
        onFocus?.(event)
      }}
      onBlur={(event) => {
        event.currentTarget.style.borderColor = 'var(--border)'
        onBlur?.(event)
      }}
    />
  )

  if (!label && !descriptionId) return input

  return (
    <div>
      {label && (
        <label htmlFor={controlId} className="mb-1 block text-[14px] font-[400] text-[var(--text-muted)]">
          {label}
        </label>
      )}
      {input}
      {(error || hint) && (
        <p
          id={descriptionId}
          role={error ? 'alert' : undefined}
          className={`mt-1 text-[12px] ${error ? 'text-[var(--danger-text)]' : 'text-[var(--text-muted)]'}`}
        >
          {error || hint}
        </p>
      )}
    </div>
  )
}
