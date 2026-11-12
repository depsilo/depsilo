import { useTranslation } from 'react-i18next'

import type { NowResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { formatUptimeSeconds } from '@/lib/dashboardOverview'
import { formatBytes } from '@/lib/utils'

import { CardNote, CockpitCard } from './cockpitParts'

interface RuntimeCardProps {
  runtime?: RuntimeResponse
  runtimePending: boolean
  runtimeError: boolean
  onRetryRuntime: () => void
  now?: NowResponse
}

/** The process itself: what it uses, how long it has been up, and which build it is. */
export default function RuntimeCard({ runtime, runtimePending, runtimeError, onRetryRuntime, now }: RuntimeCardProps) {
  const { t } = useTranslation()
  const process = runtime?.process
  const loading = runtimePending && !runtime
  const pendingValue = loading ? t('loading') : '—'
  // macOS reports peak RSS and some platforms only the Go runtime's view; the
  // label says which, instead of presenting them as the same measurement.
  const memorySupported = process?.memory.supported === true && process.rss_bytes !== undefined
  const memory = memorySupported
    ? formatBytes(process.rss_bytes as number)
    : runtime ? formatBytes(runtime.go_runtime.sys_bytes) : pendingValue
  const memoryBasis = memorySupported
    ? (process?.rss_basis === 'peak' ? t('overview.memoryPeakBadge') : undefined)
    : runtime ? t('overview.memoryRuntimeBadge') : undefined

  return (
    <CockpitCard id="runtime" title={t('overview.runtimeTitle')} busy={loading} data-dashboard-resources="" data-dashboard-resources-panel="">
      {runtimeError && (
        <CardNote tone="warning" role="status" onRetry={onRetryRuntime} retryLabel={t('overview.refresh')}>
          {t(runtime ? 'overview.resourcesStale' : 'overview.resourcesUnavailable')}
        </CardNote>
      )}
      <div className="dash-card-body">
        <dl className="dash-kv">
          <div data-testid="resource-cpu" className="is-wide">
            <dt>{t('overview.cpuShort')}</dt>
            <dd>
              {process?.cpu.supported === false ? t('overview.notSupported')
                : process?.cpu_percent === undefined ? pendingValue
                : `${process.cpu_percent.toFixed(1)}%`}
              {process?.cpu_percent !== undefined && process.cpu_cores ? <em> · {t('overview.cpuCores', { count: process.cpu_cores })}</em> : null}
            </dd>
          </div>
          <div data-testid="resource-memory">
            <dt>{t('overview.memoryShort')}</dt>
            <dd>{memory}{memoryBasis && <em> · {memoryBasis}</em>}</dd>
          </div>
          <div>
            <dt>{t('overview.goHeap')}</dt>
            <dd>{runtime ? formatBytes(runtime.go_runtime.heap_alloc_bytes) : pendingValue}</dd>
          </div>
          <div>
            <dt>{t('overview.uptimeShort')}</dt>
            <dd>{now ? formatUptimeSeconds(now.uptime_seconds, t) : '—'}</dd>
          </div>
          <div className="is-text">
            <dt>{t('overview.hostLabel')}</dt>
            <dd title={runtime?.host.hostname}>{runtime?.host.hostname ?? pendingValue}</dd>
          </div>
          <div className="is-wide is-text">
            <dt>{t('overview.versionLabel')}</dt>
            <dd title={now?.version}>{now?.version ?? '—'}</dd>
          </div>
        </dl>
      </div>
    </CockpitCard>
  )
}
