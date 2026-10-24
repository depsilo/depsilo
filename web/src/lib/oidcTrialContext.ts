import { readSessionStorage, removeSessionStorage, writeSessionStorage } from './storage'

const key = 'depsilo:oidc-trial-navigation'
const maxAge = 10 * 60 * 1000
const sourcePattern = /^[a-z][a-z0-9-]{0,31}$/

/** Navigation only: never evidence of authentication, role, or trial success. */
export function rememberOIDCTrial(sourceID: string): void {
  if (sourcePattern.test(sourceID)) writeSessionStorage(key, JSON.stringify({ sourceID, startedAt: Date.now() }))
}

export function clearOIDCTrial(): void {
  removeSessionStorage(key)
}

export function readOIDCTrial(): { sourceID: string; startedAt: number } | null {
  try {
    const value: unknown = JSON.parse(readSessionStorage(key) ?? 'null')
    if (value && typeof value === 'object' && 'sourceID' in value && 'startedAt' in value &&
      typeof value.sourceID === 'string' && sourcePattern.test(value.sourceID) &&
      typeof value.startedAt === 'number' && Number.isFinite(value.startedAt) &&
      value.startedAt <= Date.now() && Date.now() - value.startedAt < maxAge) {
      return { sourceID: value.sourceID, startedAt: value.startedAt }
    }
  } catch { /* Storage is optional and can contain stale or malformed data. */ }
  clearOIDCTrial()
  return null
}
