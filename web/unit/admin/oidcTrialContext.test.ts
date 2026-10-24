import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearOIDCTrial, readOIDCTrial, rememberOIDCTrial } from '../../src/lib/oidcTrialContext'

afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
describe('trial recovery navigation', () => {
  it('stores only a validated source ID and timestamp and expires recovery hints', () => {
    const values = new Map<string, string>()
    vi.stubGlobal('window', { sessionStorage: { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value), removeItem: (key: string) => values.delete(key) } })
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-23T10:00:00Z'))
    rememberOIDCTrial('team')
    expect(readOIDCTrial()).toEqual({ sourceID: 'team', startedAt: Date.now() })
    expect(Object.keys(JSON.parse([...values.values()][0]))).toEqual(['sourceID', 'startedAt'])
    vi.advanceTimersByTime(10 * 60 * 1000)
    expect(readOIDCTrial()).toBeNull()
    expect(values.size).toBe(0)
    rememberOIDCTrial('//external.example')
    expect(values.size).toBe(0)
    rememberOIDCTrial('team'); clearOIDCTrial()
    expect(readOIDCTrial()).toBeNull()
  })
  it('treats malformed or blocked browser storage as no recovery context', () => {
    vi.stubGlobal('window', { sessionStorage: { getItem: () => '{broken', removeItem: () => {} } })
    expect(readOIDCTrial()).toBeNull()
    vi.stubGlobal('window', { get sessionStorage() { throw new Error('blocked') } })
    expect(() => rememberOIDCTrial('team')).not.toThrow()
    expect(readOIDCTrial()).toBeNull()
  })
})
