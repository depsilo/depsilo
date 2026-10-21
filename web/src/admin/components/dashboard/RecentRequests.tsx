import type { UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableContainer, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { getAdminRouteHref } from '@/admin/routes'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { REQUEST_OUTCOME_META, requestOutcome } from '@/lib/dashboardOverview'
import { formatBytes, formatLatency, formatTime } from '@/lib/utils'

import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'

interface RecentRequestsProps {
  query: UseQueryResult<Awaited<ReturnType<typeof adminApi.listLogs>>>
  limit?: number
  onOpenDetails: (id: number) => void
}

/**
 * Latest client requests. This is a live tail independent of the selected
 * statistics range, which the caption states explicitly so operators do not
 * read it as range-scoped data.
 */
export default function RecentRequests({ query, limit = 5, onOpenDetails }: RecentRequestsProps) {
  const { t } = useTranslation()
  const items = query.data?.data.items ?? []
  const isStale = query.isRefetchError && query.data !== undefined
  const initialError = query.isError && query.data === undefined

  return (
    <Card
      render={<section />}
      data-dashboard-recent-requests
      data-dashboard-recent-empty={!query.isPending && !initialError && items.length === 0 ? 'true' : undefined}
      aria-labelledby="overview-recent-title"
      aria-busy={query.isPending || undefined}
      className="h-full min-w-0 flex-1 gap-0 p-0"
    >
      <CardHeader className="min-h-[76px] gap-x-3 py-3">
        <div className="min-w-0">
          <CardTitle id="overview-recent-title" className="pt-2">
            {t('overview.recentRequestsTitle')}
          </CardTitle>
          <CardDescription className="mt-0.5">
            {t('overview.recentRequestsCaption')}
          </CardDescription>
        </div>
        <Button
          variant="link"
          nativeButton={false}
          role="link"
          render={<Link to={getAdminRouteHref('accessLogs')} />}
          className="px-2 text-[14px]"
        >
          {t('dashboard.viewAll')}
          <Icon name="chevron_right" size="sm" />
        </Button>
      </CardHeader>

      {isStale && (
        <div className="px-5 pb-2"><Alert role="status" variant="warning"><AlertDescription>{t('overview.dataStale')}</AlertDescription></Alert></div>
      )}

      {query.isPending ? (
        <div aria-hidden className="flex flex-col gap-px px-5 pb-5">
          {/* Match the real table's header + row heights so the populated
              state does not shift when the first response lands. */}
          <Skeleton className="h-9" />
          {Array.from({ length: limit }, (_, index) => (
            <Skeleton key={index} className="h-[55px]" />
          ))}
        </div>
      ) : initialError ? (
        <div className="px-5 pb-5">
          <Alert variant="warning" role="alert" className="flex flex-wrap items-center justify-between gap-2">
            <AlertDescription>{getApiError(query.error).status === 403 ? t('common.permissionDenied') : t('overview.recentRequestsUnavailable')}</AlertDescription>
            <Button variant="outline" size="sm" onClick={() => { void query.refetch() }}>{t('overview.refresh')}</Button>
          </Alert>
        </div>
      ) : items.length === 0 ? (
        <Empty className="min-h-[112px] gap-1.5 px-5 pb-5 pt-2">
          <EmptyHeader className="gap-1.5">
            <EmptyMedia variant="icon" className="mb-0 rounded-full"><Icon name="download" size="sm" /></EmptyMedia>
            <EmptyTitle>{t('overview.recentEmpty')}</EmptyTitle>
            <EmptyDescription>{t('overview.recentEmptyHint')}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <TableContainer label={t('overview.recentRequestsTitle')} minWidth={600}>
          <Table className="w-full border-collapse text-left">
            <TableHeader>
              <TableRow style={{ color: 'var(--dash-muted)' }}>
                <TableHead className="px-4 py-2.5 text-[13px] font-medium">{t('overview.colPackage')}</TableHead>
                <TableHead className="px-3 py-2.5 text-[13px] font-medium whitespace-nowrap">{t('overview.colEcosystem')}</TableHead>
                <TableHead className="px-3 py-2.5 text-[13px] font-medium whitespace-nowrap">{t('overview.colOutcome')}</TableHead>
                <TableHead className="px-3 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colSize')}</TableHead>
                <TableHead className="px-3 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colLatency')}</TableHead>
                <TableHead className="px-4 py-2.5 text-right text-[13px] font-medium whitespace-nowrap">{t('overview.colTime')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map(item => {
                const outcome = requestOutcome(item)
                const meta = REQUEST_OUTCOME_META[outcome]
                const name = item.package_name || t('recentDownloads.unknownPackage')
                return (
                  <TableRow
                    key={item.id}
                    data-recent-request-id={item.id}
                    className="cursor-pointer border-t transition-colors duration-150 hover:bg-[var(--dash-soft)]"
                    style={{ borderColor: 'var(--dash-border)' }}
                    onClick={() => onOpenDetails(item.id)}
                  >
                    <TableCell className="max-w-[180px] px-4 py-4">
                      <Button
                        variant="ghost"
                        type="button"
                        onClick={event => { event.stopPropagation(); onOpenDetails(item.id) }}
                        className="block h-auto min-w-0 max-w-full truncate p-0 text-left font-mono text-[14px] font-medium"
                        style={{ color: 'var(--dash-ink)' }}
                        title={item.version ? `${name}@${item.version}` : name}
                      >
                        {name}
                        {item.version && <span style={{ color: 'var(--dash-muted)' }}>@{item.version}</span>}
                      </Button>
                    </TableCell>
                    <TableCell className="px-3 py-4">
                      <span className="inline-flex items-center gap-1.5 text-[13px]" style={{ color: 'var(--dash-muted)' }}>
                        {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={14} decorative />}
                        {item.adapter_type || '—'}
                      </span>
                    </TableCell>
                    <TableCell className="px-3 py-4">
                      <Badge variant={meta.variant}>{t(meta.key)}</Badge>
                    </TableCell>
                    <TableCell className="px-3 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-ink)' }}>
                      {formatBytes(item.bytes_sent)}
                    </TableCell>
                    <TableCell className="px-3 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-muted)' }}>
                      {formatLatency(item.latency_ms)}
                    </TableCell>
                    <TableCell className="px-4 py-4 text-right font-mono text-[13px] tabular-nums whitespace-nowrap" style={{ color: 'var(--dash-muted)' }}>
                      <time dateTime={item.created_at}>{formatTime(item.created_at, 'auto')}</time>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Card>
  )
}
