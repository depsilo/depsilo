import { Switch as UiSwitch } from '@/components/ui/switch'

interface SwitchProps {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  'aria-label'?: string
}

/**
 * Label + toggle row. The whole 40px row is the label, so the target is larger
 * than the visual track without changing the switch's own geometry.
 */
export default function Switch(props: SwitchProps) {
  return (
    <label className="inline-flex min-h-10 items-center gap-3 text-[13px]">
      <UiSwitch
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        disabled={props.disabled}
        aria-label={props['aria-label']}
      />
      <span>{props.label}</span>
    </label>
  )
}
