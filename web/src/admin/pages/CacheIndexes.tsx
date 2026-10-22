import { Input } from '@/components/ui/input'
import { TableContainer, Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription } from '@/components/ui/empty'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import EcosystemIcon from '@/components/EcosystemIcon'

import Icon from '@/components/Icon'
import QueryErrorState from '@/components/QueryErrorState'
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
  const [searchParams, setSearchParams] = useSearchParams()
  const packageFilter = searchParams.get('package') ?? ''
  const [search, setSearch] = useState(packageFilter)
  const appliedSearch = packageFilter
  const [seenPackage, setSeenPackage] = useState(packageFilter)
  if (seenPackage !== packageFilter) { setSeenPackage(packageFilter); setSearch(packageFilter) }
  const [adapterType, setAdapterType] = useState('all')
  const [status, setStatus] = useState<'all' | CacheIndexStatus>('all')
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<CacheIndexEntry | null>(null)

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
    setSearchParams(search.trim() ? { package: search.trim() } : {})
    setPage(1)
  }

  function clearFilters() {
    setSearch('')
    setSearchParams({})
    setAdapterType('all')
    setStatus('all')
    setPage(1)
  }

  const hasFilters = Boolean(appliedSearch || adapterType !== 'all' || status !== 'all')

  const selectedItem = items.find(item => item.id === selected?.id) ?? selected

  return (
    <AdminPage description={t('cacheIndexes.subtitle')} actions={<Button variant="outline" disabled={query.isFetching} onClick={() => query.refetch()}><Icon name="refresh" size="sm" />{t('cacheIndexes.reload')}</Button>}>
      <div className="space-y-6">
        {query.isRefetchError && query.data && <StaleDataNotice message={t('cacheIndexes.staleNotice')} onRefresh={() => query.refetch()} />}
        {response && <section aria-label={t('cacheIndexes.summaryTitle')} className="flex flex-wrap gap-x-6 gap-y-3 border-b pb-4">
          <p className="text-sm text-muted-foreground">{t('cacheIndexes.summaryTitle')}</p>
          {summary.length === 0 ? <p className="text-sm text-muted-foreground">{t('cacheIndexes.noSummaryTitle')}</p> : summary.map(item => <div key={item.adapter_type} className="flex items-center gap-2 text-sm">
            {isAdminEcosystem(item.adapter_type) && <EcosystemIcon type={item.adapter_type} size={15} />}
            <span>{item.adapter_type.toUpperCase()}</span><span className="font-mono tabular-nums">{item.total.toLocaleString()}</span><span className="text-muted-foreground">{t('cacheIndexes.staleCount', {count:item.stale})}</span>
          </div>)}
        </section>}
        <form data-admin-filters className="grid grid-cols-1 gap-3 sm:flex sm:flex-wrap sm:items-end" onSubmit={event => { event.preventDefault(); applySearch() }}>
          <Input className="min-w-0 sm:basis-60 sm:flex-1" aria-label={t('cacheIndexes.searchLabel')} placeholder={t('cacheIndexes.searchPlaceholder')} value={search} onChange={event => setSearch(event.target.value)} />
          <NativeSelect className="sm:w-auto" aria-label={t('cacheIndexes.ecosystemFilter')} value={adapterType} onChange={event => {setAdapterType(event.target.value);setPage(1)}}><option value="all">{t('cacheIndexes.allEcosystems')}</option>{operatorEcosystems.map(ecosystem => <option key={ecosystem.id} value={ecosystem.id}>{ecosystem.label}</option>)}</NativeSelect>
          <NativeSelect className="sm:w-auto" aria-label={t('cacheIndexes.statusFilter')} value={status} onChange={event => {setStatus(event.target.value as typeof status);setPage(1)}}><option value="all">{t('cacheIndexes.allStatuses')}</option><option value="fresh">{t('cacheIndexes.statusFresh')}</option><option value="stale">{t('cacheIndexes.statusStale')}</option></NativeSelect>
          <Button type="submit" variant="outline">{t('search')}</Button>
          {hasFilters && <Button type="button" variant="ghost" onClick={clearFilters}>{t('cacheIndexes.clearFilters')}</Button>}
        </form>
        <p className="text-xs text-muted-foreground">{t('cacheIndexes.freshnessHint')}</p>
        {query.isPending ? <div role="status" className="py-12 text-center text-muted-foreground">{t('loading')}</div> : query.isError && !query.data ? <QueryErrorState message={errorMessage} onRetry={() => query.refetch()} /> : items.length === 0 ? (
          <Empty><EmptyHeader><EmptyMedia variant="icon"><Icon name="inventory_2" size="sm" /></EmptyMedia><EmptyTitle>{t(hasFilters ? 'cacheIndexes.emptyTitle' : 'cacheIndexes.emptyUnfilteredTitle')}</EmptyTitle><EmptyDescription>{t(hasFilters ? 'cacheIndexes.emptyHint' : 'cacheIndexes.emptyUnfilteredHint')}</EmptyDescription></EmptyHeader></Empty>
        ) : <TableContainer label={t('cacheIndexes.tableLabel')} minWidth={700}>
          <Table>
            <TableHeader><TableRow>{['packageName','ecosystem','status','size','expiresAt','actions'].map(key => <TableHead key={key}>{t(`cacheIndexes.${key}`)}</TableHead>)}</TableRow></TableHeader>
            <TableBody>{items.map(item => <TableRow key={item.id}>
              <TableCell className="max-w-64"><Button variant="link" className="h-auto max-w-full justify-start px-0 text-left" onClick={() => setSelected(item)}><span className="truncate">{item.package_name || item.key}</span></Button><p className="truncate text-xs text-muted-foreground">{item.key}</p></TableCell>
              <TableCell>{item.adapter_type.toUpperCase()}</TableCell>
              <TableCell><Badge variant={statusBadgeVariant(item.status)}>{t(item.status === 'fresh' ? 'cacheIndexes.statusFresh' : 'cacheIndexes.statusStale')}</Badge></TableCell>
              <TableCell className="font-mono tabular-nums">{formatBytes(item.size)}</TableCell>
              <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(item.expires_at)}</TableCell>
              <TableCell><div className="flex gap-2"><Button variant="ghost" size="sm" onClick={() => setSelected(item)}>{t('cacheIndexes.details')}</Button>{canWrite && <Button variant="outline" size="sm" aria-label={t('cacheIndexes.refreshNamed',{name:item.package_name || item.key})} disabled={refreshMutation.isPending || item.refresh_supported === false} onClick={() => refreshMutation.mutate({id:item.id,name:item.package_name || item.key})}>{t(refreshMutation.isPending && refreshMutation.variables?.id === item.id ? 'loading' : 'cacheIndexes.refresh')}</Button>}</div></TableCell>
            </TableRow>)}</TableBody>
          </Table>
        </TableContainer>}
        <AdminPagination ready={query.isSuccess && !query.isFetching} page={page} pageSize={pageSize} total={total} onPageChange={setPage} />
      </div>
      <Dialog open={selectedItem !== null} onOpenChange={open => {if (!open) setSelected(null)}}><DialogContent className="max-w-xl"><DialogTitle>{selectedItem?.package_name || t('cacheIndexes.details')}</DialogTitle>{selectedItem && <>
        <dl className="space-y-4 text-sm">{[
          [t('cacheIndexes.key'),selectedItem.key],
          [t('cacheIndexes.ecosystem'),selectedItem.adapter_type],
          [t('cacheIndexes.hitCount'),selectedItem.hit_count.toLocaleString()],
          ['ETag',selectedItem.etag || t('cacheIndexes.noValidator')],
          ['Last-Modified',selectedItem.last_modified || t('cacheIndexes.noValidator')],
          [t('cacheIndexes.lastAccessed'),formatTime(selectedItem.last_accessed)],
          [t('cacheIndexes.expiresAt'),formatTime(selectedItem.expires_at)],
          [t('cacheIndexes.updatedAt'),formatTime(selectedItem.updated_at)],
        ].map(([label,value]) => <div key={label}><dt className="text-muted-foreground">{label}</dt><dd className="mt-1 break-all font-mono">{value}</dd></div>)}</dl>
        {selectedItem.refresh_supported === false && <p className="text-sm text-muted-foreground">{t('cacheIndexes.refreshUnsupported')}</p>}
      </>}</DialogContent></Dialog>
    </AdminPage>
  )
}
