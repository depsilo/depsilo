import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

interface SegmentedOption {
  value: string
  label: string
}

interface Props {
  options: SegmentedOption[]
  value: string
  onChange: (value: string) => void
}

export default function Segmented({ options, value, onChange }: Props) {
  return (
    <ToggleGroup
      value={[value]}
      onValueChange={(next) => {
        // Single-select group: never let a re-click leave the rail empty.
        const [selected] = next
        if (selected && selected !== value) onChange(selected)
      }}
    >
      {options.map(option => (
        <ToggleGroupItem key={option.value} value={option.value}>
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
