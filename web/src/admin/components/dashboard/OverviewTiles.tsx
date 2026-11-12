import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { DashboardPeriod, DashboardResponse, RuntimeResponse } from '@/lib/adminApi.types'
import {
  buildRequestFlow,
  coverageDetail,
  formatEstimatedDuration,
  formatPercentRatio,
  hitRateValue,
  latencyComparison,
  originCoverageNote,
  periodChange,
  timeSavedMs,
} from '@/lib/dashboardOverview'
import type { ActivityBucket } from '@/lib/overviewCockpit'
import { formatBytes, formatLatency } from '@/lib/utils'

import DashboardMetricHelp from './DashboardMetricHelp'
import { useNumberLocale } from './cockpitFormat'
import { CapacityMeter, Spark, SplitBar } from './cockpitParts'

interface OverviewTilesProps {
  period?: DashboardPeriod
  prev?: DashboardPeriod
  range?: DashboardResponse['range']
  coverage?: DashboardResponse['origin_coverage']
  pending: boolean
  runtime?: RuntimeResponse
  runtimePending: boolean
  /** Coarse activity buckets for the tile sparks. */
  spark: ActivityBucket[]
}

interface Delta {
  /** Compact visible form, e.g. "↓69%". */
  text: string
  /** The same change as a sentence. */
  label: string
}

interface TileProps {
  id: string
  testId?: string
  benefit?: string
  label: string
  help?: { label: string; content: ReactNode }
  kind?: 'hit' | 'miss'
  /** A number and its unit, or one placeholder. */
  value: string
  sub?: ReactNode
  delta?: Delta | null
  alert?: boolean
  pending?: boolean
  children?: ReactNode
}

/** Splits "7.8 GB" or "12s" into the figure and its unit. */
function splitUnit(text: string): [string, string] {
  const match = /^([-−+]?[\d.,]+)\s*(.*)$/.exec(text)
  return match ? [match[1], match[2]] : [text, '']
}

function Tile({ id, testId, benefit, label, help, kind, value, sub, delta, alert = false, pending = false, children }: TileProps) {
  const [figure, unit] = splitUnit(value)
  return (
    <Card
      render={<div role="group" aria-label={label} />}
      className="dash-tile"
      data-tile={id}
      data-testid={testId}
      data-benefit={benefit}
      data-dashboard-metric
      data-alert={alert || undefined}
      aria-busy={pending || undefined}
    >
      <div className="dash-tile-label">
        {kind && <i className="dash-key" data-kind={kind} aria-hidden />}
        {alert && <Icon name="warning" size="sm" className="shrink-0 text-[var(--dash-danger)]" />}
        {help ? <DashboardMetricHelp label={label} ariaLabel={help.label}>{help.content}</DashboardMetricHelp> : <span className="truncate">{label}</span>}
      </div>
      <div className="dash-tile-main">
        {pending ? <Skeleton render={<span />} className="block h-5 w-16" /> : (
          <span className="dash-tile-value" data-tile-value>{figure}{unit && <span className="dash-tile-unit">{unit}</span>}</span>
        )}
        {!pending && children}
      </div>
      {sub !== undefined && (
        <p className="dash-tile-sub">
          <span>{sub}</span>
          {!pending && delta && (
            <span className="dash-tile-delta">
              <span aria-hidden>{delta.text}</span>
              <span className="sr-only">{delta.label}</span>
            </span>
          )}
        </p>
      )}
    </Card>
  )
}

/**
 * The period's headline figures. Byte tiles read as one sum: client downloads
 * are what the cache answered plus what came back from an Upstream.
 */
export default function OverviewTiles({ period, prev, range, coverage, pending, runtime, runtimePending, spark }: OverviewTilesProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const count = (value: number) => value.toLocaleString(locale)
  const flow = buildRequestFlow({ period, coverage, rangeStart: range?.start })
  const hasSamples = Boolean(period && period.total_requests > 0)
  const hitRate = hitRateValue(period)
  const prevRate = hitRateValue(prev)
  const latency = latencyComparison(period)
  const saved = timeSavedMs(period)
  const loading = t('loading')

  const percentDelta = (current: number | undefined, previous: number | undefined): Delta | null => {
    const change = periodChange(current ?? null, previous ?? null)
    if (change === null) return null
    const magnitude = `${Math.abs(change).toFixed(0)}%`
    return {
      text: `${change >= 0 ? '↑' : '↓'}${magnitude}`,
      label: t(change >= 0 ? 'overview.deltaUp' : 'overview.deltaDown', { value: magnitude }),
    }
  }

  // Origin metering began at some instant. A comparison is only honest when
  // both the selected period and the one before it were metered throughout.
  const originNote = originCoverageNote(coverage, range?.start)
  const previousStart = range ? new Date(range.start).getTime() - (new Date(range.end).getTime() - new Date(range.start).getTime()) : NaN
  const originComparable = originNote.measured && !originNote.partial && Boolean(coverage?.since)
    && new Date(coverage?.since ?? '').getTime() <= previousStart
  const coverageNote = coverageDetail(coverage, range?.start, t, locale)
  // A tile has room for one fact under its figure. When metering covered only
  // part of the period, that caveat outranks the request count, which moves
  // into the explanation.
  const originRequests = flow.originRequests === null ? '' : t('overview.originRequestsTotal', { count: count(flow.originRequests) })
  const originSub = pending ? loading
    : !flow.originMeasured ? t('overview.notCollected')
    : coverageNote ? t('overview.originPartialShort')
    : originRequests

  const hitRatePoints = hitRate !== null && prevRate !== null ? (hitRate - prevRate) * 100 : null
  const errorChange = period && prev && prev.total_requests > 0 ? period.errors - prev.errors : 0

  const cache = runtime?.cache
  const cacheLoading = runtimePending && !runtime
  const inventoryKnown = cache?.packages != null && cache.entries != null
  const quota = cache?.quota_bytes ?? 0
  const cacheRatio = cache && inventoryKnown && quota > 0 ? cache.logical_bytes / quota : null
  const cacheValue = !cache || !inventoryKnown ? '—'
    : cacheRatio !== null ? `${(cacheRatio * 100).toFixed(cacheRatio < 0.1 ? 1 : 0)}%` : formatBytes(cache.logical_bytes)
  const cacheSub = cacheLoading ? loading
    : !cache ? ''
    : !inventoryKnown ? t('overview.cacheInventoryUnavailable')
    : quota > 0 ? `${formatBytes(cache.logical_bytes)} / ${formatBytes(quota)}`
    : t(cache.storage_type === 's3' ? 'overview.storageS3' : 'overview.cacheNoQuota')

  return (
    <div data-dashboard-benefits data-dashboard-kpis role="group" aria-label={t('overview.tilesLabel')} className="dash-tiles">
      <Tile
        id="cache" benefit="saved" kind="hit" pending={pending}
        label={t('overview.cacheServedLabel')}
        help={{ label: t('overview.savedBytesInfoLabel'), content: <p>{t('overview.hintSavedBytes')}</p> }}
        value={period ? formatBytes(period.hit_bytes) : '—'}
        sub={pending ? loading : period && period.bytes_served > 0
          ? t('overview.shareOfDownloads', { percent: formatPercentRatio(period.hit_bytes / period.bytes_served, 0) })
          : t('overview.noSamples')}
        delta={percentDelta(period?.hit_bytes, prev?.hit_bytes)}
      >
        <Spark values={spark.map(bucket => bucket.bytesHit)} kind="hit" />
      </Tile>

      <Tile
        id="origin" testId="traffic-origin-total" kind="miss" pending={pending}
        label={t('overview.originTrafficLabel')}
        help={{
          label: t('overview.originTotalInfoLabel'),
          content: <><p>{t('overview.hintOriginTotal')}</p>{coverageNote && <p>{coverageNote}</p>}{flow.originMeasured && originRequests && <p>{originRequests}</p>}</>,
        }}
        value={flow.originBytes === null ? '—' : formatBytes(flow.originBytes)}
        sub={originSub}
        delta={originComparable ? percentDelta(period?.upstream_bytes, prev?.upstream_bytes) : null}
      >
        {/* Time before metering would draw as measured zero, so a partly metered period gets no spark. */}
        {flow.originMeasured && !originNote.partial && <Spark values={spark.map(bucket => bucket.bytesUpstream)} kind="miss" />}
      </Tile>

      <Tile
        id="served" testId="traffic-served-total" pending={pending}
        label={t('overview.servedLabel')}
        value={flow.servedBytes === null ? '—' : formatBytes(flow.servedBytes)}
        sub={pending ? loading : flow.servedRequests === null ? '' : t('overview.servedRequests', { count: count(flow.servedRequests) })}
        delta={percentDelta(period?.bytes_served, prev?.bytes_served)}
      >
        <Spark values={spark.map(bucket => bucket.bytesHit + bucket.bytesMiss)} />
      </Tile>

      <Tile
        id="hit-rate" benefit="hit" pending={pending}
        label={t('overview.hitRateTile')}
        help={{ label: t('overview.hitRateInfoLabel'), content: <p>{t('overview.hintHitRate')}</p> }}
        value={formatPercentRatio(hitRate)}
        sub={pending ? loading : hasSamples && period
          ? `${count(period.hit_requests)} / ${count(period.total_requests)}`
          : t('overview.noSamples')}
        delta={hitRatePoints === null ? null : {
          text: `${hitRatePoints >= 0 ? '↑' : '↓'}${Math.abs(hitRatePoints).toFixed(1)}pp`,
          label: t(hitRatePoints >= 0 ? 'overview.deltaPointsUp' : 'overview.deltaPointsDown', { value: Math.abs(hitRatePoints).toFixed(1) }),
        }}
      >
        {hasSamples && period && (
          <SplitBar
            hit={period.hit_requests}
            miss={period.miss_requests}
            label={t('overview.hitRateDetail', { hits: count(period.hit_requests), total: count(period.total_requests) })}
          />
        )}
      </Tile>

      <Card
        render={<div role="group" aria-label={t('overview.latencyTile')} />}
        className="dash-tile" data-tile="latency" data-benefit="latency" data-dashboard-metric aria-busy={pending || undefined}
      >
        <div className="dash-tile-label">
          <DashboardMetricHelp label={t('overview.latencyTile')} ariaLabel={t('overview.responseInfoLabel')}>
            <p>{t('overview.responseComparisonBasis')}</p>
            {pending ? <p>{t('overview.loadingStatistics')}</p> : (
              <dl className="dashboard-help-samples">
                <div><dt>{t('overview.latencyHit')}</dt><dd>{latency.hitSamples > 0 ? t('overview.samples', { count: count(latency.hitSamples) }) : t('overview.noSamples')}</dd></div>
                <div><dt>{t('overview.latencyMiss')}</dt><dd>{latency.missSamples > 0 ? t('overview.samples', { count: count(latency.missSamples) }) : t('overview.noSamples')}</dd></div>
              </dl>
            )}
          </DashboardMetricHelp>
        </div>
        <dl className="dash-tile-pair">
          <div>
            <dt><i className="dash-key mr-1.5" data-kind="hit" aria-hidden />{t('overview.latencyHit')}</dt>
            <dd>{pending ? <Skeleton render={<span />} className="block h-4 w-10" /> : latency.hitMs === null ? '—' : formatLatency(latency.hitMs)}</dd>
          </div>
          <div>
            <dt><i className="dash-key mr-1.5" data-kind="miss" aria-hidden />{t('overview.latencyMiss')}</dt>
            <dd>{pending ? <Skeleton render={<span />} className="block h-4 w-10" /> : latency.missMs === null ? '—' : formatLatency(latency.missMs)}</dd>
          </div>
        </dl>
      </Card>

      <Tile
        id="time-saved" pending={pending}
        label={t('overview.timeSavedLabel')}
        help={{ label: t('overview.timeSavedInfoLabel'), content: <p>{t('overview.hintTimeSaved')}</p> }}
        value={saved === null ? '—' : formatEstimatedDuration(saved, t)}
        sub={pending ? loading
          : saved !== null ? t('overview.timeSavedBasis')
          : !hasSamples ? t('overview.noSamples')
          : !latency.sufficient ? t('overview.timeSavedInsufficient')
          : t('overview.timeSavedNoGap')}
      />

      <Tile
        id="errors" pending={pending} alert={Boolean(period && period.errors > 0)}
        label={t('overview.errorsLabel')}
        help={{ label: t('overview.errorsInfoLabel'), content: <p>{t('overview.hintErrors')}</p> }}
        value={period ? count(period.errors) : '—'}
        sub={pending ? loading : hasSamples && period
          ? t('overview.errorRate', { percent: formatPercentRatio(period.errors / period.total_requests) })
          : t('overview.noSamples')}
        delta={errorChange === 0 ? null : {
          text: `${errorChange > 0 ? '↑' : '↓'}${count(Math.abs(errorChange))}`,
          label: t(errorChange > 0 ? 'overview.deltaCountUp' : 'overview.deltaCountDown', { count: count(Math.abs(errorChange)) }),
        }}
      />

      <Tile
        id="cache-usage" testId="resource-cache" pending={cacheLoading}
        label={t('overview.cacheUsageLabel')}
        help={{ label: t('overview.cacheInfoLabel'), content: <><p>{t('overview.hintCache')}</p><p>{t('overview.hintCacheInventory')}</p></> }}
        value={cacheValue}
        sub={cacheSub}
      >
        {cacheRatio !== null && (
          <CapacityMeter
            ratio={cacheRatio}
            label={t('overview.cacheUsageLabel')}
            valueText={t('overview.cacheUsagePercentage', { percent: (cacheRatio * 100).toFixed(1) })}
            testRatio
          />
        )}
      </Tile>
    </div>
  )
}
