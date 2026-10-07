/**
 * Category tones for the Overview. They express identity — which resource or
 * request path a metric belongs to — never health. Real warnings use the
 * warn/danger tones.
 *
 * Identity is deliberately narrow: every category resolves to the one brand
 * accent, except the origin/secondary path which takes the neutral slate.
 * Earlier each category carried its own hue (green CPU, purple cache, amber
 * origin), which competed with the status colours and read as decoration.
 * The actual values live in `.dashboard-surface` (web/src/index.css) so the
 * whole convergence can be reviewed or rolled back in one place.
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
  cpu: { strong: 'var(--dash-accent)', soft: 'var(--dash-accent-soft)' },
  memory: { strong: 'var(--dash-accent)', soft: 'var(--dash-accent-soft)' },
  cache: { strong: 'var(--dash-accent)', soft: 'var(--dash-accent-soft)' },
  origin: { strong: 'var(--dash-origin)', soft: 'var(--dash-origin-soft)' },
  download: { strong: 'var(--dash-accent)', soft: 'var(--dash-accent-soft)' },
}
