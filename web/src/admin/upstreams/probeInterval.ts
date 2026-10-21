export const probeIntervals = ['15s', '30s', '1m', '5m', '10m', '30m', '1h'] as const

const units: Record<string, number> = { ns: 1e-9, us: 1e-6, 'µs': 1e-6, 'μs': 1e-6, ms: 1e-3, s: 1, m: 60, h: 3600 }

/** Match Go duration values by time, preserving valid non-preset intervals. */
export function normalizeProbeInterval(value: string): string {
  const parts = [...value.matchAll(/(\d+(?:\.\d+)?)(ns|us|µs|μs|ms|s|m|h)/g)]
  if (parts.length === 0 || parts.map(part => part[0]).join('') !== value) return value
  const seconds = parts.reduce((total, part) => total + Number(part[1]) * units[part[2]], 0)
  const presets = [15, 30, 60, 300, 600, 1800, 3600]
  const index = presets.indexOf(seconds)
  return index < 0 ? value : probeIntervals[index]
}
