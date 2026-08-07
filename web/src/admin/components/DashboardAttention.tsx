import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import ButtonV2 from '@/components/Button'
import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import type { DashboardUpstream } from '@/lib/adminApi.types'
import { upstreamStatus } from '@/lib/upstreamStatus'

interface DashboardAttentionProps {
  isPending: boolean
  isFetching: boolean
  initialErrorMessage?: string
  isStale: boolean
  upstreams: DashboardUpstream[]
  cacheUsagePercent?: number
  onRetry: () => void
}

interface AttentionItemProps {
  icon: string
  title: string
  detail: string
  tone: 'danger' | 'warning'
  to: string
  action: string
}

function AttentionItem({ icon, title, detail, tone, to, action }: AttentionItemProps) {
  const toneColor = tone === 'danger' ? 'var(--danger-text)' : 'var(--warn-text)'
  const toneFill = tone === 'danger' ? 'var(--danger-fill)' : 'var(--warn-fill)'

  return (
    <li className="min-w-0 py-1 first:pt-0 last:pb-0">
      <Link
        to={to}
        aria-label={action}
        className="stripe-focus-ring group flex min-h-16 min-w-0 items-center gap-3 rounded-[7px] px-2 py-2 no-underline transition-colors duration-150 hover:bg-[var(--bg-card)]"
      >
        <span
          aria-hidden
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-[7px]"
          style={{ color: toneColor, background: toneFill }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <div className="min-w-0 flex-1">
          <h3 className="text-[12px] font-[650] text-[var(--text)]">{title}</h3>
          <p className="mt-1 text-[11px] leading-[1.55] text-[var(--text-soft)]">{detail}</p>
        </div>
        <span className="hidden shrink-0 items-center gap-1 text-[11px] font-[650] text-[var(--brand-text)] sm:inline-flex">
          {action}
          <span aria-hidden>→</span>
        </span>
        <span aria-hidden="true" className="shrink-0 text-[var(--brand-text)] sm:hidden">
          <Icon name="chevron_right" size="sm" />
        </span>
      </Link>
    </li>
  )
}

export default function DashboardAttention({
  isPending,
  isFetching,
  initialErrorMessage,
  isStale,
  upstreams,
  cacheUsagePercent,
  onRetry,
}: DashboardAttentionProps) {
  const { t } = useTranslation()
  const cacheNeedsAttention = cacheUsagePercent !== undefined && cacheUsagePercent > 80
  const issueCount = (upstreams.length > 0 ? 1 : 0) + (cacheNeedsAttention ? 1 : 0)
  const hasIssues = issueCount > 0
  const upstreamNames = upstreams
    .slice(0, 3)
    .map(item => item.name)
    .join(t('dashboard.listSeparator'))

  return (
    <section
      aria-labelledby="dashboard-attention-title"
      aria-describedby="dashboard-attention-description"
      aria-busy={isPending || undefined}
      className="admin-secondary-panel min-w-0 overflow-hidden rounded-[var(--r-card)]"
    >
      <header className="flex min-h-12 items-center justify-between gap-3 border-b border-[var(--border-soft)] px-4 py-2">
        <div className="min-w-0">
          <h2 id="dashboard-attention-title" className="text-[13px] font-[680] text-[var(--text)]">
            {t('dashboard.needsAttention')}
          </h2>
          <p id="dashboard-attention-description" className="sr-only">
            {t('dashboard.attentionHint')}
          </p>
        </div>
        {!isPending && !initialErrorMessage && (
          <span
            className="shrink-0 font-mono text-[12px] font-[650] tabular-nums"
            style={{ color: hasIssues ? 'var(--warn-text)' : 'var(--ok-text)' }}
            aria-label={t('dashboard.attentionCount', { count: issueCount })}
          >
            {issueCount}
          </span>
        )}
      </header>

      {isPending ? (
        <div aria-hidden="true" className="space-y-3 p-4">
          <div className="h-16 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
          <div className="h-12 animate-pulse rounded-[6px] bg-[var(--bg-soft)]" />
        </div>
      ) : initialErrorMessage ? (
        <div className="p-4">
          <QueryErrorState message={initialErrorMessage} onRetry={onRetry} />
        </div>
      ) : (
        <div className="p-4">
          {isStale && (
            <div
              role="status"
              className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-[6px] bg-[var(--warn-fill)] px-3 py-2 text-[11px] text-[var(--warn-text)]"
            >
              <span>{t('attention.queueStale')}</span>
              <ButtonV2
                type="button"
                variant="secondary"
                size="sm"
                aria-busy={isFetching || undefined}
                disabled={isFetching}
                onClick={onRetry}
              >
                {t('attention.retry')}
              </ButtonV2>
            </div>
          )}

          {hasIssues ? (
            <ul className="divide-y divide-[var(--border)]">
              {upstreams.length > 0 && (
                <AttentionItem
                  icon="warning"
                  title={t('attention.upstreamsTitle')}
                  detail={t('dashboard.upstreamWarning', {
                    count: upstreams.length,
                    names: upstreamNames,
                  })}
                  tone={upstreams.some(item => upstreamStatus(item) === 'failed') ? 'danger' : 'warning'}
                  to={getAdminRouteHref('upstreams')}
                  action={t('dashboard.viewUpstreams')}
                />
              )}
              {cacheNeedsAttention && (
                <AttentionItem
                  icon="storage"
                  title={t('attention.cacheTitle')}
                  detail={t('dashboard.storageWarning', { percent: cacheUsagePercent.toFixed(1) })}
                  tone={cacheUsagePercent > 95 ? 'danger' : 'warning'}
                  to={getAdminRouteHref('cache')}
                  action={t('dashboard.manageCache')}
                />
              )}
            </ul>
          ) : (
            <div role="status" className="flex min-h-28 items-center gap-3 py-2">
              <span
                aria-hidden
                className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-[8px] bg-[var(--ok-fill)] text-[var(--ok-text)]"
              >
                <Icon name="check_circle" size="sm" />
              </span>
              <div className="min-w-0">
                <h3 className="text-[13px] font-[680] text-[var(--text)]">
                  {t('dashboard.noActiveIssues')}
                </h3>
                <p className="mt-1.5 text-[12px] leading-[1.55] text-[var(--text-soft)]">
                  {t('dashboard.noActiveIssuesHint')}
                </p>
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
