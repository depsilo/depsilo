import { useId, type TextareaHTMLAttributes } from 'react'

import { Textarea as UiTextarea } from '@/components/ui/textarea'
import Field from '@/components/app/field'
import { mergeDescriptionIds } from '@/lib/aria'
import { cn } from '@/lib/utils'

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  hint?: string
  error?: string
  layout?: 'stacked' | 'row'
}

export default function Textarea({
  className,
  layout,
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
    <Field controlId={controlId} label={label} hint={hint} error={error} layout={layout}>
      <UiTextarea
        {...rest}
        id={controlId}
        aria-invalid={error ? true : ariaInvalid}
        aria-describedby={mergeDescriptionIds(ariaDescribedBy, messageId)}
        className={cn('min-h-20 px-3 py-2 text-field md:text-body', className)}
      />
    </Field>
  )
}
