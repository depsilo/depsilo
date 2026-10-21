import { useTranslation } from 'react-i18next'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import StatusDot from '@/components/StatusDot'
import { upstreamStatus } from '@/lib/upstreamStatus'
import type { AdminUpstream, CheckUpstreamResponse } from '@/lib/adminApi.types'

export interface CheckRecord {
  upstream: AdminUpstream
  receivedAt: string
  result?: CheckUpstreamResponse
  requestError?: string
}

export function CheckResults({ open, onOpenChange, records }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  records: CheckRecord[]
}) {
  const { t, i18n } = useTranslation()
  const formatter = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'medium' })
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[640px] text-[14px] leading-relaxed">
        <DialogTitle className="pr-10 text-[18px]">{t('upstreams.checkResults')}</DialogTitle>
        <DialogDescription className="text-[13px] leading-relaxed">{t('upstreams.checkResultsHint')}</DialogDescription>
        <div className="divide-y divide-border">
          {records.map(record => {
            const probe = record.result?.check
            const status = upstreamStatus(record.result?.upstream ?? record.upstream)
            const time = probe?.checked_at || record.receivedAt
            return (
              <section key={record.upstream.id} data-upstream-check-result={record.upstream.id} className="space-y-2 py-4 first:pt-0 last:pb-0">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <h3 className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-1 font-semibold"><span className="min-w-0 break-words">{record.upstream.name}</span><span className="text-[12px] font-normal text-muted-foreground">{record.upstream.adapter_type}</span></h3>
                  <time dateTime={time} className="text-[12px] text-muted-foreground">{formatter.format(new Date(time))}</time>
                </div>
                <p className="flex items-center gap-2">
                  <StatusDot status={probe ? probe.healthy ? 'healthy' : 'failed' : 'unknown'} />
                  {probe ? t(probe.healthy ? 'upstreams.probePassed' : 'upstreams.probeFailed') : t('upstreams.checkRequestFailedShort')}
                  {probe?.healthy && <span className="font-mono tabular-nums text-muted-foreground">{probe.latency_ms} ms</span>}
                </p>
                {(record.requestError || (probe && !probe.healthy)) && (
                  <p className="break-words text-[13px] text-muted-foreground">{record.requestError || probe?.error || t('upstreams.checkReasonUnavailable')}</p>
                )}
                <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
                  <StatusDot status={status} />
                  {t(record.result ? 'upstreams.runtimeStatusAtCheck' : 'upstreams.lastRetrievedRuntimeStatus', { status: t(`monitor.${status}`) })}
                </p>
                {probe?.healthy && status === 'failed' && <p className="text-[13px] text-muted-foreground">{t('upstreams.probePassedRuntimeFailed')}</p>}
              </section>
            )
          })}
        </div>
      </DialogContent>
    </Dialog>
  )
}
