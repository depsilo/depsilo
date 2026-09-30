import { useId, type InputHTMLAttributes } from 'react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
    <Input
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={composedDescriptionIds}
      className={`${mono ? 'font-mono' : ''} ${className}`}
      style={style}
      onFocus={onFocus}
      onBlur={onBlur}
    />
  )

  if (!label && !descriptionId) return input

  return (
    <div>
      {label && (
        <Label htmlFor={controlId} className="mb-1 block">
          {label}
        </Label>
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
