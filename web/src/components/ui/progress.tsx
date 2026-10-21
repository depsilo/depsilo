import { Progress as ProgressPrimitive } from '@base-ui/react/progress'

import { cn } from '@/lib/utils'

function Progress({ className, children, ...props }: ProgressPrimitive.Root.Props) {
  return (
    <ProgressPrimitive.Root data-slot="progress" className={cn('w-full', className)} {...props}>
      {children ?? <ProgressTrack><ProgressIndicator /></ProgressTrack>}
    </ProgressPrimitive.Root>
  )
}

function ProgressTrack({ className, ...props }: ProgressPrimitive.Track.Props) {
  return <ProgressPrimitive.Track data-slot="progress-track" className={cn('h-1.5 w-full overflow-hidden rounded-full bg-muted', className)} {...props} />
}

function ProgressIndicator({ className, ...props }: ProgressPrimitive.Indicator.Props) {
  return (
    <ProgressPrimitive.Indicator
      data-slot="progress-indicator"
      className={cn('h-full rounded-full bg-primary transition-[width] duration-300 motion-reduce:transition-none data-[indeterminate]:w-full motion-safe:data-[indeterminate]:animate-pulse data-[indeterminate]:bg-primary/30', className)}
      {...props}
    />
  )
}

function ProgressLabel({ className, ...props }: ProgressPrimitive.Label.Props) {
  return <ProgressPrimitive.Label data-slot="progress-label" className={cn('text-sm font-medium', className)} {...props} />
}

function ProgressValue({ className, ...props }: ProgressPrimitive.Value.Props) {
  return <ProgressPrimitive.Value data-slot="progress-value" className={cn('text-sm text-muted-foreground tabular-nums', className)} {...props} />
}

export { Progress, ProgressIndicator, ProgressLabel, ProgressTrack, ProgressValue }
