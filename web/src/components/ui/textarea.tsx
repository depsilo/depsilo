import { Field as FieldPrimitive } from '@base-ui/react/field'
import { cn } from '@/lib/utils'

function Textarea({ className, id, name, value, defaultValue, disabled, 'aria-describedby': describedBy, 'aria-invalid': invalid, ...props }: React.ComponentProps<'textarea'>) {
  return (
    <FieldPrimitive.Control
      id={id}
      name={name}
      value={value}
      defaultValue={defaultValue}
      disabled={disabled}
      aria-describedby={describedBy}
      aria-invalid={invalid}
      {...(props['aria-label'] && !props['aria-labelledby'] ? { 'aria-labelledby': '' } : {})}
      render={<textarea {...props} />}
      data-slot="textarea"
      className={cn(
        'flex field-sizing-content min-h-16 w-full rounded-md border border-input bg-transparent px-3 py-2 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40',
        className,
      )}
    />
  )
}

export { Textarea }
