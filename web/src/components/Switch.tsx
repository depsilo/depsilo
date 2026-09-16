// React Aria's `Switch` is deprecated in favour of SwitchField + SwitchButton,
// but it is still the export BoardUI's own switch block builds on, so the two
// stay structurally interchangeable. RAC renders a <label> carrying the state
// attributes and a visually hidden <input type="checkbox" role="switch">, so
// the visual track is ours and the keyboard focus lands on the hidden input.
import { Switch } from 'react-aria-components'

interface SwitchV2Props {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
  'aria-label'?: string
}

export default function SwitchV2(props: SwitchV2Props) {
  return (
    <Switch
      isSelected={props.checked}
      onChange={props.onCheckedChange}
      isDisabled={props.disabled}
      aria-label={props['aria-label']}
      // React Aria puts the switch role on a hidden input, so the label is the
      // only pointer surface a test can click. This mirrors the existing
      // data-icon-button / data-toast-tone hooks the browser suite relies on.
      data-switch-control=""
      className="inline-flex min-h-10 items-center gap-3 text-[13px] data-[focus-visible]:outline-2 data-[focus-visible]:outline-offset-2 data-[focus-visible]:outline-[var(--focus-ring)] data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50"
    >
      {({ isSelected }) => (
        <>
          {/* React Aria puts role="switch" on a visually hidden input and
              paints our markup over it, so the input becomes a covered 1x1
              target. Making the presentational layer transparent to pointers
              restores the ARIA element as the real click target — a label
              click still toggles it natively, and assistive technology that
              dispatches a click on the focused element reaches it too. */}
          <span
            className={`pointer-events-none relative h-6 w-10 rounded-full ${isSelected ? 'bg-[var(--btn)]' : 'bg-[var(--bg-soft)]'}`}
          >
            <span
              className={`block h-5 w-5 rounded-full bg-white transition-transform ${isSelected ? 'translate-x-[18px]' : 'translate-x-0.5'}`}
            />
          </span>
          <span>{props.label}</span>
        </>
      )}
    </Switch>
  )
}
