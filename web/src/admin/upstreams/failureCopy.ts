import { useTranslation } from 'react-i18next'

import type { UpstreamFailure, UpstreamFailureCode, UpstreamProxySource } from '@/lib/adminApi.types'

const knownCodes: readonly UpstreamFailureCode[] = [
  'egress_fake_ip',
  'egress_shared_address',
  'egress_reserved',
  'egress_private',
  'egress_proxy',
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
  // Without a CONNECT status, Depsilo could not connect to the proxy at all.
  if (code === 'egress_proxy' && !failure.status) return 'upstreams.failure.egress_proxy.titleUnreachable'
  return `upstreams.failure.${code}.title`
}

/**
 * The configuration that resolves a failure. [egress] no_proxy never bypasses
 * an upstream's own HTTP proxy, so a failure at that proxy has its own remedy.
 */
export function failureActionKey(code: UpstreamFailureCode, proxySource?: UpstreamProxySource): string {
  if (code === 'egress_proxy' && proxySource === 'upstream') return 'upstreams.failure.egress_proxy.actionOwnProxy'
  return `upstreams.failure.${code}.action`
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
