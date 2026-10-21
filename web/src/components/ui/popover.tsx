import { Popover as PopoverPrimitive } from '@base-ui/react/popover'

import { cn } from '@/lib/utils'

function Popover(props: PopoverPrimitive.Root.Props) {
  return <PopoverPrimitive.Root {...props} />
}

function PopoverTrigger(props: PopoverPrimitive.Trigger.Props) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />
}

function PopoverClose(props: PopoverPrimitive.Close.Props) {
  return <PopoverPrimitive.Close data-slot="popover-close" {...props} />
}

// shadcn's Base UI composition, with the shared readable 14px type and bounded
// scrolling used by Admin explanations. Positioning and focus remain Base UI's.
function PopoverContent({
  className,
  align = 'center',
  alignOffset = 0,
  side = 'bottom',
  sideOffset = 8,
  collisionPadding = 12,
  ...props
}: PopoverPrimitive.Popup.Props & Pick<PopoverPrimitive.Positioner.Props, 'align' | 'alignOffset' | 'side' | 'sideOffset' | 'collisionPadding'>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Positioner align={align} alignOffset={alignOffset} side={side} sideOffset={sideOffset} collisionPadding={collisionPadding} className="isolate z-50">
        <PopoverPrimitive.Popup
          data-slot="popover-content"
          className={cn('w-80 max-w-[calc(100vw-24px)] max-h-[var(--available-height)] overflow-y-auto rounded-lg border border-border bg-popover p-4 text-[14px] leading-[1.65] text-popover-foreground shadow-lg outline-none [overflow-wrap:anywhere]', className)}
          {...props}
        />
      </PopoverPrimitive.Positioner>
    </PopoverPrimitive.Portal>
  )
}

function PopoverHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="popover-header" className={cn('mb-2 flex items-start gap-2', className)} {...props} />
}

function PopoverTitle({ className, ...props }: PopoverPrimitive.Title.Props) {
  return <PopoverPrimitive.Title data-slot="popover-title" className={cn('min-w-0 flex-1 text-[15px] font-semibold leading-6', className)} {...props} />
}

function PopoverDescription({ className, ...props }: PopoverPrimitive.Description.Props) {
  return <PopoverPrimitive.Description data-slot="popover-description" className={cn('text-sm text-muted-foreground', className)} {...props} />
}

export { Popover, PopoverClose, PopoverContent, PopoverDescription, PopoverHeader, PopoverTitle, PopoverTrigger }
