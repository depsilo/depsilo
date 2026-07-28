import type { MirrorStatus } from '@/lib/ecosystemData'

interface UpstreamStatusInput {
  healthy: boolean
  avg_latency_ms: number
}

export function upstreamStatus(upstream: UpstreamStatusInput): MirrorStatus {
  if (!upstream.healthy) return 'failed'
  if (upstream.avg_latency_ms >= 150) return 'degraded'
  return 'healthy'
}
