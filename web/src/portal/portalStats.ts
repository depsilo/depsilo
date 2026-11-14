import { useQuery } from '@tanstack/react-query'

import { statsApi } from '@/lib/api'

export interface PortalUpstream {
  id: number
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
}

/** The anonymous GET /api/v1/stats payload, as far as the Portal reads it. */
export interface PortalStats {
  service: { status: string; version: string; uptime_seconds?: number }
  today?: { total_requests: number; hit_count: number; hit_rate: number; bytes_served: number; bytes_saved: number }
  week?: { total_requests: number; hit_rate: number; bytes_saved: number }
  cache?: { total_files: number; total_size_bytes: number }
  series?: { interval_minutes: number; points: { time_unix: number; requests: number; hits: number }[] }
  upstreams?: PortalUpstream[]
  extra_indexes?: { kind?: string; name?: string; path: string }[]
}

/**
 * The Portal's one stats poll. The header, the Quick Start status band, and
 * the Monitor share this query key, so a page shows one consistent snapshot
 * and makes one request per interval.
 */
export function usePortalStats() {
  return useQuery<PortalStats>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => (await statsApi.getStats({ signal })).data as PortalStats,
    staleTime: 30_000,
    refetchInterval: 30_000,
    retry: false,
  })
}

/**
 * Whether Depsilo is serving. Upstream mirror health is a separate axis with
 * its own page, so `degraded` (some mirror is unhealthy) still reads as
 * healthy here.
 */
export function serviceHealth(stats: PortalStats | undefined): 'healthy' | 'failed' | 'unknown' {
  const status = stats?.service.status
  if (status === 'failed') return 'failed'
  return status === 'healthy' || status === 'degraded' ? 'healthy' : 'unknown'
}
