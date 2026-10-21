import { describe, expect, it } from 'vitest'

import type { OnboardingDeliveryEvent, OnboardingEvent, OnboardingStatusResponse } from '../src/lib/adminApi.types'
import {
  advanceOnboardingSession,
  isSuccessfulOnboardingDownload,
  ONBOARDING_SESSION_KEY,
  onboardingOutcomeKind,
  parseOnboardingSession,
  startOnboardingSession,
  type OnboardingScope,
} from '../src/lib/onboarding'

const scope: OnboardingScope = {
  languageId: 'python',
  managerId: 'pip',
  ecosystem: 'pypi',
  targetPackage: 'six',
  targetVersion: '1.17.0',
  configurationScope: 'user',
  shell: 'posix',
}

const baseline: OnboardingStatusResponse = {
  status: 'not_started',
  started_at: '2026-08-20T00:00:00Z',
  next_after_id: 41,
  events: [],
  has_more: false,
  next_delivery_after_id: 'opaque-baseline',
  delivery_events: [],
  deliveries_has_more: false,
}

function request(overrides: Partial<OnboardingEvent> = {}): OnboardingEvent {
  return {
    id: 42,
    request_id: 'request-a',
    action: 'metadata',
    ecosystem: 'pypi',
    package_name: 'six',
    version: '',
    outcome: 'miss',
    status_code: 200,
    created_at: '2026-08-20T00:00:01Z',
    ...overrides,
  }
}

function delivery(overrides: Partial<OnboardingDeliveryEvent> = {}): OnboardingDeliveryEvent {
  return {
    ...request(),
    id: 501,
    request_id: 'request-a',
    action: 'download',
    version: '1.17.0',
    cache_key: 'pypi/six-1.17.0.whl',
    method: 'GET',
    dependency_kind: 'artifact',
    delivery_result: 'completed',
    bytes_sent: 11050,
    successful_artifact_download: true,
    ...overrides,
  }
}

function poll(overrides: Partial<OnboardingStatusResponse> = {}): OnboardingStatusResponse {
  return {
    ...baseline,
    next_after_id: 50,
    next_delivery_after_id: 'opaque-next',
    ...overrides,
  }
}

describe('scoped connection evidence', () => {
  it('starts from both server cursors with selection, ignoring baseline history', () => {
    const initial = startOnboardingSession({ ...baseline, events: [request()], delivery_events: [delivery()] }, scope)
    expect(initial).toEqual({
      ...scope,
      schemaVersion: 2,
      startedAt: baseline.started_at,
      afterId: 41,
      deliveryAfterId: 'opaque-baseline',
    })
  })

  it('retains arrival and latest failure without completing a download from metadata HITs', () => {
    const metadataHit = request({ outcome: 'hit' })
    const blocked = request({ id: 43, outcome: 'blocked', status_code: 403 })
    const error = request({ id: 44, outcome: 'error', status_code: 502 })
    const next = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({
      events: [metadataHit, blocked, error],
      delivery_events: [delivery({ dependency_kind: 'metadata', outcome: 'hit' })],
    }))
    expect(next.firstRequest).toEqual(metadataHit)
    expect(next.latestRequest).toEqual(error)
    expect(next.successfulDownload).toBeUndefined()
    expect(next.firstCacheHit).toBeUndefined()
    expect(onboardingOutcomeKind(blocked)).toBe('blocked')
    expect(onboardingOutcomeKind(error)).toBe('error')
    expect(onboardingOutcomeKind(request({ status_code: 404 }))).toBe('error')
  })

  it('ignores other ecosystems, packages, versions and pre-session evidence while advancing both cursors', () => {
    const invalid = [
      { ecosystem: 'npm' },
      { package_name: 'requests' },
      { version: '1.16.0' },
      { created_at: '2026-08-19T23:59:59Z' },
    ]
    const next = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({
      events: invalid.map((overrides, index) => request({ ...overrides, id: 42 + index })),
      delivery_events: invalid.map((overrides, index) => delivery({ ...overrides, id: 501 + index })),
    }))
    expect(next.firstRequest).toBeUndefined()
    expect(next.successfulDownload).toBeUndefined()
    expect(next.afterId).toBe(50)
    expect(next.deliveryAfterId).toBe('opaque-next')
  })

  it('requires a successful HIT of the same artifact after a successful download', () => {
    const miss = delivery()
    const wrongArtifactHit = delivery({ id: 502, outcome: 'hit', cache_key: 'pypi/six-1.17.0.tar.gz' })
    const first = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({
      events: [request()],
      delivery_events: [miss, wrongArtifactHit],
    }))
    expect(first.successfulDownload).toEqual(miss)
    expect(first.firstCacheHit).toBeUndefined()

    const hit = delivery({ id: 503, outcome: 'hit' })
    const second = advanceOnboardingSession(first, poll({ delivery_events: [hit], next_delivery_after_id: 'opaque-final' }))
    expect(second.firstRequest).toEqual(first.firstRequest)
    expect(second.successfulDownload).toEqual(miss)
    expect(second.firstCacheHit).toEqual(hit)
    expect(second.deliveryAfterId).toBe('opaque-final')
  })

  it('accepts a fully delivered artifact HIT on the first download', () => {
    const hit = delivery({ outcome: 'hit' })
    const next = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({ delivery_events: [hit] }))
    expect(next.successfulDownload).toEqual(hit)
    expect(next.firstCacheHit).toEqual(hit)
  })

  it('retains an interrupted delivery as diagnostic evidence without claiming success', () => {
    const cancelled = delivery({ delivery_result: 'cancelled', successful_artifact_download: false })
    const next = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({
      events: [request({ outcome: 'miss' })],
      delivery_events: [cancelled],
    }))
    expect(next.latestDelivery).toEqual(cancelled)
    expect(next.successfulDownload).toBeUndefined()
    expect(parseOnboardingSession(JSON.stringify(next))).toEqual(next)
  })

  it('keeps the most recent diagnostic when a delayed older delivery arrives', () => {
    const success = delivery({ id: 502, created_at: '2026-08-20T00:00:03Z' })
    const first = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({ delivery_events: [success] }))
    const second = advanceOnboardingSession(first, poll({
      delivery_events: [delivery({ delivery_result: 'failed', successful_artifact_download: false })],
    }))
    expect(second.latestDelivery).toEqual(success)
    expect(second.successfulDownload).toEqual(success)
  })

  it('keeps a newer blocked request when an older request is committed to the audit log later', () => {
    const blocked = request({ id: 42, outcome: 'blocked', status_code: 403, created_at: '2026-08-20T00:00:03Z' })
    const first = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({ next_after_id: 42, events: [blocked] }))
    const second = advanceOnboardingSession(first, poll({ events: [request({ id: 43 })] }))
    expect(second.firstRequest).toEqual(blocked)
    expect(second.latestRequest).toEqual(blocked)
    expect(second.afterId).toBe(50)
    expect(parseOnboardingSession(JSON.stringify(second))).toEqual(second)
  })

  it.each([
    { successful_artifact_download: false },
    { dependency_kind: 'metadata' },
    { dependency_kind: 'unknown' },
    { method: 'HEAD' },
    { delivery_result: 'cancelled' },
    { delivery_result: 'failed' },
    { delivery_result: 'unknown' },
    { bytes_sent: 0 },
    { bytes_sent: Number.NaN },
    { status_code: 304 },
    { status_code: 403, outcome: 'blocked' },
    { status_code: 502, outcome: 'hit' },
    { cache_key: '' },
    { request_id: '' },
  ])('requires complete delivery facts even when backend success is asserted: %j', overrides => {
    const event = delivery(overrides)
    expect(isSuccessfulOnboardingDownload(event)).toBe(false)
    const next = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({ delivery_events: [event] }))
    expect(next.successfulDownload).toBeUndefined()
    expect(next.firstCacheHit).toBeUndefined()
  })

  it('does not replay old audit rows or responses belonging to another session', () => {
    const session = startOnboardingSession(baseline, scope)
    const next = advanceOnboardingSession(session, poll({ events: [request({ id: 41 })] }))
    expect(next.firstRequest).toBeUndefined()
    expect(advanceOnboardingSession(next, poll({ next_after_id: 49, delivery_events: [delivery()] }))).toBe(next)
    expect(advanceOnboardingSession(next, poll({
      started_at: '2026-08-20T01:00:00Z',
      delivery_events: [delivery()],
    }))).toBe(next)
    expect(advanceOnboardingSession(next, poll({
      started_at: '2026-08-20T00:00:00.000001Z',
      delivery_events: [delivery()],
    }))).toBe(next)
  })

  it('treats a legacy response as arrival only, never as verified cache delivery', () => {
    const legacy = { ...baseline, next_delivery_after_id: undefined, delivery_events: undefined }
    const session = startOnboardingSession(legacy, scope)
    const next = advanceOnboardingSession(session, {
      ...legacy,
      next_after_id: 42,
      events: [request({ outcome: 'hit' })],
    })
    expect(next.deliveryAfterId).toBe('')
    expect(next.firstRequest?.outcome).toBe('hit')
    expect(next.successfulDownload).toBeUndefined()
    expect(next.firstCacheHit).toBeUndefined()
  })

  it('requires an established delivery baseline before consuming delivery events', () => {
    const session = startOnboardingSession({ ...baseline, next_delivery_after_id: undefined }, scope)
    const next = advanceOnboardingSession(session, poll({ delivery_events: [delivery()] }))
    expect(next.successfulDownload).toBeUndefined()
    expect(next.deliveryAfterId).toBe('opaque-next')
  })
})

describe('connection session restoration', () => {
  it('restores the same client selection, configuration scope and independent evidence', () => {
    const session = advanceOnboardingSession(startOnboardingSession(baseline, scope), poll({
      events: [request()],
      delivery_events: [delivery({ outcome: 'hit' })],
    }))
    expect(ONBOARDING_SESSION_KEY).toContain('.v2')
    expect(parseOnboardingSession(JSON.stringify(session))).toEqual(session)
  })

  it('validates manager-specific canonical ecosystems', () => {
    const conda = startOnboardingSession(baseline, { languageId: 'python', managerId: 'conda', ecosystem: 'conda' })
    expect(parseOnboardingSession(JSON.stringify(conda))).toEqual(conda)
    expect(parseOnboardingSession(JSON.stringify({ ...conda, ecosystem: 'pypi' }))).toBeNull()
  })

  it.each([
    { schemaVersion: 1 },
    { startedAt: 'invalid-date' },
    { afterId: -1 },
    { afterId: 0.5 },
    { deliveryAfterId: 501 },
    { languageId: 'imaginary' },
    { managerId: 'npm' },
    { ecosystem: 'npm' },
    { configurationScope: '' },
    { shell: false },
    { firstRequest: request({ id: 500 }), latestRequest: request({ id: 500 }) },
    { firstRequest: request({ ecosystem: 'npm' }), latestRequest: request({ ecosystem: 'npm' }) },
    { successfulDownload: delivery({ delivery_result: 'cancelled' }) },
    { successfulDownload: delivery({ version: '1.16.0' }) },
    { firstCacheHit: delivery({ outcome: 'hit' }) },
    { successfulDownload: delivery(), firstCacheHit: delivery({ cache_key: 'another-artifact', outcome: 'hit' }) },
  ])('rejects incompatible or inconsistent persisted state: %j', invalid => {
    const session = { ...startOnboardingSession(baseline, scope), afterId: 50, ...invalid }
    expect(parseOnboardingSession(JSON.stringify(session))).toBeNull()
  })

  it('rejects malformed JSON and pre-v2 sessions', () => {
    expect(parseOnboardingSession('{broken')).toBeNull()
    expect(parseOnboardingSession('{"startedAt":"2026-08-20T00:00:00Z","afterId":41}')).toBeNull()
  })
})
