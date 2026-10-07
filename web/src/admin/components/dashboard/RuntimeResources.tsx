import { useTranslation } from 'react-i18next'

import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { useSampleSeries } from '@/hooks/useSampleSeries'
import { formatBytes } from '@/lib/utils'

import MetricTile from './MetricTile'

interface RuntimeResourcesProps {
  runtime?: RuntimeResponse
  runtimePending: boolean
  now?: NowResponse
  nowPending: boolean
}

// The tile keeps the localized label; the platform's own reason sentence is a
// diagnostic and belongs in the hint, not as English copy in a zh-CN tile.
function capabilityHint(base: string, capability: { supported: boolean; reason?: string } | undefined): string {
  if (!capability || capability.supported || !capability.reason) return base
  return `${base} · ${capability.reason}`
}

export default function RuntimeResources({
  runtime,
  runtimePending,
  now,
  nowPending,
}: RuntimeResourcesProps) {
  const { t } = useTranslation()
  const process = runtime?.process
  const sampledAt = runtime?.sampled_at

  const cpuPercent = process?.cpu_percent
  const cpuSeries = useSampleSeries(cpuPercent ?? null, sampledAt, 48)
  const rssSeries = useSampleSeries(process?.rss_bytes ?? null, sampledAt, 48)
  const cacheSeries = useSampleSeries(runtime?.cache.logical_bytes ?? null, sampledAt, 48)

  const cpuValue = cpuPercent === undefined
    ? (runtimePending ? '' : '—')
    : cpuPercent.toFixed(1)
  const cpuDetail = process?.cpu.supported === false
    ? t('overview.notSupported')
    : runtimePending && cpuPercent === undefined
      ? t('overview.collecting')
      : undefined
  // Only the unsupported platform carries a hint worth opening; the normal
  // case says nothing the tile does not already show.
  const cpuHint = process?.cpu.supported === false
    ? capabilityHint(t('overview.hintCpu'), process?.cpu)
    : undefined
  // A single-core progress bar only makes sense up to one full core. Above
  // 100% the number carries the truth instead of a silently full bar.
  const cpuProgress = process?.cpu.supported === true && cpuPercent !== undefined && cpuPercent <= 100
    ? { ratio: cpuPercent / 100, tone: 'cpu' as const }
    : null

  const memorySupported = process?.memory.supported === true && process.rss_bytes !== undefined
  const limit = process?.memory_limit_bytes
  const used = process?.memory_used_bytes
  // Prefer real process memory. macOS only exposes peak RSS, so it is labelled
  // as a peak instead of being presented as current usage; platforms with no
  // process reading fall back to the labelled Go runtime figure. Never surface
  // a platform reason sentence in the tile.
  const memoryValue = memorySupported
    ? formatBytes(process.rss_bytes as number)
    : (runtime ? formatBytes(runtime.go_runtime.sys_bytes) : '—')
  const memoryBadge = memorySupported
    ? (process?.rss_basis === 'peak' ? t('overview.memoryPeakBadge') : undefined)
    : (runtime ? t('overview.memoryRuntimeBadge') : undefined)
  const memoryProgress = limit !== undefined && used !== undefined && limit > 0
    ? { ratio: used / limit, tone: 'memory' as const }
    : null
  const cache = runtime?.cache
  const quota = cache?.quota_bytes ?? null
  const logical = cache?.logical_bytes ?? 0
  // Only claim a storage fact once the sample exists: before the first payload
  // the tile shows its skeleton, not "no quota configured".
  const quotaDetail = !cache
    ? undefined
    : quota && quota > 0
      ? undefined
      : (cache.storage_type === 's3' ? t('overview.storageS3') : t('overview.cacheNoQuota'))
  // Package and object counts come from the server's metadata aggregate and
  // are absent until it has succeeded once, so an unreadable cache never reads
  // as an empty one.
  const inventoryDetail = cache?.packages != null && cache.entries != null
    ? t('overview.cacheInventory', {
      packages: cache.packages.toLocaleString(),
      entries: cache.entries.toLocaleString(),
    })
    : undefined
  const cacheDetailLines = [inventoryDetail, quotaDetail].filter((line): line is string => Boolean(line))
  const cacheRatio = quota && quota > 0 ? logical / quota : null

  const measured = now?.rate.measured === true
  const serviceRate = now?.rate.service_requests_per_sec ?? 0
  const originRate = now?.rate.origin_requests_per_sec ?? 0
  // Real 1-minute request buckets over the last 30 minutes from /now.
  const networkSeries = now?.sparkline?.map(point => point.requests) ?? []

  return (
    // One row of divided cells inside the merged Runtime status card. The
    // accessible name replaces the old "程序资源占用" heading; the tiles label
    // themselves and the section no longer pays for a second card surface.
    <div
      data-dashboard-resources
      data-dashboard-kpis
      role="group"
      aria-label={t('overview.resourcesTitle')}
      className="grid min-w-0 grid-cols-1 divide-y divide-[var(--dash-border)] sm:grid-cols-2 sm:divide-y-0 xl:grid-cols-4 xl:divide-x"
    >
      <MetricTile
        testId="resource-cpu"
        framed={false}
        label={t('overview.cpuLabel')}
        icon="memory"
        tone="cpu"
        value={cpuValue}
        unit="%"
        detail={cpuDetail}
        progress={cpuProgress}
        series={cpuSeries}
        loading={runtimePending && cpuPercent === undefined}
        info={cpuHint}
        infoLabel={cpuHint ? t('overview.cpuInfoLabel') : undefined}
      />
      <MetricTile
        testId="resource-memory"
        framed={false}
        label={t('overview.memoryLabel')}
        icon="ram"
        tone="memory"
        value={memoryValue}
        badge={memoryBadge}
        progress={memoryProgress}
        series={memorySupported ? rssSeries : undefined}
        loading={runtimePending && !runtime}
      />
      <MetricTile
        testId="resource-cache"
        framed={false}
        label={t('overview.cacheLabel')}
        icon="storage"
        tone="cache"
        value={cache ? formatBytes(logical) : '—'}
        detail={cacheDetailLines.length > 0
          ? cacheDetailLines.map((line, index) => <span key={index} className="block">{line}</span>)
          : undefined}
        progress={cacheRatio !== null ? { ratio: cacheRatio, tone: 'cache' } : null}
        series={cache ? cacheSeries : undefined}
        loading={runtimePending && !runtime}
        info={t('overview.hintCache')}
        infoLabel={t('overview.cacheInfoLabel')}
      />
      <MetricTile
        testId="resource-network"
        framed={false}
        label={t('overview.networkLabel')}
        icon="hub"
        tone="memory"
        badge={t('overview.liveBadge')}
        // The two rows share one unit, so it belongs in the tile's basis line
        // instead of stressing each row at narrow widths.
        rows={[
          {
            label: t('overview.serviceRequests'),
            value: measured ? serviceRate.toFixed(2) : '—',
            tone: 'memory',
          },
          {
            label: t('overview.originRequests'),
            value: measured ? originRate.toFixed(2) : '—',
            tone: 'origin',
          },
        ]}
        series={measured ? networkSeries : undefined}
        seriesTone="memory"
        detail={measured ? t('overview.perSecond') : t('overview.notCollected')}
        loading={nowPending && !now}
      />
    </div>
  )
}
