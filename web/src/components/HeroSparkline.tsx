import { useId } from 'react'

interface Props {
  values: number[]
}

export default function HeroSparkline({ values }: Props) {
  const uid = useId()
  const strokeId = `hero-stroke-${uid}`
  const areaId = `hero-area-${uid}`

  const W = 760
  const H = 110
  const padY = 6

  if (!values || values.length < 2) return null

  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1

  const points = values.map((v, i) => {
    const x = (i / (values.length - 1)) * W
    const y = padY + (1 - (v - min) / range) * (H - padY * 2)
    return [x, y] as [number, number]
  })

  const linePath = points
    .map((p, i) => (i === 0 ? `M${p[0]},${p[1]}` : `L${p[0]},${p[1]}`))
    .join(' ')
  const areaPath = `${linePath} L${W},${H} L0,${H} Z`
  const last = points[points.length - 1]

  const timeLabels = ['−60m', '−45m', '−30m', '−15m', 'now']

  return (
    <svg
      viewBox={`0 0 ${W} ${H + 16}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      style={{ width: '100%', height: 110, overflow: 'visible' }}
    >
      <defs>
        <linearGradient id={strokeId} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%"   stopColor="var(--spec-1)" />
          <stop offset="55%"  stopColor="var(--spec-2)" />
          <stop offset="100%" stopColor="var(--spec-3)" />
        </linearGradient>
        <linearGradient id={areaId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%"   stopColor="oklch(0.62 0.18 305)" stopOpacity={0.18} />
          <stop offset="100%" stopColor="oklch(0.72 0.12 210)" stopOpacity={0} />
        </linearGradient>
      </defs>

      {[0.25, 0.5, 0.75].map(g => (
        <line
          key={g}
          x1="0" x2={W}
          y1={H * g} y2={H * g}
          stroke="var(--border)"
          strokeWidth="0.5"
          strokeDasharray="2 3"
        />
      ))}

      <path d={areaPath} fill={`url(#${areaId})`} />
      <path
        d={linePath}
        fill="none"
        stroke={`url(#${strokeId})`}
        strokeWidth="1.4"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      <circle cx={last[0]} cy={last[1]} r={3} fill="var(--spec-2)" />
      <circle cx={last[0]} cy={last[1]} r={7} fill="var(--spec-2)" opacity={0.18} />

      {timeLabels.map((label, i) => (
        <text
          key={label}
          x={(i / 4) * W}
          y={H + 12}
          fontFamily="var(--font-mono)"
          fontSize={9}
          fill="var(--text-subtle)"
          textAnchor={i === 0 ? 'start' : i === 4 ? 'end' : 'middle'}
        >
          {label}
        </text>
      ))}
    </svg>
  )
}
