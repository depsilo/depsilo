import { describe, expect, it } from 'vitest'
import { ldapBrowserCredentialsAllowed } from '../../src/lib/ldapBrowserOrigin'

describe('browser transport for directory credentials', () => {
  it.each(['https://depsilo.example', 'https://depsilo.example:8443', 'http://localhost:5173', 'http://127.0.0.1:18081', 'http://127.42.1.2', 'http://[::1]:18081'])('accepts HTTPS or loopback: %s', origin => {
    expect(ldapBrowserCredentialsAllowed(origin)).toBe(true)
  })
  it.each(['http://depsilo.example', 'http://192.168.50.102:23333', 'http://localhost.example', 'http://127.0.0.1.example', 'http://[::ffff:127.0.0.1]', 'ftp://localhost', 'null', '', 'https://user:password@depsilo.example', 'https://depsilo.example/path', 'https://depsilo.example?unexpected=1'])('rejects insecure or malformed origins: %s', origin => {
    expect(ldapBrowserCredentialsAllowed(origin)).toBe(false)
  })
})
