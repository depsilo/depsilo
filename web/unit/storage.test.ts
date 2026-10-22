import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  readLocalStorage,
  readSessionStorage,
  removeLocalStorage,
  removeSessionStorage,
  writeLocalStorage,
  writeSessionStorage,
} from '../src/lib/storage'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('safe local storage', () => {
  it('clears only configuration drafts when the authentication token is removed', () => {
    const session: Record<string, unknown> = {
      'depsilo:settings-draft:https://instance.test:1': '{}',
      'unrelated-cursor': '12',
      removeItem: (key: string) => { delete session[key] },
    }
    const removeItem = vi.fn()
    vi.stubGlobal('window', { localStorage: { removeItem }, sessionStorage: session })
    expect(removeLocalStorage('lang')).toBe(true)
    expect(session['depsilo:settings-draft:https://instance.test:1']).toBe('{}')
    expect(removeLocalStorage('token')).toBe(true)
    expect(session['depsilo:settings-draft:https://instance.test:1']).toBeUndefined()
    expect(session['unrelated-cursor']).toBe('12')
    expect(removeItem).toHaveBeenCalledWith('token')
  })
  it('still logs out when session draft storage is inaccessible', () => {
    const removeItem = vi.fn()
    vi.stubGlobal('window', { localStorage: { removeItem }, get sessionStorage() { throw new Error('blocked') } })
    expect(removeLocalStorage('token')).toBe(true)
    expect(removeItem).toHaveBeenCalledWith('token')
  })
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
    vi.stubGlobal('window', { localStorage: storage, sessionStorage: storage })

    expect(writeLocalStorage('lang', 'en')).toBe(true)
    expect(readLocalStorage('lang')).toBe('en')
    expect(removeLocalStorage('lang')).toBe(true)
    expect(readLocalStorage('lang')).toBeNull()
    expect(writeSessionStorage('cursor', '12')).toBe(true)
    expect(readSessionStorage('cursor')).toBe('12')
    expect(removeSessionStorage('cursor')).toBe(true)
    expect(readSessionStorage('cursor')).toBeNull()
  })
})
