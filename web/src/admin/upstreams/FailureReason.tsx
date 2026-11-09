import { useTranslation } from 'react-i18next'

import type { UpstreamFailure, UpstreamFailureCode } from '@/lib/adminApi.types'

const knownCodes: readonly UpstreamFailureCode[] = [
  'egress_fake_ip',
  'egress_shared_address',
  'egress_reserved',
  'egress_private',
  'dns',
  'timeout',
  'tls',
  'connection_refused',
  'http_status',
  'network',
]

function titleKey(failure: UpstreamFailure, code: UpstreamFailureCode): string {
  if (code === 'egress_reserved' && !failure.address) return 'upstreams.failure.egress_reserved.titleMixed'
  if (code === 'egress_private' && !failure.address) return 'upstreams.failure.egress_private.titleNoAddress'
  return `upstreams.failure.${code}.title`
}

/**
 * Operator guidance for a failed upstream check: what the server observed and
 * the configuration that resolves it. The explanation never contains a URL or
 * credential, so it is shown to every Admin role.
 */
export function FailureReason({ failure, short = false }: {
  failure: UpstreamFailure
  /** One line, for a fake-IP failure whose remedy a page-level notice already gives. */
  short?: boolean
}) {
  const { t } = useTranslation()
  const code = knownCodes.includes(failure.code) ? failure.code : 'network'
  const values = { address: failure.address ?? '', range: failure.range ?? '', status: failure.status ?? '' }
  if (short && code === 'egress_fake_ip') {
    return (
      <p data-upstream-failure={code} className="break-words text-[13px] leading-relaxed text-foreground">
        {t('upstreams.failure.egress_fake_ip.short', values)}
      </p>
    )
  }
  return (
    <div data-upstream-failure={code} className="space-y-1 text-[13px] leading-relaxed">
      <p className="break-words text-foreground">{t(titleKey(failure, code), values)}</p>
      <p className="break-words text-muted-foreground">{t(`upstreams.failure.${code}.action`)}</p>
    </div>
  )
}
