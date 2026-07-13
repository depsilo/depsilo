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

import EmptyState from '@/components/EmptyState'
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

function fmtTime(bucket: number, granularity: 'minute' | 'hour' | 'day'): string {
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

function toChartPoint(p: RawTrendPoint, granularity: 'minute' | 'hour' | 'day'): ChartPoint {
  return {
    bucket: p.bucket,
    label: fmtTime(p.bucket, granularity),
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
  granularity: 'minute' | 'hour' | 'day'
}

function ChartTooltip({ active, payload, label, granularity }: ChartTooltipProps) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  const formattedLabel = typeof label === 'number' ? fmtTime(label, granularity) : label
  return (
    <div
      className="rounded-[4px] px-3 py-2 text-[12px]"
      style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}
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

export default function TrendsCard({ raw, range, onRangeChange }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TrendsTab>('requests')
  const isMobile = useMediaQuery('(max-width: 640px)')
  const granularity = range === '1h' ? 'minute' : range === '30d' ? 'day' : 'hour'

  const points = useMemo<ChartPoint[]>(() => {
    const pointGranularity = range === '1h' ? 'minute' : range === '30d' ? 'day' : 'hour'
    return raw.map(point => toChartPoint(point, pointGranularity))
  }, [raw, range])

  const allEmpty = points.length === 0 || points.every(p => !p.hits && !p.misses)

  return (
    <section>
      <SectionHeader
        title={t('dashboard.hitMissTrend')}
        action={
          <div className="flex flex-wrap items-center gap-4">
            {/* Tabs */}
            <div
              className="flex flex-wrap items-center gap-1"
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
                    className="whitespace-nowrap rounded-[4px] px-2.5 py-1 text-[11px] font-[500] transition-[background,color,border-color,transform] duration-150 active:scale-[0.96] cursor-pointer"
                    style={{
                      background: active ? 'var(--bg-soft)' : 'transparent',
                      color: active ? 'var(--text)' : 'var(--text-soft)',
                      border: active ? '0.5px solid var(--border)' : '0.5px solid transparent',
                    }}
                  >
                    {t(`dashboard.${tb.key}`)}
                  </button>
                )
              })}
            </div>
            <span style={{ width: 1, height: 14, background: 'var(--border)' }} aria-hidden />
            {/* Range selector */}
            <div
              className="flex flex-wrap items-center gap-1"
              role="group"
              aria-label={t('dashboard.hitMissTrend')}
            >
              {RANGES.map(r => {
                const active = range === r.value
                return (
                  <button
                    key={r.value}
                    onClick={() => onRangeChange(r.value)}
                    aria-pressed={active}
                    className="whitespace-nowrap rounded-[4px] px-2.5 py-1 text-[11px] font-[500] transition-[background,color,border-color,transform] duration-150 active:scale-[0.96] cursor-pointer"
                    style={{
                      background: active ? 'var(--hit)' : 'transparent',
                      color: active ? 'var(--on-hit)' : 'var(--text-soft)',
                      border: active ? 'none' : '1px solid transparent',
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

      {allEmpty ? (
        <EmptyState
          icon="show_chart"
          title={t('dashboard.emptyTrendTitle')}
          hint={t('dashboard.emptyTrendHint')}
          minHeight={180}
        />
      ) : (
        <ResponsiveContainer width="100%" height={240}>
          <ComposedChart data={points} margin={{ top: 4, right: 12, bottom: 0, left: 0 }}>
            <defs>
              <linearGradient id="trBrand" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--brand)" stopOpacity={0.32} />
                <stop offset="100%" stopColor="var(--brand)" stopOpacity={0.02} />
              </linearGradient>
              <linearGradient id="trDanger" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--danger)" stopOpacity={0.24} />
                <stop offset="100%" stopColor="var(--danger)" stopOpacity={0.02} />
              </linearGradient>
              <linearGradient id="trOk" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--ok)" stopOpacity={0.32} />
                <stop offset="100%" stopColor="var(--ok)" stopOpacity={0.02} />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey="bucket"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickCount={isMobile ? 4 : 8}
              tickFormatter={(value: number) => fmtTime(value, granularity)}
              minTickGap={12}
              {...axisProps}
            />
            <Tooltip content={props => <ChartTooltip {...props} granularity={granularity} />} />
            <Legend wrapperStyle={{ fontSize: 11, paddingTop: 4 }} />

            {tab === 'requests' && (
              <>
                <YAxis yAxisId="count" {...axisProps} width={36} />
                <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tickFormatter={(v: number) => `${v}%`} {...axisProps} width={36} />
                <Area yAxisId="count" type="linear" dataKey="hits" stroke="var(--brand)" strokeWidth={1.5} fill="url(#trBrand)" name={t('dashboard.hits')} isAnimationActive={false} />
                <Area yAxisId="count" type="linear" dataKey="misses" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.misses')} isAnimationActive={false} />
                <Line yAxisId="rate" type="linear" dataKey="hit_rate_pct" stroke="var(--warn-text)" name={t('dashboard.hitRate2')} strokeWidth={1.6} dot={false} strokeDasharray="4 3" isAnimationActive={false} />
              </>
            )}

            {tab === 'bandwidth' && (
              <>
                <YAxis yAxisId="bytes" tickFormatter={(v: number) => formatBytes(v)} {...axisProps} width={56} />
                <Area yAxisId="bytes" type="linear" dataKey="bytes_hit" stroke="var(--ok)" strokeWidth={1.5} fill="url(#trOk)" name={t('dashboard.bytesHit')} stackId="bw" isAnimationActive={false} />
                <Area yAxisId="bytes" type="linear" dataKey="bytes_miss" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.bytesMiss')} stackId="bw" isAnimationActive={false} />
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
                <Area yAxisId="count" type="linear" dataKey="errors" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.trendTabErrors')} isAnimationActive={false} />
                <Line yAxisId="rate" type="linear" dataKey="error_rate_pct" stroke="var(--warn-text)" name={t('dashboard.errorRate')} strokeWidth={1.6} dot={false} strokeDasharray="4 3" isAnimationActive={false} />
              </>
            )}
          </ComposedChart>
        </ResponsiveContainer>
      )}
    </section>
  )
}
