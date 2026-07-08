import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import StatusDot from '@/components/StatusDot'
import Sparkline from '@/components/Sparkline'
import HeroSparkline from '@/components/HeroSparkline'
import { UpstreamGroupedPanel, type UpstreamItem } from '@/components/UpstreamCard'
import type { MirrorStatus } from '@/lib/ecosystemData'

interface LatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
  requests: number
}

interface UpstreamInfo {
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  latency_series?: LatencyPoint[]
}

interface SeriesPoint {
  time_unix: number
  requests: number
  hits: number
  hit_rate: number
  bytes_saved: number
  avg_latency_ms: number
}

interface StatsData {
  service: { status: string }
  today: {
    total_requests: number
    hit_count: number
    miss_count: number
    hit_rate: number
    bytes_served: number
    bytes_saved: number
  }
  cache: { total_files: number; total_size_bytes: number }
  upstreams: UpstreamInfo[]
  series?: {
    interval_minutes: number
    points: SeriesPoint[]
  }
}

function formatBytes(bytes: number): { value: string; unit: string } {
  if (bytes >= 1e12) return { value: (bytes / 1e12).toFixed(1), unit: 'TB' }
  if (bytes >= 1e9)  return { value: (bytes / 1e9).toFixed(0),  unit: 'GB' }
  if (bytes >= 1e6)  return { value: (bytes / 1e6).toFixed(0),  unit: 'MB' }
  return { value: String(bytes), unit: 'B' }
}

function formatRequests(n: number): { value: string; unit: string } {
  if (n >= 1e6) return { value: (n / 1e6).toFixed(2), unit: 'M' }
  if (n >= 1e3) return { value: (n / 1e3).toFixed(1), unit: 'K' }
  return { value: String(n), unit: '' }
}

// ── HitRateHero ───────────────────────────────────────────────────

function HitRateHero({ hitRate, series }: { hitRate: number; series: SeriesPoint[] }) {
  const { t } = useTranslation()
  const displayRate = (hitRate * 100).toFixed(1)
  const sparkValues = series.length > 0 ? series.map(p => p.hit_rate) : [0, 0]

  return (
    <div
      className="card aurora-rim"
      style={{
        padding: '28px 32px',
        display: 'grid',
        gridTemplateColumns: '320px 1fr',
        alignItems: 'stretch',
        gap: 32,
        minHeight: 168,
        position: 'relative',
        overflow: 'hidden',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <div>
          <div
            className="eyebrow"
            style={{ display: 'flex', alignItems: 'center', gap: 8 }}
          >
            <StatusDot status="healthy" live />
            <span>{t('monitor.hitRateToday')}</span>
          </div>
          <div
            className="aurora-glow"
            style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 14 }}
          >
            <span
              className="grad-text-aurora"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 76,
                fontWeight: 600,
                letterSpacing: '-0.06em',
                lineHeight: 1,
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              {displayRate}
            </span>
            <span style={{ fontSize: 22, color: 'var(--text-soft)' }}>%</span>
          </div>
        </div>
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          position: 'relative',
          borderLeft: '0.5px solid var(--border)',
          paddingLeft: 32,
        }}
      >
        <HeroSparkline values={sparkValues} />
      </div>
    </div>
  )
}

// ── StatStrip ─────────────────────────────────────────────────────

function StatStrip({ data, upstreams, series }: { data: StatsData['today']; upstreams: UpstreamInfo[]; series: SeriesPoint[] }) {
  const { t } = useTranslation()
  const reqFmt   = formatRequests(data.total_requests)
  const savedFmt = formatBytes(data.bytes_saved)

  const healthyLatencies = upstreams.filter(u => u.healthy && u.avg_latency_ms > 0).map(u => u.avg_latency_ms)
  const p50Ms = healthyLatencies.length > 0
    ? Math.round(healthyLatencies.reduce((a, b) => a + b, 0) / healthyLatencies.length)
    : null

  const reqSeries     = series.length > 0 ? series.map(p => p.requests) : [0, 0]
  const savedSeries   = series.length > 0 ? series.map(p => p.bytes_saved) : [0, 0]
  const latencySeries = series.length > 0 ? series.map(p => p.avg_latency_ms) : [0, 0]

  const items = [
    {
      label: t('monitor.requests'),
      value: reqFmt.value,
      unit: reqFmt.unit,
      tone: 'brand' as const,
      series: reqSeries,
    },
    {
      label: t('monitor.bandwidthSaved'),
      value: savedFmt.value,
      unit: savedFmt.unit,
      tone: 'brand' as const,
      series: savedSeries,
    },
    {
      label: t('monitor.avgLatency'),
      value: p50Ms !== null ? String(p50Ms) : '-',
      unit: p50Ms !== null ? 'ms' : '',
      tone: 'neutral' as const,
      series: latencySeries,
    },
  ]

  return (
    <div
      className="card"
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(3, 1fr)',
        gap: 0,
        padding: 0,
      }}
    >
      {items.map((it, i) => (
        <div
          key={it.label}
          style={{
            padding: '16px 20px',
            borderRight: i < items.length - 1 ? '0.5px solid var(--border)' : 'none',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{it.label}</span>
          </div>
          <div
            style={{
              display: 'flex',
              alignItems: 'baseline',
              justifyContent: 'space-between',
              gap: 8,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 32,
                  fontWeight: 600,
                  letterSpacing: '-0.04em',
                  fontVariantNumeric: 'tabular-nums',
                }}
              >
                {it.value}
              </span>
              <span style={{ fontSize: 13, color: 'var(--text-soft)' }}>{it.unit}</span>
            </div>
            <Sparkline data={it.series} width={120} height={26} tone={it.tone} />
          </div>
        </div>
      ))}
    </div>
  )
}

// ── MirrorTile ────────────────────────────────────────────────────

function mirrorStatus(u: UpstreamInfo): MirrorStatus {
  if (!u.healthy) return 'failed'
  if (u.avg_latency_ms > 150) return 'degraded'
  return 'healthy'
}

function latencySeriesToBeats(series?: LatencyPoint[]): (number | null)[] {
  if (!series || series.length === 0) return []
  return series.filter(pt => pt.requests > 0).map(pt => {
    if (!pt.healthy) return -1
    return pt.latency_ms
  })
}

function latencySeriesToLabels(series: LatencyPoint[] | undefined, locale: string): string[] {
  return series?.filter(pt => pt.requests > 0).map(pt =>
    new Date(pt.time).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  ) ?? []
}

function toUpstreamItem(upstream: UpstreamInfo, locale: string): UpstreamItem {
  const beats = latencySeriesToBeats(upstream.latency_series)
  return {
    name: upstream.name,
    adapter: upstream.adapter,
    healthy: upstream.healthy,
    avg_latency_ms: upstream.avg_latency_ms,
    success_rate: upstream.success_rate,
    beats: beats.length > 0 ? beats : undefined,
    beatLabels: latencySeriesToLabels(upstream.latency_series, locale),
  }
}

export default function MonitorPage() {
  const { t, i18n } = useTranslation()
  const locale = i18n.language === 'zh' ? 'zh-CN' : 'en-US'
  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  // Latency series loaded separately (heavy query, ~160KB)
  const { data: latencyMap } = useQuery<Record<string, LatencyPoint[]>>({
    queryKey: ['latency-series'],
    queryFn: async () => {
      const res = await statsApi.getLatencySeries()
      return res.data
    },
    refetchInterval: 60000,
  })

  // Merge latency_series into upstream objects
  const rawUpstreams = data?.upstreams ?? []
  const upstreams = rawUpstreams.map(u => ({
    ...u,
    latency_series: latencyMap?.[u.name],
  }))
  const hitRate   = data?.today.hit_rate ?? 0
  const series    = data?.series?.points ?? []
  const today     = data?.today ?? {
    total_requests: 0,
    hit_count: 0,
    miss_count: 0,
    hit_rate: 0,
    bytes_served: 0,
    bytes_saved: 0,
  }

  const healthyCounts = upstreams.reduce(
    (acc, u) => {
      const s = mirrorStatus(u)
      acc[s] = (acc[s] ?? 0) + 1
      return acc
    },
    {} as Record<MirrorStatus, number>
  )
  const upstreamItems = upstreams.map(u => toUpstreamItem(u, locale))

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      {/* Page summary */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div>
          <h1
            style={{
              margin: 0,
              fontSize: 44,
              fontWeight: 700,
              letterSpacing: '-0.04em',
              lineHeight: 1.02,
              color: 'var(--text)',
            }}
          >
            {t('monitor.title')}
          </h1>
          <p
            style={{
              margin: '10px 0 0 0',
              display: 'flex',
              alignItems: 'center',
              flexWrap: 'wrap',
              gap: '8px 12px',
              fontSize: 13,
              lineHeight: 1.3,
              color: 'var(--text-muted)',
            }}
          >
            <span>
              <span className="num">{upstreams.length}</span> {t('monitor.upstreams')}
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
              <StatusDot status="healthy" />
              <span className="num">{healthyCounts.healthy ?? 0}</span> {t('monitor.healthy')}
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
              <StatusDot status="degraded" />
              <span className="num">{healthyCounts.degraded ?? 0}</span> {t('monitor.degraded')}
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
              <StatusDot status="failed" />
              <span className="num">{healthyCounts.failed ?? 0}</span> {t('monitor.failed')}
            </span>
          </p>
        </div>
      </div>

      {today.total_requests > 0 && (
        <>
          <HitRateHero hitRate={hitRate} series={series} />
          <StatStrip data={today} upstreams={upstreams} series={series} />
        </>
      )}

      {/* Mirrors section */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
        {upstreamItems.length > 0 && (
          <UpstreamGroupedPanel upstreams={upstreamItems} variant="cards" minColumnWidth={380} />
        )}
      </div>
    </div>
  )
}
