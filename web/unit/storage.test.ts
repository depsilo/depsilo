import { afterEach, describe, expect, it, vi } from 'vitest'
import { readLocalStorage, removeLocalStorage, writeLocalStorage } from '../src/lib/storage'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('safe local storage', () => {
  it('degrades when the browser storage object is unavailable', () => {
    vi.stubGlobal('window', undefined)

    expect(readLocalStorage('lang')).toBeNull()
    expect(writeLocalStorage('lang', 'en')).toBe(false)
    expect(removeLocalStorage('lang')).toBe(false)
  })

  it('contains storage access and operation failures', () => {
    const blockedStorage = {
      getItem: () => { throw new DOMException('blocked', 'SecurityError') },
      setItem: () => { throw new DOMException('blocked', 'SecurityError') },
      removeItem: () => { throw new DOMException('blocked', 'SecurityError') },
    } as unknown as Storage
    vi.stubGlobal('window', { localStorage: blockedStorage })

    expect(readLocalStorage('lang')).toBeNull()
    expect(writeLocalStorage('lang', 'en')).toBe(false)
    expect(removeLocalStorage('lang')).toBe(false)
  })

  it('returns stored values and successful mutation results', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
      removeItem: (key: string) => { values.delete(key) },
    } as unknown as Storage
    vi.stubGlobal('window', { localStorage: storage })

    expect(writeLocalStorage('lang', 'en')).toBe(true)
    expect(readLocalStorage('lang')).toBe('en')
    expect(removeLocalStorage('lang')).toBe(true)
    expect(readLocalStorage('lang')).toBeNull()
  })
})
