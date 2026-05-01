import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { statsApi } from '@/lib/api'
import StatusDot from '@/components/StatusDot'
import Sparkline from '@/components/Sparkline'
import Segmented from '@/components/Segmented'
import HeroSparkline from '@/components/HeroSparkline'
import { KPI_SERIES, MIRROR_DEFS, genSeries, type MirrorStatus } from '@/lib/ecosystemData'

interface UpstreamInfo {
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
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

function HitRateHero({ hitRate }: { hitRate: number }) {
  const displayRate = (hitRate * 100).toFixed(1)

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
            <span>Cache hit rate · today</span>
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
        <HeroSparkline values={KPI_SERIES.hitRate} />
      </div>
    </div>
  )
}

// ── StatStrip ─────────────────────────────────────────────────────

function StatStrip({ data }: { data: StatsData['today'] }) {
  const reqFmt   = formatRequests(data.total_requests)
  const savedFmt = formatBytes(data.bytes_saved)

  const items = [
    {
      label: 'Total requests',
      value: reqFmt.value,
      unit: reqFmt.unit,
      tone: 'brand' as const,
      series: KPI_SERIES.requests,
    },
    {
      label: 'Bandwidth saved',
      value: savedFmt.value,
      unit: savedFmt.unit,
      tone: 'brand' as const,
      series: KPI_SERIES.saved,
    },
    {
      label: 'P50 latency',
      value: '—',
      unit: 'ms',
      tone: 'neutral' as const,
      series: KPI_SERIES.latency,
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

function MirrorTile({ upstream }: { upstream: UpstreamInfo }) {
  const status = mirrorStatus(upstream)
  const isFailed = status === 'failed'
  const tone = status === 'healthy' ? 'ok' : status === 'degraded' ? 'warn' : 'danger'
  const def = MIRROR_DEFS.find(m => m.type === upstream.adapter)
  const series = def?.series ?? genSeries(40, 42, {})

  return (
    <div
      className="row-hover"
      style={{
        display: 'grid',
        gridTemplateColumns: '50px 1fr 96px',
        alignItems: 'center',
        gap: 12,
        padding: '10px 14px',
        borderBottom: '0.5px solid var(--border)',
        cursor: 'pointer',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot status={status} />
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--text)',
          }}
        >
          {upstream.adapter}
        </span>
      </div>
      <div style={{ minWidth: 0 }}>
        <div
          className="mono"
          style={{
            fontSize: 12,
            color: isFailed ? 'var(--text-subtle)' : 'var(--text)',
            textDecoration: isFailed ? 'line-through' : 'none',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {(upstream.url || upstream.name).replace(/^https?:\/\//, '')}
        </div>
        <div style={{ display: 'flex', gap: 14, marginTop: 2, fontSize: 11 }}>
          <span style={{ color: 'var(--text-subtle)' }}>
            P50{' '}
            <span
              className="num"
              style={{
                color: isFailed
                  ? 'var(--text-subtle)'
                  : upstream.avg_latency_ms > 100
                  ? 'var(--warn-text)'
                  : 'var(--text-muted)',
              }}
            >
              {isFailed ? '—' : `${upstream.avg_latency_ms}ms`}
            </span>
          </span>
          <span style={{ color: 'var(--text-subtle)' }}>
            hit{' '}
            <span
              className="num"
              style={{ color: isFailed ? 'var(--text-subtle)' : 'var(--text-muted)' }}
            >
              {isFailed ? '—' : `${(upstream.success_rate * 100).toFixed(1)}%`}
            </span>
          </span>
        </div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Sparkline data={series} width={92} height={22} tone={tone} />
      </div>
    </div>
  )
}

function MirrorMatrix({ upstreams }: { upstreams: UpstreamInfo[] }) {
  const half = Math.ceil(upstreams.length / 2)
  const left  = upstreams.slice(0, half)
  const right = upstreams.slice(half)

  return (
    <div
      className="card"
      style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', overflow: 'hidden' }}
    >
      <div style={{ borderRight: '0.5px solid var(--border)' }}>
        {left.map(u => (
          <MirrorTile key={`${u.adapter}-${u.name}`} upstream={u} />
        ))}
      </div>
      <div>
        {right.map(u => (
          <MirrorTile key={`${u.adapter}-${u.name}`} upstream={u} />
        ))}
      </div>
    </div>
  )
}

// ── Banner ────────────────────────────────────────────────────────

function Banner({ onDismiss }: { onDismiss: () => void }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '8px 12px',
        background: 'var(--warn-fill)',
        border: '0.5px solid var(--warn-border)',
        borderRadius: 8,
        fontSize: 12,
      }}
    >
      <StatusDot status="degraded" />
      <span style={{ flex: 1, color: 'var(--text)' }}>
        SSE stream disconnected — real-time updates paused.
      </span>
      <button
        type="button"
        onClick={onDismiss}
        style={{ color: 'var(--text-muted)', padding: 4, lineHeight: 0 }}
      >
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
          <path
            d="M2 2l6 6M8 2l-6 6"
            stroke="currentColor"
            strokeWidth="1.2"
            strokeLinecap="round"
          />
        </svg>
      </button>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────

const TIME_RANGES = [
  { value: '15m', label: '15m' },
  { value: '1h',  label: '1h'  },
  { value: '24h', label: '24h' },
  { value: '7d',  label: '7d'  },
]

export default function MonitorPage() {
  const [timeRange, setTimeRange] = useState('1h')
  const [showBanner, setShowBanner] = useState(false) // TODO: set to true when SSE disconnect is detected via LiveDetector

  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor', timeRange],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const upstreams = data?.upstreams ?? []
  const hitRate   = data?.today.hit_rate ?? 0
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
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 16,
        }}
      >
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
            Live monitoring
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
            Real-time view of cache performance and upstream mirror health.
          </p>
        </div>
        <Segmented options={TIME_RANGES} value={timeRange} onChange={setTimeRange} />
      </div>

      {showBanner && <Banner onDismiss={() => setShowBanner(false)} />}

      <HitRateHero hitRate={hitRate} />
      <StatStrip data={today} />

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
              Upstream mirrors{' '}
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
                <span className="num">{healthyCounts.healthy ?? 0}</span> healthy
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="degraded" />
                <span className="num">{healthyCounts.degraded ?? 0}</span> degraded
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="failed" />
                <span className="num">{healthyCounts.failed ?? 0}</span> failed
              </span>
            </p>
          </div>
        </div>
        {upstreams.length > 0 && <MirrorMatrix upstreams={upstreams} />}
      </div>
    </div>
  )
}
