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

/**
 * Format ISO timestamp for display. Three modes:
 *   'auto'     (default) — today: "HH:mm:ss", older: "MM-DD HH:mm"
 *   'time'              — always "HH:mm:ss" (for real-time streams)
 *   'relative'          — today: "HH:mm", <30d: "Nd ago", older: "MM-DD"
 */
export function formatTime(ts: string, mode: FormatTimeMode = 'auto'): string {
  if (!ts) return '-'
  const d = new Date(ts)
  const now = new Date()

  if (mode === 'time') {
    return d.toLocaleTimeString('zh-CN', { hour12: false })
  }

  if (mode === 'relative') {
    const diff = Math.floor((now.getTime() - d.getTime()) / 86400000)
    if (diff === 0) return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    if (diff < 30) return `${diff}d ago`
    return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }

  if (d.toDateString() === now.toDateString()) {
    return d.toLocaleTimeString('zh-CN', { hour12: false })
  }
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
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
