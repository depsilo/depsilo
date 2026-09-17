import { cn } from '@/lib/utils'

export type StatusTone = 'healthy' | 'degraded' | 'failed' | 'unknown'

interface StatusDotProps {
  status: StatusTone
  /** Diameter in px. Defaults to the compact 6px inline marker. */
  size?: number
  live?: boolean
}

const TONE_CLASS: Record<StatusTone, string> = {
  healthy: 'bg-success',
  degraded: 'bg-warning',
  failed: 'bg-destructive',
  unknown: 'bg-muted-foreground',
}

/**
 * Colour is never the only carrier of meaning: every call site pairs this with
 * text naming the same state.
 */
export default function StatusDot({ status, size = 6, live = false }: StatusDotProps) {
  return (
    <span
      aria-hidden="true"
      className={cn('inline-block shrink-0 rounded-full', TONE_CLASS[status], live && 'animate-pulse')}
      style={{ width: size, height: size }}
    />
  )
}
