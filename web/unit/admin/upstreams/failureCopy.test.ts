import { describe, expect, it } from 'vitest'

import { failureActionKey, failureTitleKey, resolveFailureCode } from '../../../src/admin/upstreams/failureCopy'
import en from '../../../src/i18n/en'
import zh from '../../../src/i18n/zh'
import type { UpstreamFailure, UpstreamFailureCode } from '../../../src/lib/adminApi.types'

const locales = { en: en.translation, zh: zh.translation }

function copy(key: string): Record<string, unknown> {
  return Object.fromEntries(Object.entries(locales).map(([name, translation]) => [name, key.split('.').reduce<unknown>(
    (node, part) => (node as Record<string, unknown> | undefined)?.[part], translation)]))
}

function titleKey(failure: UpstreamFailure): string {
  return failureTitleKey(failure, resolveFailureCode(failure))
}

describe('upstream failure copy', () => {
  it('explains egress_proxy with its own copy instead of a generic network failure', () => {
    expect(resolveFailureCode({ code: 'egress_proxy', status: 502 })).toBe('egress_proxy')
    expect(resolveFailureCode({ code: 'egress_proxy' })).toBe('egress_proxy')
    // A code from a newer server still reads as a network failure.
    expect(resolveFailureCode({ code: 'egress_future' as UpstreamFailureCode })).toBe('network')
  })

  it('names the CONNECT status when the proxy answered, and the unreachable proxy when it did not', () => {
    expect(titleKey({ code: 'egress_proxy', status: 502 })).toBe('upstreams.failure.egress_proxy.title')
    expect(titleKey({ code: 'egress_proxy', status: 407 })).toBe('upstreams.failure.egress_proxy.title')
    expect(titleKey({ code: 'egress_proxy' })).toBe('upstreams.failure.egress_proxy.titleUnreachable')
  })

  // [egress] no_proxy keeps an upstream off upstream_proxy, never off its own
  // HTTP proxy, so the own proxy's remedy clears that proxy first; no_proxy
  // only matters for the default proxy that may then apply.
  it('gives an upstream\'s own proxy a remedy that clears that proxy first', () => {
    expect(failureActionKey('egress_proxy', 'upstream')).toBe('upstreams.failure.egress_proxy.actionOwnProxy')
    expect(failureActionKey('egress_proxy', 'default')).toBe('upstreams.failure.egress_proxy.action')
    expect(failureActionKey('egress_proxy')).toBe('upstreams.failure.egress_proxy.action')
    // Other codes keep their single remedy.
    expect(failureActionKey('connection_refused', 'upstream')).toBe('upstreams.failure.connection_refused.action')
    expect(failureActionKey('egress_fake_ip', 'default')).toBe('upstreams.failure.egress_fake_ip.action')
    const noProxyRemedy: Record<string, string> = { en: 'under [egress] no_proxy and restart', zh: '加入 [egress] no_proxy 后重启' }
    const directPromise: Record<string, string> = { en: 'so Depsilo connects to it directly', zh: 'Depsilo 就会直接连接它' }
    const clearProxy: Record<string, string> = { en: 'clear this upstream\'s HTTP proxy', zh: '清空这个上游的 HTTP 代理' }
    for (const [name, value] of Object.entries(copy('upstreams.failure.egress_proxy.action'))) {
      expect(value, `${name}: default proxy remedy`).toContain(noProxyRemedy[name])
      expect(value, `${name}: default proxy remedy`).toContain(directPromise[name])
    }
    for (const [name, value] of Object.entries(copy('upstreams.failure.egress_proxy.actionOwnProxy'))) {
      expect(value, `${name}: own proxy remedy`).toContain(clearProxy[name])
      expect(value, `${name}: own proxy remedy`).not.toContain(directPromise[name])
    }
  })

  // The keys are built from the code, so `make lint-i18n`, which reads only
  // literal t('...') calls, cannot report a missing one.
  it('has every egress_proxy key in both languages, the status only in the CONNECT title', () => {
    for (const key of ['title', 'titleUnreachable', 'action', 'actionOwnProxy']) {
      for (const [name, value] of Object.entries(copy(`upstreams.failure.egress_proxy.${key}`))) {
        expect(typeof value === 'string' && value !== '', `${name}: upstreams.failure.egress_proxy.${key}`).toBe(true)
        expect((value as string).includes('{{status}}'), `${name}: ${key} placeholder`).toBe(key === 'title')
      }
    }
  })
})
