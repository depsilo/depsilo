import type { ReactNode } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import EcosystemIcon from '@/components/EcosystemIcon'
import Icon, { type IconName } from '@/components/Icon'
import { Card } from '@/components/ui/card'
import { Meter, MeterIndicator, MeterTrack } from '@/components/ui/meter'
import { Skeleton } from '@/components/ui/skeleton'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { getApiError } from '@/lib/apiError'

import { ecosystemLabel } from './cockpitFormat'
import DashboardMetricHelp from './DashboardMetricHelp'

export type CockpitTone = 'ok' | 'warning' | 'danger' | 'info' | 'unknown'

const TONE_ICON: Record<CockpitTone, IconName> = {
  ok: 'check_circle',
  warning: 'warning',
  danger: 'cancel',
  info: 'info',
  unknown: 'help',
}

/** Status is never colour alone: every tone has its own glyph. */
export function ToneIcon({ tone }: { tone: CockpitTone }) {
  return <span className="dash-tone" data-tone={tone} aria-hidden><Icon name={TONE_ICON[tone]} size="sm" /></span>
}

export function EcoName({ id }: { id: string }) {
  return (
    <span className="dash-eco">
      {isAdminEcosystem(id) && <EcosystemIcon type={id} size={13} decorative />}
      <span>{ecosystemLabel(id)}</span>
    </span>
  )
}

export function MoreLink({ to, children }: { to: string; children: ReactNode }) {
  return <Link className="dash-more dash-focus" to={to}>{children}<Icon name="chevron_right" size="sm" /></Link>
}

/** The two request paths, in the order every chart and bar on the page uses. */
export function SeriesLegend({ hit, miss }: { hit: string; miss: string }) {
  return (
    <ul className="dash-legend">
      <li><i className="dash-key" data-kind="hit" aria-hidden />{hit}</li>
      <li><i className="dash-key" data-kind="miss" aria-hidden />{miss}</li>
    </ul>
  )
}

/** One whole split into what the cache answered and what went upstream. */
export function SplitBar({ hit, miss, label }: { hit: number; miss: number; label: string }) {
  return (
    <span className="dash-split" role="img" aria-label={label}>
      {hit + miss <= 0 && <i className="is-empty" />}
      {hit > 0 && <i className="is-hit" style={{ flexGrow: hit }} />}
      {miss > 0 && <i className="is-miss" style={{ flexGrow: miss }} />}
    </span>
  )
}

/** A bar whose length ranks a row against the largest row of its list. */
export function RankBar({ ratio, kind }: { ratio: number; kind?: 'miss' }) {
  return <span className="dash-rank" data-kind={kind} aria-hidden><i style={{ width: `${Math.max(0, Math.min(1, ratio)) * 100}%` }} /></span>
}

/** Discrete counts over time, so bars rather than a line. Decorative: the tile states the total. */
export function Spark({ values, kind }: { values: number[]; kind?: 'hit' | 'miss' }) {
  const max = Math.max(0, ...values)
  return (
    <span className="dash-spark" data-kind={kind} aria-hidden>
      {values.map((value, index) => (
        <i key={index} style={{ height: max > 0 && value > 0 ? `${Math.max(8, (value / max) * 100)}%` : '0%' }} />
      ))}
    </span>
  )
}

/**
 * A breakdown table's frame. When the card is too narrow, the table scrolls
 * inside this named, focusable region rather than dropping columns.
 */
export function ScrollTable({ label, children }: { label: string; children: ReactNode }) {
  return <div className="dash-scroll" role="region" aria-label={label} tabIndex={0}>{children}</div>
}

/** A capacity bar that stays neutral until it nears its limit. */
export function CapacityMeter({ ratio, label, valueText, testRatio }: { ratio: number; label: string; valueText?: string; testRatio?: boolean }) {
  const clamped = Math.max(0, Math.min(1, ratio))
  return (
    <Meter
      className="dash-meter"
      aria-label={label}
      aria-valuetext={valueText}
      value={clamped * 100}
      data-tone={clamped >= 0.95 ? 'danger' : clamped >= 0.8 ? 'warning' : undefined}
      data-progress-ratio={testRatio ? clamped : undefined}
    >
      <MeterTrack><MeterIndicator /></MeterTrack>
    </Meter>
  )
}

interface CockpitCardProps {
  id: string
  title: string
  chip?: ReactNode
  /** Measurement notes behind the title, opened from the title itself. */
  help?: { label: string; content: ReactNode }
  aside?: ReactNode
  busy?: boolean
  children: ReactNode
  [dataAttribute: `data-${string}`]: string | undefined
}

/** A labelled section of the cockpit. The heading names the region for assistive technology. */
export function CockpitCard({ id, title, chip, help, aside, busy, children, ...data }: CockpitCardProps) {
  const titleId = `overview-${id}-title`
  return (
    <Card render={<section />} aria-labelledby={titleId} aria-busy={busy || undefined} className="dash-card" {...data}>
      <header className="dash-card-h">
        <h2 id={titleId} className="dash-card-title">
          {help ? <DashboardMetricHelp label={title} ariaLabel={help.label}>{help.content}</DashboardMetricHelp> : title}
          {chip && <span className="dash-chip">{chip}</span>}
        </h2>
        {aside}
      </header>
      {children}
    </Card>
  )
}

/** A one-line state inside a card: unavailable, stale, or forbidden, with a retry. */
export function CardNote({ tone, children, onRetry, retryLabel, role }: {
  tone?: 'warning' | 'danger'
  children: ReactNode
  onRetry?: () => void
  retryLabel?: string
  role?: 'status' | 'alert'
}) {
  return (
    <p className="dash-note" data-tone={tone} role={role}>
      <span>{children}</span>
      {onRetry && <button type="button" className="dash-note-action dash-focus" onClick={onRetry}>{retryLabel}</button>}
    </p>
  )
}

interface QueryBodyProps<T> {
  query: Pick<UseQueryResult<T>, 'data' | 'isPending' | 'isError' | 'error' | 'refetch'>
  /** Shown instead of the body when the first load fails. */
  unavailable: string
  /** Placeholder rows that reserve the settled height. */
  rows?: number
  children: (data: T) => ReactNode
}

/**
 * The shared first-load, failure, and stale states of a card backed by one
 * query. A failed refresh keeps the last data and says so; it never blanks.
 */
export function QueryBody<T>({ query, unavailable, rows = 3, children }: QueryBodyProps<T>) {
  const { t } = useTranslation()
  if (query.data === undefined) {
    if (query.isError) {
      const forbidden = getApiError(query.error).status === 403
      return (
        <CardNote tone="warning" role="alert" onRetry={forbidden ? undefined : () => { void query.refetch() }} retryLabel={t('common.retry')}>
          {forbidden ? t('common.permissionDenied') : unavailable}
        </CardNote>
      )
    }
    return (
      <div className="dash-skeleton">
        <span className="sr-only">{t('loading')}</span>
        {Array.from({ length: rows }, (_, index) => <Skeleton key={index} className="h-[18px]" />)}
      </div>
    )
  }
  return (
    <>
      {query.isError && (
        <CardNote tone="warning" role="status" onRetry={() => { void query.refetch() }} retryLabel={t('common.retry')}>
          {t('overview.dataStale')}
        </CardNote>
      )}
      {children(query.data)}
    </>
  )
}
