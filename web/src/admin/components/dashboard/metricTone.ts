/**
 * Category tones for the Overview. They express identity — which resource or
 * request path a metric belongs to — never health. Real warnings use the
 * warn/danger tones.
 *
 * Each category keeps its own hue (green CPU, blue memory/service path, purple
 * cache, amber origin, teal download). That palette is a confirmed product
 * decision: an all-accent convergence was tried and came back as "too plain",
 * because the operator scans these colours for resource identity. The values
 * live in `.dashboard-surface` (web/src/index.css), and status colours stay
 * exclusive to ok/warn/danger.
 *
 * Lives outside MetricTile so the request-flow component can share the same
 * palette without re-exporting a runtime object from a component module.
 */
export type MetricTone =
  | 'default'
  | 'ok'
  | 'warn'
  | 'danger'
  | 'accent'
  | 'cpu'
  | 'memory'
  | 'cache'
  | 'origin'
  | 'download'

export const METRIC_TONE_PALETTE: Record<MetricTone, { strong: string; soft: string }> = {
  default: { strong: 'var(--dash-ink)', soft: 'var(--dash-soft)' },
  ok: { strong: 'var(--dash-ok)', soft: 'var(--dash-ok-soft)' },
  warn: { strong: 'var(--dash-warn)', soft: 'var(--dash-warn-soft)' },
  danger: { strong: 'var(--dash-danger)', soft: 'var(--dash-danger-soft)' },
  accent: { strong: 'var(--dash-accent)', soft: 'var(--dash-accent-soft)' },
  cpu: { strong: 'var(--dash-cpu)', soft: 'var(--dash-cpu-soft)' },
  memory: { strong: 'var(--dash-memory)', soft: 'var(--dash-memory-soft)' },
  cache: { strong: 'var(--dash-cache)', soft: 'var(--dash-cache-soft)' },
  origin: { strong: 'var(--dash-origin)', soft: 'var(--dash-origin-soft)' },
  download: { strong: 'var(--dash-download)', soft: 'var(--dash-download-soft)' },
}
