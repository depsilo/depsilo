import { describe, expect, it } from 'vitest'

import type { OnboardingStatusResponse } from '../src/lib/adminApi.types'
import { LANGUAGES } from '../src/lib/ecosystemData'
import {
  advanceOnboardingSession,
  onboardingOutcomeKind,
  parseOnboardingSession,
  startOnboardingSession,
} from '../src/lib/onboarding'

const baseline: OnboardingStatusResponse = {
  status: 'not_started',
  started_at: '2026-08-20T00:00:00Z',
  next_after_id: 41,
  events: [],
  has_more: false,
}

describe('onboarding cursor', () => {
  it('starts after the backend baseline and ignores historical state', () => {
    expect(startOnboardingSession(baseline)).toEqual({ startedAt: baseline.started_at, afterId: 41 })
  })

  it('records the first handled request and only a real hit as cache milestone', () => {
    const blocked = { id: 42, ecosystem: 'pypi', package_name: 'six', version: '', outcome: 'blocked', status_code: 403, created_at: '2026-08-20T00:00:01Z' }
    const miss = { ...blocked, id: 43, package_name: 'requests', outcome: 'miss', status_code: 200 }
    const hit = { ...miss, id: 44, outcome: 'hit' }
    const first = advanceOnboardingSession(startOnboardingSession(baseline), { ...baseline, next_after_id: 43, events: [blocked, miss] })
    const second = advanceOnboardingSession(first, { ...baseline, next_after_id: 44, events: [hit] })

    expect(first.firstRequest).toEqual(blocked)
    expect(first.firstCacheHit).toBeUndefined()
    expect(second.firstRequest).toEqual(blocked)
    expect(second.firstCacheHit).toEqual(hit)
    expect(onboardingOutcomeKind(blocked)).toBe('blocked')
  })

  it('rejects malformed session storage', () => {
    expect(parseOnboardingSession('{"startedAt":"x","afterId":-1}')).toBeNull()
    expect(parseOnboardingSession('{broken')).toBeNull()
  })
})

describe('onboarding test commands', () => {
  it('does not fall back to configuration, project-mutating, or heavy quick commands', () => {
    const manager = (language: string, id: string) => LANGUAGES
      .find(candidate => candidate.id === language)?.managers.find(candidate => candidate.id === id)

    expect(manager('node', 'yarn')?.test).toBeUndefined()
    expect(manager('debian', 'apt')?.test).toBeUndefined()
    expect(manager('huggingface', 'hf-cli')?.test).toBeUndefined()
    expect(manager('python', 'pip')?.test?.body).toContain('--dry-run')
    expect(manager('node', 'npm')?.test?.body).toContain('npm view')
  })
})
