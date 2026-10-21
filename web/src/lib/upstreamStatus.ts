/** Health of one upstream mirror, as reported by the public stats endpoint. */
export type MirrorStatus = 'healthy' | 'degraded' | 'failed' | 'unknown'

interface UpstreamStatusInput {
  healthy: boolean
  avg_latency_ms: number
  has_observations?: boolean
}

// A reachable mirror is only "degraded" once it averages a full second. The
// bar is deliberately generous: this is a rolling average over a shared link,
// and mirrors from another region answer in hundreds of milliseconds as a
// matter of geography. The backend keeps the same value in
// internal/api/public/stats.go.
export function upstreamStatus(upstream: UpstreamStatusInput): MirrorStatus {
  if (!upstream.healthy) return 'failed'
  // New runtimes start eligible for routing, but that is not yet evidence of
  // health. Missing metadata preserves compatibility with the public API.
  if (upstream.has_observations === false) return 'unknown'
  if (upstream.avg_latency_ms >= 1000) return 'degraded'
  return 'healthy'
}
