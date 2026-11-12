import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { getAdminRouteHref } from '@/admin/routes'
import type { CacheDistributionResponse, RuntimeResponse } from '@/lib/adminApi.types'
import { formatBytes } from '@/lib/utils'

import { useNumberLocale } from './cockpitFormat'
import { CapacityMeter, CardNote, CockpitCard, EcoName, MoreLink, RankBar, ScrollTable } from './cockpitParts'

const TYPE_ROWS = 5

interface StorageCardProps {
  runtime?: RuntimeResponse
  runtimePending: boolean
  runtimeError: boolean
  onRetryRuntime: () => void
  distribution: UseQueryResult<CacheDistributionResponse>
}

/** What the cache holds per ecosystem, and how much room the disk under it has left. */
export default function StorageCard({ runtime, runtimePending, runtimeError, onRetryRuntime, distribution }: StorageCardProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const cache = runtime?.cache
  const disk = runtime?.disk
  const types = [...(distribution.data?.by_type ?? [])].sort((a, b) => b.size - a.size).slice(0, TYPE_ROWS)
  const widest = Math.max(1, ...types.map(type => type.size))
  const inventoryKnown = cache?.packages != null && cache.entries != null

  return (
    <CockpitCard
      id="storage"
      title={t('overview.storageTitle')}
      chip={cache ? t(cache.storage_type === 's3' ? 'overview.storageObject' : 'overview.storageLocal') : undefined}
      aside={<MoreLink to={getAdminRouteHref('cache')}>{t('dashboard.manageCache')}</MoreLink>}
      busy={runtimePending && !runtime}
      data-dashboard-storage=""
    >
      {runtimeError && (
        <CardNote tone="warning" role="status" onRetry={onRetryRuntime} retryLabel={t('overview.refresh')}>
          {t(runtime ? 'overview.resourcesStale' : 'overview.resourcesUnavailable')}
        </CardNote>
      )}
      <div className="dash-card-body">
        {types.length > 0 && (
          <ScrollTable label={t('overview.colComposition')}>
          <table className="dash-table" data-cache-composition>
            <thead>
              <tr>
                <th scope="col">{t('overview.colComposition')}</th>
                <th scope="col">{t('overview.colUsed')}</th>
                <th scope="col">{t('overview.colObjects')}</th>
              </tr>
            </thead>
            <tbody>
              {types.map(type => (
                <tr key={type.type}>
                  <td><EcoName id={type.type} /></td>
                  <td><RankBar ratio={type.size / widest} />{formatBytes(type.size)}</td>
                  <td>{type.file_count.toLocaleString(locale)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          </ScrollTable>
        )}
        {distribution.isError && (
          <p className="dash-sub" role="status">{t(types.length === 0 ? 'overview.distributionUnavailable' : 'overview.dataStale')}</p>
        )}
        <dl className={`dash-kv ${types.length > 0 ? 'is-top' : ''}`}>
          <div>
            <dt>{t('overview.packagesLabel')}</dt>
            <dd>{inventoryKnown ? cache.packages!.toLocaleString(locale) : '—'}</dd>
          </div>
          <div>
            <dt>{t('overview.objectsLabel')}</dt>
            <dd>{inventoryKnown ? cache.entries!.toLocaleString(locale) : '—'}</dd>
          </div>
        </dl>
        {disk?.supported && disk.total_bytes !== undefined && disk.total_bytes > 0 && disk.free_bytes !== undefined && (
          <div className="dash-disk" data-testid="resource-disk">
            <p className="dash-capline">
              <span>{t('overview.diskFreeLabel')}</span>
              <b>{formatBytes(disk.free_bytes)}<em> / {formatBytes(disk.total_bytes)}</em></b>
            </p>
            <CapacityMeter ratio={1 - disk.free_bytes / disk.total_bytes} label={t('overview.diskUsedLabel')} />
          </div>
        )}
      </div>
    </CockpitCard>
  )
}
