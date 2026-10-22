import { describe, expect, it } from 'vitest'
import { tokenStatus } from '../src/lib/tokenStatus'
import type { AdminUser, APITokenSummary } from '../src/lib/adminApi.types'

const owner: AdminUser = { id: 1, username: 'admin', role: 'admin', enabled: true, created_at: '', last_login_at: null }
const token: APITokenSummary = { id: 1, user_id: 1, name: 'build', permissions: 'readwrite', expires_at: null, last_used_at: null, created_at: '' }

describe('effective API token status', () => {
  it('does not infer owner availability from a failed users read', () => {
    expect(tokenStatus(token, undefined, 100)).toBe('unknown')
    expect(tokenStatus(token, [], 100)).toBe('ownerMissing')
  })
  it('retains expiry independently of the owner query', () => {
    expect(tokenStatus({ ...token, expires_at: '2026-10-01T00:00:00Z' }, undefined, Date.parse('2026-10-22'))).toBe('expired')
  })
  it('bounds granted permissions by the account state', () => {
    expect(tokenStatus(token, [owner], 100)).toBe('active')
    expect(tokenStatus(token, [{ ...owner, enabled: false }], 100)).toBe('ownerDisabled')
    expect(tokenStatus(token, [{ ...owner, role: 'readonly' }], 100)).toBe('restricted')
    expect(tokenStatus({ ...token, permissions: 'readonly' }, [{ ...owner, role: 'readonly' }], 100)).toBe('active')
  })
})
