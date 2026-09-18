import { Download, Package2 } from 'lucide-react'
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import BadgeV2 from '@/components/app/badge'
import ButtonV2 from '@/components/app/button'
import EcosystemIcon from '@/components/app/ecosystem-icon'
import { getAdminRouteHref } from '@/admin/routes'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem, type RecentDownload } from '@/lib/adminApi.types'
import { cacheTone, deliveryTone, type Tone } from '@/lib/status'
import { formatBytes } from '@/lib/utils'

const REFRESH_INTERVAL_MS = 5_000
const MAX_RECENT_DOWNLOADS = 20

interface RecentDownloadsProps {
  limit?: number
  variant?: 'grid' | 'rail'
}

type DownloadOutcome = {
  label: string
  variant: Tone
}

function normalizeLimit(limit: number) {
  if (!Number.isFinite(limit)) return 3
  return Math.min(Math.max(Math.trunc(limit), 1), MAX_RECENT_DOWNLOADS)
}

function normalizeItems(items: RecentDownload[], limit: number) {
  const seen = new Set<number>()
  return [...items]
    .sort((left, right) => right.id - left.id)
    .filter(item => {
      if (seen.has(item.id)) return false
      seen.add(item.id)
      return true
    })
    .slice(0, limit)
}

/**
 * A download's outcome is a cache result and a delivery result. They are
 * mapped through the shared vocabulary rather than decided here, because this
 * feed showing a cache miss as a warning while the logs showed it as neutral
 * is exactly the inconsistency the vocabulary exists to prevent: a miss is how
 * a cache behaves the first time, not a problem.
 */
function downloadOutcome(item: RecentDownload, t: TFunction): DownloadOutcome {
  if (item.status_code >= 400) {
    return {
      label: t('recentDownloads.httpError', { status: item.status_code }),
      variant: deliveryTone('failed'),
    }
  }
  if (item.cache_result === 'error') {
    return { label: t('recentDownloads.failed'), variant: cacheTone('error') }
  }
  if (item.cache_result === 'hit') {
    return { label: t('recentDownloads.cacheHit'), variant: cacheTone('hit') }
  }
  if (item.cache_result === 'miss') {
    return { label: t('recentDownloads.upstreamFetch'), variant: cacheTone('miss') }
  }
  return { label: t('recentDownloads.completed'), variant: deliveryTone('completed') }
}

function relativeTime(value: string, t: TFunction) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return t('recentDownloads.unknownTime')
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  if (seconds < 10) return t('now.justNow')
  if (seconds < 60) return t('now.secondsAgo', { count: seconds })
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return t('now.minutesAgo', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('now.hoursAgo', { count: hours })
  return t('now.daysAgo', { count: Math.floor(hours / 24) })
}

function exactTime(value: string, locale: string) {
  const timestamp = Date.parse(value)
  if (!Number.isFinite(timestamp)) return value
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(timestamp)
}

export default function RecentDownloads({ limit = 3, variant = 'grid' }: RecentDownloadsProps) {
  const { t, i18n } = useTranslation()
  const safeLimit = normalizeLimit(limit)
  const query = useQuery({
    queryKey: ['admin', 'dashboard', 'recent-downloads', safeLimit],
    queryFn: ({ signal }) => adminApi.getRecentDownloads(safeLimit, { signal }),
    refetchInterval: REFRESH_INTERVAL_MS,
    refetchIntervalInBackground: false,
    staleTime: REFRESH_INTERVAL_MS - 1_000,
    refetchOnWindowFocus: 'always',
    retry: false,
  })
  const items = useMemo(
    () => normalizeItems(query.data?.data.items ?? [], safeLimit),
    [query.data, safeLimit],
  )
  const hasStaleData = query.isRefetchError && query.data !== undefined
  const hasConnectionError = hasStaleData || query.isError
  const locale = i18n.resolvedLanguage === 'en' ? 'en-US' : 'zh-CN'
  const isRail = variant === 'rail'

  return (
    <section
      data-recent-downloads
      data-query-key="dashboard-recent-downloads"
      aria-labelledby="recent-downloads-title"
      aria-busy={query.isPending || undefined}
      className="min-w-0 overflow-hidden rounded-lg bg-muted"
    >
      <header className="flex min-h-14 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden
            data-live-pulse
            className={`size-1.5 shrink-0 rounded-full ${hasConnectionError ? 'bg-warning' : 'bg-info'} ${hasConnectionError ? '' : 'animate-live-pulse'}`}
          />
          <h2 id="recent-downloads-title" className="text-body font-semibold text-foreground">
            {t('recentDownloads.title')}
          </h2>
          <span className="text-meta text-muted-foreground">
            {hasConnectionError ? t('recentDownloads.retrying') : t('recentDownloads.liveRefresh')}
          </span>
        </div>
        <Link
          to={getAdminRouteHref('auditLogs')}
          className="inline-flex min-h-10 items-center gap-1 rounded-sm px-2 whitespace-nowrap text-label font-semibold no-underline text-primary hover:bg-card"
        >
          {t('recentDownloads.viewAudit')}
          <span aria-hidden>→</span>
        </Link>
      </header>

      <div
        data-live-flow
        aria-hidden
        className={`relative h-px overflow-hidden bg-border after:absolute after:inset-y-0 after:left-0 after:w-[18%] after:rounded-full after:bg-info after:content-[''] ${hasConnectionError ? 'after:animate-none' : 'after:animate-live-sweep'}`}
      />

      {query.isPending ? (
        <div aria-hidden="true" className={isRail ? 'grid grid-cols-1' : 'grid grid-cols-1 sm:grid-cols-3'}>
          {/* Items are divided from `sm` up; on a stacked rail the rule sits above each later row. */}

          {Array.from({ length: safeLimit }, (_, index) => (
            <div
              key={index}
              className={`space-y-2 px-4 py-3 ${isRail ? (index > 0 ? 'border-t border-border' : '') : 'sm:[&]:border-l sm:[&]:border-border empty:border-l-0'}`}
            >
              <div className="h-3 w-3/4 animate-pulse rounded bg-muted" />
              <div className="h-2.5 w-1/2 animate-pulse rounded bg-muted" />
            </div>
          ))}
        </div>
      ) : query.isError && !query.data ? (
        <div role="alert" className="flex min-h-20 flex-wrap items-center justify-between gap-2 px-4 py-3 text-label text-warning">
          <span>
            {getApiError(query.error).status === 403
              ? t('common.permissionDenied')
              : t('recentDownloads.unavailable')}
          </span>
          <ButtonV2 type="button" variant="secondary" size="sm" onClick={() => { void query.refetch() }}>
            {t('recentDownloads.retry')}
          </ButtonV2>
        </div>
      ) : items.length === 0 ? (
        <div className="flex min-h-24 items-center gap-2 px-4 py-3 text-label text-muted-foreground">
          <Download className="icon icon-sm" aria-hidden />
          <span>{t('recentDownloads.empty')}</span>
        </div>
      ) : (
        <>
          {hasStaleData && (
            <div role="status" className="flex flex-wrap items-center justify-between gap-2 border-b-[0.5px] border-warning/35 bg-warning/10 px-3 py-1.5 text-meta text-warning">
              <span>{t('recentDownloads.stale')}</span>
              <button type="button" className="min-h-7 rounded px-2 font-semibold" onClick={() => { void query.refetch() }}>
                {t('recentDownloads.retry')}
              </button>
            </div>
          )}
          <ol aria-label={t('recentDownloads.listLabel')} className={isRail ? 'grid grid-cols-1' : 'grid grid-cols-1 sm:grid-cols-3'}>
            {items.map((item, index) => {
              const outcome = downloadOutcome(item, t)
              const packageName = item.package_name || t('recentDownloads.unknownPackage')
              const when = relativeTime(item.created_at, t)
              const size = formatBytes(item.bytes_sent)
              const ecosystem = item.ecosystem || t('recentDownloads.unknownEcosystem')
              const fullPackageName = item.version ? `${packageName}@${item.version}` : packageName
              return (
                <li
                  key={item.id}
                  data-download-id={item.id}
                  className={`min-w-0 animate-live-enter px-4 py-3 ${isRail ? (index > 0 ? 'border-t border-border' : '') : 'sm:border-l sm:border-border'}`}
                  aria-label={t('recentDownloads.itemLabel', {
                    ecosystem,
                    package: fullPackageName,
                    status: outcome.label,
                    size,
                    latency: item.latency_ms,
                    time: when,
                  })}
                >
                  <div className="flex min-w-0 items-center gap-2">
                    {isAdminEcosystem(item.ecosystem) ? (
                      <EcosystemIcon type={item.ecosystem} size={13} decorative />
                    ) : (
                      <Package2 className="icon icon-sm" aria-hidden />
                    )}
                    <span className="min-w-0 flex-1 truncate font-mono text-body font-medium text-foreground" title={fullPackageName}>
                      {packageName}
                      {item.version && <span className="text-muted-foreground">@{item.version}</span>}
                    </span>
                    <BadgeV2 variant={outcome.variant} className="shrink-0">{outcome.label}</BadgeV2>
                  </div>
                  <div className="mt-1.5 flex min-w-0 items-center gap-1.5 pl-[21px] text-meta text-muted-foreground">
                    <span className="shrink-0 font-medium uppercase">{ecosystem}</span>
                    <span aria-hidden>·</span>
                    <span className="min-w-0 flex-1 truncate font-mono tabular-nums text-muted-foreground">
                      {size} · {item.latency_ms} ms
                    </span>
                    <time
                      dateTime={item.created_at}
                      title={exactTime(item.created_at, locale)}
                      className="shrink-0 font-mono tabular-nums"
                    >
                      {when}
                    </time>
                  </div>
                </li>
              )
            })}
          </ol>
        </>
      )}
    </section>
  )
}
