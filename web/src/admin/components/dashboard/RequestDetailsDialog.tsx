import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import BadgeV2 from '@/components/Badge'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import ModalV2 from '@/components/Modal'
import QueryErrorState from '@/components/QueryErrorState'
import TabsV2 from '@/components/Tabs'
import { useAppToast } from '@/components/Toast'
import { getAdminRouteHref } from '@/admin/routes'
import type { AccessLogDetail } from '@/lib/adminApi.types'
import { isAdminEcosystem } from '@/lib/adminApi.types'
import { adminApi } from '@/lib/api'
import { getApiError } from '@/lib/apiError'
import { copyText } from '@/lib/clipboard'
import { formatBytes, formatTime } from '@/lib/utils'
import { REQUEST_OUTCOME_META, requestOutcome } from '@/lib/dashboardOverview'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{label}</dt>
      <dd className="min-w-0 break-words text-[14px]" style={{ color: 'var(--text)' }}>{children}</dd>
    </div>
  )
}

function CopyableField({ label, value }: { label: string; value: string }) {
  const { t } = useTranslation()
  const toast = useAppToast()
  const [copied, setCopied] = useState(false)
  const available = value && value !== '—'

  async function handleCopy() {
    if (!available) return
    const ok = await copyText(value)
    if (ok) {
      setCopied(true)
      toast.show({ tone: 'success', message: t('common.copied') })
      window.setTimeout(() => setCopied(false), 1500)
    }
  }

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-[13px]" style={{ color: 'var(--text-soft)' }}>{label}</dt>
      <dd className="flex min-w-0 items-center gap-1.5">
        <span className="min-w-0 break-all font-mono text-[13px]" style={{ color: 'var(--text)' }}>{value || '—'}</span>
        {available && (
          <button
            type="button"
            onClick={handleCopy}
            aria-label={copied ? t('common.copied') : t('overview.copyValue', { label })}
            className="dash-focus grid size-6 shrink-0 place-items-center rounded hover:bg-[var(--bg-hover)]"
            style={{ color: 'var(--text-soft)' }}
          >
            <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
          </button>
        )}
      </dd>
    </div>
  )
}

function BasicTab({ detail, t }: { detail: AccessLogDetail; t: TFunction }) {
  const ecosystem = detail.adapter_type || '—'
  return (
    <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
      <Field label={t('overview.fieldTime')}>{formatTime(detail.created_at, 'auto')}</Field>
      <Field label={t('overview.fieldEcosystem')}>
        <span className="inline-flex items-center gap-1.5">
          {isAdminEcosystem(ecosystem) && <EcosystemIcon type={ecosystem} size={14} decorative />}
          {ecosystem}
        </span>
      </Field>
      <Field label={t('overview.fieldPackage')}>
        <span className="font-mono">{detail.package_name || '—'}</span>
      </Field>
      <Field label={t('overview.fieldVersion')}>
        <span className="font-mono">{detail.version || '—'}</span>
      </Field>
      <Field label={t('overview.fieldResponseSize')}>{formatBytes(detail.bytes_sent)}</Field>
      <Field label={t('overview.fieldLatency')}>{detail.latency_ms.toLocaleString()} ms</Field>
      <Field label={t('overview.fieldClientIP')}><span className="font-mono">{detail.client_ip || '—'}</span></Field>
      <Field label={t('overview.fieldUpstream')}><span className="font-mono">{detail.upstream || '—'}</span></Field>
      <Field label={t('overview.fieldMethod')}>{detail.method || '—'}</Field>
      <Field label={t('overview.fieldStatus')}>{detail.status_code || '—'}</Field>
      <CopyableField label={t('overview.fieldRequestID')} value={detail.request_id || '—'} />
      <CopyableField label={t('overview.fieldCacheKey')} value={detail.cache_key || '—'} />
    </div>
  )
}

function ChainTab({ detail, t }: { detail: AccessLogDetail; t: TFunction }) {
  const originRequests = detail.upstream_requests ?? 0
  const originBytes = detail.upstream_bytes ?? 0
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2">
        <Field label={t('overview.fieldCacheResult')}>{detail.cache_result || '—'}</Field>
        <Field label={t('overview.fieldCacheReason')}>{detail.cache_reason || '—'}</Field>
        <Field label={t('overview.fieldPolicyDecision')}>{detail.policy_decision || '—'}</Field>
        <Field label={t('overview.fieldPolicyReason')}>{detail.policy_reason || '—'}</Field>
        <Field label={t('overview.fieldDeliveryResult')}>{detail.delivery_result || '—'}</Field>
        <Field label={t('overview.fieldDeliveryReason')}>{detail.delivery_reason || '—'}</Field>
        <Field label={t('overview.fieldUpstreamRequests')}>{originRequests.toLocaleString()}</Field>
        <Field label={t('overview.fieldUpstreamBytes')}>{formatBytes(originBytes)}</Field>
      </div>
      <p className="rounded-md px-3 py-2 text-[13px] leading-[1.6]" style={{ background: 'var(--bg-soft)', color: 'var(--text-soft)' }}>
        {t('overview.chainNote')}
      </p>
    </div>
  )
}

function LogTab({ detail, t }: { detail: AccessLogDetail; t: TFunction }) {
  if (!detail.audit_events || detail.audit_events.length === 0) {
    return <p className="text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('overview.noAuditEvents')}</p>
  }
  return (
    <ol className="flex flex-col gap-3">
      {detail.audit_events.map(event => (
        <li key={event.id} className="flex min-w-0 flex-col gap-1 rounded-md px-3 py-2" style={{ background: 'var(--bg-soft)' }}>
          <div className="flex flex-wrap items-center gap-2">
            <BadgeV2 variant="neutral">{event.action}</BadgeV2>
            <span className="font-mono text-[13px]" style={{ color: 'var(--text)' }}>
              {event.package_name}{event.version ? `@${event.version}` : ''}
            </span>
            <span className="ml-auto text-[12px]" style={{ color: 'var(--text-soft)' }}>{formatTime(event.created_at, 'auto')}</span>
          </div>
          <span className="text-[13px]" style={{ color: 'var(--text-soft)' }}>
            {t('overview.auditEventMeta', { ecosystem: event.ecosystem, result: event.cache_result, status: event.status_code })}
          </span>
        </li>
      ))}
    </ol>
  )
}

interface RequestDetailsDialogProps {
  logId: number | null
  onClose: () => void
}

export default function RequestDetailsDialog({ logId, onClose }: RequestDetailsDialogProps) {
  const { t } = useTranslation()
  const [tab, setTab] = useState('basic')
  const query = useQuery({
    queryKey: ['admin', 'log-detail', logId],
    queryFn: ({ signal }) => adminApi.getLogDetail(logId as number, { signal }),
    enabled: logId !== null,
    retry: false,
    staleTime: 30_000,
  })

  const detail = query.data?.data
  const outcome = detail ? requestOutcome(detail) : 'unknown'
  const outcomeMeta = REQUEST_OUTCOME_META[outcome]

  let body: ReactNode
  if (query.isPending && !detail) {
    body = <div className="h-40 animate-pulse rounded-md" style={{ background: 'var(--bg-soft)' }} />
  } else if (query.isError && !detail) {
    body = (
      <QueryErrorState
        message={getApiError(query.error).status === 403 ? t('common.permissionDenied') : getApiError(query.error).message}
        onRetry={() => { void query.refetch() }}
      />
    )
  } else if (!detail) {
    body = <p className="text-[14px]" style={{ color: 'var(--text-soft)' }}>{t('overview.requestUnavailable')}</p>
  } else {
    body = (
      <TabsV2
        ariaLabel={t('overview.requestDetails')}
        value={tab}
        onValueChange={setTab}
        items={[
          { key: 'basic', label: t('overview.tabBasic'), content: <BasicTab detail={detail} t={t} /> },
          { key: 'chain', label: t('overview.tabChain'), content: <ChainTab detail={detail} t={t} /> },
          { key: 'logs', label: t('overview.tabLogs'), content: <LogTab detail={detail} t={t} /> },
        ]}
      />
    )
  }

  return (
    <ModalV2 open={logId !== null} onClose={onClose} title={t('overview.requestDetails')} width={880}>
      <div className="flex min-w-0 flex-col gap-4">
        {detail && (
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {isAdminEcosystem(detail.adapter_type) && <EcosystemIcon type={detail.adapter_type} size={18} decorative />}
            <span className="min-w-0 truncate font-mono text-[16px] font-semibold" style={{ color: 'var(--text)' }}>
              {detail.package_name || t('recentDownloads.unknownPackage')}
              {detail.version && <span style={{ color: 'var(--text-soft)' }}>@{detail.version}</span>}
            </span>
            <BadgeV2 variant="neutral">{detail.adapter_type || '—'}</BadgeV2>
            <BadgeV2 variant={outcomeMeta.variant}>{t(outcomeMeta.key)}</BadgeV2>
          </div>
        )}
        {body}
        <div className="flex flex-wrap justify-end gap-2">
          {detail?.upstream && (
            <Link
              to={getAdminRouteHref('upstreams')}
              onClick={onClose}
              className="dash-focus inline-flex min-h-9 items-center gap-1.5 rounded-md border px-3 text-[14px] font-medium no-underline"
              style={{ borderColor: 'var(--border)', color: 'var(--text)' }}
            >
              {t('overview.viewUpstreams')}
            </Link>
          )}
          <Link
            to={getAdminRouteHref('accessLogs')}
            onClick={onClose}
            className="dash-focus inline-flex min-h-9 items-center gap-1.5 rounded-md border px-3 text-[14px] font-medium no-underline"
            style={{ borderColor: 'var(--border)', color: 'var(--text)' }}
          >
            <Icon name="receipt_long" size="sm" />
            {t('overview.openLogs')}
          </Link>
        </div>
      </div>
    </ModalV2>
  )
}
