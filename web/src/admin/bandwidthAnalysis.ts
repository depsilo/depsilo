import { queryOptions } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import type { BandwidthReportResponse, BandwidthSummary } from '@/lib/adminApi.types'
import { getAdminRouteHref } from '@/admin/routes'

export const REPORT_RANGES = ['7d', '30d', '90d', 'custom'] as const
export type ReportRange = typeof REPORT_RANGES[number]
export type TrafficGroup = 'packages' | 'ecosystems' | 'upstreams'

export function reportRange(value: string | null): ReportRange {
  return REPORT_RANGES.find(range => range === value) ?? '7d'
}
export function trafficGroup(value: string | null): TrafficGroup {
  return value === 'ecosystems' || value === 'upstreams' ? value : 'packages'
}
export function reportParams(range: ReportRange, start: string, end: string) {
  return range === 'custom' ? { range, start, end } : { range }
}
export function validReportDates(start: string, end: string) {
  const valid = (date: string) => /^\d{4}-\d{2}-\d{2}$/.test(date)
    && Number.isFinite(Date.parse(date)) && new Date(date).toISOString().slice(0, 10) === date
  return valid(start) && valid(end) && start <= end
}

// Both entry points share query keys and cancellation. Reports refresh once
// per minute, independently of the five-second live operational snapshot.
export function bandwidthQuery(params: ReturnType<typeof reportParams>) {
  return queryOptions({
    queryKey: ['admin', 'bandwidth', params],
    queryFn: ({ signal }) => adminApi.getBandwidthReport(params, { signal }),
    enabled: params.range !== 'custom' || validReportDates(params.start ?? '', params.end ?? ''),
    staleTime: 60_000,
    refetchInterval: 60_000,
    retry: false,
  })
}

export function analysisHref(destination: 'dashboard' | 'bandwidth', search: URLSearchParams) {
  const params = new URLSearchParams(search)
  const source = destination === 'bandwidth' ? 'analysisRange' : 'range'
  const target = destination === 'bandwidth' ? 'range' : 'analysisRange'
  params.set(target, reportRange(params.get(source)))
  params.delete(source)
  return `${getAdminRouteHref(destination)}?${params}`
}

export function byteShare(bytes: number, total: number): number | null {
  return Number.isFinite(bytes) && Number.isFinite(total) && total > 0 && bytes >= 0 && bytes <= total
    ? bytes / total : null
}

export function trafficRows(report: BandwidthReportResponse, group: TrafficGroup) {
  const rows = group === 'packages'
    ? (report.top_packages ?? []).map(p => ({ key: `${p.ecosystem}:${p.package_name}`, name: p.package_name, ecosystem: p.ecosystem, bytes: p.total_bytes }))
    : group === 'ecosystems'
      ? (report.by_ecosystem ?? []).map(e => ({ key: e.ecosystem, name: e.ecosystem, ecosystem: '', bytes: e.hit_bytes + e.miss_bytes }))
      : (report.by_upstream ?? []).map(u => ({ key: u.upstream, name: u.upstream, ecosystem: '', bytes: u.miss_bytes }))
  // Top packages and named upstreams are not complete populations. Percentages
  // use the report's full response-byte totals, never the visible five rows.
  const total = group === 'upstreams' ? report.summary.miss_bytes : report.summary.total_bytes
  return rows.sort((a, b) => b.bytes - a.bytes).map(row => ({ ...row, share: byteShare(row.bytes, total) }))
}

export function estimatedTimeSaved(summary: BandwidthSummary): number | null {
  return summary.hit_requests > 0 && summary.miss_requests > 0
    && summary.avg_miss_latency > 0 && Number.isFinite(summary.time_saved_ms)
    ? summary.time_saved_ms : null
}
