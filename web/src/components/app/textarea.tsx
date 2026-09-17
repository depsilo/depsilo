import { useId, type TextareaHTMLAttributes } from 'react'

import { Textarea as UiTextarea } from '@/components/ui/textarea'
import Field from '@/components/app/field'
import { mergeDescriptionIds } from '@/lib/aria'
import { cn } from '@/lib/utils'

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  hint?: string
  error?: string
}

export default function Textarea({
  className,
  label,
  hint,
  error,
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: TextareaProps) {
  const generatedId = useId()
  const controlId = id ?? generatedId
  const messageId = hint || error ? `${controlId}-description` : undefined

  return (
    <Field controlId={controlId} label={label} hint={hint} error={error}>
      <UiTextarea
        {...rest}
        id={controlId}
        aria-invalid={error ? true : ariaInvalid}
        aria-describedby={mergeDescriptionIds(ariaDescribedBy, messageId)}
        className={cn('min-h-20 px-3 py-2 text-[16px] md:text-[13px]', className)}
      />
    </Field>
  )
}
