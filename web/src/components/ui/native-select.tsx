import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

// shadcn/ui Native Select. Depsilo keeps the platform listbox for form
// controls: it preserves native keyboard, typeahead, and form semantics plus
// the `combobox` + `<option>` contract that Playwright and operators rely on.
//
// A one-column grid keeps the chevron aligned without an absolutely
// positioned wrapper, so the select stretches to the row and caller sizing
// classes (min-h-10, sm:w-auto) keep working.
function NativeSelect({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <div
      className={cn(
        'relative grid w-full min-w-0 grid-cols-1 has-[select:disabled]:opacity-50',
        className,
      )}
    >
      <select
        data-slot="native-select"
        className="col-start-1 row-start-1 h-9 w-full min-w-0 cursor-pointer appearance-none rounded-md border border-input bg-transparent py-1 pr-8 pl-3 text-base text-foreground transition-colors outline-none select-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:hover:bg-input/50 dark:aria-invalid:border-destructive/50"
        {...props}
      />
      <ChevronDown
        aria-hidden="true"
        data-slot="native-select-icon"
        className="pointer-events-none col-start-1 row-start-1 mr-2.5 size-4 self-center justify-self-end text-muted-foreground select-none"
      />
    </div>
  )
}

function NativeSelectOption({
  className,
  ...props
}: React.ComponentProps<'option'>) {
  return (
    <option
      data-slot="native-select-option"
      className={cn('bg-[Canvas] text-[CanvasText]', className)}
      {...props}
    />
  )
}

function NativeSelectOptGroup({
  className,
  ...props
}: React.ComponentProps<'optgroup'>) {
  return (
    <optgroup
      data-slot="native-select-optgroup"
      className={cn('bg-[Canvas] text-[CanvasText]', className)}
      {...props}
    />
  )
}

export { NativeSelect, NativeSelectOptGroup, NativeSelectOption }
