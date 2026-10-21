import type { OnboardingDeliveryEvent, OnboardingEvent, OnboardingStatusResponse } from './adminApi.types'
import { getClientSelection } from './clientConnection'

export const ONBOARDING_SESSION_KEY = 'depsilo.admin.onboarding-session.v2'
export const ONBOARDING_GATE_QUERY_KEY = ['admin', 'onboarding', 'gate'] as const

export interface OnboardingScope {
  languageId: string
  managerId: string
  ecosystem: string
  targetPackage?: string
  targetVersion?: string
  configurationScope?: string
  shell?: string
}

export interface OnboardingSession extends OnboardingScope {
  schemaVersion: 2
  startedAt: string
  afterId: number
  /** Opaque backend cursor; empty only when delivery evidence is unsupported. */
  deliveryAfterId: string
  firstRequest?: OnboardingEvent
  latestRequest?: OnboardingEvent
  latestDelivery?: OnboardingDeliveryEvent
  successfulDownload?: OnboardingDeliveryEvent
  firstCacheHit?: OnboardingDeliveryEvent
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value))
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0
}

function validTimestamp(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d\d-\d\dT/.test(value) && Number.isFinite(Date.parse(value))
}

function validID(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function validDeliveryCursor(value: unknown): value is string {
  return nonEmptyString(value) && value.length <= 4096
}

function validScope(value: Record<string, unknown>): value is Record<string, unknown> & OnboardingScope {
  if (!nonEmptyString(value.languageId) || !nonEmptyString(value.managerId) || !nonEmptyString(value.ecosystem)) return false
  const selection = getClientSelection(value.languageId, value.managerId)
  if (!selection || selection.ecosystem !== value.ecosystem) return false
  return ['targetPackage', 'targetVersion', 'configurationScope', 'shell'].every(
    key => value[key] === undefined || nonEmptyString(value[key]),
  ) && (value.targetVersion === undefined || value.targetPackage !== undefined)
}

function validEvent(value: unknown): value is OnboardingEvent {
  if (!isRecord(value)) return false
  return validID(value.id) && value.id > 0 &&
    nonEmptyString(value.ecosystem) && nonEmptyString(value.package_name) &&
    typeof value.version === 'string' && typeof value.outcome === 'string' &&
    typeof value.status_code === 'number' && Number.isInteger(value.status_code) &&
    value.status_code >= 100 && value.status_code <= 599 &&
    validTimestamp(value.created_at) &&
    (value.request_id === undefined || nonEmptyString(value.request_id)) &&
    (value.action === undefined || value.action === 'metadata' || value.action === 'download')
}

function validDelivery(value: unknown): value is OnboardingDeliveryEvent {
  if (!validEvent(value) || !isRecord(value)) return false
  return nonEmptyString(value.request_id) && typeof value.cache_key === 'string' &&
    nonEmptyString(value.method) && nonEmptyString(value.dependency_kind) && nonEmptyString(value.delivery_result) &&
    typeof value.bytes_sent === 'number' && Number.isFinite(value.bytes_sent) && value.bytes_sent >= 0 &&
    typeof value.successful_artifact_download === 'boolean'
}

export function isSuccessfulOnboardingDownload(value: unknown): value is OnboardingDeliveryEvent {
  if (!validDelivery(value)) return false
  return value.successful_artifact_download === true && nonEmptyString(value.cache_key) &&
    value.dependency_kind === 'artifact' && value.method === 'GET' &&
    value.status_code >= 200 && value.status_code < 300 &&
    value.bytes_sent > 0 &&
    value.delivery_result === 'completed' && (value.outcome === 'hit' || value.outcome === 'miss')
}

function matchesScope(event: OnboardingEvent, scope: OnboardingScope, allowUnknownVersion = false): boolean {
  return event.ecosystem === scope.ecosystem &&
    (!scope.targetPackage || event.package_name === scope.targetPackage) &&
    (!scope.targetVersion || event.version === scope.targetVersion || (allowUnknownVersion && event.version === ''))
}

function withinSession(event: OnboardingEvent, session: OnboardingSession, allowUnknownVersion = false): boolean {
  return Date.parse(event.created_at) >= Date.parse(session.startedAt) && matchesScope(event, session, allowUnknownVersion)
}

export function startOnboardingSession(response: OnboardingStatusResponse, scope: OnboardingScope): OnboardingSession {
  if (!validTimestamp(response.started_at) || !validID(response.next_after_id) || !validScope({ ...scope })) {
    throw new Error('Invalid client connection baseline')
  }
  return {
    ...scope,
    schemaVersion: 2,
    startedAt: response.started_at,
    afterId: response.next_after_id,
    deliveryAfterId: validDeliveryCursor(response.next_delivery_after_id) ? response.next_delivery_after_id : '',
  }
}

export function advanceOnboardingSession(
  session: OnboardingSession,
  response: OnboardingStatusResponse,
): OnboardingSession {
  // A response from a previous selection or session cannot advance this one.
  if (!validTimestamp(response.started_at) || response.started_at !== session.startedAt ||
      !validID(response.next_after_id) || response.next_after_id < session.afterId) return session

  const events = (Array.isArray(response.events) ? response.events : [])
    .filter(event => validEvent(event) && event.id > session.afterId && event.id <= response.next_after_id && withinSession(event, session, true))
    .sort((left, right) => left.id - right.id)
  const canReadDeliveries = validDeliveryCursor(session.deliveryAfterId) && validDeliveryCursor(response.next_delivery_after_id)
  const deliveries = (canReadDeliveries && Array.isArray(response.delivery_events) ? response.delivery_events : [])
    .filter(event => validDelivery(event) && withinSession(event, session, true))
    .sort((left, right) => left.id - right.id)
  const successes = deliveries.filter(event => isSuccessfulOnboardingDownload(event) && withinSession(event, session))
  const successfulDownload = session.successfulDownload ?? successes[0]
  const firstCacheHit = session.firstCacheHit ?? successes.find(
    event => event.outcome === 'hit' && event.cache_key === successfulDownload?.cache_key,
  )
  const latestDelivery = [session.latestDelivery, ...deliveries]
    .filter((event): event is OnboardingDeliveryEvent => event !== undefined)
    .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at) || left.id - right.id)
    .at(-1)
  const latestRequest = [session.latestRequest, ...events]
    .filter((event): event is OnboardingEvent => event !== undefined)
    .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at) || left.id - right.id)
    .at(-1)
  return {
    ...session,
    afterId: response.next_after_id,
    deliveryAfterId: validDeliveryCursor(response.next_delivery_after_id) ? response.next_delivery_after_id : session.deliveryAfterId,
    firstRequest: session.firstRequest ?? events[0],
    latestRequest,
    latestDelivery,
    successfulDownload,
    firstCacheHit,
  }
}

export function parseOnboardingSession(value: string | null): OnboardingSession | null {
  if (!value) return null
  try {
    const candidate: unknown = JSON.parse(value)
    if (!isRecord(candidate) || candidate.schemaVersion !== 2 || !validScope(candidate) ||
        !validTimestamp(candidate.startedAt) || !validID(candidate.afterId) ||
        (candidate.deliveryAfterId !== '' && !validDeliveryCursor(candidate.deliveryAfterId))) return null
    const session = candidate as unknown as OnboardingSession
    for (const event of [session.firstRequest, session.latestRequest]) {
      if (event !== undefined && (!validEvent(event) || !withinSession(event, session, true) || event.id > session.afterId)) return null
    }
    if (session.firstRequest && !session.latestRequest) return null
    if (session.latestRequest && !session.firstRequest) return null
    if (session.firstRequest && session.latestRequest && Date.parse(session.latestRequest.created_at) < Date.parse(session.firstRequest.created_at)) return null
    if (session.latestDelivery !== undefined && (!validDelivery(session.latestDelivery) ||
        !withinSession(session.latestDelivery, session, true) || !validDeliveryCursor(session.deliveryAfterId))) return null
    for (const event of [session.successfulDownload, session.firstCacheHit]) {
      if (event !== undefined && (!isSuccessfulOnboardingDownload(event) || !withinSession(event, session) || !validDeliveryCursor(session.deliveryAfterId))) return null
    }
    if (session.firstCacheHit && (!session.successfulDownload || session.firstCacheHit.outcome !== 'hit' ||
        session.firstCacheHit.cache_key !== session.successfulDownload.cache_key)) return null
    return session
  } catch {
    return null
  }
}

/** Request outcome alone describes arrival, never successful artifact delivery. */
export function onboardingOutcomeKind(event: OnboardingEvent): 'hit' | 'miss' | 'blocked' | 'error' {
  if (event.outcome === 'blocked') return 'blocked'
  if (event.outcome === 'error' || event.status_code >= 400) return 'error'
  if (event.outcome === 'hit') return 'hit'
  return 'miss'
}
