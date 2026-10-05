import { useId, type ReactNode } from 'react'

import Icon, { type IconName } from '@/components/Icon'
import TooltipV2 from '@/components/Tooltip'
import { sparklineGeometry } from '@/lib/dashboardOverview'

/** Dense Overview hints open fast; other surfaces keep the app-wide 350ms. */
export const HINT_TOOLTIP_DELAY_MS = 120

/**
 * Category tones. They express identity — which resource or request path a
 * metric belongs to — never health. Real warnings use the warn/danger tones.
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

const TONE: Record<MetricTone, { strong: string; soft: string }> = {
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

export interface MetricProgress {
  /** 0..1; values are clamped for the bar while the numeric value stays exact. */
  ratio: number
  label?: string
  tone?: MetricTone
}

/** One independent line inside a tile, used for the two request paths. */
export interface MetricRow {
  label: string
  value: string
  unit?: string
  tone: MetricTone
}

interface MetricTileProps {
  label: string
  /** Large headline value. Omit when the tile is rows-only (no totals). */
  value?: string
  unit?: string
  /** Short scope chip next to the label, e.g. "Live" or "30d". */
  badge?: string
  detail?: ReactNode
  tone?: MetricTone
  icon?: IconName
  /** Real progress (memory limit / cache quota / disk). Omit when no honest denominator exists. */
  progress?: MetricProgress | null
  /** Real sampled series; omitted when nothing has been sampled yet. */
  series?: number[]
  seriesTone?: MetricTone
  /** Independent value rows instead of one headline number. */
  rows?: MetricRow[]
  /**
   * Reserve the sparkline and progress rows even when this tile has neither, so
   * every tile in a row keeps identical slot positions. Use for rows whose
   * members mix those graphics; leave off for compact rows (traffic).
   */
  reserveSlots?: boolean
  loading?: boolean
  /** Short measurement-basis hint shown on hover/focus of the info affordance. */
  info?: string
  infoLabel?: string
  testId?: string
}

function Sparkline({ series, tone }: { series: number[]; tone: MetricTone }) {
  const gradientId = useId()
  const geometry = sparklineGeometry(series)
  if (!geometry) return null
  const color = TONE[tone].strong

  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 96 30"
      width={96}
      height={30}
      className="ml-auto shrink-0"
    >
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity={0.22} />
          <stop offset="100%" stopColor={color} stopOpacity={0} />
        </linearGradient>
      </defs>
      <path d={geometry.area} fill={`url(#${gradientId})`} />
      <path
        d={geometry.line}
        fill="none"
        stroke={color}
        strokeWidth={1.75}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx={geometry.last.x} cy={geometry.last.y} r={2.2} fill={color} />
    </svg>
  )
}

/**
 * Overview metric tile: an identity-coloured icon base, a large tabular value
 * with a de-emphasized unit, an optional real progress bar and sparkline, and a
 * measurement-basis line. Main numbers stay dark; colour carries category.
 */
export default function MetricTile({
  label,
  value,
  unit,
  badge,
  detail,
  tone = 'default',
  icon,
  progress = null,
  series,
  seriesTone,
  rows,
  reserveSlots = false,
  loading = false,
  info,
  infoLabel,
  testId,
}: MetricTileProps) {
  const ratio = progress ? Math.min(1, Math.max(0, progress.ratio)) : null
  const progressTone = progress?.tone ?? tone
  const palette = TONE[tone]
  const sparkTone = seriesTone ?? tone

  return (
    <div
      data-dashboard-metric
      data-testid={testId}
      className="dash-card dash-metric flex min-w-0 items-start gap-4 p-5"
    >
      {icon && (
        <span
          aria-hidden="true"
          className="dash-metric-icon grid shrink-0 place-items-center rounded-xl"
          style={{ background: palette.soft, color: palette.strong }}
        >
          <Icon name={icon} />
        </span>
      )}

      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <p className="min-w-0 truncate text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>
            {label}
          </p>
          {badge && (
            <span
              className="shrink-0 rounded-full px-2 py-0.5 text-[12px] font-medium"
              style={{ background: 'var(--dash-soft)', color: 'var(--dash-muted)' }}
            >
              {badge}
            </span>
          )}
          {info && (
            <TooltipV2 delay={HINT_TOOLTIP_DELAY_MS} content={<span className="block max-w-[240px] leading-[1.5]">{info}</span>}>
              <button
                type="button"
                aria-label={infoLabel}
                className="dash-focus ml-auto grid size-7 shrink-0 place-items-center rounded-full transition-colors duration-150 hover:bg-[var(--dash-soft)]"
                style={{ color: 'var(--dash-muted)' }}
              >
                <Icon name="info" size="sm" />
              </button>
            </TooltipV2>
          )}
        </div>

        {/* Fixed vertical rhythm so every tile in a row lines up. reserveSlots
            keeps the sparkline/progress rows in place for rows that mix those
            graphics; compact rows omit them entirely. */}
        <div
          className="flex min-w-0 flex-col justify-start gap-1.5"
          style={reserveSlots ? { minHeight: 52 } : undefined}
        >
          {value !== undefined && (
            <div className="flex min-w-0 items-baseline gap-1.5 whitespace-nowrap">
              {loading ? (
                <span aria-hidden="true" className="block h-9 w-24 animate-pulse rounded bg-[var(--dash-soft)]" />
              ) : (
                <>
                  <span
                    className="dash-metric-value font-mono font-semibold leading-none tabular-nums"
                    style={{ color: 'var(--dash-ink)' }}
                    title={unit ? `${value} ${unit}` : value}
                  >
                    {value}
                  </span>
                  {unit && (
                    <span className="text-[15px] font-medium" style={{ color: 'var(--dash-muted)' }}>{unit}</span>
                  )}
                </>
              )}
            </div>
          )}

          {rows && rows.length > 0 && (
            <dl className="flex flex-col gap-1.5">
              {rows.map(row => (
                <div key={row.label} className="flex min-h-5 min-w-0 items-center justify-between gap-3 leading-none">
                  <dt className="flex min-w-0 items-center gap-1.5 text-[14px] leading-none" style={{ color: 'var(--dash-muted)' }}>
                    <span aria-hidden className="size-2 shrink-0 rounded-full" style={{ background: TONE[row.tone].strong }} />
                    <span className="truncate">{row.label}</span>
                  </dt>
                  <dd className="shrink-0 font-mono text-[18px] font-semibold leading-none tabular-nums" style={{ color: 'var(--dash-ink)' }}>
                    {row.value}
                    {row.unit && <span className="ml-1 text-[13px] font-medium" style={{ color: 'var(--dash-muted)' }}>{row.unit}</span>}
                  </dd>
                </div>
              ))}
            </dl>
          )}
        </div>

        {(series && series.length >= 2) || reserveSlots ? (
          <div className="flex h-[30px] min-w-0 items-center justify-end">
            {series && series.length >= 2 && <Sparkline series={series} tone={sparkTone} />}
          </div>
        ) : null}

        {ratio !== null ? (
          <div
            className="h-1.5 w-full overflow-hidden rounded-full"
            style={{ background: TONE[progressTone].soft }}
            aria-hidden="true"
            data-progress-label={progress?.label}
          >
            <div
              className="h-full rounded-full transition-[width] duration-300"
              style={{ width: `${ratio * 100}%`, background: TONE[progressTone].strong }}
            />
          </div>
        ) : reserveSlots ? (
          <div aria-hidden="true" className="h-1.5" />
        ) : null}

        {detail && (
          <p className="min-w-0 text-[14px] leading-[1.5]" style={{ color: 'var(--dash-muted)' }}>{detail}</p>
        )}
      </div>
    </div>
  )
}
