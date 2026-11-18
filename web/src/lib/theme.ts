import { useEffect, useSyncExternalStore } from 'react'
import { readLocalStorage, writeLocalStorage } from './storage'

export type ThemePreference = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

// Keep in step with the pre-paint script in index.html.
const STORAGE_KEY = 'depsilo-theme'
const DEFAULT_THEME: ThemePreference = 'system'
const DEFAULT_RESOLVED_THEME: ResolvedTheme = 'light'

let memoryPreference: ThemePreference | undefined
const subscribers = new Set<() => void>()

function isThemePreference(value: string | null): value is ThemePreference {
  return value === 'light' || value === 'dark' || value === 'system'
}

function readThemePreference(): ThemePreference {
  if (memoryPreference) return memoryPreference

  const stored = readLocalStorage(STORAGE_KEY)
  if (isThemePreference(stored)) {
    memoryPreference = stored
    return stored
  }

  memoryPreference = DEFAULT_THEME
  return DEFAULT_THEME
}

function writeThemePreference(theme: ThemePreference) {
  writeLocalStorage(STORAGE_KEY, theme)
}

function resolveTheme(theme: ThemePreference): ResolvedTheme {
  if (theme !== 'system') return theme
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function resolvedThemeSnapshot(): ResolvedTheme {
  if (typeof document === 'undefined') return DEFAULT_RESOLVED_THEME
  const declared = document.documentElement.getAttribute('data-theme')
  if (declared === 'light' || declared === 'dark') return declared
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

function applyThemePreference(theme: ThemePreference) {
  const resolved = resolveTheme(theme)
  const root = document.documentElement
  root.classList.remove('light', 'dark')
  root.classList.add(resolved)
  root.setAttribute('data-theme', resolved)
  document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]')?.setAttribute('content', resolved)
}

function announceThemeChange() {
  subscribers.forEach(subscriber => subscriber())
}

function handleStorage(event: StorageEvent) {
  if (event.key !== STORAGE_KEY) return
  memoryPreference = undefined
  applyThemePreference(readThemePreference())
  announceThemeChange()
}

function subscribeToTheme(onStoreChange: () => void) {
  subscribers.add(onStoreChange)
  if (subscribers.size === 1) window.addEventListener('storage', handleStorage)
  return () => {
    subscribers.delete(onStoreChange)
    if (subscribers.size === 0) window.removeEventListener('storage', handleStorage)
  }
}

function setThemePreference(theme: ThemePreference) {
  memoryPreference = theme
  writeThemePreference(theme)
  applyThemePreference(theme)
  announceThemeChange()
}

export function useThemePreference(): readonly [ThemePreference, (theme: ThemePreference) => void] {
  const theme = useSyncExternalStore(subscribeToTheme, readThemePreference, () => DEFAULT_THEME)

  useEffect(() => {
    applyThemePreference(theme)
    announceThemeChange()

    if (theme !== 'system') return
    const media = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!media) return

    const handleSystemThemeChange = () => {
      applyThemePreference('system')
      announceThemeChange()
    }
    media.addEventListener('change', handleSystemThemeChange)
    return () => media.removeEventListener('change', handleSystemThemeChange)
  }, [theme])

  return [theme, setThemePreference] as const
}

export function useResolvedTheme(): ResolvedTheme {
  return useSyncExternalStore(subscribeToTheme, resolvedThemeSnapshot, () => DEFAULT_RESOLVED_THEME)
}
