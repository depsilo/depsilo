import type { ReactNode } from 'react'

import Icon, { type IconName } from '@/components/Icon'
import type { MetricChangeIntent } from '@/components/Metric'

export type StatTone = 'default' | 'ok' | 'warning' | 'danger'

interface StatTileProps {
  icon: IconName
  label: string
  value: string
  /** Secondary line: units, context, or an explicit unavailable hint. */
  detail?: ReactNode
  tone?: StatTone
  /**
   * Percentage delta against the comparison window. Direction is supplied by
   * the domain; a neutral intent never renders as success or failure.
   */
  change?: number | null
  changeIntent?: MetricChangeIntent
  loading?: boolean
}

const TONE_STYLE: Record<StatTone, { fill: string; text: string }> = {
  default: { fill: 'var(--bg-soft)', text: 'var(--text-soft)' },
  ok: { fill: 'var(--ok-fill)', text: 'var(--ok-text)' },
  warning: { fill: 'var(--warn-fill)', text: 'var(--warn-text)' },
  danger: { fill: 'var(--danger-fill)', text: 'var(--danger-text)' },
}

/**
 * Dashboard stat tile: one prominent status icon over a label and a single
 * tabular value. It replaces the bare KPI rail so every instrument on the
 * page reads as "icon + data" instead of loose fragments.
 */
export default function StatTile({
  icon,
  label,
  value,
  detail,
  tone = 'default',
  change,
  changeIntent = 'neutral',
  loading = false,
}: StatTileProps) {
  const toneStyle = TONE_STYLE[tone]
  const changeTone = typeof change !== 'number' || change === 0 || changeIntent === 'neutral'
    ? 'neutral'
    : (change > 0) === (changeIntent === 'higher-is-better')
      ? 'positive'
      : 'negative'

  return (
    <div
      data-dashboard-stat
      data-stat-tone={tone}
      className="flex min-w-0 flex-col gap-3 rounded-xl border p-4"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-card)' }}
    >
      <span
        aria-hidden="true"
        className="inline-flex size-11 shrink-0 items-center justify-center rounded-lg"
        style={{ background: toneStyle.fill, color: toneStyle.text }}
      >
        <Icon name={icon} size="lg" />
      </span>
      <div className="flex min-w-0 flex-col items-start">
        <p className="text-[11px] font-[600]" style={{ color: 'var(--text-subtle)' }}>{label}</p>
        {loading ? (
          <span aria-hidden="true" className="mt-1.5 block h-7 w-24 animate-pulse rounded bg-[var(--bg-soft)]" />
        ) : (
          <p
            data-metric-value
            className="mt-1 min-w-0 max-w-full truncate font-mono text-[26px] font-[650] leading-tight tabular-nums"
            style={{ color: tone === 'default' ? 'var(--text)' : toneStyle.text }}
            title={value}
          >
            {value}
          </p>
        )}
        {detail && (
          <p className="mt-1 text-[11px] leading-[1.5]" style={{ color: 'var(--text-soft)' }}>{detail}</p>
        )}
        {typeof change === 'number' && (
          <span
            data-metric-change
            data-change-intent={changeIntent}
            data-change-tone={changeTone}
            className="mt-1.5 font-mono text-[11px] tabular-nums"
            style={{
              color: changeTone === 'positive'
                ? 'var(--ok-text)'
                : changeTone === 'negative'
                  ? 'var(--danger-text)'
                  : 'var(--text-soft)',
            }}
          >
            {change >= 0 ? '+' : ''}{change.toFixed(1)}%
          </span>
        )}
      </div>
    </div>
  )
}
