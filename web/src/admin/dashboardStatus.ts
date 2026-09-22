import type { DashboardResponse, PolicyStatus } from '@/lib/adminApi.types'
import { upstreamStatus } from '@/lib/upstreamStatus'

export type ObservedStatus = 'healthy' | 'degraded' | 'unknown'
export type DashboardIssueCategory = 'upstream' | 'cache' | 'policy'
export type OverallStatus = 'healthy' | 'attention' | 'unknown'

export function dashboardStatus(snapshot: DashboardResponse | undefined, policy: PolicyStatus | undefined, freshness: {
  snapshotError: boolean
  policyError: boolean
  proxyResponding: boolean
}) {
  const upstreams = snapshot?.upstreams ?? []
  const attention = upstreams.filter(item => upstreamStatus(item) !== 'healthy')
  const upstreamHealth = snapshot ? {
    healthy: upstreams.filter(item => upstreamStatus(item) === 'healthy').length,
    reachable: upstreams.filter(item => upstreamStatus(item) !== 'failed').length,
    slow: upstreams.filter(item => upstreamStatus(item) === 'degraded').length,
    failed: upstreams.filter(item => upstreamStatus(item) === 'failed').length,
    total: upstreams.length,
  } : undefined
  const policyState: ObservedStatus = policy?.status === 'degraded' || policy?.degraded || policy?.using_stale_snapshot
    ? 'degraded'
    : !freshness.policyError && (policy?.status === 'healthy' || policy?.status === 'ready') ? 'healthy' : 'unknown'
  const issueCategories: DashboardIssueCategory[] = []
  if (attention.length) issueCategories.push('upstream')
  if ((snapshot?.cache_usage_percent ?? 0) > 80) issueCategories.push('cache')
  if ((policy || freshness.policyError) && policyState !== 'healthy') issueCategories.push('policy')
  const overall: OverallStatus = issueCategories.length > 0 ? 'attention'
    : !freshness.proxyResponding || freshness.snapshotError || freshness.policyError || !upstreamHealth?.total || policyState === 'unknown'
      ? 'unknown' : 'healthy'
  return { upstreamHealth, attention, policyState, overall, issueCategories }
}
