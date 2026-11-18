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

  // Depsilo dials fake-IP answers by default, so egress_fake_ip means that
  // nothing carried the connection, that [egress] allow_fake_ip is off, or
  // that the guard always refuses the answer, which mixes fake-IP and other
  // addresses or holds a unique-local IPv6 fake-IP address. The remedy turns
  // on the proxy software's TUN mode, sets the proxy port with the container
  // form, checks allow_fake_ip without undoing a deliberate choice, or moves
  // the IPv6 fake-IP range.
  it('explains egress_fake_ip with every cause the code records and every remedy', () => {
    const notCarried: Record<string, string> = { en: 'nothing carried', zh: '没有被接管' }
    const switchOff: Record<string, string> = { en: '[egress] allow_fake_ip is off', zh: '[egress] allow_fake_ip 已关闭' }
    const refused: Record<string, string> = { en: 'which Depsilo always refuses', zh: '这类应答 Depsilo 一律拒绝' }
    for (const key of ['upstreams.failure.egress_fake_ip.title', 'upstreams.failure.egress_fake_ip.shared', 'onboarding.upstreamFakeIP']) {
      for (const [name, value] of Object.entries(copy(key))) {
        expect(value, `${name}: ${key}`).toContain(notCarried[name])
        expect(value, `${name}: ${key}`).toContain(switchOff[name])
        expect(value, `${name}: ${key}`).toContain(refused[name])
      }
    }
    // The card line holds for every cause, so it names none.
    const notConnected: Record<string, string> = { en: 'not connected', zh: '未连接' }
    for (const [name, value] of Object.entries(copy('upstreams.failure.egress_fake_ip.short'))) {
      expect(value, `${name}: short`).toContain('{{address}}')
      expect(value, `${name}: short`).toContain(notConnected[name])
      expect(value, `${name}: short`).not.toContain(notCarried[name])
    }
    for (const [name, value] of Object.entries(copy('upstreams.failure.egress_fake_ip.title'))) {
      expect(value, `${name}: title`).toContain('{{address}}')
      expect(value, `${name}: title`).toContain('{{range}}')
    }
    const tunMode: Record<string, string> = { en: 'TUN mode', zh: 'TUN 模式' }
    const onPurpose: Record<string, string> = { en: 'turned off on purpose', zh: '特意关闭' }
    for (const [name, value] of Object.entries(copy('upstreams.failure.egress_fake_ip.action'))) {
      for (const part of [tunMode[name], 'upstream_proxy', 'DEPSILO_EGRESS_UPSTREAM_PROXY=http://host.docker.internal:7890',
        '--add-host=host.docker.internal:host-gateway', 'allow_fake_ip', 'DEPSILO_EGRESS_ALLOW_FAKE_IP', onPurpose[name], '2001:2::/48']) {
        expect(value, `${name}: action`).toContain(part)
      }
    }
  })
})
