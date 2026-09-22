import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { analysisHref, bandwidthQuery, byteShare, reportParams, reportRange, trafficGroup, trafficRows, validReportDates } from '@/admin/bandwidthAnalysis'
import Button, { LinkButton } from '@/components/app/button'
import Select from '@/components/app/select'
import QueryErrorState from '@/components/app/error-state'
import Modal from '@/components/app/modal'
import { formatBytes } from '@/lib/utils'
import { getApiError } from '@/lib/apiError'

export default function TrafficAnalysis() {
  const { t, i18n } = useTranslation()
  const [search, setSearch] = useSearchParams()
  const [infoOpen, setInfoOpen] = useState(false)
  const range = reportRange(search.get('analysisRange'))
  const group = trafficGroup(search.get('group'))
  const params = reportParams(range, search.get('start') ?? '', search.get('end') ?? '')
  const query = useQuery(bandwidthQuery(params))
  const report = query.data?.data
  const summary = report?.summary
  const rows = report ? trafficRows(report, group).slice(0, 5) : []
  const share = summary ? byteShare(summary.hit_bytes, summary.total_bytes) : null
  const invalid = range === 'custom' && !validReportDates(params.start ?? '', params.end ?? '')
  const update = (key: string, value: string) => setSearch(previous => {
    const next = new URLSearchParams(previous)
    next.set(key, value)
    return next
  })
  const percent = (value: number | null) => value === null ? '—' : new Intl.NumberFormat(i18n.language, { style: 'percent', maximumFractionDigits: 1 }).format(value)
  const bytes = (value: number | undefined) => value !== undefined && Number.isFinite(value) ? formatBytes(value) : '—'

  return (
    <section data-query-key="overview-analysis" aria-labelledby="traffic-analysis-title" aria-busy={query.isFetching || undefined} className="min-w-0 border-t border-border px-4 pt-3">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="traffic-analysis-title">{t('dashboard.trafficComposition')}</h2>
        <div className="flex flex-wrap items-center gap-2">
          <Select aria-label={t('dashboard.analysisRange')} value={range} onChange={event => update('analysisRange', event.target.value)}>
            <option value="7d">{t('dashboard.report7d')}</option>
            <option value="30d">{t('dashboard.report30d')}</option>
            {range === '90d' && <option value="90d">{t('dashboard.report90d')}</option>}
            {range === 'custom' && <option value="custom">{t('bandwidth.custom')}</option>}
          </Select>
          <LinkButton variant="ghost" size="sm" to={analysisHref('bandwidth', search)}>{t('dashboard.fullAnalysis')} →</LinkButton>
        </div>
      </header>
      {invalid ? <p className="py-4 text-sm text-muted-foreground">{t('dashboard.reportCustomHint')}</p>
        : query.isPending ? <div aria-hidden className="my-4 h-44 animate-pulse rounded-sm bg-muted" />
          : query.isError && !report ? <div className="py-4"><QueryErrorState message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : t('dashboard.analysisUnavailable')} onRetry={() => { void query.refetch() }} /></div>
            : report && <>
              {query.isRefetchError && <div role="status" className="flex flex-wrap items-center justify-between gap-2 py-2 text-sm text-warning"><span>{t('now.staleData')}</span><Button variant="ghost" size="sm" onClick={() => { void query.refetch() }}>{t('now.refresh')}</Button></div>}
              <p className="mt-1 text-meta text-muted-foreground" data-analysis-window>{report.range ? t('dashboard.reportWindow', report.range) : t('dashboard.reportWindowUnknown')}</p>
              <div data-cache-summary className="flex flex-wrap items-baseline gap-x-4 gap-y-1 py-3 text-sm">
                <span>{t('dashboard.cacheServedTraffic')} <strong className="ml-1 font-mono font-medium tabular-nums">{bytes(summary?.hit_bytes)}</strong></span>
                <span className="text-muted-foreground">{t('dashboard.cacheByteShare')} <span className="font-mono tabular-nums">{percent(share)}</span></span>
                <Button variant="ghost" size="sm" className="ml-auto" onClick={() => setInfoOpen(true)} aria-haspopup="dialog">{t('dashboard.analysisBasis')}</Button>
              </div>
              <div role="group" aria-label={t('dashboard.trafficGroup')} className="flex flex-wrap gap-1 pb-2">
                {(['packages', 'ecosystems', 'upstreams'] as const).map(value => <Button key={value} variant="ghost" size="sm" aria-pressed={group === value} onClick={() => update('group', value)} className={group === value ? 'bg-accent text-foreground' : 'text-muted-foreground'}>{t(`dashboard.group_${value}`)}</Button>)}
              </div>
              <p className="mb-1 text-meta text-muted-foreground">{t(group === 'upstreams' ? 'dashboard.missResponseShare' : 'dashboard.allResponseShare')}</p>
              {rows.length ? <table className="w-full table-fixed text-sm" aria-label={t('dashboard.trafficComposition')}>
                <thead className="sr-only"><tr><th>{t('dashboard.trafficName')}</th><th>{t('dashboard.trendTabBandwidth')}</th><th>{t('dashboard.trafficShare')}</th></tr></thead>
                <tbody className="divide-y divide-border">
                  {rows.map(row => <tr key={row.key}>
                    <th scope="row" className="py-2 pr-2 text-left font-normal"><span className="block break-all">{row.name}</span>{row.ecosystem && <span className="text-meta text-muted-foreground">{row.ecosystem}</span>}</th>
                    <td className="w-28 py-2 text-right font-mono tabular-nums">{bytes(row.bytes)}</td>
                    <td className="w-20 py-2 pl-2 text-right font-mono tabular-nums text-muted-foreground">{percent(row.share)}</td>
                  </tr>)}
                </tbody>
              </table> : <p className="py-5 text-sm text-muted-foreground">{t('dashboard.compositionEmpty')}</p>}
            </>}
      <Modal open={infoOpen} onClose={() => setInfoOpen(false)} title={t('dashboard.analysisBasis')} width={560}>
        <div className="space-y-3 text-sm leading-6 text-muted-foreground">
          <p>{t('dashboard.reportBasis')}</p>
          <p>{t('dashboard.cacheBenefitBasis')}</p>
          <p>{t('dashboard.distributionBasis')}</p>
        </div>
      </Modal>
    </section>
  )
}
