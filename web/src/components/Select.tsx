import { useId, type SelectHTMLAttributes } from 'react'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
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
    <NativeSelect
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={composedDescriptionIds}
      className={className}
      style={style}
    >
      {children}
    </NativeSelect>
  )

  if (!label && !descriptionId) return select

  return (
    <div>
      {label && (
        <Label htmlFor={controlId} className="mb-1 block">
          {label}
        </Label>
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
