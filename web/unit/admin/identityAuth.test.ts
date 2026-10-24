import { describe, expect, it } from 'vitest'
import { canManageCredentials } from '../../src/lib/principalPermissions'
import type { Principal } from '../../src/lib/adminApi.types'
import { identityAuthorizationURL } from '../../src/lib/identityAuth'

const local: Principal = { id: 1, username: 'admin', enabled: true, role: 'admin', auth_method: 'jwt', token_permissions: null, can_write: true }

describe('credential-management authority', () => {
  it('preserves legacy local sessions and respects an explicit denial', () => {
    expect(canManageCredentials(undefined)).toBe(false)
    expect(canManageCredentials(local)).toBe(true)
    expect(canManageCredentials({ ...local, can_manage_credentials: false })).toBe(false)
    expect(canManageCredentials({ ...local, can_write: false })).toBe(false)
  })
  it.each(['oidc', 'ldap'] as const)('fails closed for %s accounts and sessions even without new authority fields', protocol => {
    expect(canManageCredentials({ ...local, permission_source: protocol })).toBe(false)
    expect(canManageCredentials({ ...local, permission_source: 'local', login_method: protocol })).toBe(false)
    expect(canManageCredentials({ ...local, permission_source: protocol, can_manage_credentials: true })).toBe(false)
  })
})

describe('provider navigation', () => {
  it('accepts HTTPS and the local test-provider URL while rejecting executable or credential-bearing URLs', () => {
    expect(identityAuthorizationURL('https://casdoor.example/login/oauth/authorize?state=a')).toContain('state=a')
    expect(identityAuthorizationURL('http://127.0.0.1:18000/login')).toBe('http://127.0.0.1:18000/login')
    for (const value of ['javascript:alert(1)', '//casdoor.example', 'https://user:pass@casdoor.example']) expect(() => identityAuthorizationURL(value)).toThrow()
  })
})
