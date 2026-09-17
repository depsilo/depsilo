import { Switch } from '@base-ui/react/switch'

interface SwitchV2Props {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  'aria-label'?: string
}

export default function SwitchV2(props: SwitchV2Props) {
  return (
    <label className="inline-flex min-h-10 items-center gap-3 text-[13px]">
      <Switch.Root
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        disabled={props.disabled}
        aria-label={props['aria-label']}
        className="relative h-6 w-10 rounded-full bg-[var(--bg-soft)] data-[checked]:bg-[var(--btn)] stripe-focus-ring"
      >
        <Switch.Thumb className="block h-5 w-5 translate-x-0.5 rounded-full bg-white transition-transform data-[checked]:translate-x-[18px]" />
      </Switch.Root>
      <span>{props.label}</span>
    </label>
  )
}
