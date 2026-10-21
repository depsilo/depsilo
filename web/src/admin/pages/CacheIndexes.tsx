import { Input } from '@/components/ui/input'
import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription, EmptyContent } from '@/components/ui/empty'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import EcosystemIcon from '@/components/EcosystemIcon'

import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
import SectionHeader from '@/components/SectionHeader'
import { NativeSelect } from '@/components/ui/native-select'

import { useAppToast } from '@/components/Toast'
import AdminPage from '@/admin/components/AdminPage'
import AdminPagination from '@/admin/components/AdminPagination'
import StaleDataNotice from '@/admin/components/StaleDataNotice'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi } from '@/lib/api'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import type { CacheIndexEntry, CacheIndexQuery, CacheIndexStatus } from '@/lib/adminApi.types'
import { getApiError } from '@/lib/apiError'
import { formatBytes, formatTime } from '@/lib/utils'

const PAGE_SIZE = 25

function statusBadgeVariant(status: CacheIndexStatus): 'success' | 'warning' {
  return status === 'fresh' ? 'success' : 'warning'
}

export default function CacheIndexes() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useAppToast()
  const { canWrite } = usePrincipal()
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const [adapterType, setAdapterType] = useState('all')
  const [status, setStatus] = useState<'all' | CacheIndexStatus>('all')
  const [page, setPage] = useState(1)

  const params: CacheIndexQuery = { page, page_size: PAGE_SIZE }
  if (appliedSearch) params.search = appliedSearch
  if (adapterType !== 'all') params.adapter_type = adapterType
  if (status !== 'all') params.status = status

  const query = useQuery({
    queryKey: ['admin', 'cache', 'indexes', params],
    queryFn: ({ signal }) => adminApi.listCacheIndexes(params, { signal }),
    retry: false,
    refetchInterval: 30000,
  })

  const refreshMutation = useMutation({
    mutationFn: ({ id }: { id: number; name: string }) => adminApi.refreshCacheIndex(id),
    onSuccess: (_response, { name }) => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'cache', 'indexes'] })
      toast.show({ tone: 'success', message: t('cacheIndexes.refreshSuccess', { name }) })
    },
    onError: (error, { name }) => {
      toast.show({
        tone: 'danger',
        message: t('cacheIndexes.refreshFailed', { name, reason: getApiError(error).message }),
      })
    },
  })

  const response = query.data?.data
  const items: CacheIndexEntry[] = response?.items ?? []
  const summary = response?.summary ?? []
  const total = response?.total ?? 0
  const pageSize = response?.page_size || PAGE_SIZE
  const apiError = getApiError(query.error)
  const errorMessage = apiError.status === 403
    ? t('common.permissionDenied')
    : t('cacheIndexes.loadError')

  function applySearch() {
    setAppliedSearch(search.trim())
    setPage(1)
  }

  function clearFilters() {
    setSearch('')
    setAppliedSearch('')
    setAdapterType('all')
    setStatus('all')
    setPage(1)
  }

  const hasFilters = Boolean(appliedSearch || adapterType !== 'all' || status !== 'all')

  if (query.isPending) {
    return (
      <AdminPage description={t('cacheIndexes.subtitle')}>
        <div aria-busy="true" className="py-16 text-center text-[13px] text-[var(--text-soft)]">
          <span aria-hidden="true">{t('loading')}</span>
        </div>
      </AdminPage>
    )
  }

  if (query.isError && !query.data) {
    return (
      <AdminPage description={t('cacheIndexes.subtitle')}>
        <QueryErrorState message={errorMessage} onRetry={() => { void query.refetch() }} />
      </AdminPage>
    )
  }

  return (
    <AdminPage description={t('cacheIndexes.subtitle')}>
      <div className="space-y-8">
      {query.isRefetchError && query.data && (
        <StaleDataNotice
          message={t('cacheIndexes.staleNotice')}
          onRefresh={() => { void query.refetch() }}
        />
      )}

      <section aria-label={t('cacheIndexes.summaryTitle')}>
        <SectionHeader
          title={t('cacheIndexes.summaryTitle')}
          hint={t('cacheIndexes.summaryHint')}
        />
        {summary.length === 0 ? (
          <Empty style={{ minHeight: 96 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"inventory_2"} size="sm" /></EmptyMedia><EmptyTitle>{t('cacheIndexes.noSummaryTitle')}</EmptyTitle>{t('cacheIndexes.noSummaryHint') && <EmptyDescription>{t('cacheIndexes.noSummaryHint')}</EmptyDescription>}</EmptyHeader></Empty>
        ) : (
          <div className="grid gap-x-8 gap-y-6 sm:grid-cols-2 xl:grid-cols-4">
            {summary.map((item) => (
              <article key={item.adapter_type} className="border-l-2 border-[var(--border)] pl-4">
                <div className="mb-3 flex items-center gap-2">
                  {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={15} />}
                  <span className="text-[12px] font-[600] uppercase" style={{ color: 'var(--text)' }}>
                    {item.adapter_type}
                  </span>
                </div>
                <div className="flex items-end justify-between gap-4">
                  <div>
                    <p className="text-[10px] font-mono font-[600] uppercase" style={{ color: 'var(--text-subtle)' }}>
                      {t('cacheIndexes.total')}
                    </p>
                    <p data-metric-value className="mt-1 font-mono text-[28px] font-[600] leading-none tabular-nums" style={{ color: 'var(--text)' }}>
                      {item.total.toLocaleString()}
                    </p>
                  </div>
                  <div className="space-y-1 text-right text-[11px] font-mono tabular-nums">
                    <p style={{ color: 'var(--ok-text)' }}>
                      {t('cacheIndexes.freshCount', { count: item.fresh })}
                    </p>
                    <p style={{ color: item.stale > 0 ? 'var(--warn-text)' : 'var(--text-subtle)' }}>
                      {t('cacheIndexes.staleCount', { count: item.stale })}
                    </p>
                  </div>
                </div>
                <p className="mt-3 text-[10px]" style={{ color: 'var(--text-subtle)' }}>
                  {t('cacheIndexes.lastUpdated')}: {item.last_updated ? formatTime(item.last_updated) : t('cacheIndexes.neverUpdated')}
                </p>
              </article>
            ))}
          </div>
        )}
      </section>

      <form
        data-admin-filters
        className="flex flex-col items-stretch gap-3 sm:flex-row sm:flex-wrap sm:items-center"
        onSubmit={(event) => { event.preventDefault(); applySearch() }}
      >
        <div className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 rounded-sm px-3 py-1.5" style={{ border: '1px solid var(--border)' }}>
          <Icon name="search" size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
            <Input
              aria-label={t('cacheIndexes.searchLabel')}
              className={"min-w-0 flex-1 bg-transparent text-[16px] outline-none md:text-[13px]" + ' h-9 rounded-none border-0 px-0 shadow-none focus-visible:ring-0 dark:bg-transparent'}
              style={{ color: 'var(--text)' }}
              placeholder={t('cacheIndexes.searchPlaceholder')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
        </div>

        <div className="grid grid-cols-2 gap-3 sm:contents">
            <NativeSelect className="min-h-10 sm:w-auto" aria-label={t('cacheIndexes.ecosystemFilter')} value={adapterType} onChange={(event) => { setAdapterType(event.target.value); setPage(1) }}>
              <option value="all">{t('cacheIndexes.allEcosystems')}</option>
              {operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}
            </NativeSelect>
            <NativeSelect className="min-h-10 sm:w-auto" aria-label={t('cacheIndexes.statusFilter')} value={status} onChange={(event) => { setStatus(event.target.value as 'all' | CacheIndexStatus); setPage(1) }}>
              <option value="all">{t('cacheIndexes.allStatuses')}</option>
              <option value="fresh">{t('cacheIndexes.statusFresh')}</option>
              <option value="stale">{t('cacheIndexes.statusStale')}</option>
            </NativeSelect>
        </div>
        <Button type="submit" variant="default" size="sm" className="min-h-10 self-start sm:min-h-0">
          {t('search')}
        </Button>
      </form>

      {items.length === 0 ? (
        <Empty style={{ minHeight: 160 }}><EmptyHeader><EmptyMedia variant="icon"><Icon name={"inventory_2"} size="sm" /></EmptyMedia><EmptyTitle>{t(hasFilters ? 'cacheIndexes.emptyTitle' : 'cacheIndexes.emptyUnfilteredTitle')}</EmptyTitle>{t(hasFilters ? 'cacheIndexes.emptyHint' : 'cacheIndexes.emptyUnfilteredHint') && <EmptyDescription>{t(hasFilters ? 'cacheIndexes.emptyHint' : 'cacheIndexes.emptyUnfilteredHint')}</EmptyDescription>}</EmptyHeader><EmptyContent>{hasFilters ? (
            <Button type="button" variant="outline" size="sm" onClick={clearFilters}>
              {t('cacheIndexes.clearFilters')}
            </Button>
          ) : undefined}</EmptyContent></Empty>
      ) : (
        <TableContainer label={t('cacheIndexes.tableLabel')} minWidth={canWrite ? 1210 : 1160}>
          <Table className="w-full text-[12px]">
            <TableHeader>
              <TableRow style={{ borderBottom: '1px solid var(--border)' }}>
                {[
                  t('cacheIndexes.key'),
                  t('cacheIndexes.ecosystem'),
                  t('cacheIndexes.packageName'),
                  t('cacheIndexes.status'),
                  t('cacheIndexes.size'),
                  t('cacheIndexes.hitCount'),
                  t('cacheIndexes.validator'),
                  t('cacheIndexes.lastAccessed'),
                  t('cacheIndexes.expiresAt'),
                  t('cacheIndexes.updatedAt'),
                ].map((heading) => (
                  <TableHead key={heading} scope="col" className="px-3 py-2 text-left font-mono text-[10px] font-[600] uppercase first:pl-0" style={{ color: 'var(--text-subtle)' }}>
                    {heading}
                  </TableHead>
                ))}
                {canWrite && (
                  <TableHead scope="col" className="px-3 py-2 text-left font-mono text-[10px] font-[600] uppercase" style={{ color: 'var(--text-subtle)' }}>
                    {t('cacheIndexes.actions')}
                  </TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow
                  key={item.id}
                  className="transition-colors duration-75 hover:bg-[var(--bg-soft)]"
                  style={{ borderBottom: '1px solid var(--border-soft, var(--border))' }}
                >
                  <TableCell className="max-w-[280px] py-2 pr-3">
                    <span className="block truncate font-mono text-[11px]" style={{ color: 'var(--text-soft)' }} title={item.key}>
                      {item.key}
                    </span>
                  </TableCell>
                  <TableCell className="px-3 py-2">
                    <div className="flex items-center gap-1.5">
                      {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={13} />}
                      <span className="uppercase" style={{ color: 'var(--text)' }}>{item.adapter_type}</span>
                    </div>
                  </TableCell>
                  <TableCell className="max-w-[220px] px-3 py-2">
                    <span className="block truncate font-mono" style={{ color: 'var(--text)' }} title={item.package_name}>
                      {item.package_name || '—'}
                    </span>
                  </TableCell>
                  <TableCell className="px-3 py-2">
                    <Badge variant={statusBadgeVariant(item.status)}>
                      {item.status === 'fresh' ? t('cacheIndexes.statusFresh') : t('cacheIndexes.statusStale')}
                    </Badge>
                  </TableCell>
                  <TableCell className="whitespace-nowrap px-3 py-2 font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                    {formatBytes(item.size)}
                  </TableCell>
                  <TableCell className="px-3 py-2 font-mono tabular-nums" style={{ color: 'var(--text)' }}>
                    {item.hit_count.toLocaleString()}
                  </TableCell>
                  <TableCell className="max-w-[220px] px-3 py-2">
                    {item.etag || item.last_modified ? (
                      <div className="space-y-0.5 font-mono text-[10px]" style={{ color: 'var(--text-soft)' }}>
                        {item.etag && <p className="truncate" title={item.etag}>ETag: {item.etag}</p>}
                        {item.last_modified && <p className="truncate" title={item.last_modified}>Last-Modified: {item.last_modified}</p>}
                      </div>
                    ) : <span style={{ color: 'var(--text-subtle)' }}>{t('cacheIndexes.noValidator')}</span>}
                  </TableCell>
                  <TableCell className="whitespace-nowrap px-3 py-2" style={{ color: 'var(--text-soft)' }}>{formatTime(item.last_accessed)}</TableCell>
                  <TableCell className="whitespace-nowrap px-3 py-2" style={{ color: item.status === 'stale' ? 'var(--warn-text)' : 'var(--text-soft)' }}>{formatTime(item.expires_at)}</TableCell>
                  <TableCell className="whitespace-nowrap px-3 py-2" style={{ color: 'var(--text-soft)' }}>{formatTime(item.updated_at)}</TableCell>
                  {canWrite && (
                    <TableCell className="px-3 py-2">
                      <Tooltip>
                        <TooltipTrigger render={
                          <Button
                            onClick={() => refreshMutation.mutate({ id: item.id, name: item.package_name || item.key })}
                            variant="ghost"
                            size="icon"
                            data-icon-button
                            aria-label={t('cacheIndexes.refreshNamed', { name: item.package_name || item.key })}
                            aria-busy={refreshMutation.isPending && refreshMutation.variables?.id === item.id || undefined}
                            disabled={refreshMutation.isPending}
                            style={{ color: 'var(--text-soft)' }}
                          />
                        }>
                          <Icon name={refreshMutation.isPending && refreshMutation.variables?.id === item.id ? 'progress_activity' : ("refresh")} size="sm" className={refreshMutation.isPending && refreshMutation.variables?.id === item.id ? 'animate-spin' : ''} />
                        </TooltipTrigger>
                        <TooltipContent>{t('cacheIndexes.refreshNamed', { name: item.package_name || item.key })}</TooltipContent>
                      </Tooltip>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <AdminPagination page={page} pageSize={pageSize} total={total} onPageChange={setPage} />
      </div>
    </AdminPage>
  )
}
