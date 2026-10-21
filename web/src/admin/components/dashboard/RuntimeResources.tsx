import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { Meter, MeterIndicator, MeterTrack } from '@/components/ui/meter'
import { Skeleton } from '@/components/ui/skeleton'
import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { useSampleSeries } from '@/hooks/useSampleSeries'
import { formatBytes } from '@/lib/utils'

import MetricTile from './MetricTile'
import DashboardMetricHelp from './DashboardMetricHelp'

interface RuntimeResourcesProps {
  runtime?: RuntimeResponse
  runtimePending: boolean
  now?: NowResponse
  nowPending: boolean
  nowError?: boolean
  nowStale?: boolean
}

// Platform diagnostics belong in the hint rather than untranslated tile copy.
function capabilityHint(base: string, capability: { supported: boolean; reason?: string } | undefined): string {
  if (!capability || capability.supported || !capability.reason) return base
  return `${base} · ${capability.reason}`
}

/** Current cache inventory is separate from both process resources and the
 * selected-period traffic. A failed initial inventory must not read as 0 B. */
export function CacheStorageOverview({ runtime, runtimePending }: Pick<RuntimeResourcesProps, 'runtime' | 'runtimePending'>) {
  const { t } = useTranslation()
  const cache = runtime?.cache
  const inventoryKnown = cache?.packages != null && cache.entries != null
  const quota = cache?.quota_bytes
  const cacheRatio = inventoryKnown && quota != null && quota > 0 ? cache.logical_bytes / quota : null
  const inventoryDetail = inventoryKnown
    ? t('overview.cacheInventory', { packages: cache.packages!.toLocaleString(), entries: cache.entries!.toLocaleString() })
    : cache ? t('overview.cacheInventoryUnavailable') : undefined
  const quotaDetail = cache
    ? quota != null && quota > 0
      ? t('overview.cacheQuota', { value: formatBytes(quota) })
      : t(cache.storage_type === 's3' ? 'overview.storageS3' : 'overview.cacheNoQuota')
    : undefined
  const loading = runtimePending && !runtime
  const ratio = cacheRatio === null ? null : Math.min(1, Math.max(0, cacheRatio))

  return (
    <section
      data-dashboard-metric
      data-testid="resource-cache"
      aria-label={t('overview.cacheLabel')}
      aria-busy={loading}
      className="flex min-w-0 items-center gap-2"
      style={{ flex: '1 1 480px' }}
    >
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <h2 className="inline-flex items-center gap-2 text-[13px] font-medium text-[var(--dash-muted)]">
            <Icon name="storage" size="sm" />
            <DashboardMetricHelp label={t('overview.cacheLabel')} ariaLabel={t('overview.cacheInfoLabel')}>
              <p>{t('overview.hintCache')}</p>
              <p>{t('overview.hintCacheInventory')}</p>
            </DashboardMetricHelp>
          </h2>
          <div data-cache-capacity-group className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
            {loading
              ? <Skeleton className="h-[26px] w-40" />
              : <p className="flex flex-wrap items-baseline gap-1.5 tabular-nums">
                <span className="dash-metric-value whitespace-nowrap font-semibold leading-tight text-[var(--dash-ink)]" style={{ fontSize: '20px' }}>{inventoryKnown ? formatBytes(cache.logical_bytes) : '—'}</span>
                {quota != null && quota > 0 && <span className="whitespace-nowrap text-[13px] text-[var(--dash-muted)]" aria-label={t('overview.cacheQuota', { value: formatBytes(quota) })}>/ {formatBytes(quota)}</span>}
              </p>}
            {ratio !== null && <div className="flex items-center gap-2">
              <Meter
                aria-label={t('overview.cacheLabel')}
                value={ratio * 100}
                aria-valuetext={t('overview.cacheUsagePercentage', { percent: (cacheRatio! * 100).toFixed(1) })}
                data-progress-ratio={ratio}
                className="w-24"
              >
                <MeterTrack><MeterIndicator /></MeterTrack>
              </Meter>
              <span className="text-[12px] tabular-nums text-[var(--dash-muted)]">{(cacheRatio! * 100).toFixed(1)}%</span>
            </div>}
          </div>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] leading-relaxed text-[var(--dash-muted)]">
          {loading ? <><span className="sr-only">{t('loading')}</span><Skeleton className="h-4 w-40 max-w-full" /></> : <>
            {inventoryDetail && <p>{inventoryDetail}</p>}
            {(quota == null || quota <= 0) && quotaDetail && <p>{quotaDetail}</p>}
          </>}
        </div>
      </div>
    </section>
  )
}

export default function RuntimeResources({
  runtime,
  runtimePending,
  now,
  nowPending,
  nowError = false,
  nowStale = false,
}: RuntimeResourcesProps) {
  const { t } = useTranslation()
  const process = runtime?.process
  const sampledAt = runtime?.sampled_at
  const cpuPercent = process?.cpu_percent
  const cpuSeries = useSampleSeries(cpuPercent ?? null, sampledAt, 48)
  const rssSeries = useSampleSeries(process?.rss_bytes ?? null, sampledAt, 48)
  const cpuValue = cpuPercent === undefined ? '—' : cpuPercent.toFixed(1)
  const cpuDetail = process?.cpu.supported === false
    ? t('overview.notSupported')
    : runtimePending && cpuPercent === undefined ? t('loading') : undefined
  const cpuHint = process?.cpu.supported === false
    ? capabilityHint(t('overview.hintCpu'), process?.cpu)
    : undefined

  // macOS peak RSS and Go-runtime fallback keep their existing explicit basis.
  const memorySupported = process?.memory.supported === true && process.rss_bytes !== undefined
  const memoryValue = memorySupported
    ? formatBytes(process.rss_bytes as number)
    : (runtime ? formatBytes(runtime.go_runtime.sys_bytes) : '—')
  const memoryBadge = memorySupported
    ? (process?.rss_basis === 'peak' ? t('overview.memoryPeakBadge') : undefined)
    : (runtime ? t('overview.memoryRuntimeBadge') : undefined)

  const measured = now?.rate.measured === true
  const networkSeries = now?.sparkline?.map(point => point.requests) ?? []
  const networkPending = nowPending && !now
  const networkDetail = networkPending
    ? t('loading')
    : nowError && !now
      ? t('overview.flowRateUnavailable')
      : measured ? t('overview.perSecond') : t('overview.notCollected')

  return (
    <div
      data-dashboard-resources
      data-dashboard-kpis
      role="group"
      aria-label={t('overview.resourcesTitle')}
      className="grid min-w-0 grid-cols-1 gap-y-2 sm:grid-cols-2 xl:grid-cols-3"
    >
      <MetricTile
        testId="resource-cpu"
        framed={false}
        compact
        label={t('overview.cpuLabel')}
        icon="memory"
        tone="default"
        value={cpuValue}
        unit="%"
        detail={cpuDetail}
        series={cpuSeries}
        loading={runtimePending && cpuPercent === undefined}
        info={cpuHint}
        infoLabel={cpuHint ? t('overview.cpuInfoLabel') : undefined}
      />
      <MetricTile
        testId="resource-memory"
        framed={false}
        compact
        label={t('overview.memoryLabel')}
        icon="ram"
        tone="default"
        value={memoryValue}
        badge={memoryBadge}
        series={memorySupported ? rssSeries : undefined}
        loading={runtimePending && !runtime}
      />
      <MetricTile
        testId="resource-network"
        framed={false}
        compact
        label={t('overview.networkLabel')}
        icon="hub"
        tone="default"
        badge={nowStale ? t('overview.flowRateStale') : t('overview.rateWindow')}
        rows={[
          {
            label: t('overview.serviceRequests'),
            value: measured ? (now.rate.service_requests_per_sec ?? 0).toFixed(2) : '—',
            tone: 'accent',
          },
          {
            label: t('overview.originRequests'),
            value: measured ? (now.rate.origin_requests_per_sec ?? 0).toFixed(2) : '—',
            tone: 'default',
          },
        ]}
        series={measured ? networkSeries : undefined}
        detail={networkDetail}
        loading={networkPending}
      />
    </div>
  )
}
