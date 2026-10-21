import { useEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { operatorEcosystems } from '@/admin/operatorEcosystems'
import { useTranslation } from 'react-i18next'
import { adminApi } from '@/lib/api'
import { formatBytes, formatTime } from '@/lib/utils'
import { getApiError } from '@/lib/apiError'
import { Badge } from '@/components/ui/badge'
import SectionHeader from '@/components/SectionHeader'
import QueryErrorState from '@/components/QueryErrorState'
import StaleDataNotice from '@/admin/components/StaleDataNotice'

export default function DownloadTransfers() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['cache-transfers'],
    queryFn: async ({ signal }) => (await adminApi.getCacheTransfers({ signal })).data,
    refetchInterval: 2000,
    retry: false,
  })
  const data = query.data
  const completed = data?.items.filter(item => item.status === 'cached').map(item => `${item.id}:${item.finished_at}`).join('|') ?? ''
  const previousCompleted = useRef('')
  useEffect(() => {
    if (!query.isError && completed !== previousCompleted.current) {
      previousCompleted.current = completed
      void queryClient.invalidateQueries({ queryKey: ['admin', 'cache'] })
    }
  }, [completed, query.isError, queryClient])
  const error = getApiError(query.error)
  const items = data?.items.slice(0, 12) ?? []
  return (
    <section aria-label={t('cache.transfers.title')} className="min-w-0">
      <SectionHeader title={t('cache.transfers.title')} hint={t('cache.transfers.scope')} />
      {query.isPending ? <p role="status" className="text-[13px] text-[var(--text-soft)]">{t('cache.transfers.loading')}</p>
        : query.isError && (!data || error.status === 403) ? <QueryErrorState message={t(error.status === 403 ? 'cache.transfers.permission' : 'cache.transfers.unavailable')} onRetry={() => { void query.refetch() }} />
          : <>
            {query.isRefetchError && <div className="mb-3"><StaleDataNotice onRefresh={() => query.refetch()} /></div>}
            {items.length === 0 ? <p className="text-[13px] text-[var(--text-soft)]">{t('cache.transfers.empty')}</p> : <ul className="divide-y divide-[var(--border)]">
              {items.map(item => {
                const ecosystem = item.ecosystem === 'extra:pytorch' ? 'PyTorch' : operatorEcosystems.find(ecosystem => ecosystem.id === item.ecosystem)?.label ?? item.ecosystem.replace(/^extra:/, '')
                const percent = item.total_bytes > 0 ? Math.min(100, Math.max(0, Math.round(item.downloaded_bytes / item.total_bytes * 100))) : undefined
                const size = `${formatBytes(item.downloaded_bytes)} / ${item.total_bytes > 0 ? formatBytes(item.total_bytes) : t('cache.transfers.unknown')}`
                return <li key={item.id} className="min-w-0 space-y-2 py-4">
                  <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                    <p className="min-w-0 flex-1 break-all font-mono text-[12px] text-[var(--text)]">{item.name}</p>
                    <Badge variant={item.status === 'cached' ? 'success' : item.status === 'failed' ? "destructive" : item.status === 'resuming' ? 'warning' : 'neutral'}>{t(`cache.transfers.${item.status}`)}</Badge>
                  </div>
                  <div role="progressbar" aria-label={t('cache.transfers.progress', { name: item.name })} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent} aria-valuetext={size} className="h-1.5 overflow-hidden rounded-full bg-[var(--bg-soft)]">
                    <div className="h-full rounded-full bg-[var(--brand)]" style={{ width: percent === undefined ? '25%' : `${percent}%` }} />
                  </div>
                  <div className="flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-[var(--text-soft)]">
                    <span className="font-mono tabular-nums">{size}</span>
                    {percent !== undefined && <span>{percent}%</span>}
                    {!item.finished_at && <span>{t('cache.transfers.rate', { rate: formatBytes(item.bytes_per_second) })}</span>}
                    <span>{ecosystem}{item.upstream ? ` · ${item.upstream}` : ''}</span>
                    <span>{formatTime(item.started_at)}</span>
                  </div>
                  {item.resume_attempts > 0 && <p className="text-[12px] text-[var(--text-soft)]">{t('cache.transfers.resumes', { count: item.resume_attempts })} · {t('cache.transfers.offset', { bytes: formatBytes(item.resume_offset) })}</p>}
                  {item.status === 'cached' && <p className="text-[12px] text-[var(--text-soft)]">{t('cache.transfers.saved')}</p>}
                  {item.status === 'failed' && <p className="text-[12px] text-[var(--text-soft)]">{t('cache.transfers.failedDetail')}</p>}
                </li>
              })}
            </ul>}
            {data && data.items.length > items.length && <p className="text-[12px] text-[var(--text-soft)]">{t('cache.transfers.shown', { shown: items.length, count: data.items.length })}</p>}
          </>}
    </section>
  )
}
