import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Format bytes into human-readable string (e.g., "1.5 GB").
 */
export function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(i === 0 ? 0 : 1)} ${sizes[i]}`
}

/**
 * Format a byte-rate as e.g. "1.2 MB/s". Used by the Now strip's
 * ingress / egress display.
 */
export function formatBps(bytesPerSecond: number): string {
  if (!bytesPerSecond || bytesPerSecond <= 0) return '0 B/s'
  return `${formatBytes(bytesPerSecond)}/s`
}

export type FormatTimeMode = 'auto' | 'time' | 'relative'

function resolveTimeLocale(locale?: string): 'zh-CN' | 'en-US' {
  const language = locale || (typeof document !== 'undefined' ? document.documentElement.lang : '')
  return language.toLowerCase().startsWith('en') ? 'en-US' : 'zh-CN'
}

function isSameCalendarDay(left: Date, right: Date): boolean {
  return left.getFullYear() === right.getFullYear()
    && left.getMonth() === right.getMonth()
    && left.getDate() === right.getDate()
}

/**
 * Format ISO timestamp for display. Three modes:
 *   'auto'     (default) — today: "HH:mm:ss", older: "MM-DD HH:mm"
 *   'time'              — always "HH:mm:ss" (for real-time streams)
 *   'relative'          — today: localized time, <30d: localized relative day, older: localized date
 * Locale follows the i18n-controlled <html lang> by default and can be
 * supplied explicitly for pure rendering and tests.
 */
export function formatTime(ts: string, mode: FormatTimeMode = 'auto', locale?: string): string {
  if (!ts) return '-'
  const d = new Date(ts)
  if (!Number.isFinite(d.getTime())) return '-'
  const now = new Date()
  const resolvedLocale = resolveTimeLocale(locale)
  const formatClock = (includeSeconds: boolean) => new Intl.DateTimeFormat(resolvedLocale, {
    hour: '2-digit',
    minute: '2-digit',
    second: includeSeconds ? '2-digit' : undefined,
    hourCycle: 'h23',
  }).format(d)

  if (mode === 'time') {
    return formatClock(true)
  }

  if (mode === 'relative') {
    if (isSameCalendarDay(d, now)) return formatClock(false)
    const dateStart = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
    const nowStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const dayOffset = Math.round((dateStart - nowStart) / 86_400_000)
    if (Math.abs(dayOffset) < 30) {
      return new Intl.RelativeTimeFormat(resolvedLocale, { numeric: 'always' }).format(dayOffset, 'day')
    }
    return new Intl.DateTimeFormat(resolvedLocale, {
      month: '2-digit',
      day: '2-digit',
    }).format(d)
  }

  if (isSameCalendarDay(d, now)) {
    return formatClock(true)
  }
  return new Intl.DateTimeFormat(resolvedLocale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(d)
}

/**
 * Format a service version string for compact pill display.
 * Examples:
 *   "0.2.3"                          → "v0.2.3"   (clean release)
 *   "0.2.0-126-g43ca7fe-dirty"       → "v0.2.0+dev" (in-progress build)
 *   "dev"                            → "dev"     (no ldflags injected)
 *   undefined / null                 → "—"
 * Pair with title={v} on the rendering element so the full string is
 * still discoverable via hover.
 */
export function formatVersion(v?: string | null): string {
  if (!v) return '—'
  const semver = v.match(/^(\d+\.\d+\.\d+)/)
  if (semver) return v === semver[1] ? `v${semver[1]}` : `v${semver[1]}+dev`
  return v
}

/**
 * Format ISO timestamp as YYYY-MM-DD date string.
 */
export function formatDate(ts: string): string {
  if (!ts) return '-'
  const d = new Date(ts)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
