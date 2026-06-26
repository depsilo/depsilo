// Tabbed trends chart. One backend request per range carries every
// dimension a tab could render (requests / bandwidth / latency / errors),
// so switching tabs is a pure re-render — no refetch, no jitter.
//
// Bucket timestamps are always rendered in the browser's timezone via
// Intl.DateTimeFormat. The 7d/30d ranges arrive as hourly granularity and
// are re-aggregated into days using local-tz boundaries (so a Beijing user
// sees "周三 2026-06-26" rather than the UTC date the server happens to
// hold the row under).
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

import EmptyState from '@/components/EmptyState'
import SectionHeader from '@/components/SectionHeader'
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

// dayKey returns a stable YYYY-MM-DD string in the browser's local timezone.
// Used for re-aggregating hourly buckets into local days.
function dayKey(bucket: number): string {
  const d = new Date(bucket * 1000)
  return d.toLocaleDateString('en-CA', { timeZone: TZ }) // en-CA happens to give YYYY-MM-DD
}

function dayStartUnix(bucket: number): number {
  // Bucket start in local-tz at 00:00. We reconstruct it via toLocaleDateString
  // to avoid pulling in a date-fns dependency.
  const k = dayKey(bucket)
  return Math.floor(new Date(`${k}T00:00:00`).getTime() / 1000)
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

function rebucketDays(raw: RawTrendPoint[]): ChartPoint[] {
  const byDay = new Map<string, RawTrendPoint>()
  for (const p of raw) {
    const k = dayKey(p.bucket)
    const acc = byDay.get(k)
    if (!acc) {
      byDay.set(k, {
        ...p,
        bucket: dayStartUnix(p.bucket),
      })
      continue
    }
    acc.requests += p.requests
    acc.hits += p.hits
    acc.misses += p.misses
    acc.bytes_hit += p.bytes_hit
    acc.bytes_miss += p.bytes_miss
    acc.bytes_served += p.bytes_served
    acc.sum_latency_ms += p.sum_latency_ms
    acc.errors += p.errors
    if (acc.requests > 0) {
      acc.hit_rate = acc.hits / acc.requests
      acc.avg_latency_ms = acc.sum_latency_ms / acc.requests
    }
  }
  const days = Array.from(byDay.values()).sort((a, b) => a.bucket - b.bucket)
  return days.map(p => toChartPoint(p, 'day'))
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

function ChartTooltip({ active, payload, label }: any) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  return (
    <div
      className="rounded-[4px] px-3 py-2 text-[12px]"
      style={{ background: 'var(--bg-card)', border: '1px solid var(--border)' }}
    >
      <p className="font-[400] mb-1" style={{ color: 'var(--text)' }}>{label}</p>
      {payload.map((entry: any) => {
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
          <p key={entry.dataKey} className="font-mono tabular-nums" style={{ color: entry.color }}>
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

  const points = useMemo<ChartPoint[]>(() => {
    if (range === '1h') return raw.map(p => toChartPoint(p, 'minute'))
    if (range === '24h') return raw.map(p => toChartPoint(p, 'hour'))
    return rebucketDays(raw)
  }, [raw, range])

  const allEmpty = points.length === 0 || points.every(p => !p.hits && !p.misses)

  return (
    <section>
      <SectionHeader
        title={t('dashboard.hitMissTrend')}
        action={
          <div className="flex items-center gap-4">
            {/* Tabs */}
            <div className="flex items-center gap-1">
              {TABS.map(tb => {
                const active = tab === tb.value
                return (
                  <button
                    key={tb.value}
                    onClick={() => setTab(tb.value)}
                    className="px-2 py-0.5 text-[11px] font-[500] rounded-[4px] cursor-pointer transition-colors duration-150"
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
            <div className="flex items-center gap-1">
              {RANGES.map(r => {
                const active = range === r.value
                return (
                  <button
                    key={r.value}
                    onClick={() => onRangeChange(r.value)}
                    className="px-2 py-0.5 text-[11px] font-[500] rounded-[4px] cursor-pointer transition-colors duration-150"
                    style={{
                      background: active ? 'var(--brand)' : 'transparent',
                      color: active ? 'white' : 'var(--text-soft)',
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
        <ResponsiveContainer width="100%" height={180}>
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
            <XAxis dataKey="label" {...axisProps} />
            <Tooltip content={<ChartTooltip />} />
            <Legend wrapperStyle={{ fontSize: 11, paddingTop: 4 }} />

            {tab === 'requests' && (
              <>
                <YAxis yAxisId="count" {...axisProps} width={36} />
                <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tickFormatter={(v: number) => `${v}%`} {...axisProps} width={36} />
                <Area yAxisId="count" type="monotone" dataKey="hits" stroke="var(--brand)" strokeWidth={1.5} fill="url(#trBrand)" name={t('dashboard.hits')} />
                <Area yAxisId="count" type="monotone" dataKey="misses" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.misses')} />
                <Line yAxisId="rate" type="monotone" dataKey="hit_rate_pct" stroke="var(--warn-text)" name={t('dashboard.hitRate2')} strokeWidth={1.6} dot={false} strokeDasharray="4 3" />
              </>
            )}

            {tab === 'bandwidth' && (
              <>
                <YAxis yAxisId="bytes" tickFormatter={(v: number) => formatBytes(v)} {...axisProps} width={56} />
                <Area yAxisId="bytes" type="monotone" dataKey="bytes_hit" stroke="var(--ok)" strokeWidth={1.5} fill="url(#trOk)" name={t('dashboard.bytesHit')} stackId="bw" />
                <Area yAxisId="bytes" type="monotone" dataKey="bytes_miss" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.bytesMiss')} stackId="bw" />
              </>
            )}

            {tab === 'latency' && (
              <>
                <YAxis yAxisId="ms" {...axisProps} width={48} tickFormatter={(v: number) => `${v}ms`} />
                <Line yAxisId="ms" type="monotone" dataKey="avg_latency_ms" stroke="var(--brand)" strokeWidth={1.8} dot={false} name={t('dashboard.avgLatency')} />
              </>
            )}

            {tab === 'errors' && (
              <>
                <YAxis yAxisId="count" {...axisProps} width={36} />
                <YAxis yAxisId="rate" orientation="right" domain={[0, 'auto']} tickFormatter={(v: number) => `${v}%`} {...axisProps} width={42} />
                <Area yAxisId="count" type="monotone" dataKey="errors" stroke="var(--danger)" strokeWidth={1.5} fill="url(#trDanger)" name={t('dashboard.trendTabErrors')} />
                <Line yAxisId="rate" type="monotone" dataKey="error_rate_pct" stroke="var(--warn-text)" name={t('dashboard.errorRate')} strokeWidth={1.6} dot={false} strokeDasharray="4 3" />
              </>
            )}
          </ComposedChart>
        </ResponsiveContainer>
      )}
    </section>
  )
}
