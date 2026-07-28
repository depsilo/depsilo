import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import BadgeV2 from '@/components/Badge'
import ButtonV2 from '@/components/Button'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import { getAdminRouteHref } from '@/admin/routes'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem, type RecentDownload } from '@/lib/adminApi.types'
import { formatBytes } from '@/lib/utils'

const REFRESH_INTERVAL_MS = 5_000
const MAX_RECENT_DOWNLOADS = 20

interface RecentDownloadsProps {
  limit?: number
}

type DownloadOutcome = {
  label: string
  variant: 'default' | 'success' | 'error' | 'warning'
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

function downloadOutcome(item: RecentDownload, t: TFunction): DownloadOutcome {
  if (item.status_code >= 400 || item.cache_result === 'error') {
    return {
      label: item.status_code > 0
        ? t('recentDownloads.httpError', { status: item.status_code })
        : t('recentDownloads.failed'),
      variant: 'error',
    }
  }
  if (item.cache_result === 'hit') {
    return { label: t('recentDownloads.cacheHit'), variant: 'success' }
  }
  if (item.cache_result === 'miss') {
    return { label: t('recentDownloads.upstreamFetch'), variant: 'warning' }
  }
  return { label: t('recentDownloads.completed'), variant: 'default' }
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

export default function RecentDownloads({ limit = 3 }: RecentDownloadsProps) {
  const { t, i18n } = useTranslation()
  const safeLimit = normalizeLimit(limit)
  const query = useQuery({
    queryKey: ['admin', 'dashboard', 'recent-downloads', safeLimit],
    queryFn: () => adminApi.getRecentDownloads(safeLimit),
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

  return (
    <section
      data-recent-downloads
      data-query-key="dashboard-recent-downloads"
      aria-labelledby="recent-downloads-title"
      aria-busy={query.isPending || undefined}
      className="overflow-hidden rounded-[var(--r-card)] border-[0.5px] border-[var(--border)] bg-[var(--bg-card)]"
    >
      <header className="flex min-h-10 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <span
            aria-hidden
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${hasConnectionError ? '' : 'live-download-pulse'}`}
            style={{ background: hasConnectionError ? 'var(--warn-text)' : 'var(--live)' }}
          />
          <h2 id="recent-downloads-title" className="text-[12px] font-[650] text-[var(--text)]">
            {t('recentDownloads.title')}
          </h2>
          <span className="text-[10px] text-[var(--text-subtle)]">
            {hasConnectionError ? t('recentDownloads.retrying') : t('recentDownloads.liveRefresh')}
          </span>
        </div>
        <Link
          to={getAdminRouteHref('auditLogs')}
          className="stripe-focus-ring inline-flex min-h-[40px] items-center gap-1 rounded-[5px] px-2 whitespace-nowrap text-[11px] font-[600] no-underline text-[var(--brand-text)] hover:bg-[var(--bg-hover)]"
        >
          {t('recentDownloads.viewAudit')}
          <span aria-hidden>→</span>
        </Link>
      </header>

      <div className={`live-download-flow ${hasConnectionError ? 'live-download-flow-paused' : ''}`} aria-hidden />

      {query.isPending ? (
        <div aria-hidden="true" className="live-download-grid">
          {Array.from({ length: safeLimit }, (_, index) => (
            <div key={index} className="live-download-item space-y-2 px-3 py-2.5">
              <div className="h-3 w-3/4 animate-pulse rounded bg-[var(--bg-soft)]" />
              <div className="h-2.5 w-1/2 animate-pulse rounded bg-[var(--bg-soft)]" />
            </div>
          ))}
        </div>
      ) : query.isError && !query.data ? (
        <div role="alert" className="flex min-h-14 flex-wrap items-center justify-between gap-2 px-3 py-2 text-[12px] text-[var(--warn-text)]">
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
        <div className="flex min-h-14 items-center gap-2 px-3 py-2 text-[12px] text-[var(--text-soft)]">
          <Icon name="download" size="sm" aria-hidden />
          <span>{t('recentDownloads.empty')}</span>
        </div>
      ) : (
        <>
          {hasStaleData && (
            <div role="status" className="flex flex-wrap items-center justify-between gap-2 border-b-[0.5px] border-[var(--warn-border)] bg-[var(--warn-fill)] px-3 py-1.5 text-[11px] text-[var(--warn-text)]">
              <span>{t('recentDownloads.stale')}</span>
              <button type="button" className="stripe-focus-ring min-h-7 rounded px-2 font-[600]" onClick={() => { void query.refetch() }}>
                {t('recentDownloads.retry')}
              </button>
            </div>
          )}
          <ol className="live-download-grid" aria-label={t('recentDownloads.listLabel')}>
            {items.map(item => {
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
                  className="live-download-item live-download-row min-w-0 px-3 py-2.5"
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
                      <Icon name="package_2" size="sm" aria-hidden />
                    )}
                    <span className="shrink-0 font-mono text-[10px] uppercase text-[var(--text-subtle)]">
                      {ecosystem}
                    </span>
                    <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-[var(--text)]" title={fullPackageName}>
                      {packageName}
                      {item.version && <span className="text-[var(--text-subtle)]">@{item.version}</span>}
                    </span>
                    <time
                      dateTime={item.created_at}
                      title={exactTime(item.created_at, locale)}
                      className="shrink-0 font-mono text-[10px] tabular-nums text-[var(--text-subtle)]"
                    >
                      {when}
                    </time>
                  </div>
                  <div className="mt-1.5 flex min-w-0 items-center gap-2 pl-[21px]">
                    <BadgeV2 variant={outcome.variant} className="shrink-0">{outcome.label}</BadgeV2>
                    <span className="truncate font-mono text-[10px] tabular-nums text-[var(--text-soft)]">
                      {size} · {item.latency_ms} ms
                    </span>
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
