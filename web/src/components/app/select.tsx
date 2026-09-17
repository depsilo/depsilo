import { useId, type SelectHTMLAttributes } from 'react'

import Field from '@/components/app/field'
import { mergeDescriptionIds } from '@/lib/aria'
import { cn } from '@/lib/utils'

interface FeedbackProps {
  label?: string
  hint?: string
  error?: string
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement>, FeedbackProps {}

/**
 * Depsilo's Select is a native `<select>`.
 *
 * The product uses selects for dense filter bars and enum fields, where the
 * native control is the accessible baseline: OS typeahead, the platform picker
 * on touch devices, `aria-invalid`/`aria-describedby` support, and the
 * `role="combobox"` + value contract the Admin keyboard specs assert. shadcn's
 * Base UI select is a scripted popup and would trade all of that for styling
 * the neutral Stage A theme does not need, so it is deliberately not adopted.
 */
export default function Select({
  className = '',
  label,
  hint,
  error,
  children,
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  ...rest
}: SelectProps) {
  const generatedId = useId()
  const controlId = id ?? generatedId
  const messageId = hint || error ? `${controlId}-description` : undefined

  const select = (
    <select
      {...rest}
      id={controlId}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={mergeDescriptionIds(ariaDescribedBy, messageId)}
      className={cn(
        'h-9 w-full cursor-pointer rounded-md border border-input bg-transparent px-3 py-1.5 text-[16px] text-foreground',
        'transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50',
        'aria-invalid:border-destructive disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 md:text-[13px]',
        className,
      )}
    >
      {children}
    </select>
  )

  return (
    <Field controlId={controlId} label={label} hint={hint} error={error}>
      {select}
    </Field>
  )
}
