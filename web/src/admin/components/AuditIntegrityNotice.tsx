import { Alert, AlertDescription } from '@/components/ui/alert'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { Button } from '@/components/ui/button'

function shortHash(hash: string) {
  return hash.length > 12 ? `${hash.slice(0, 12)}…` : hash
}

// AuditIntegrityNotice recomputes the tamper-evident audit chain and shows the
// result: verified head, the pre-chain prefix (rows written before schema v8),
// or the exact row where the chain stopped matching.
export default function AuditIntegrityNotice() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['admin', 'audit', 'integrity'],
    queryFn: ({ signal }) => adminApi.getAuditIntegrity({ signal }),
    retry: false,
    staleTime: 60_000,
  })
  const report = query.data?.data.integrity
  const anchors = query.data?.data.anchors
  const feed = query.data?.data.anchor_feed

  const verifyButton = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      aria-busy={query.isFetching || undefined}
      disabled={query.isFetching}
      onClick={() => { void query.refetch() }}
    >
      {query.isFetching ? t('auditIntegrity.verifying') : t('auditIntegrity.verify')}
    </Button>
  )

  if (query.isPending && !report) {
    return (
      <div aria-busy="true" className="rounded-md px-3 py-2 text-[13px]" style={{ background: 'var(--bg-soft)', color: 'var(--text-soft)' }}>
        {t('auditIntegrity.verifying')}
      </div>
    )
  }
  if (query.isError && !report) {
    return (
      <Alert variant={"warning"}><AlertDescription>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>{t('auditIntegrity.unavailable', { message: getApiError(query.error).message })}</span>
          {verifyButton}
        </div>
      </AlertDescription></Alert>
    )
  }
  if (!report) return null
  const chainBroken = !report.ok
  const anchorBroken = anchors?.configured && !anchors.ok
  const remoteError = feed?.remote_configured && feed.remote_last_error
  if (chainBroken || anchorBroken || remoteError || query.isRefetchError) {
    return (
      <div className="space-y-2">
        {chainBroken && (
          <Alert variant={"destructive"}><AlertDescription>
            {t('auditIntegrity.broken', { id: report.broken_at_id ?? 0, reason: report.reason ?? '' })}
          </AlertDescription></Alert>
        )}
        {anchorBroken && (
          <Alert variant={"destructive"}><AlertDescription>
            {t('auditIntegrity.anchorBroken', { id: anchors.broken_at_id ?? 0, reason: anchors.reason ?? '' })}
          </AlertDescription></Alert>
        )}
        {query.isRefetchError && (
          <Alert variant={"warning"}><AlertDescription>{t('auditIntegrity.stale')}</AlertDescription></Alert>
        )}
        {remoteError && (
          <Alert variant={"warning"}><AlertDescription>{t('auditIntegrity.remoteError', { message: feed.remote_last_error })}</AlertDescription></Alert>
        )}
        <div className="flex justify-end">
          {verifyButton}
        </div>
      </div>
    )
  }
  return (
    <Alert variant={"success"}><AlertDescription>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span>
          {t('auditIntegrity.verified', {
            count: report.chained_rows.toLocaleString(),
            head: report.head_id ?? 0,
            hash: shortHash(report.head_hash ?? ''),
          })}
          {report.unchained_rows > 0
            ? ` · ${t('auditIntegrity.preChain', { count: report.unchained_rows.toLocaleString() })}`
            : ''}
          {anchors?.configured
            ? ` · ${anchors.checkpoints > 0
              ? t('auditIntegrity.anchorVerified', {
                count: anchors.checkpoints,
                head: anchors.latest_head_id ?? 0,
              })
              : t('auditIntegrity.anchorEmpty')}`
            : ''}
          {feed?.remote_configured && feed.remote_last_success_at
            ? ` · ${t('auditIntegrity.remoteVerified', { head: feed.remote_head_id ?? 0 })}`
            : ''}
        </span>
        {verifyButton}
      </div>
    </AlertDescription></Alert>
  )
}
