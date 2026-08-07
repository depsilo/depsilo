import { useId, type SelectHTMLAttributes } from 'react'
import { mergeDescriptionIds } from './fieldFeedback'

interface FeedbackProps {
  label?: string
  hint?: string
  error?: string
}

interface SelectV2Props extends SelectHTMLAttributes<HTMLSelectElement>, FeedbackProps {}

export default function SelectV2({
  className = '',
  label,
  hint,
  error,
  children,
  style,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: SelectV2Props) {
  const generatedId = useId()
  const controlId = rest.id ?? generatedId
  const descriptionId = hint || error ? `${controlId}-description` : undefined
  const composedDescriptionIds = mergeDescriptionIds(ariaDescribedBy, descriptionId)

  const select = (
    <select
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={composedDescriptionIds}
      className={`w-full cursor-pointer rounded-[4px] px-3 py-2 text-[16px] md:text-[13px] transition-colors duration-150 stripe-focus-ring ${className}`}
      style={{
        background: 'var(--bg-card)',
        border: '1px solid var(--border)',
        color: 'var(--text)',
        ...style,
      }}
    >
      {children}
    </select>
  )

  if (!label && !descriptionId) return select

  return (
    <div>
      {label && (
        <label htmlFor={controlId} className="mb-1 block text-[14px] font-[400] text-[var(--text-muted)]">
          {label}
        </label>
      )}
      {select}
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
