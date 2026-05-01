interface Props {
  status: 'healthy' | 'degraded' | 'failed' | 'unknown';
  size?: number;
  live?: boolean;
}

const colorMap: Record<string, string> = {
  healthy:  'var(--ok)',
  ok:       'var(--ok)',
  degraded: 'var(--warn)',
  warning:  'var(--warn)',
  warn:     'var(--warn)',
  failed:   'var(--danger)',
  error:    'var(--danger)',
  danger:   'var(--danger)',
  unknown:  'var(--text-subtle)',
};

export default function StatusDot({ status, size = 6, live = false }: Props) {
  const color = colorMap[status] ?? 'var(--text-subtle)';
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
