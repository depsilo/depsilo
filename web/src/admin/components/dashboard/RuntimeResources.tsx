import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import TooltipV2 from '@/components/Tooltip'
import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { useSampleSeries } from '@/hooks/useSampleSeries'
import { formatBytes } from '@/lib/utils'

import MetricTile, { HINT_TOOLTIP_DELAY_MS } from './MetricTile'

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
      className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2 py-3"
      style={{ flex: '1 1 480px' }}
    >
      <div className="flex min-w-0 items-center gap-3">
        <Icon name="storage" size="sm" style={{ color: 'var(--dash-muted)' }} />
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h2 className="text-[13px] font-medium leading-tight" style={{ color: 'var(--dash-muted)' }}>{t('overview.cacheLabel')}</h2>
            <span className="rounded px-1.5 py-0.5 text-[11px] leading-none" style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}>{t('overview.liveBadge')}</span>
          </div>
          {loading
            ? <span aria-hidden className="block h-[26px] w-20 animate-pulse rounded bg-[var(--dash-soft)]" />
            : <p className="dash-metric-value whitespace-nowrap font-mono font-semibold leading-tight tabular-nums" style={{ fontSize: '22px', color: 'var(--dash-ink)' }}>{inventoryKnown ? formatBytes(cache.logical_bytes) : '—'}</p>}
        </div>
      </div>
      <div className="min-w-0 grow basis-[180px] text-[13px] leading-relaxed" style={{ color: 'var(--dash-muted)' }}>
        {loading ? <>
          <span className="sr-only">{t('loading')}</span>
          <span aria-hidden className="mb-1 block h-4 w-40 max-w-full animate-pulse rounded bg-[var(--dash-soft)]" />
          <span aria-hidden className="block h-4 w-28 max-w-full animate-pulse rounded bg-[var(--dash-soft)]" />
        </> : <>
          {inventoryDetail && <p>{inventoryDetail}</p>}
          {quotaDetail && <p>{quotaDetail}</p>}
        </>}
      </div>
      {ratio !== null && (
        <div
          aria-hidden
          data-progress-ratio={ratio}
          className="h-1.5 w-32 max-w-[160px] overflow-hidden rounded-full"
          style={{ background: 'var(--dash-soft)' }}
        >
          {ratio > 0 && <div className="h-full rounded-full transition-[width] duration-300" style={{ width: `${ratio * 100}%`, background: 'var(--dash-accent)' }} />}
        </div>
      )}
      <TooltipV2 delay={HINT_TOOLTIP_DELAY_MS} content={<span className="block max-w-[260px] leading-relaxed">{t('overview.hintCache')}</span>}>
        <button
          type="button"
          aria-label={t('overview.cacheInfoLabel')}
          className="dash-focus grid size-[40px] shrink-0 place-items-center rounded-full transition-colors duration-150 hover:bg-[var(--dash-soft)]"
          style={{ color: 'var(--dash-muted)' }}
        >
          <Icon name="info" size="sm" />
        </button>
      </TooltipV2>
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
