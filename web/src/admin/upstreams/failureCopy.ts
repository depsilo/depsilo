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

/** A code this build has copy for; anything newer reads as a generic network failure. */
export function resolveFailureCode(failure: UpstreamFailure): UpstreamFailureCode {
  return knownCodes.includes(failure.code) ? failure.code : 'network'
}

export function failureTitleKey(failure: UpstreamFailure, code: UpstreamFailureCode): string {
  if (code === 'egress_reserved' && !failure.address) return 'upstreams.failure.egress_reserved.titleMixed'
  if (code === 'egress_private' && !failure.address) return 'upstreams.failure.egress_private.titleNoAddress'
  return `upstreams.failure.${code}.title`
}

/** What the server observed, as one sentence, for summaries outside the Upstreams page. */
export function useFailureSummary(): (failure: UpstreamFailure) => string {
  const { t } = useTranslation()
  return failure => t(failureTitleKey(failure, resolveFailureCode(failure)), {
    address: failure.address ?? '',
    range: failure.range ?? '',
    status: failure.status ?? '',
  })
}
