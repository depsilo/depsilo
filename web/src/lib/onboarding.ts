import type { OnboardingEvent, OnboardingStatusResponse } from './adminApi.types'

export const ONBOARDING_SESSION_KEY = 'depsilo.admin.onboarding-session.v1'
export const ONBOARDING_GATE_QUERY_KEY = ['admin', 'onboarding', 'gate'] as const

export interface OnboardingSession {
  startedAt: string
  afterId: number
  firstRequest?: OnboardingEvent
  firstCacheHit?: OnboardingEvent
}

export function startOnboardingSession(response: OnboardingStatusResponse): OnboardingSession {
  return {
    startedAt: response.started_at,
    afterId: response.next_after_id,
  }
}

export function advanceOnboardingSession(
  session: OnboardingSession,
  response: OnboardingStatusResponse,
): OnboardingSession {
  const firstRequest = session.firstRequest ?? response.events[0]
  const firstCacheHit = session.firstCacheHit ?? response.events.find(event => event.outcome === 'hit')
  return {
    ...session,
    afterId: Math.max(session.afterId, response.next_after_id),
    firstRequest,
    firstCacheHit,
  }
}

export function parseOnboardingSession(value: string | null): OnboardingSession | null {
  if (!value) return null
  try {
    const candidate: unknown = JSON.parse(value)
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) return null
    const session = candidate as Partial<OnboardingSession>
    if (typeof session.startedAt !== 'string' || !session.startedAt ||
        typeof session.afterId !== 'number' || !Number.isSafeInteger(session.afterId) || session.afterId < 0) {
      return null
    }
    return session as OnboardingSession
  } catch {
    return null
  }
}

export function onboardingOutcomeKind(event: OnboardingEvent): 'hit' | 'miss' | 'blocked' | 'error' {
  if (event.outcome === 'hit') return 'hit'
  if (event.outcome === 'blocked') return 'blocked'
  if (event.outcome === 'error' || event.status_code >= 500) return 'error'
  return 'miss'
}
