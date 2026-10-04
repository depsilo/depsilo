import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { adminApi } from '@/lib/api'
import type { PolicyStatus } from '@/lib/adminApi.types'
import { formatTime } from '@/lib/utils'

export interface PolicyStatusSignal {
  status?: PolicyStatus
  /** The status probe itself failed, so nothing is confirmed either way. */
  unavailable: boolean
  /** The engine is serving a known snapshot whose refresh failed. */
  isStale: boolean
  /** The engine reports a failure without a usable last-known-good snapshot. */
  isUnverified: boolean
  needsAttention: boolean
  /** Pre-formatted relative age of the last good snapshot, when one exists. */
  refreshTime: string | null
  refreshing: boolean
  refresh: () => void
}

function formatSnapshotAge(seconds: number, language: string): string {
  if (!Number.isFinite(seconds) || seconds < 0) return ''

  const age = Math.round(seconds)
  const locale = language.startsWith('zh') ? 'zh-CN' : 'en-US'
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'always' })
  if (age < 60) return relative.format(-age, 'second')
  const minutes = Math.round(age / 60)
  if (minutes < 60) return relative.format(-minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (hours < 24) return relative.format(-hours, 'hour')
  return relative.format(-Math.round(hours / 24), 'day')
}

/**
 * Shared read of the policy-runtime status. It has no route awareness: mount
 * it only on the surfaces that own policy context (Overview and the Security
 * workspace) so unrelated pages never probe or reserve space for it.
 */
export function usePolicyStatus(): PolicyStatusSignal {
  const { i18n } = useTranslation()
  const query = useQuery<PolicyStatus>({
    queryKey: ['admin', 'policy', 'status'],
    queryFn: async ({ signal }) => (await adminApi.getPolicyStatus({ signal })).data,
    refetchInterval: 30_000,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    retry: false,
  })

  const status = query.data
  // `degraded` also describes the no-last-known-good case. Only call the
  // snapshot message when the engine explicitly says that an old snapshot is
  // being used; otherwise operators must not be told that a snapshot exists
  // when the first policy load has never succeeded.
  const isStale = status?.using_stale_snapshot === true
  const unavailable = query.isError
  const isUnverified = unavailable || (
    status !== undefined
    && status.status !== 'healthy'
    && status.status !== 'ready'
    && !isStale
  )
  const snapshotLoadedAt = status?.snapshot_loaded_at ?? status?.last_successful_refresh
  const refreshTime = snapshotLoadedAt
    ? (formatSnapshotAge(status?.snapshot_age_seconds ?? Number.NaN, i18n.language)
      || formatTime(snapshotLoadedAt, 'relative', i18n.language))
    : null

  return {
    status,
    unavailable,
    isStale,
    isUnverified,
    needsAttention: isStale || isUnverified,
    refreshTime,
    refreshing: query.isFetching,
    refresh: () => { void query.refetch() },
  }
}
