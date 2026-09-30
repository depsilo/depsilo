import { Switch } from '@/components/ui/switch'

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
      <Switch
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        disabled={props.disabled}
        aria-label={props['aria-label']}
      />
      <span>{props.label}</span>
    </label>
  )
}
