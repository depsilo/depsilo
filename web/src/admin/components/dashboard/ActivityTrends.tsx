import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bar,
  BarChart,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { TooltipContentProps, TooltipValueType } from 'recharts'

import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import type { DashboardRange, DashboardTrendPoint } from '@/lib/adminApi.types'
import { formatBytes } from '@/lib/utils'

export type TrendTab = 'requests' | 'bandwidth' | 'latency' | 'errors'

const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone

const ACCENT = 'var(--dash-accent, var(--brand))'
const ORIGIN = 'var(--dash-origin, var(--warn-text))'
const WARN = 'var(--dash-warn, var(--warn-text))'
const DANGER = 'var(--dash-danger, var(--danger))'

function fmtAxisTime(bucket: number, range: DashboardRange): string {
  const d = new Date(bucket * 1000)
  if (range === '1h') {
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', timeZone: TZ })
  }
  if (range === '30d' || range === '7d') {
    // Coarse ranges use bar categories; include the time so same-day buckets
    // stay distinct instead of collapsing onto one category label.
    return d.toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: TZ })
  }
  return d.toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: TZ })
}

function fmtTooltipTime(bucket: number, range: DashboardRange): string {
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

interface ChartPoint {
  bucket: number
  label: string
  serviceRequests: number
  originRequests: number
  serviceBytes: number
  originBytes: number
  latency: number
  errors: number
  errorRatePct: number
}

function buildPoints(raw: DashboardTrendPoint[], range: DashboardRange): ChartPoint[] {
  return raw.map(point => ({
    bucket: point.bucket,
    label: fmtAxisTime(point.bucket, range),
    serviceRequests: point.requests,
    originRequests: point.upstream_requests ?? 0,
    serviceBytes: point.bytes_served,
    originBytes: point.upstream_bytes ?? 0,
    latency: Math.round(point.avg_latency_ms || 0),
    errors: point.errors,
    errorRatePct: point.requests > 0 ? Math.round((point.errors / point.requests) * 1000) / 10 : 0,
  }))
}

function ChartTooltip({ active, payload, label, range }: TooltipContentProps<TooltipValueType, string | number> & { range: DashboardRange }) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  const payloadBucket = (payload[0]?.payload as { bucket?: number } | undefined)?.bucket
  const formatted = typeof payloadBucket === 'number'
    ? fmtTooltipTime(payloadBucket, range)
    : (typeof label === 'number' ? fmtTooltipTime(label, range) : label)
  return (
    <div
      className="rounded-md px-3 py-2 text-[13px]"
      style={{ background: 'var(--bg-card)', color: 'var(--text)', boxShadow: 'var(--shadow-pop)' }}
    >
      <p className="mb-1 font-medium">{formatted}</p>
      {payload.map(entry => {
        const value = Number(entry.value)
        let display: string
        if (entry.dataKey === 'errorRatePct') display = `${value.toFixed(1)}%`
        else if (entry.dataKey === 'serviceBytes' || entry.dataKey === 'originBytes') display = formatBytes(value)
        else if (entry.dataKey === 'latency') display = `${value.toLocaleString()} ${t('dashboard.msUnit')}`
        else display = value.toLocaleString()
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
  tick: { fill: 'var(--dash-muted, var(--text-soft))', fontSize: 12 },
  axisLine: false as const,
  tickLine: false as const,
}

interface ActivityTrendsProps {
  raw: DashboardTrendPoint[]
  range: DashboardRange
  dataRange: DashboardRange
  isStale: boolean
  onRetry: () => void
}

export default function ActivityTrends({ raw, range, dataRange, isStale, onRetry }: ActivityTrendsProps) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TrendTab>('requests')

  const points = useMemo(() => buildPoints(raw, dataRange), [raw, dataRange])
  const coarse = dataRange === '7d' || dataRange === '30d'
  const allEmpty = points.length === 0 || points.every(point => (
    point.serviceRequests === 0 && point.originRequests === 0 && point.latency === 0 && point.errors === 0
  ))

  const tabs: { value: TrendTab; key: string }[] = [
    { value: 'requests', key: 'trendTabRequests' },
    { value: 'bandwidth', key: 'trendTabBandwidth' },
    { value: 'latency', key: 'trendTabLatency' },
    { value: 'errors', key: 'trendTabErrors' },
  ]
  const activeTab = tabs.find(item => item.value === tab) ?? tabs[0]
  const chartDescription = t('dashboard.trendChartDescription', {
    metric: t(`dashboard.${activeTab.key}`),
    range: t(`dashboard.range${dataRange}`),
  })

  return (
    <section
      data-dashboard-trends
      data-query-key="dashboard-trends"
      data-trend-range={dataRange}
      data-trend-tab={tab}
      data-trend-pending={range !== dataRange ? 'true' : undefined}
      aria-labelledby="overview-trends-title"
      aria-busy={isStale || range !== dataRange || undefined}
      className="dash-card flex min-w-0 flex-col"
    >
      <header className="flex flex-wrap items-center justify-between gap-3 px-5 pb-2 pt-4">
        <h2 id="overview-trends-title" className="text-[19px] font-semibold" style={{ color: 'var(--dash-ink)' }}>
          {t('overview.trendsTitle')}
        </h2>
        <div role="tablist" aria-label={t('overview.trendsTitle')} className="flex items-center gap-1">
          {tabs.map(item => {
            const active = tab === item.value
            return (
              <button
                key={item.value}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setTab(item.value)}
                className="dash-focus min-h-9 rounded-md px-3 text-[14px] font-medium transition-colors duration-150"
                style={{
                  background: active ? 'var(--dash-accent-soft)' : 'transparent',
                  color: active ? 'var(--dash-accent)' : 'var(--dash-muted)',
                }}
              >
                {t(`dashboard.${item.key}`)}
              </button>
            )
          })}
        </div>
      </header>

      {isStale && (
        <div
          role="status"
          className="mx-5 mb-2 flex flex-wrap items-center justify-between gap-2 rounded-md px-3 py-2 text-[13px]"
          style={{ background: 'var(--dash-warn-soft)', color: 'var(--dash-warn)' }}
        >
          <span>{t('overview.dataStale')}</span>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={onRetry}>{t('now.refresh')}</ButtonV2>
        </div>
      )}

      {allEmpty ? (
        <div className="flex min-h-[240px] flex-col items-center justify-center gap-2 px-5 pb-6 text-center">
          <span aria-hidden className="grid size-9 place-items-center rounded-full" style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}>
            <Icon name="show_chart" size="md" />
          </span>
          <h3 className="text-[15px] font-semibold" style={{ color: 'var(--dash-ink)' }}>{t('dashboard.emptyTrendTitle')}</h3>
          <p className="max-w-[48ch] text-[14px]" style={{ color: 'var(--dash-muted)' }}>{t('dashboard.emptyTrendHint')}</p>
        </div>
      ) : (
        <div className="px-2 pb-4">
          <ResponsiveContainer width="100%" height={272}>
            {coarse ? (
              <BarChart data={points} margin={{ top: 6, right: 16, bottom: 0, left: 0 }} desc={chartDescription}>
                <CartesianGrid stroke="var(--dash-border, var(--grid))" vertical={false} />
                <XAxis dataKey="label" minTickGap={20} {...axisProps} />
                {renderBarAxes(tab, t)}
                <Tooltip content={props => <ChartTooltip {...props} range={dataRange} />} />
                <Legend wrapperStyle={{ fontSize: 13, paddingTop: 8 }} />
                {renderBars(tab, t)}
              </BarChart>
            ) : (
              <ComposedChart data={points} margin={{ top: 6, right: 16, bottom: 0, left: 0 }} desc={chartDescription}>
                <CartesianGrid stroke="var(--dash-border, var(--grid))" vertical={false} />
                <XAxis dataKey="bucket" type="number" domain={['dataMin', 'dataMax']} minTickGap={24} tickFormatter={(value: number) => fmtAxisTime(value, dataRange)} {...axisProps} />
                {renderLineAxes(tab, t)}
                <Tooltip content={props => <ChartTooltip {...props} range={dataRange} />} />
                <Legend wrapperStyle={{ fontSize: 13, paddingTop: 8 }} />
                {renderLines(tab, t)}
              </ComposedChart>
            )}
          </ResponsiveContainer>
          <p className="px-3 pt-1 text-[13px]" style={{ color: 'var(--dash-muted)' }}>
            {t('overview.trendAggregation', { range: t(`dashboard.range${dataRange}`) })}
            {range !== dataRange ? ` · ${t('overview.switchingRange')}` : ''}
          </p>
        </div>
      )}
    </section>
  )
}

type Translator = (key: string) => string

function renderLineAxes(tab: TrendTab, t: Translator) {
  if (tab === 'bandwidth') {
    return <YAxis yAxisId="value" width={64} tickFormatter={(value: number) => formatBytes(value)} {...axisProps} />
  }
  if (tab === 'latency') {
    return <YAxis yAxisId="value" width={52} tickFormatter={(value: number) => `${value}${t('dashboard.msUnit')}`} {...axisProps} />
  }
  if (tab === 'errors') {
    return (
      <>
        <YAxis yAxisId="value" width={44} {...axisProps} />
        <YAxis yAxisId="rate" orientation="right" width={48} tickFormatter={(value: number) => `${value}%`} {...axisProps} />
      </>
    )
  }
  return <YAxis yAxisId="value" width={44} {...axisProps} />
}

function renderBarAxes(tab: TrendTab, t: Translator) {
  return renderLineAxes(tab, t)
}

function renderLines(tab: TrendTab, t: Translator) {
  switch (tab) {
    case 'bandwidth':
      return (
        <>
          <Line yAxisId="value" type="linear" dataKey="serviceBytes" stroke={ACCENT} strokeWidth={2} dot={false} name={t('overview.serviceFlow')} isAnimationActive={false} />
          <Line yAxisId="value" type="linear" dataKey="originBytes" stroke={ORIGIN} strokeWidth={2} dot={false} name={t('overview.originFlow')} isAnimationActive={false} />
        </>
      )
    case 'latency':
      return <Line yAxisId="value" type="linear" dataKey="latency" stroke={ACCENT} strokeWidth={2} dot={false} name={t('overview.clientLatency')} isAnimationActive={false} />
    case 'errors':
      return (
        <>
          <Line yAxisId="value" type="linear" dataKey="errors" stroke={DANGER} strokeWidth={2} dot={false} name={t('dashboard.trendTabErrors')} isAnimationActive={false} />
          <Line yAxisId="rate" type="linear" dataKey="errorRatePct" stroke={WARN} strokeWidth={1.6} dot={false} strokeDasharray="4 3" name={t('dashboard.errorRate')} isAnimationActive={false} />
        </>
      )
    default:
      return (
        <>
          <Line yAxisId="value" type="linear" dataKey="serviceRequests" stroke={ACCENT} strokeWidth={2} dot={false} name={t('overview.serviceRequests')} isAnimationActive={false} />
          <Line yAxisId="value" type="linear" dataKey="originRequests" stroke={ORIGIN} strokeWidth={2} dot={false} name={t('overview.originRequests')} isAnimationActive={false} />
        </>
      )
  }
}

function renderBars(tab: TrendTab, t: Translator) {
  switch (tab) {
    case 'bandwidth':
      return (
        <>
          <Bar yAxisId="value" dataKey="serviceBytes" fill={ACCENT} radius={[3, 3, 0, 0]} name={t('overview.serviceFlow')} isAnimationActive={false} />
          <Bar yAxisId="value" dataKey="originBytes" fill={ORIGIN} radius={[3, 3, 0, 0]} name={t('overview.originFlow')} isAnimationActive={false} />
        </>
      )
    case 'latency':
      return <Bar yAxisId="value" dataKey="latency" fill={ACCENT} radius={[3, 3, 0, 0]} name={t('overview.clientLatency')} isAnimationActive={false} />
    case 'errors':
      return (
        <>
          <Bar yAxisId="value" dataKey="errors" fill={DANGER} radius={[3, 3, 0, 0]} name={t('dashboard.trendTabErrors')} isAnimationActive={false} />
          <Line yAxisId="rate" type="linear" dataKey="errorRatePct" stroke={WARN} strokeWidth={1.6} dot={false} strokeDasharray="4 3" name={t('dashboard.errorRate')} isAnimationActive={false} />
        </>
      )
    default:
      return (
        <>
          <Bar yAxisId="value" dataKey="serviceRequests" fill={ACCENT} radius={[3, 3, 0, 0]} name={t('overview.serviceRequests')} isAnimationActive={false} />
          <Bar yAxisId="value" dataKey="originRequests" fill={ORIGIN} radius={[3, 3, 0, 0]} name={t('overview.originRequests')} isAnimationActive={false} />
        </>
      )
  }
}
