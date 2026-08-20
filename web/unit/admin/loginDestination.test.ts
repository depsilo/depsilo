import { describe, expect, it } from 'vitest'

import { adminLoginURL, loginDestination, safeAdminDestination } from '../../src/lib/adminLoginDestination'

const origin = 'https://depsilo.example.com'

describe('Admin login destination', () => {
  it('accepts an internal onboarding next query', () => {
    expect(loginDestination(undefined, '?next=%2Fadmin%2Fconnect%3Fnew%3D1', origin))
      .toBe('/admin/connect?new=1')
  })

  it('prefers a safe router state deep link', () => {
    expect(loginDestination({ from: { pathname: '/admin/cache', search: '?q=six', hash: '#row' } }, '?next=/admin/connect', origin))
      .toBe('/admin/cache?q=six#row')
  })

  it('rejects external, protocol-relative, and recursive login targets', () => {
    expect(safeAdminDestination('https://evil.example/admin', origin)).toBeNull()
    expect(safeAdminDestination('//evil.example/admin', origin)).toBeNull()
    expect(safeAdminDestination('/admin/login?next=/admin/connect', origin)).toBeNull()
    expect(loginDestination(undefined, '?next=https://evil.example/admin', origin)).toBe('/admin')
  })

  it('re-enters a restarted cross-origin instance through its durable Admin gate', () => {
    expect(adminLoginURL('http://10.0.0.10:24444/'))
      .toBe('http://10.0.0.10:24444/admin/login?next=%2Fadmin')
  })
})
