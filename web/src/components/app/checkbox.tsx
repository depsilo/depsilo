import { useId } from 'react'

import { Checkbox as UiCheckbox } from '@/components/ui/checkbox'

interface CheckboxProps {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  id?: string
}

/**
 * Label + checkbox row. The label is part of the click target, so the small
 * painted box is still a comfortable control.
 */
export default function Checkbox({ label, checked, onCheckedChange, disabled, id }: CheckboxProps) {
  const generatedId = useId()
  const controlId = id ?? generatedId

  return (
    <div className="flex min-h-10 items-center gap-2.5">
      <UiCheckbox
        id={controlId}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(next) => onCheckedChange(next === true)}
      />
      <label htmlFor={controlId} className="cursor-pointer text-body select-none">
        {label}
      </label>
    </div>
  )
}
