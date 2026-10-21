import { useEffect, useId } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import {
  advanceOnboardingSession,
  ONBOARDING_SESSION_KEY,
  startOnboardingSession,
  type OnboardingScope,
  type OnboardingSession,
} from '@/lib/onboarding'
import { writeSessionStorage } from '@/lib/storage'

/**
 * One observation ID owns one query. The query serializes polling;
 * its opaque cursor and observations are persisted together for reloads.
 * Changing client settings switches queries and cancels obsolete reads while
 * leaving the selection controls mounted so keyboard focus stays intact.
 */
export function useConnectionVerification(scope: OnboardingScope, restored: OnboardingSession | null, observationId: string, paused = false) {
  const instance = useId()
  const queryClient = useQueryClient()
  const queryKey = ['admin', 'connection-verification', instance, observationId]
  const query = useQuery({
    queryKey,
    enabled: !paused,
    queryFn: async ({ signal }) => {
      const previous = queryClient.getQueryData<{ session: OnboardingSession }>(queryKey)?.session ?? restored
      const { data } = await adminApi.getOnboardingStatus(previous ? {
        after_id: previous.afterId,
        delivery_after_id: previous.deliveryAfterId || undefined,
        started_at: previous.startedAt,
      } : {}, { signal })
      const session = previous
        ? advanceOnboardingSession(previous, data)
        : startOnboardingSession(data, scope)
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      return { session, status: data.status, more: data.has_more || data.deliveries_has_more }
    },
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    refetchInterval: query => paused || query.state.data?.session.firstCacheHit
      ? false
      : query.state.data?.more ? 10 : 2_500,
  })
  const session = query.data?.session ?? restored
  useEffect(() => {
    if (session) writeSessionStorage(ONBOARDING_SESSION_KEY, JSON.stringify(session))
  }, [session])
  return { ...query, session, stop: () => queryClient.cancelQueries({ queryKey }) }
}
