/** Health of one upstream mirror, as reported by the public stats endpoint. */
export type MirrorStatus = 'healthy' | 'degraded' | 'failed'

interface UpstreamStatusInput {
  healthy: boolean
  avg_latency_ms: number
}

// A reachable mirror is only "degraded" once it averages a full second. The
// bar is deliberately generous: this is a rolling average over a shared link,
// and mirrors from another region answer in hundreds of milliseconds as a
// matter of geography. The backend keeps the same value in
// internal/api/public/stats.go.
export function upstreamStatus(upstream: UpstreamStatusInput): MirrorStatus {
  if (!upstream.healthy) return 'failed'
  if (upstream.avg_latency_ms >= 1000) return 'degraded'
  return 'healthy'
}
