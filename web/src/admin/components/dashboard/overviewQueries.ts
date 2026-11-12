import { keepPreviousData, useQuery } from '@tanstack/react-query'

import type { DashboardRange } from '@/lib/adminApi.types'
import { adminApi } from '@/lib/api'

/** Prefix of the Overview's secondary queries, so one call can refresh them all. */
export const COCKPIT_QUERY_KEY = ['admin', 'cockpit'] as const

const secondary = { refetchIntervalInBackground: false, retry: false } as const

/** Where the selected period's traffic went. Keeps the previous range on screen while the next one loads. */
export function useBreakdown(range: DashboardRange) {
  return useQuery({
    queryKey: [...COCKPIT_QUERY_KEY, 'breakdown', range],
    queryFn: async ({ signal }) => (await adminApi.getDashboardBreakdown(range, { signal })).data,
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
    ...secondary,
  })
}

/** The Upstream list, for selection order and the newest failure explanation. */
export function useUpstreamList() {
  return useQuery({
    queryKey: [...COCKPIT_QUERY_KEY, 'upstreams'],
    queryFn: async ({ signal }) => (await adminApi.listUpstreams({ signal })).data.items,
    refetchInterval: 30_000,
    ...secondary,
  })
}

export function useSuggestionsTotal() {
  return useQuery({
    queryKey: [...COCKPIT_QUERY_KEY, 'suggestions'],
    queryFn: async ({ signal }) => (await adminApi.listSuggestions({ page: 1, per_page: 1 }, { signal })).data.total,
    refetchInterval: 60_000,
    ...secondary,
  })
}

export function useSecuritySummary() {
  return useQuery({
    queryKey: [...COCKPIT_QUERY_KEY, 'security'],
    queryFn: async ({ signal }) => (await adminApi.getSecurityDashboard({ signal })).data,
    refetchInterval: 60_000,
    ...secondary,
  })
}

/** Shares the shell's capability query, so the Overview adds no request for it. */
export function useCapabilityFacts() {
  return useQuery({
    queryKey: ['admin', 'capabilities', 'summary'],
    queryFn: ({ signal }) => adminApi.getCapabilitySummary({ signal }),
    select: response => response.data.capabilities ?? [],
    staleTime: 60_000,
    // A full-screen Overview never regains focus, so coverage refreshes on a timer.
    refetchInterval: 60_000,
    ...secondary,
  })
}

export function useCacheDistribution() {
  return useQuery({
    queryKey: [...COCKPIT_QUERY_KEY, 'distribution'],
    queryFn: async ({ signal }) => (await adminApi.getCacheDistribution({ signal })).data,
    refetchInterval: 60_000,
    ...secondary,
  })
}
