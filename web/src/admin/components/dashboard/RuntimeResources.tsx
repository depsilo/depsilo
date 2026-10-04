import { useTranslation } from 'react-i18next'

import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { useSampleSeries } from '@/hooks/useSampleSeries'
import { formatBytes } from '@/lib/utils'

import MetricTile from './MetricTile'

export type ResourceInfoKind = 'cpu' | 'memory' | 'cache' | 'network'

interface RuntimeResourcesProps {
  runtime?: RuntimeResponse
  runtimePending: boolean
  now?: NowResponse
  nowPending: boolean
  onInfo: (kind: ResourceInfoKind) => void
}

function capabilityText(
  capability: { supported: boolean; reason?: string } | undefined,
  fallback: string,
): string {
  if (!capability) return fallback
  return capability.supported ? fallback : (capability.reason || fallback)
}

export default function RuntimeResources({
  runtime,
  runtimePending,
  now,
  nowPending,
  onInfo,
}: RuntimeResourcesProps) {
  const { t } = useTranslation()
  const process = runtime?.process
  const sampledAt = runtime?.sampled_at

  const cpuPercent = process?.cpu_percent
  const cpuSeries = useSampleSeries(cpuPercent ?? null, sampledAt)
  const rssSeries = useSampleSeries(process?.rss_bytes ?? null, sampledAt)
  const cacheSeries = useSampleSeries(runtime?.cache.logical_bytes ?? null, sampledAt)

  const cpuValue = cpuPercent === undefined
    ? (runtimePending ? '' : '—')
    : cpuPercent.toFixed(1)
  const cpuDetail = process?.cpu.supported === false
    ? capabilityText(process.cpu, t('overview.notSupported'))
    : t('overview.cpuBasis', { cores: process?.cpu_cores ?? 0 })
  // A single-core progress bar only makes sense up to one full core. Above
  // 100% the number carries the truth instead of a silently full bar.
  const cpuProgress = process?.cpu.supported === true && cpuPercent !== undefined && cpuPercent <= 100
    ? { ratio: cpuPercent / 100, tone: 'cpu' as const }
    : null

  const memorySupported = process?.memory.supported === true && process.rss_bytes !== undefined
  const limit = process?.memory_limit_bytes
  const used = process?.memory_used_bytes
  const memoryValue = memorySupported ? formatBytes(process.rss_bytes as number) : '—'
  const memoryDetail = memorySupported
    ? (limit !== undefined && used !== undefined
      ? t('overview.memoryWithLimit', { used: formatBytes(used), limit: formatBytes(limit) })
      : t(process?.rss_basis === 'peak' ? 'overview.memoryPeak' : 'overview.memoryProcess'))
    : (process?.memory.reason || t('overview.memoryUnsupported'))
  const memoryProgress = limit !== undefined && used !== undefined && limit > 0
    ? { ratio: used / limit, tone: 'memory' as const }
    : null

  const cache = runtime?.cache
  const quota = cache?.quota_bytes ?? null
  const logical = cache?.logical_bytes ?? 0
  const cacheDetailParts: string[] = []
  if (quota && quota > 0) {
    cacheDetailParts.push(t('overview.cacheQuota', { value: formatBytes(quota) }))
  } else {
    cacheDetailParts.push(t('overview.cacheNoQuota'))
  }
  if (runtime?.disk.supported) {
    cacheDetailParts.push(t('overview.diskFree', { free: formatBytes(runtime.disk.free_bytes ?? 0), total: formatBytes(runtime.disk.total_bytes ?? 0) }))
  } else if (runtime) {
    cacheDetailParts.push(cache?.storage_type === 's3' ? t('overview.storageS3') : t('overview.diskUnsupported'))
  }
  const cacheRatio = quota && quota > 0 ? logical / quota : null

  const measured = now?.rate.measured === true
  const serviceRate = now?.rate.service_requests_per_sec ?? 0
  const originRate = now?.rate.origin_requests_per_sec ?? 0
  // Real 1-minute request buckets over the last 30 minutes from /now.
  const networkSeries = now?.sparkline?.map(point => point.requests) ?? []

  return (
    <section data-dashboard-resources aria-labelledby="overview-resources-title" className="flex min-w-0 flex-col gap-3">
      <h2 id="overview-resources-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
        {t('overview.resourcesTitle')}
      </h2>
      <div data-dashboard-kpis className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <MetricTile
          testId="resource-cpu"
          label={t('overview.cpuLabel')}
          icon="memory"
          tone="cpu"
          value={cpuValue}
          unit="%"
          detail={runtimePending && cpuPercent === undefined ? t('overview.collecting') : cpuDetail}
          progress={cpuProgress}
          series={cpuSeries}
          loading={runtimePending && cpuPercent === undefined}
          onInfo={() => onInfo('cpu')}
          infoLabel={t('overview.cpuInfoLabel')}
        />
        <MetricTile
          testId="resource-memory"
          label={t('overview.memoryLabel')}
          icon="ram"
          tone="memory"
          value={memoryValue}
          detail={process?.memory.supported === false
            ? `${process.memory.reason} · ${t('overview.goRuntimeMemory', { value: formatBytes(runtime?.go_runtime.heap_alloc_bytes ?? 0) })}`
            : memoryDetail}
          progress={memoryProgress}
          series={memorySupported ? rssSeries : undefined}
          loading={runtimePending && !runtime}
          onInfo={() => onInfo('memory')}
          infoLabel={t('overview.memoryInfoLabel')}
        />
        <MetricTile
          testId="resource-cache"
          label={t('overview.cacheLabel')}
          icon="storage"
          tone="cache"
          value={cache ? formatBytes(logical) : '—'}
          detail={cacheDetailParts.join(' · ')}
          progress={cacheRatio !== null ? { ratio: cacheRatio, tone: 'cache' } : null}
          series={cache ? cacheSeries : undefined}
          loading={runtimePending && !runtime}
          onInfo={() => onInfo('cache')}
          infoLabel={t('overview.cacheInfoLabel')}
        />
        <MetricTile
          testId="resource-network"
          label={t('overview.networkLabel')}
          icon="hub"
          tone="memory"
          rows={[
            {
              label: t('overview.serviceRequests'),
              value: measured ? serviceRate.toFixed(2) : '—',
              unit: t('overview.perSecond'),
              tone: 'memory',
            },
            {
              label: t('overview.originRequests'),
              value: measured ? originRate.toFixed(2) : '—',
              unit: t('overview.perSecond'),
              tone: 'origin',
            },
          ]}
          series={measured ? networkSeries : undefined}
          seriesTone="memory"
          detail={measured ? t('overview.realTimeWindow') : t('overview.notCollected')}
          loading={nowPending && !now}
          onInfo={() => onInfo('network')}
          infoLabel={t('overview.networkInfoLabel')}
        />
      </div>
    </section>
  )
}
