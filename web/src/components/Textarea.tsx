import { useId, type TextareaHTMLAttributes } from 'react'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { mergeDescriptionIds } from './fieldFeedback'

interface FeedbackProps {
  label?: string
  hint?: string
  error?: string
}

interface TextareaV2Props extends TextareaHTMLAttributes<HTMLTextAreaElement>, FeedbackProps {}

export default function TextareaV2({
  className = '',
  label,
  hint,
  error,
  onFocus,
  onBlur,
  style,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: TextareaV2Props) {
  const generatedId = useId()
  const controlId = rest.id ?? generatedId
  const descriptionId = hint || error ? `${controlId}-description` : undefined
  const composedDescriptionIds = mergeDescriptionIds(ariaDescribedBy, descriptionId)

  const textarea = (
    <Textarea
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={composedDescriptionIds}
      className={className}
      style={style}
      onFocus={onFocus}
      onBlur={onBlur}
    />
  )

  if (!label && !descriptionId) return textarea

  return (
    <div>
      {label && (
        <Label htmlFor={controlId} className="mb-1 block">
          {label}
        </Label>
      )}
      {textarea}
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
