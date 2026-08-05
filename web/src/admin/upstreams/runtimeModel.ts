import type {
  AdminUpstream,
  AdminUpstreamListResponse,
  CheckUpstreamResponse,
} from '../../lib/adminApi.types'
import { standardUpstreamEcosystems } from '../operatorEcosystems'

const ecosystemRank = new Map<string, number>(
  standardUpstreamEcosystems.map((ecosystem, index) => [ecosystem.id, index] as const),
)

function sortUpstreams(items: AdminUpstream[]): AdminUpstream[] {
  return items.sort((left, right) => (
    (ecosystemRank.get(left.adapter_type) ?? Number.MAX_SAFE_INTEGER)
      - (ecosystemRank.get(right.adapter_type) ?? Number.MAX_SAFE_INTEGER)
    || left.priority - right.priority
    || left.id - right.id
  ))
}

export interface RuntimeCheckTicket {
  readonly upstreamId: number
  readonly generation: number
  readonly updatedAt: string
}

export interface RuntimeCheckSuccess {
  readonly status: 'fulfilled'
  readonly ticket: RuntimeCheckTicket
  readonly result: CheckUpstreamResponse
}

export interface RuntimeCheckFailure {
  readonly status: 'rejected'
  readonly ticket: RuntimeCheckTicket
  readonly reason: unknown
}

export type RuntimeCheckOutcome = RuntimeCheckSuccess | RuntimeCheckFailure

export interface CurrentCheckFailure {
  readonly upstreamId: number
  readonly reason: unknown
}

export interface RuntimeCheckReconciliation {
  readonly data: AdminUpstreamListResponse | undefined
  readonly accepted: readonly CheckUpstreamResponse[]
  readonly failures: readonly CurrentCheckFailure[]
}

export type RuntimeMutation =
  | {
      readonly kind: 'saved'
      readonly upstream: AdminUpstream
    }
  | {
      readonly kind: 'deleted'
      readonly upstreamId: number
    }

export interface UpstreamRuntimeModel {
  beginCheck(upstream: AdminUpstream): RuntimeCheckTicket
  applyMutation(
    current: AdminUpstreamListResponse | undefined,
    mutation: RuntimeMutation,
  ): AdminUpstreamListResponse
  applyChecks(
    current: AdminUpstreamListResponse | undefined,
    outcomes: readonly RuntimeCheckOutcome[],
  ): RuntimeCheckReconciliation
}

/**
 * Owns the concurrency rules between Upstream mutations and health checks.
 * Callers keep the server list in TanStack Query; this module owns only the
 * in-flight generation knowledge required to reject obsolete check results.
 */
export function createUpstreamRuntimeModel(): UpstreamRuntimeModel {
  const generations = new Map<number, number>()

  function isCurrent(
    current: AdminUpstreamListResponse | undefined,
    ticket: RuntimeCheckTicket,
  ): boolean {
    const upstream = current?.items.find(candidate => candidate.id === ticket.upstreamId)
    return Boolean(
      upstream
      && (generations.get(ticket.upstreamId) ?? 0) === ticket.generation
      && upstream.updated_at === ticket.updatedAt,
    )
  }

  return {
    beginCheck(upstream) {
      return {
        upstreamId: upstream.id,
        generation: generations.get(upstream.id) ?? 0,
        updatedAt: upstream.updated_at,
      }
    },

    applyMutation(current, mutation) {
      const upstreamId = mutation.kind === 'saved'
        ? mutation.upstream.id
        : mutation.upstreamId
      generations.set(
        upstreamId,
        (generations.get(upstreamId) ?? 0) + 1,
      )
      const items = current?.items ?? []
      if (mutation.kind === 'deleted') {
        const next = items.filter(item => item.id !== upstreamId)
        return { items: next, total: next.length }
      }
      const exists = items.some(item => item.id === mutation.upstream.id)
      const next = exists
        ? items.map(item => item.id === mutation.upstream.id ? mutation.upstream : item)
        : [...items, mutation.upstream]
      sortUpstreams(next)
      return { items: next, total: next.length }
    },

    applyChecks(current, outcomes) {
      const currentOutcomes = outcomes.filter(outcome => isCurrent(current, outcome.ticket))
      const accepted = currentOutcomes.flatMap(outcome => (
        outcome.status === 'fulfilled' ? [outcome.result] : []
      ))
      const failures = currentOutcomes.flatMap(outcome => (
        outcome.status === 'rejected'
          ? [{ upstreamId: outcome.ticket.upstreamId, reason: outcome.reason }]
          : []
      ))
      if (!current) return { data: current, accepted, failures }

      const replacements = new Map(accepted.map(result => [result.upstream.id, result.upstream]))
      const items = current.items.map(item => replacements.get(item.id) ?? item)
      sortUpstreams(items)
      return {
        data: { items, total: items.length },
        accepted,
        failures,
      }
    },
  }
}
