import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import type { NowResponse } from '@/lib/adminApi.types'
import { formatBps } from '@/lib/utils'

import { useNumberLocale } from './cockpitFormat'
import { Spark } from './cockpitParts'

interface LiveStripProps {
  now?: NowResponse
  pending: boolean
  error: boolean
  stale: boolean
}

/**
 * What the service is doing right now: the meter's last-60-second averages,
 * beside a strip of the last half hour. A measured zero is shown as zero;
 * "not collected" and "unavailable" stay distinct from it.
 */
export default function LiveStrip({ now, pending, error, stale }: LiveStripProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const measured = now?.rate.measured === true

  return (
    <div
      data-dashboard-live
      data-testid="resource-network"
      role="group"
      aria-label={t('overview.liveLabel')}
      aria-busy={(pending && !now) || undefined}
      className="dash-live"
    >
      <span className="dash-live-tag">{t('overview.liveLabel')}</span>
      {pending && !now ? (
        <><span className="sr-only">{t('loading')}</span><Skeleton className="h-3 w-40" /></>
      ) : !now ? (
        <span>{t(error ? 'overview.flowRateUnavailable' : 'overview.notCollected')}</span>
      ) : !measured ? (
        <span>{t('overview.notCollected')}</span>
      ) : (
        <>
          <Spark values={now.sparkline.map(point => point.requests)} />
          <span data-live="requests">{t('overview.liveRequests', { count: (Math.round(now.rate.requests_per_min * 10) / 10).toLocaleString(locale) })}</span>
          <span data-testid="traffic-service-flow">{t('overview.liveDelivery')} <b>{formatBps(now.rate.service_bytes_per_sec ?? 0)}</b></span>
          <span data-testid="traffic-origin-flow">{t('overview.liveOrigin')} <b>{formatBps(now.rate.origin_bytes_per_sec ?? 0)}</b></span>
          {stale && <span>{t('overview.flowRateStale')}</span>}
        </>
      )}
    </div>
  )
}
