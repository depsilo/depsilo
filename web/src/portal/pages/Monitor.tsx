import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import StatusDot from '@/components/StatusDot'
import Sparkline from '@/components/Sparkline'
import HeroSparkline from '@/components/HeroSparkline'
import StatusBar from '@/components/StatusBar'
import EcosystemIcon from '@/components/EcosystemIcon'
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
      value: p50Ms !== null ? String(p50Ms) : '—',
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

function UpstreamRow({ upstream }: { upstream: UpstreamInfo }) {
  const status = mirrorStatus(upstream)
  const isFailed = status === 'failed'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
        <StatusDot status={status} />
        <span
          className="mono"
          style={{
            fontSize: 11.5,
            color: isFailed ? 'var(--text-subtle)' : 'var(--text)',
            textDecoration: isFailed ? 'line-through' : 'none',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
            flex: 1,
            minWidth: 0,
          }}
        >
          {upstream.name}
        </span>
        <span
          className="num"
          style={{
            fontSize: 11,
            flexShrink: 0,
            color: isFailed
              ? 'var(--text-subtle)'
              : upstream.avg_latency_ms > 100
              ? 'var(--warn-text)'
              : 'var(--text-muted)',
          }}
        >
          {isFailed ? '—' : `${upstream.avg_latency_ms}ms`}
        </span>
      </div>
      <StatusBar points={upstream.latency_series ?? []} />
    </div>
  )
}

function EcosystemCard({ adapter, upstreams }: { adapter: string; upstreams: UpstreamInfo[] }) {
  const worstStatus = upstreams.some(u => !u.healthy)
    ? 'failed'
    : upstreams.some(u => u.avg_latency_ms > 150)
    ? 'degraded'
    : 'healthy'

  return (
    <div
      className="card"
      style={{
        padding: '12px 14px',
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        breakInside: 'avoid',
        marginBottom: 12,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <EcosystemIcon type={adapter as any} size={16} useColor />
        <span style={{ fontSize: 13, fontWeight: 600, letterSpacing: '-0.01em' }}>
          {adapter}
        </span>
        <StatusDot status={worstStatus as MirrorStatus} />
        <span style={{ fontSize: 10, color: 'var(--text-subtle)', marginLeft: 'auto' }}>
          {upstreams.length} {upstreams.length === 1 ? 'mirror' : 'mirrors'}
        </span>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {upstreams.map(u => (
          <UpstreamRow key={u.name} upstream={u} />
        ))}
      </div>
    </div>
  )
}

function MirrorMatrix({ upstreams }: { upstreams: UpstreamInfo[] }) {
  // Group by adapter
  const groups = new Map<string, UpstreamInfo[]>()
  for (const u of upstreams) {
    const list = groups.get(u.adapter) ?? []
    list.push(u)
    groups.set(u.adapter, list)
  }

  return (
    <div style={{ columns: 3, columnGap: 12 }}>
      {Array.from(groups.entries()).map(([adapter, list]) => (
        <EcosystemCard key={adapter} adapter={adapter} upstreams={list} />
      ))}
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────


export default function MonitorPage() {
  const { t } = useTranslation()
  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const upstreams = data?.upstreams ?? []
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

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Title row */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
        <div>
          <h1
            className="grad-text"
            style={{
              margin: 0,
              fontSize: 44,
              fontWeight: 700,
              letterSpacing: '-0.04em',
              lineHeight: 1.02,
            }}
          >
            {t('monitor.title')}
          </h1>
          <p
            style={{
              margin: '14px 0 0 0',
              fontSize: 17,
              lineHeight: 1.45,
              color: 'var(--text)',
              maxWidth: 620,
              fontWeight: 400,
              letterSpacing: '-0.005em',
            }}
          >
            {t('monitor.subtitle')}
          </p>
        </div>
      </div>

      <HitRateHero hitRate={hitRate} series={series} />
      <StatStrip data={today} upstreams={upstreams} series={series} />

      {/* Mirrors section */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'flex-end',
            justifyContent: 'space-between',
          }}
        >
          <div>
            <h2
              style={{
                margin: 0,
                fontSize: 26,
                fontWeight: 700,
                letterSpacing: '-0.03em',
                lineHeight: 1.1,
              }}
            >
              {t('monitor.upstreams')}{' '}
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 14,
                  fontWeight: 500,
                  letterSpacing: '-0.02em',
                  color: 'var(--text-subtle)',
                  marginLeft: 6,
                }}
              >
                {upstreams.length}
              </span>
            </h2>
            <p style={{ margin: '2px 0 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="healthy" />
                <span className="num">{healthyCounts.healthy ?? 0}</span> {t('monitor.healthy')}
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="degraded" />
                <span className="num">{healthyCounts.degraded ?? 0}</span> {t('monitor.degraded')}
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="failed" />
                <span className="num">{healthyCounts.failed ?? 0}</span> {t('monitor.failed')}
              </span>
            </p>
          </div>
        </div>
        {upstreams.length > 0 && <MirrorMatrix upstreams={upstreams} />}
      </div>
    </div>
  )
}
