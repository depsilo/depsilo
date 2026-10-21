import { Field as FieldPrimitive } from '@base-ui/react/field'
import { cn } from '@/lib/utils'

// Base UI owns the label, control and feedback relationships. Keep the
// shadcn field composition and theme here instead of repeating ID wiring in
// individual Input, Textarea and Select wrappers.
function Field({ className, orientation = 'vertical', ...props }: FieldPrimitive.Root.Props & {
  orientation?: 'vertical' | 'horizontal'
}) {
  return (
    <FieldPrimitive.Root
      data-slot="field"
      data-orientation={orientation}
      className={cn(
        'group/field flex min-w-0 flex-col gap-1 data-[orientation=horizontal]:min-h-10 data-[orientation=horizontal]:flex-row data-[orientation=horizontal]:items-center data-[orientation=horizontal]:gap-3',
        className,
      )}
      {...props}
    />
  )
}

function FieldLabel({ className, ...props }: FieldPrimitive.Label.Props) {
  return (
    <FieldPrimitive.Label
      data-slot="field-label"
      className={cn('text-sm font-medium leading-5 data-disabled:opacity-50', className)}
      {...props}
    />
  )
}

function FieldDescription({ className, ...props }: FieldPrimitive.Description.Props) {
  return (
    <FieldPrimitive.Description
      data-slot="field-description"
      className={cn('text-sm leading-5 text-muted-foreground', className)}
      {...props}
    />
  )
}

function FieldError({ className, ...props }: FieldPrimitive.Error.Props) {
  return (
    <FieldPrimitive.Error
      data-slot="field-error"
      role="alert"
      className={cn('text-sm leading-5 text-destructive', className)}
      {...props}
    />
  )
}

export { Field, FieldLabel, FieldDescription, FieldError }
