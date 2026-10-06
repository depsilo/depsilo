import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import ButtonV2 from '@/components/Button'
import InlineNotice from '@/components/InlineNotice'

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

  const verifyButton = (
    <ButtonV2
      type="button"
      variant="secondary"
      size="sm"
      aria-busy={query.isFetching || undefined}
      disabled={query.isFetching}
      onClick={() => { void query.refetch() }}
    >
      {query.isFetching ? t('auditIntegrity.verifying') : t('auditIntegrity.verify')}
    </ButtonV2>
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
      <InlineNotice tone="warning">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>{t('auditIntegrity.unavailable', { message: getApiError(query.error).message })}</span>
          {verifyButton}
        </div>
      </InlineNotice>
    )
  }
  if (!report) return null
  if (anchors?.configured && !anchors.ok) {
    return (
      <InlineNotice tone="danger">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>
            {t('auditIntegrity.anchorBroken', {
              id: anchors.broken_at_id ?? 0,
              reason: anchors.reason ?? '',
            })}
          </span>
          {verifyButton}
        </div>
      </InlineNotice>
    )
  }
  if (!report.ok) {
    return (
      <InlineNotice tone="danger">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>
            {t('auditIntegrity.broken', {
              id: report.broken_at_id ?? 0,
              reason: report.reason ?? '',
            })}
          </span>
          {verifyButton}
        </div>
      </InlineNotice>
    )
  }
  return (
    <InlineNotice tone="success">
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
        </span>
        {verifyButton}
      </div>
    </InlineNotice>
  )
}
