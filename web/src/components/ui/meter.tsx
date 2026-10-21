import { Meter as MeterPrimitive } from '@base-ui/react/meter'

import { cn } from '@/lib/utils'

// A bounded measurement, not task completion. Base UI owns the accessible
// value and the indicator width; callers only supply a known denominator.
function Meter({ className, ...props }: MeterPrimitive.Root.Props) {
  return <MeterPrimitive.Root data-slot="meter" className={cn('w-full', className)} {...props} />
}

function MeterTrack({ className, ...props }: MeterPrimitive.Track.Props) {
  return <MeterPrimitive.Track data-slot="meter-track" className={cn('h-1.5 w-full overflow-hidden rounded-full bg-muted', className)} {...props} />
}

function MeterIndicator({ className, ...props }: MeterPrimitive.Indicator.Props) {
  return <MeterPrimitive.Indicator data-slot="meter-indicator" className={cn('h-full rounded-full bg-primary transition-[width] duration-300 motion-reduce:transition-none', className)} {...props} />
}

function MeterLabel({ className, ...props }: MeterPrimitive.Label.Props) {
  return <MeterPrimitive.Label data-slot="meter-label" className={cn('text-sm font-medium', className)} {...props} />
}

function MeterValue({ className, ...props }: MeterPrimitive.Value.Props) {
  return <MeterPrimitive.Value data-slot="meter-value" className={cn('text-sm text-muted-foreground tabular-nums', className)} {...props} />
}

export { Meter, MeterIndicator, MeterLabel, MeterTrack, MeterValue }
