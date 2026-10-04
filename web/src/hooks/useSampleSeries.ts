import { useState } from 'react'

/**
 * Buffers a bounded series of real values the page already receives, so a tile
 * can draw a genuine sparkline after a few polls. A `sampleKey` change (the
 * server's sampled_at) appends a point even when the value is unchanged, so a
 * flat idle line renders flat instead of collapsing to a single dot.
 *
 * This is presentation-only accumulation of server samples; it is never a data
 * source for totals, and with fewer than two samples no curve is drawn.
 */
export function useSampleSeries(
  value: number | null | undefined,
  sampleKey: string | number | null | undefined,
  max = 24,
): number[] {
  const [state, setState] = useState<{ key: string | number | null | undefined; series: number[] }>(() => ({
    key: sampleKey,
    series: Number.isFinite(value) && value !== null && value !== undefined ? [value] : [],
  }))

  if (state.key !== sampleKey) {
    const next = Number.isFinite(value) && value !== null && value !== undefined
      ? [...state.series, value].slice(-max)
      : state.series
    setState({ key: sampleKey, series: next })
  }

  return state.series
}
