import { useId } from 'react';

interface Props {
  data: number[];
  width?: number;
  height?: number;
  tone?: 'ok' | 'warn' | 'danger' | 'brand' | 'neutral';
}

// Tone → [strokeStart, strokeEnd, areaOpacity]
const toneConfig = {
  ok:      { stroke: ['oklch(0.58 0.12 155)', 'oklch(0.52 0.10 155)'], area: 0.12 },
  warn:    { stroke: ['oklch(0.70 0.14 65)',  'oklch(0.62 0.12 65)'],  area: 0.12 },
  danger:  { stroke: ['oklch(0.60 0.17 25)',  'oklch(0.54 0.15 25)'],  area: 0.10 },
  brand:   { stroke: ['oklch(0.62 0.18 305)', 'oklch(0.55 0.16 295)'], area: 0.10 },
  neutral: { stroke: ['var(--border-strong)', 'var(--border)'],         area: 0.06 },
} as const;

export default function Sparkline({ data, width = 80, height = 28, tone = 'brand' }: Props) {
  const uid = useId().replace(/:/g, '');
  const ids = { stroke: `spk-s-${uid}`, area: `spk-a-${uid}` };

  if (data.length < 2) return null;

  const cfg = toneConfig[tone];
  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;

  // Map data points to SVG coordinates
  const pts = data.map((v, i) => ({
    x: (i / (data.length - 1)) * width,
    y: height - ((v - min) / range) * (height - 4) - 2,
  }));

  // Build SVG path (polyline via L commands)
  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');

  // Close the area shape: go to bottom-right, then bottom-left
  const areaPath = `${linePath} L${width},${height} L0,${height} Z`;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      fill="none"
      aria-hidden="true"
      style={{ display: 'block', overflow: 'visible' }}
    >
      <defs>
        {/* Horizontal gradient for stroke — left color to right color */}
        <linearGradient id={ids.stroke} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%"   stopColor={cfg.stroke[0]} />
          <stop offset="100%" stopColor={cfg.stroke[1]} />
        </linearGradient>
        {/* Vertical gradient for area fill — opaque at top, transparent at bottom */}
        <linearGradient id={ids.area} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%"   stopColor={cfg.stroke[0]} stopOpacity={cfg.area} />
          <stop offset="100%" stopColor={cfg.stroke[0]} stopOpacity={0} />
        </linearGradient>
      </defs>
      {/* Area fill */}
      <path d={areaPath} fill={`url(#${ids.area})`} />
      {/* Stroke line */}
      <path d={linePath} stroke={`url(#${ids.stroke})`} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
