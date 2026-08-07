// Tabbed trends chart. One backend request per range carries every
// dimension a tab could render (requests / bandwidth / latency / errors),
// so switching tabs is a pure re-render — no refetch, no jitter.
//
// Every API bucket is preserved. Bucket timestamps are rendered in the
// browser's timezone via Intl.DateTimeFormat.
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import type { TooltipContentProps, TooltipValueType } from 'recharts'

import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import SectionHeader from '@/components/SectionHeader'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { formatBytes } from '@/lib/utils'

export type TrendsRange = '1h' | '24h' | '7d' | '30d'
export type TrendsTab = 'requests' | 'bandwidth' | 'latency' | 'errors'

export interface RawTrendPoint {
  bucket: number
  date: string
  requests: number
  hits: number
  misses: number
  hit_rate: number
  bytes_served: number
  bytes_hit: number
  bytes_miss: number
  sum_latency_ms: number
  avg_latency_ms: number
  errors: number
}

interface ChartPoint {
  bucket: number
  label: string
  requests: number
  hits: number
  misses: number
  hit_rate_pct: number
  bytes_hit: number
  bytes_miss: number
  bytes_total: number
  avg_latency_ms: number
  error_rate_pct: number
  errors: number
}

const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone

function fmtAxisTime(bucket: number, granularity: 'minute' | 'hour' | 'day'): string {
  const d = new Date(bucket * 1000)
  if (granularity === 'minute') {
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', timeZone: TZ })
  }
  if (granularity === 'hour') {
    // Include short date so the 24h tail across midnight is unambiguous.
    return d.toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: TZ })
  }
  // day
  return d.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit', timeZone: TZ })
}

function fmtTooltipTime(bucket: number, range: TrendsRange): string {
  const d = new Date(bucket * 1000)
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: range === '1h' ? '2-digit' : undefined,
    timeZoneName: 'short',
    timeZone: TZ,
  })
}

function toChartPoint(p: RawTrendPoint, granularity: 'minute' | 'hour' | 'day'): ChartPoint {
  return {
    bucket: p.bucket,
    label: fmtAxisTime(p.bucket, granularity),
    requests: p.requests,
    hits: p.hits,
    misses: p.misses,
    hit_rate_pct: Math.round((p.hit_rate || 0) * 1000) / 10,
    bytes_hit: p.bytes_hit,
    bytes_miss: p.bytes_miss,
    bytes_total: p.bytes_served,
    avg_latency_ms: Math.round(p.avg_latency_ms || 0),
    error_rate_pct: p.requests > 0 ? Math.round((p.errors / p.requests) * 1000) / 10 : 0,
    errors: p.errors,
  }
}

interface Props {
  raw: RawTrendPoint[]
  range: TrendsRange
  dataRange: TrendsRange
  isStale?: boolean
  onRetry?: () => void
  onRangeChange: (r: TrendsRange) => void
}

const RANGES: { value: TrendsRange; key: string }[] = [
  { value: '1h', key: 'range1h' },
  { value: '24h', key: 'range24h' },
  { value: '7d', key: 'range7d' },
  { value: '30d', key: 'range30d' },
]

const TABS: { value: TrendsTab; key: string }[] = [
  { value: 'requests', key: 'trendTabRequests' },
  { value: 'bandwidth', key: 'trendTabBandwidth' },
  { value: 'latency', key: 'trendTabLatency' },
  { value: 'errors', key: 'trendTabErrors' },
]

interface ChartTooltipProps extends TooltipContentProps<TooltipValueType, string | number> {
  dataRange: TrendsRange
}

function ChartTooltip({ active, payload, label, dataRange }: ChartTooltipProps) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  const formattedLabel = typeof label === 'number' ? fmtTooltipTime(label, dataRange) : label
  return (
    <div
      className="rounded-[6px] px-3 py-2 text-[12px]"
      style={{ background: 'var(--bg-card)', boxShadow: 'var(--shadow-pop)' }}
    >
      <p className="font-[400] mb-1" style={{ color: 'var(--text)' }}>{formattedLabel}</p>
      {payload.map((entry) => {
        const v = entry.value
        let display: string
        if (entry.dataKey === 'hit_rate_pct' || entry.dataKey === 'error_rate_pct') {
          display = `${Number(v).toFixed(1)}%`
        } else if (entry.dataKey === 'bytes_hit' || entry.dataKey === 'bytes_miss' || entry.dataKey === 'bytes_total') {
          display = formatBytes(Number(v))
        } else if (entry.dataKey === 'avg_latency_ms') {
          display = `${Number(v).toLocaleString()} ${t('dashboard.msUnit')}`
        } else {
          display = Number(v).toLocaleString()
        }
        return (
          <p key={String(entry.dataKey)} className="font-mono tabular-nums" style={{ color: entry.color }}>
            {entry.name}: {display}
          </p>
        )
      })}
    </div>
  )
}

const axisProps = {
  tick: { fill: 'var(--text-soft)', fontSize: 10 },
  axisLine: false as const,
  tickLine: false as const,
}

export default function TrendsCard({
  raw,
  range,
  dataRange,
  isStale = false,
  onRetry,
  onRangeChange,
}: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TrendsTab>('requests')
  const isMobile = useMediaQuery('(max-width: 640px)')
  const granularity = dataRange === '1h' ? 'minute' : dataRange === '30d' ? 'day' : 'hour'

  const points = useMemo<ChartPoint[]>(() => {
    const pointGranularity = dataRange === '1h' ? 'minute' : dataRange === '30d' ? 'day' : 'hour'
    return raw.map(point => toChartPoint(point, pointGranularity))
  }, [raw, dataRange])

  const allEmpty = points.length === 0 || points.every(point => (
    !point.requests
    && !point.bytes_total
    && !point.avg_latency_ms
    && !point.errors
  ))
  const activeTab = TABS.find(item => item.value === tab)
  const activeRange = RANGES.find(item => item.value === dataRange)
  const chartDescription = t('dashboard.trendChartDescription', {
    metric: activeTab ? t(`dashboard.${activeTab.key}`) : '',
    range: activeRange ? t(`dashboard.${activeRange.key}`) : '',
  })

  return (
    <section className="admin-primary-panel min-w-0 overflow-hidden">
      <div className="px-4 pt-4">
        <SectionHeader
          title={t('dashboard.hitMissTrend')}
          divider={false}
          action={
            <div className="grid w-full grid-cols-1 gap-2 sm:flex sm:w-auto sm:items-center sm:gap-4">
              <div
                className="grid grid-cols-4 gap-1 sm:flex"
                role="group"
                aria-label={t('dashboard.trendMetricGroup')}
              >
                {TABS.map(tb => {
                  const active = tab === tb.value
                  return (
                    <button
                      key={tb.value}
                      type="button"
                      onClick={() => setTab(tb.value)}
                      aria-pressed={active}
                      className="stripe-focus-ring min-h-[40px] min-w-0 cursor-pointer whitespace-nowrap rounded-[3px] px-2 text-[11px] transition-[color,border-color] duration-150 sm:px-2.5"
                      style={{
                        background: 'transparent',
                        color: active ? 'var(--text)' : 'var(--text-soft)',
                        border: '0 solid transparent',
                        borderBottomWidth: 2,
                        borderBottomColor: active ? 'var(--brand)' : 'transparent',
                        fontWeight: active ? 650 : 500,
                      }}
                    >
                      {t(`dashboard.${tb.key}`)}
                    </button>
                  )
                })}
              </div>
              <div
                data-trend-range-control
                className="grid grid-cols-4 overflow-hidden rounded-[7px] border-[0.5px] border-[var(--border)] bg-[var(--bg-soft)] sm:flex"
                role="group"
                aria-label={t('dashboard.hitMissTrend')}
              >
                {RANGES.map(r => {
                  const active = range === r.value
                  return (
                    <button
                      key={r.value}
                      type="button"
                      onClick={() => onRangeChange(r.value)}
                      aria-pressed={active}
                      className="stripe-focus-ring min-h-[40px] min-w-0 cursor-pointer whitespace-nowrap rounded-[5px] px-2 text-[11px] font-[500] transition-[background,color,border-color] duration-150 sm:px-2.5"
                      style={{
                        background: active ? 'var(--bg-card)' : 'transparent',
                        color: active ? 'var(--text)' : 'var(--text-soft)',
                        border: active ? '1px solid var(--border-strong)' : '1px solid transparent',
                      }}
                    >
                      {t(`dashboard.${r.key}`)}
                    </button>
                  )
                })}
              </div>
            </div>
          }
        />
      </div>

      {isStale && (
        <div className="mx-4 mb-3 flex flex-wrap items-center justify-between gap-2 rounded-[6px] bg-[var(--warn-fill)] px-3 py-2 text-[11px] text-[var(--warn-text)]" role="status">
          <span>{t('now.staleData')}</span>
          {onRetry && (
            <ButtonV2 type="button" variant="secondary" size="sm" onClick={onRetry}>
              {t('now.refresh')}
            </ButtonV2>
          )}
        </div>
      )}

      {allEmpty ? (
        <div className="flex min-h-24 items-center justify-center gap-3 px-4 pb-4 text-left">
          <span
            aria-hidden
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[6px] bg-[var(--bg-soft)] text-[var(--text-subtle)]"
          >
            <Icon name="show_chart" size="sm" />
          </span>
          <div className="min-w-0">
            <h3 className="text-[12px] font-[650] text-[var(--text)]">{t('dashboard.emptyTrendTitle')}</h3>
            <p className="mt-1 max-w-[52ch] text-[11px] leading-[1.5] text-[var(--text-soft)]">
              {t('dashboard.emptyTrendHint')}
            </p>
          </div>
        </div>
      ) : (
        <div className="px-2 pb-3">
          <ResponsiveContainer width="100%" height={220}>
            <ComposedChart
              data={points}
              margin={{ top: 4, right: 12, bottom: 0, left: 0 }}
              desc={chartDescription}
            >
            <CartesianGrid stroke="var(--grid)" vertical={false} />
            <XAxis
              dataKey="bucket"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickCount={isMobile ? 4 : 8}
              tickFormatter={(value: number) => fmtAxisTime(value, granularity)}
              minTickGap={12}
              {...axisProps}
            />
            <Tooltip content={props => <ChartTooltip {...props} dataRange={dataRange} />} />
            <Legend wrapperStyle={{ color: 'var(--text-soft)', fontSize: 11, paddingTop: 6 }} />

            {tab === 'requests' && (
              <>
                <YAxis yAxisId="count" {...axisProps} width={36} />
                <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tickFormatter={(v: number) => `${v}%`} {...axisProps} width={36} />
                <Area yAxisId="count" type="linear" dataKey="hits" stroke="var(--brand)" strokeWidth={1.8} fill="var(--brand)" fillOpacity={0.08} name={t('dashboard.hits')} isAnimationActive={false} />
                <Area yAxisId="count" type="linear" dataKey="misses" stroke="var(--danger)" strokeOpacity={0.72} strokeWidth={1.35} fill="var(--danger)" fillOpacity={0.05} name={t('dashboard.misses')} isAnimationActive={false} />
                <Line yAxisId="rate" type="linear" dataKey="hit_rate_pct" stroke="var(--warn-text)" strokeOpacity={0.74} name={t('dashboard.hitRate2')} strokeWidth={1.4} dot={false} strokeDasharray="4 4" isAnimationActive={false} />
              </>
            )}

            {tab === 'bandwidth' && (
              <>
                <YAxis yAxisId="bytes" tickFormatter={(v: number) => formatBytes(v)} {...axisProps} width={56} />
                <Area yAxisId="bytes" type="linear" dataKey="bytes_hit" stroke="var(--ok)" strokeWidth={1.5} fill="var(--ok)" fillOpacity={0.1} name={t('dashboard.bytesHit')} stackId="bw" isAnimationActive={false} />
                <Area yAxisId="bytes" type="linear" dataKey="bytes_miss" stroke="var(--danger)" strokeWidth={1.5} fill="var(--danger)" fillOpacity={0.08} name={t('dashboard.bytesMiss')} stackId="bw" isAnimationActive={false} />
              </>
            )}

            {tab === 'latency' && (
              <>
                <YAxis yAxisId="ms" {...axisProps} width={48} tickFormatter={(v: number) => `${v}ms`} />
                <Line yAxisId="ms" type="linear" dataKey="avg_latency_ms" stroke="var(--brand)" strokeWidth={1.8} dot={false} name={t('dashboard.avgLatency')} isAnimationActive={false} />
              </>
            )}

            {tab === 'errors' && (
              <>
                <YAxis yAxisId="count" {...axisProps} width={36} />
                <YAxis yAxisId="rate" orientation="right" domain={[0, 'auto']} tickFormatter={(v: number) => `${v}%`} {...axisProps} width={42} />
                <Area yAxisId="count" type="linear" dataKey="errors" stroke="var(--danger)" strokeWidth={1.5} fill="var(--danger)" fillOpacity={0.08} name={t('dashboard.trendTabErrors')} isAnimationActive={false} />
                <Line yAxisId="rate" type="linear" dataKey="error_rate_pct" stroke="var(--warn-text)" name={t('dashboard.errorRate')} strokeWidth={1.6} dot={false} strokeDasharray="4 3" isAnimationActive={false} />
              </>
            )}
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}
    </section>
  )
}
