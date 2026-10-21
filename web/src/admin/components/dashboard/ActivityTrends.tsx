import Icon from '@/components/Icon'
import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { DotItemDotProps, TooltipContentProps, TooltipValueType } from 'recharts'

import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { DashboardRange, DashboardTrendPoint } from '@/lib/adminApi.types'
import { formatBytes } from '@/lib/utils'

import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import DashboardMetricHelp from './DashboardMetricHelp'

export type TrendTab = 'requests' | 'bandwidth' | 'latency' | 'errors'

/** Each metric aggregates differently; the measurement notes must say which. */
const TREND_MEASUREMENT_KEY: Record<TrendTab, string> = {
  requests: 'overview.trendAggregationRequests',
  bandwidth: 'overview.trendAggregationTraffic',
  latency: 'overview.trendAggregationLatency',
  errors: 'overview.trendAggregationErrors',
}

const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone

const ACCENT = 'var(--dash-accent, var(--brand))'
const ORIGIN = 'var(--dash-origin, var(--warn-text))'
const WARN = 'var(--dash-warn, var(--warn-text))'
const DANGER = 'var(--dash-danger, var(--danger))'

function fmtRangeTime(bucket: number, range: DashboardRange, locale: string): string {
  const d = new Date(bucket * 1000)
  if (range === '1h') {
    return d.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', timeZone: TZ })
  }
  return d.toLocaleString(locale, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: TZ })
}

function fmtTooltipTime(bucket: number, range: DashboardRange, locale: string): string {
  const d = new Date(bucket * 1000)
  return d.toLocaleString(locale, {
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

function trendTotals(raw: DashboardTrendPoint[]): Omit<ChartPoint, 'bucket'> {
  const total = raw.reduce((sum, point) => ({
    serviceRequests: sum.serviceRequests + point.requests,
    originRequests: sum.originRequests + (point.upstream_requests ?? 0),
    serviceBytes: sum.serviceBytes + point.bytes_served,
    originBytes: sum.originBytes + (point.upstream_bytes ?? 0),
    latencySum: sum.latencySum + point.sum_latency_ms,
    errors: sum.errors + point.errors,
  }), { serviceRequests: 0, originRequests: 0, serviceBytes: 0, originBytes: 0, latencySum: 0, errors: 0 })
  return {
    ...total,
    latency: total.serviceRequests > 0 ? Math.round(total.latencySum / total.serviceRequests) : null,
    errorRatePct: total.serviceRequests > 0 ? total.errors / total.serviceRequests * 100 : null,
  }
}

function formatTrendValue(key: TrendDataKey, value: number | null, locale: string, t: Translator): string {
  if (value == null) return '—'
  if (key === 'errorRatePct') return `${value.toFixed(1)}%`
  if (key === 'serviceBytes' || key === 'originBytes') return formatBytes(value)
  if (key === 'latency') return `${value.toLocaleString(locale)} ${t('dashboard.msUnit')}`
  return value.toLocaleString(locale)
}

interface ChartPoint {
  bucket: number
  serviceRequests: number
  originRequests: number
  serviceBytes: number
  originBytes: number
  latency: number | null
  errors: number
  errorRatePct: number | null
}

function buildPoints(raw: DashboardTrendPoint[]): ChartPoint[] {
  return raw.map(point => ({
    bucket: point.bucket,
    serviceRequests: point.requests,
    originRequests: point.upstream_requests ?? 0,
    serviceBytes: point.bytes_served,
    originBytes: point.upstream_bytes ?? 0,
    latency: point.requests > 0 ? Math.round(point.avg_latency_ms) : null,
    errors: point.errors,
    errorRatePct: point.requests > 0 ? Math.round((point.errors / point.requests) * 1000) / 10 : null,
  }))
}

function ChartTooltip({ active, payload, label, range }: TooltipContentProps<TooltipValueType, string | number> & { range: DashboardRange }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en'
  if (!active || !payload?.length) return null
  const payloadBucket = (payload[0]?.payload as { bucket?: number } | undefined)?.bucket
  const formatted = typeof payloadBucket === 'number'
    ? fmtTooltipTime(payloadBucket, range, locale)
    : (typeof label === 'number' ? fmtTooltipTime(label, range, locale) : label)
  return (
    <div
      className="dashboard-trend-tooltip rounded-md border border-[var(--border)] px-3 py-2 text-[14px] leading-[1.65]"
      style={{ background: 'var(--bg-card)', color: 'var(--text)', boxShadow: 'var(--shadow-pop)' }}
    >
      <p className="mb-1 font-medium">{formatted}</p>
      {payload.map(entry => {
        const value = Number(entry.value)
        let display: string
        if (entry.value == null) display = t('overview.noSamples')
        else if (entry.dataKey === 'errorRatePct') display = `${value.toFixed(1)}%`
        else if (entry.dataKey === 'serviceBytes' || entry.dataKey === 'originBytes') display = formatBytes(value)
        else if (entry.dataKey === 'latency') {
          const samples = (payload[0]?.payload as { serviceRequests?: number } | undefined)?.serviceRequests
          display = `${value.toLocaleString(locale)} ${t('dashboard.msUnit')}`
          if (samples && samples > 0) {
            display += ` · ${t('overview.samplesShort', { count: samples.toLocaleString(locale) })}`
          }
        }
        else display = value.toLocaleString(locale)
        return (
          <p key={String(entry.dataKey)} className="font-mono tabular-nums" style={{ color: entry.color }}>
            {entry.name}: {display}
          </p>
        )
      })}
    </div>
  )
}

interface ActivityTrendsProps {
  raw: DashboardTrendPoint[]
  range: DashboardRange
  dataRange: DashboardRange
  isStale: boolean
  onRetry: () => void
}

export default function ActivityTrends({ raw, range, dataRange, isStale, onRetry }: ActivityTrendsProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.resolvedLanguage?.startsWith('zh') ? 'zh-CN' : 'en'
  const [tab, setTab] = useState<TrendTab>('requests')
  const gradientId = useId()

  const points = useMemo(() => buildPoints(raw), [raw])
  const totals = useMemo(() => trendTotals(raw), [raw])
  const series = trendSeries(tab, t)
  const allEmpty = points.length === 0 || points.every(point => (
    point.serviceRequests === 0 && point.originRequests === 0 && point.errors === 0
  ))

  const tabs: { value: TrendTab; key: string }[] = [
    { value: 'requests', key: 'trendTabRequests' },
    { value: 'bandwidth', key: 'trendTabBandwidth' },
    { value: 'latency', key: 'trendTabLatency' },
    { value: 'errors', key: 'trendTabErrors' },
  ]
  const activeTab = tabs.find(item => item.value === tab) ?? tabs[0]

  return (
    <Tabs
      value={tab}
      onValueChange={value => setTab(value as TrendTab)}
      render={<Card render={<section />} />}
      data-dashboard-trends
      data-query-key="dashboard-trends"
      data-trend-range={dataRange}
      data-trend-tab={tab}
      data-trend-pending={range !== dataRange ? 'true' : undefined}
      aria-labelledby="overview-trends-title"
      aria-busy={isStale || range !== dataRange || undefined}
      className="h-full min-w-0 flex-1 gap-0 p-0"
    >
      <CardHeader className="min-h-[76px] gap-x-3 py-3">
        <div className="flex flex-wrap items-center gap-x-2">
          <CardTitle id="overview-trends-title">
            <DashboardMetricHelp label={t('overview.trendsTitle')} ariaLabel={t('overview.trendMeasurementInfo')}>
              <p>{t('overview.trendSummaryBasis')}</p>
              <p>{t(TREND_MEASUREMENT_KEY[tab], { range: t(`dashboard.range${dataRange}`) })}</p>
              <p>{t('overview.trendScaleHint')}</p>
            </DashboardMetricHelp>
          </CardTitle>
          <span className="text-[13px] text-[var(--dash-muted)]">{t(`dashboard.range${dataRange}`)}</span>
        </div>
        {/* Wraps rather than shrinking: four segments do not fit beside the
            heading on a phone, and the rail had been overflowing the card. */}
        <TabsList aria-label={t('overview.trendsTitle')} activateOnFocus>
          {tabs.map(item => (
            <TabsTrigger key={item.value} value={item.value}>
              {t(`dashboard.${item.key}`)}
            </TabsTrigger>
          ))}
        </TabsList>
      </CardHeader>

      <TabsContent value={tab} keepMounted className="flex flex-col">
        {range !== dataRange && <p role="status" className="px-5 pb-2 text-[13px] text-[var(--dash-muted)]">{t('overview.switchingRange')}</p>}
        {isStale && (
          <div className="px-5 pb-2">
            <Alert variant="warning" role="status" className="flex flex-wrap items-center justify-between gap-2">
              <AlertDescription>{t('overview.dataStale')}</AlertDescription>
              <Button type="button" variant="outline" size="sm" onClick={onRetry}>{t('now.refresh')}</Button>
            </Alert>
          </div>
        )}

        {allEmpty ? (
          <Empty className="min-h-[112px] gap-1.5 px-5 pb-5 pt-2">
            <EmptyHeader className="gap-1.5">
              <EmptyMedia variant="icon" className="mb-0 rounded-full"><Icon name="show_chart" size="sm" /></EmptyMedia>
              <EmptyTitle>{t('overview.emptyTrend')}</EmptyTitle>
              <EmptyDescription>{t('overview.emptyTrendHint')}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="dashboard-trend-content pb-3">
            <dl aria-label={t('overview.trendSummary')} className="dashboard-trend-summary px-5 pb-3">
              {series.map(item => (
                <div key={item.key} data-trend-series={item.key} className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1">
                  <dt className="flex items-center gap-2 text-[13px] text-[var(--dash-muted)]">
                    <span aria-hidden className="w-3 shrink-0 border-t-2" style={{ borderColor: item.color, borderStyle: item.key === 'errorRatePct' ? 'dashed' : 'solid' }} />
                    {item.name}
                  </dt>
                  <dd data-trend-value={item.key} className="font-mono text-[16px] font-semibold leading-tight tabular-nums text-[var(--dash-ink)]">
                    {formatTrendValue(item.key, totals[item.key], locale, t)}
                  </dd>
                  {totals[item.key] == null && <dd className="mt-1 text-[12px] text-[var(--dash-muted)]">{t('overview.noSamples')}</dd>}
                </div>
              ))}
            </dl>
            <div data-trend-chart className="dashboard-trend-chart px-3">
              <ResponsiveContainer width="100%" height={250}>
                <ComposedChart data={points} margin={{ top: 12, right: 12, bottom: 4, left: 12 }} desc={t('dashboard.trendChartDescription', { metric: t(`dashboard.${activeTab.key}`), range: t(`dashboard.range${dataRange}`) })}>
                  <defs>
                    {series.filter(item => item.key !== 'errorRatePct').map(item => (
                      <linearGradient key={item.key} id={`${gradientId}-${item.key}`} x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={item.color} stopOpacity={0.5} />
                        <stop offset="100%" stopColor={item.color} stopOpacity={0.04} />
                      </linearGradient>
                    ))}
                  </defs>
                  <CartesianGrid yAxisId="value" vertical={false} stroke="var(--dash-border)" strokeOpacity={0.55} />
                  <XAxis
                    dataKey="bucket"
                    type="number"
                    domain={['dataMin', 'dataMax']}
                    interval="preserveStartEnd"
                    minTickGap={40}
                    tickMargin={10}
                    tick={{ fill: 'var(--dash-muted)', fontSize: 12 }}
                    tickFormatter={(value: number) => fmtRangeTime(value, dataRange, locale)}
                    tickLine={false}
                    axisLine={false}
                    height={32}
                  />
                  <YAxis yAxisId="value" hide={tab !== 'errors'} width={30} domain={[0, 'auto']} tickCount={4} allowDecimals={tab === 'bandwidth' || tab === 'latency'} tick={{ fill: 'var(--dash-muted)', fontSize: 12 }} tickLine={false} axisLine={false} />
                  {tab === 'errors' && <YAxis yAxisId="rate" orientation="right" width={42} domain={[0, 100]} ticks={[0, 50, 100]} tickFormatter={(value: number) => `${value}%`} tick={{ fill: 'var(--dash-muted)', fontSize: 12 }} tickLine={false} axisLine={false} />}
                  <Tooltip
                    filterNull={false}
                    cursor={false}
                    isAnimationActive={false}
                    wrapperStyle={{ left: 'auto', right: 8, top: 8, transform: 'none' }}
                    content={props => <ChartTooltip {...props} range={dataRange} />}
                  />
                  {series.map(item => {
                    const props = {
                      type: 'monotone' as const,
                      dataKey: item.key,
                      name: item.name,
                      stroke: item.color,
                      strokeWidth: 1.75,
                      strokeLinecap: 'round' as const,
                      strokeLinejoin: 'round' as const,
                      connectNulls: false,
                      dot: trendDot(points, item.key, item.color),
                      activeDot: { r: 4, stroke: 'var(--dash-card)', strokeWidth: 2 },
                      isAnimationActive: false,
                    }
                    // Overlay independent quantities; stacking would add origin
                    // traffic to already-delivered traffic and invent a total.
                    return item.key === 'errorRatePct'
                      ? <Line key={item.key} {...props} yAxisId="rate" strokeDasharray="4 3" />
                      : <Area key={item.key} {...props} yAxisId="value" baseValue={0} fill={`url(#${gradientId}-${item.key})`} fillOpacity={1} />
                  })}
                </ComposedChart>
              </ResponsiveContainer>
            </div>
          </div>
        )}
      </TabsContent>
    </Tabs>
  )
}

type Translator = (key: string) => string

// Same-unit series share a zero baseline and time axis. Monotone interpolation
// smooths the joins without overshooting the observed values; gaps stay gaps.
type TrendDataKey = Exclude<keyof ChartPoint, 'bucket'>
interface TrendSeries {
  key: TrendDataKey
  name: string
  color: string
}

function trendSeries(tab: TrendTab, t: Translator): TrendSeries[] {
  switch (tab) {
    case 'bandwidth':
      return [
        { key: 'serviceBytes', name: t('overview.serviceFlow'), color: ACCENT },
        { key: 'originBytes', name: t('overview.originFlow'), color: ORIGIN },
      ]
    case 'latency':
      return [{ key: 'latency', name: t('overview.clientLatency'), color: ACCENT }]
    case 'errors':
      return [
        { key: 'errors', name: t('dashboard.trendTabErrors'), color: DANGER },
        { key: 'errorRatePct', name: t('dashboard.errorRate'), color: WARN },
      ]
    default:
      return [
        { key: 'serviceRequests', name: t('overview.serviceRequests'), color: ACCENT },
        { key: 'originRequests', name: t('overview.originRequests'), color: ORIGIN },
      ]
  }
}

function trendDot(points: ChartPoint[], key: TrendDataKey, color: string) {
  return ({ index, cx, cy }: DotItemDotProps) => {
    const value = points[index]?.[key]
    if (value == null || !Number.isFinite(value) || cx == null || cy == null) return null
    const isolated = points[index - 1]?.[key] == null && points[index + 1]?.[key] == null
    if (index !== points.length - 1 && !isolated) return null
    return <circle key={`${key}-${index}`} className="dashboard-trend-dot" data-series={key} cx={cx} cy={cy} r={2.2} fill={color} />
  }
}
