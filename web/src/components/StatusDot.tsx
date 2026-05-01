interface Props {
  status: 'healthy' | 'degraded' | 'failed' | 'unknown';
  size?: number;
  live?: boolean;
}

const colorMap = {
  healthy:  'var(--ok)',
  degraded: 'var(--warn)',
  failed:   'var(--danger)',
  unknown:  'var(--text-subtle)',
} as const;

export default function StatusDot({ status, size = 6, live = false }: Props) {
  const color = colorMap[status];
  return (
    <span
      className={live ? 'dot-live' : undefined}
      style={{
        display:      'inline-block',
        width:        size,
        height:       size,
        borderRadius: '50%',
        background:   color,
        color,
        flexShrink:   0,
      }}
    />
  );
}
