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
  if (!pt.healthy || pt.latency_ms > 500) return 'oklch(0.65 0.15 25)'
  if (pt.latency_ms > 100) return 'oklch(0.75 0.12 70)'
  return 'oklch(0.68 0.14 155)'
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export default function StatusBar({ points }: Props) {
  const [hover, setHover] = useState<number | null>(null)

  if (!points || points.length === 0) return null

  return (
    <div style={{ position: 'relative', display: 'flex', gap: 1.5, alignItems: 'center', height: 14, width: '100%' }}>
      {points.map((pt, i) => (
        <div
          key={i}
          onMouseEnter={() => setHover(i)}
          onMouseLeave={() => setHover(null)}
          style={{
            flex: 1,
            height: '100%',
            borderRadius: 2.5,
            background: barColor(pt),
            opacity: hover !== null && hover !== i ? 0.4 : 0.85,
            transition: 'opacity 100ms',
            cursor: 'pointer',
          }}
        />
      ))}
      {hover !== null && points[hover] && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: `${((hover + 0.5) / points.length) * 100}%`,
            transform: 'translateX(-50%)',
            marginTop: 6,
            padding: '4px 8px',
            background: 'var(--bg-card)',
            border: '0.5px solid var(--border)',
            borderRadius: 5,
            fontSize: 10,
            whiteSpace: 'nowrap',
            zIndex: 10,
            boxShadow: '0 2px 8px rgba(0,0,0,0.10)',
            display: 'flex',
            gap: 8,
            alignItems: 'center',
          }}
        >
          <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--text-muted)' }}>
            {formatTime(points[hover].time)}
          </span>
          <span style={{ color: 'var(--text)' }}>
            {points[hover].requests === 0
              ? 'No data'
              : `${points[hover].latency_ms}ms`}
          </span>
          {points[hover].requests > 0 && (
            <span
              style={{
                width: 6,
                height: 6,
                borderRadius: '50%',
                background: points[hover].healthy ? 'oklch(0.68 0.14 155)' : 'oklch(0.65 0.15 25)',
                flexShrink: 0,
              }}
            />
          )}
        </div>
      )}
    </div>
  )
}
