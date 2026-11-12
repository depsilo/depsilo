import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import type { DashboardRange, DashboardTrendPoint } from '@/lib/adminApi.types'
import { formatPercentRatio, rangeLabelKey } from '@/lib/dashboardOverview'
import {
  activitySpan,
  findActivityBursts,
  isActivityTick,
  niceAxisMax,
  rebucketActivity,
  type ActivityBucket,
} from '@/lib/overviewCockpit'
import { formatBytes } from '@/lib/utils'

import { useNumberLocale } from './cockpitFormat'
import { CardNote, CockpitCard, SeriesLegend, SplitBar } from './cockpitParts'

/** Bars the plot can hold while each stays wide enough to point at. */
const MAX_BARS = 72

interface ActivityCardProps {
  points: DashboardTrendPoint[]
  /** The selected range. */
  range: DashboardRange
  /** The range the points belong to; differs while a range change is loading. */
  dataRange: DashboardRange
  loading: boolean
  /** Message for a failed first load, already resolved for the response status. */
  error?: string
  stale: boolean
  onRetry: () => void
}

function useSpanFormat(locale: string) {
  return useMemo(() => {
    const day = new Intl.DateTimeFormat(locale, { month: 'numeric', day: 'numeric' })
    const clock = new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
    return {
      tick: (bucket: ActivityBucket, range: DashboardRange) => (
        (range === '1h' || range === '24h' ? clock : day).format(new Date(bucket.start * 1000))
      ),
      /**
       * "11/10 12:00–15:00". Only a span from one local midnight to another is
       * named by its days ("11/10", or "11/10–11/12"); a long burst keeps its
       * times, so a month of traffic never reads as one day's.
       */
      span: (bucket: Pick<ActivityBucket, 'end'> & { start: number; dataStart?: number }) => {
        const from = bucket.dataStart ?? bucket.start
        const start = new Date(from * 1000)
        const end = new Date(bucket.end * 1000)
        const lastDay = new Date(bucket.end * 1000 - 1)
        const atMidnight = (date: Date) => date.getHours() === 0 && date.getMinutes() === 0 && date.getSeconds() === 0
        if (atMidnight(start) && atMidnight(end) && bucket.end - from >= 86400) {
          return day.format(start) === day.format(lastDay) ? day.format(start) : `${day.format(start)}–${day.format(lastDay)}`
        }
        const sameDay = day.format(start) === day.format(lastDay)
        return `${day.format(start)} ${clock.format(start)}–${sameDay ? '' : `${day.format(end)} `}${clock.format(end)}`
      },
    }
  }, [locale])
}

function ActivityChart({ buckets, range, label }: { buckets: ActivityBucket[]; range: DashboardRange; label: string }) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const format = useSpanFormat(locale)
  const [active, setActive] = useState<number | null>(null)
  const plotRef = useRef<HTMLDivElement>(null)
  const tipRef = useRef<HTMLDivElement>(null)
  const total = (bucket: ActivityBucket) => Math.max(bucket.requests, bucket.hits + bucket.misses)
  const max = Math.max(0, ...buckets.map(total))
  const top = niceAxisMax(max)
  const axis = (value: number) => (
    top >= 10000 && value > 0 ? `${+(value / 1000).toFixed(1)}K` : value.toLocaleString(locale)
  )
  const count = buckets.length
  const current = active !== null ? buckets[active] : undefined
  const anchor = active === null ? 0 : (active + 0.5) / count

  // Place the readout beside the active bar, on whichever side has room, and
  // keep it inside the card: it may overlap the axis labels but never the edge.
  // It is measured and placed before paint, so it never flashes at the edge.
  useLayoutEffect(() => {
    const plot = plotRef.current
    const tip = tipRef.current
    if (!plot || !tip) return
    const width = plot.clientWidth
    const tipWidth = tip.offsetWidth
    const x = anchor * width
    const gap = 12
    const axisRoom = 40
    const preferred = x + gap + tipWidth <= width ? x + gap : x - gap - tipWidth
    tip.style.left = `${Math.max(-axisRoom, Math.min(preferred, width - tipWidth))}px`
  }, [active, anchor])

  const move = (event: KeyboardEvent<HTMLDivElement>) => {
    if (count === 0) return
    const last = count - 1
    const from = active ?? last
    const next = event.key === 'ArrowLeft' ? Math.max(0, from - 1)
      : event.key === 'ArrowRight' ? Math.min(last, from + 1)
      : event.key === 'Home' ? 0
      : event.key === 'End' ? last
      : null
    if (next === null) return
    event.preventDefault()
    setActive(next)
  }

  const readout = current ? [
    format.span(current),
    t('overview.servedRequests', { count: total(current).toLocaleString(locale) }),
    `${t('overview.seriesHit')} ${current.hits.toLocaleString(locale)}`,
    `${t('overview.seriesMiss')} ${current.misses.toLocaleString(locale)}`,
    current.errors > 0 ? `${t('overview.outcomeFailed')} ${current.errors.toLocaleString(locale)}` : '',
  ].filter(Boolean).join(', ') : ''

  return (
    <div className="dash-chart" onMouseLeave={() => setActive(null)}>
      {/* An interactive plot: arrow keys walk the bars, and the readout below is
          announced, so the values never depend on a pointer. */}
      <div
        ref={plotRef}
        data-trend-chart
        className="dash-chart-plot"
        role="application"
        aria-label={label}
        tabIndex={0}
        onKeyDown={move}
        onFocus={() => setActive(current => current ?? (count > 0 ? count - 1 : null))}
        onBlur={() => setActive(null)}
      >
        {(max > 0 ? [1, 0.5, 0] : [0]).map(level => (
          <div key={level} aria-hidden className="dash-chart-grid" style={{ bottom: `${level * 100}%` }} data-base={level === 0 || undefined}>
            <span>{level === 0 ? '0' : axis(top * level)}</span>
          </div>
        ))}
        {max <= 0 && <p className="dash-chart-empty">{t('overview.activityEmpty')}</p>}
        <div aria-hidden className="dash-chart-bars" style={{ gridTemplateColumns: `repeat(${count}, minmax(0, 1fr))` }}>
          {buckets.map((bucket, index) => {
            const sum = total(bucket)
            const other = Math.max(0, sum - bucket.hits - bucket.misses)
            return (
              <div
                key={bucket.start}
                className="dash-chart-col"
                data-active={active === index || undefined}
                data-requests={sum}
                onMouseEnter={() => setActive(index)}
              >
                {bucket.errors > 0 && <b className="dash-chart-error" />}
                {sum > 0 && (
                  <div className="dash-chart-stack" style={{ height: `${(sum / top) * 100}%` }}>
                    {other > 0 && <i className="is-other" style={{ flexGrow: other }} />}
                    {bucket.misses > 0 && <i className="is-miss" style={{ flexGrow: bucket.misses }} />}
                    {bucket.hits > 0 && <i className="is-hit" style={{ flexGrow: bucket.hits }} />}
                  </div>
                )}
              </div>
            )
          })}
        </div>
        {current && (
          <div
            ref={tipRef}
            aria-hidden
            data-trend-tooltip
            className="dash-chart-tip"
          >
            <p>{format.span(current)}</p>
            <p>{t('overview.servedRequests', { count: total(current).toLocaleString(locale) })}</p>
            <p>
              <i className="dash-key" data-kind="hit" />{current.hits.toLocaleString(locale)}
              <span>{t('overview.seriesHit')}{total(current) > 0 && ` · ${formatPercentRatio(current.hits / total(current), 0)}`}</span>
            </p>
            <p><i className="dash-key" data-kind="miss" />{current.misses.toLocaleString(locale)}<span>{t('overview.seriesMiss')}</span></p>
            {current.errors > 0 && <p>{current.errors.toLocaleString(locale)}<span>{t('overview.outcomeFailed')}</span></p>}
          </div>
        )}
        <p className="sr-only" aria-live="polite">{readout}</p>
      </div>
      {/* The chart's table view, for readers that cannot send arrow keys
          (touch screen readers). Quiet spans are summarised, not listed. */}
      <table className="sr-only" data-activity-table>
        <caption>{label}</caption>
        <thead>
          <tr>
            <th scope="col">{t('overview.colTime')}</th>
            <th scope="col">{t('overview.colRequests')}</th>
            <th scope="col">{t('overview.seriesHit')}</th>
            <th scope="col">{t('overview.seriesMiss')}</th>
            <th scope="col">{t('overview.outcomeFailed')}</th>
          </tr>
        </thead>
        <tbody>
          {buckets.filter(bucket => total(bucket) > 0).map(bucket => (
            <tr key={bucket.start}>
              <th scope="row">{format.span(bucket)}</th>
              <td>{total(bucket).toLocaleString(locale)}</td>
              <td>{bucket.hits.toLocaleString(locale)}</td>
              <td>{bucket.misses.toLocaleString(locale)}</td>
              <td>{bucket.errors.toLocaleString(locale)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot><tr><td colSpan={5}>{t(max > 0 ? 'overview.activityTableQuiet' : 'overview.activityEmpty')}</td></tr></tfoot>
      </table>
      <div aria-hidden className="dash-chart-x">
        {buckets.map((bucket, index) => (
          isActivityTick(bucket, range, buckets[index - 1]) && index / count < 0.94
            ? <span key={bucket.start} style={{ left: `${(index / count) * 100}%` }}>{format.tick(bucket, range)}</span>
            : null
        ))}
      </div>
    </div>
  )
}

/**
 * When the requests came, as stacked columns (cache hits at the baseline,
 * upstream fetches on top), and the most recent bursts as a list. The list is
 * the chart's table view: every burst's figures without a pointer.
 */
export default function ActivityCard({ points, range, dataRange, loading, error, stale, onRetry }: ActivityCardProps) {
  const { t } = useTranslation()
  const locale = useNumberLocale()
  const format = useSpanFormat(locale)
  const span = useMemo(() => activitySpan(points, MAX_BARS), [points])
  const buckets = useMemo(() => rebucketActivity(points, span), [points, span])
  const bursts = useMemo(() => findActivityBursts(points, 4), [points])
  const rangeLabel = t(rangeLabelKey(dataRange))
  const spanLabel = span >= 86400 ? t('overview.spanDays', { count: span / 86400 })
    : span >= 3600 ? t('overview.spanHours', { count: span / 3600 })
    : t('overview.spanMinutes', { count: Math.max(1, Math.round(span / 60)) })
  const settled = !loading && !error

  return (
    <CockpitCard
      id="trends"
      title={t('overview.activityTitle')}
      chip={settled && buckets.length > 0 ? t('overview.activityBarSpan', { span: spanLabel }) : undefined}
      help={{
        label: t('overview.trendMeasurementInfo'),
        content: <><p>{t('overview.activityHelpBars')}</p><p>{t('overview.activityHelpLast')}</p><p>{t('overview.activityHelpKeys')}</p></>,
      }}
      aside={<SeriesLegend hit={t('overview.seriesHit')} miss={t('overview.seriesMiss')} />}
      busy={loading || stale || range !== dataRange}
      data-dashboard-trends=""
      data-query-key="dashboard-trends"
      data-trend-range={dataRange}
      data-trend-pending={range !== dataRange ? 'true' : undefined}
    >
      {loading ? (
        <div className="dash-card-body" role="status" aria-label={t('overview.loadingStatistics')}>
          <Skeleton aria-hidden className="h-[196px] rounded-md" />
        </div>
      ) : error ? (
        <CardNote tone="warning" role="alert" onRetry={onRetry} retryLabel={t('common.retry')}>{error}</CardNote>
      ) : (
        <>
          {range !== dataRange && <CardNote role="status">{t('overview.switchingRange')}</CardNote>}
          {stale && <CardNote tone="warning" role="status" onRetry={onRetry} retryLabel={t('common.retry')}>{t('overview.dataStale')}</CardNote>}
          <div className="dash-card-body">
            <ActivityChart
              buckets={buckets}
              range={dataRange}
              label={t('overview.activityChartLabel', { range: rangeLabel, span: spanLabel })}
            />
            {bursts.length > 0 && (
              <>
                <h3 className="dash-subhead">{t('overview.burstsTitle')}</h3>
                <ul className="dash-bursts" data-activity-bursts>
                  {bursts.map(burst => {
                    const requests = Math.max(burst.requests, burst.hits + burst.misses)
                    return (
                      <li key={burst.start}>
                        <span>{format.span(burst)}</span>
                        <span className="is-num">{t('overview.burstRequests', { count: requests.toLocaleString(locale) })}</span>
                        <SplitBar
                          hit={burst.hits}
                          miss={burst.misses}
                          label={`${t('overview.seriesHit')} ${burst.hits.toLocaleString(locale)}, ${t('overview.seriesMiss')} ${burst.misses.toLocaleString(locale)}`}
                        />
                        <span className="is-num is-mute">{t('overview.burstHitRate', { percent: formatPercentRatio(requests > 0 ? burst.hits / requests : null, 0) })}</span>
                        <span className="is-num is-mute">{formatBytes(burst.bytesHit + burst.bytesMiss)}</span>
                      </li>
                    )
                  })}
                </ul>
              </>
            )}
          </div>
        </>
      )}
    </CockpitCard>
  )
}
