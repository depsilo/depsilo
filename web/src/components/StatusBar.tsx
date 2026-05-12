import { useState } from 'react'

interface LatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
  requests: number
}

interface Props {
  points: LatencyPoint[]
}

function barColor(pt: LatencyPoint): string {
  if (pt.requests === 0) return 'var(--border)'
  if (!pt.healthy) return 'var(--danger)'
  if (pt.latency_ms > 500) return 'var(--danger)'
  if (pt.latency_ms > 100) return 'var(--warn)'
  return 'var(--ok)'
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export default function StatusBar({ points }: Props) {
  const [hover, setHover] = useState<number | null>(null)

  if (!points || points.length === 0) return null

  return (
    <div style={{ position: 'relative', display: 'flex', gap: 1.5, alignItems: 'center', height: 22, width: '100%' }}>
      {points.map((pt, i) => (
        <div
          key={i}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(null)}
          style={{
            flex: 1,
            height: '100%',
            borderRadius: 2,
            background: barColor(pt),
            opacity: hover !== null && hover !== i ? 0.5 : 1,
            transition: 'opacity 120ms',
            cursor: 'pointer',
          }}
        />
      ))}
      {hover !== null && points[hover] && (
        <div
          style={{
            position: 'absolute',
            bottom: '100%',
            left: `${(hover / points.length) * 100}%`,
            transform: 'translateX(-50%)',
            marginBottom: 6,
            padding: '5px 8px',
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border)',
            borderRadius: 6,
            fontSize: 10,
            whiteSpace: 'nowrap',
            zIndex: 10,
            boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
            display: 'flex',
            flexDirection: 'column',
            gap: 2,
          }}
        >
          <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
            {formatTime(points[hover].time)}
          </span>
          <span style={{ color: 'var(--text)' }}>
            {points[hover].requests === 0
              ? 'No data'
              : `${points[hover].latency_ms}ms · ${points[hover].healthy ? 'healthy' : 'unhealthy'}`}
          </span>
        </div>
      )}
    </div>
  )
}
