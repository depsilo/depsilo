import { useQuery } from '@tanstack/react-query'

import { statsApi } from '@/lib/api'
import type { StatsResponse } from '@/lib/adminApi.types'

/** The anonymous GET /api/v1/stats payload. */
export type PortalStats = StatsResponse

/**
 * The Portal's one stats poll. The header, the Quick Start status band, and
 * the Monitor share this query key, so a page shows one consistent snapshot
 * and makes one request per interval.
 */
export function usePortalStats() {
  return useQuery<PortalStats>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => (await statsApi.getStats({ signal })).data,
    staleTime: 30_000,
    refetchInterval: 30_000,
    retry: false,
    // A component that mounts after a failed poll (the lazily loaded Quick
    // Start band) must show that failure, not quietly start a second request
    // that hides it from the header. Retry buttons and the interval re-fetch.
    retryOnMount: false,
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
