import { useId, type InputHTMLAttributes } from 'react'

import { Input as UiInput } from '@/components/ui/input'
import Field from '@/components/app/field'
import { mergeDescriptionIds } from '@/lib/aria'
import { cn } from '@/lib/utils'

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  hint?: string
  error?: string
  /** Machine-readable values (endpoints, tokens, globs) use the mono face. */
  mono?: boolean
}

export default function Input({
  className,
  mono,
  label,
  hint,
  error,
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: InputProps) {
  const generatedId = useId()
  const controlId = id ?? generatedId
  const messageId = hint || error ? `${controlId}-description` : undefined

  return (
    <Field controlId={controlId} label={label} hint={hint} error={error}>
      <UiInput
        {...rest}
        id={controlId}
        aria-invalid={error ? true : ariaInvalid}
        aria-describedby={mergeDescriptionIds(ariaDescribedBy, messageId)}
        className={cn(
          // 16px on small screens keeps mobile Safari from zooming a focused field.
          'h-9 px-3 text-[16px] md:text-[13px]',
          mono && 'font-mono',
          className,
        )}
      />
    </Field>
  )
}
