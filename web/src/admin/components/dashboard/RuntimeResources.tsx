import { useTranslation } from 'react-i18next'

import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
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

  const cpuPercent = process?.cpu_percent
  const cpuProgress = process?.cpu.supported === true && cpuPercent !== undefined
    ? { ratio: cpuPercent / 100, tone: cpuPercent >= 90 ? 'warn' as const : 'accent' as const }
    : null

  const cpuValue = cpuPercent === undefined
    ? (runtimePending ? '' : '—')
    : cpuPercent.toFixed(1)
  const cpuDetail = process?.cpu.supported === false
    ? capabilityText(process.cpu, t('overview.notSupported'))
    : t('overview.cpuBasis', { cores: process?.cpu_cores ?? 0 })

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
    ? { ratio: used / limit, tone: (used / limit) > 0.9 ? 'warn' as const : 'ok' as const }
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
  const cacheTone = cacheRatio !== null && cacheRatio > 0.85 ? 'warn' : 'accent'

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
          value={cpuValue}
          unit="%"
          detail={runtimePending && cpuPercent === undefined ? t('overview.collecting') : cpuDetail}
          tone="accent"
          progress={cpuProgress}
          loading={runtimePending && cpuPercent === undefined}
          onInfo={() => onInfo('cpu')}
          infoLabel={t('overview.cpuInfoLabel')}
        />
        <MetricTile
          testId="resource-memory"
          label={t('overview.memoryLabel')}
          value={memoryValue}
          detail={process?.memory.supported === false
            ? `${process.memory.reason} · ${t('overview.goRuntimeMemory', { value: formatBytes(runtime?.go_runtime.heap_alloc_bytes ?? 0) })}`
            : memoryDetail}
          tone="accent"
          progress={memoryProgress}
          loading={runtimePending && !runtime}
          onInfo={() => onInfo('memory')}
          infoLabel={t('overview.memoryInfoLabel')}
        />
        <MetricTile
          testId="resource-cache"
          label={t('overview.cacheLabel')}
          value={cache ? formatBytes(logical) : '—'}
          detail={cacheDetailParts.join(' · ')}
          tone={cacheTone}
          progress={cacheRatio !== null ? { ratio: cacheRatio, tone: cacheTone } : null}
          loading={runtimePending && !runtime}
          onInfo={() => onInfo('cache')}
          infoLabel={t('overview.cacheInfoLabel')}
        />
        <MetricTile
          testId="resource-network"
          label={t('overview.networkLabel')}
          value={measured ? serviceRate.toFixed(2) : '—'}
          unit={t('overview.perSecond')}
          detail={measured
            ? `${t('overview.serviceRequests')} ${serviceRate.toFixed(2)} · ${t('overview.originRequests')} ${originRate.toFixed(2)}`
            : t('overview.notCollected')}
          tone="accent"
          series={measured ? networkSeries : undefined}
          seriesTone="accent"
          loading={nowPending && !now}
          onInfo={() => onInfo('network')}
          infoLabel={t('overview.networkInfoLabel')}
        />
      </div>
      {runtime?.disk.supported && (runtime.disk.total_bytes ?? 0) > 0 && (
        <p className="sr-only">
          {t('overview.diskFree', { free: formatBytes(runtime.disk.free_bytes ?? 0), total: formatBytes(runtime.disk.total_bytes ?? 0) })}
        </p>
      )}
    </section>
  )
}
