import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { CLIENT_CONNECTION_LANGUAGES, getClientConnection } from '../src/lib/clientConnection'
import { LANGUAGES, ecosystemLabelKey, groupLabelKey, managerHintKey } from '../src/lib/ecosystemData'
import en from '../src/i18n/en'
import zh from '../src/i18n/zh'

// The catalog builds or stores these keys as data, so `make lint-i18n`, which
// only reads literal t('...') calls, cannot report a missing one.
const locales = { en: en.translation, zh: zh.translation }

function missing(key: string): string[] {
  return Object.entries(locales).flatMap(([name, translation]) => {
    const value = key.split('.').reduce<unknown>(
      (node, part) => (node as Record<string, unknown> | undefined)?.[part], translation)
    return typeof value === 'string' && value !== '' ? [] : [`${name}: ${key}`]
  })
}

describe('catalog translation keys', () => {
  it('every group header, ecosystem subtitle and manager description exists in both languages', () => {
    const keys = LANGUAGES.flatMap(language => [
      groupLabelKey(language.group),
      ecosystemLabelKey(language.subtitleKey),
      ...language.managers.map(manager => managerHintKey(manager.id)),
    ])
    expect(keys.length).toBeGreaterThan(LANGUAGES.length)
    expect(keys.flatMap(missing)).toEqual([])
  })

  it('every key stored in the catalog exists in both languages', () => {
    const keys = LANGUAGES.flatMap(language => language.managers.flatMap(manager =>
      (manager.methods ?? []).map(method => method.label)))
    expect(keys.length).toBeGreaterThan(0)
    expect([...new Set(keys)].flatMap(missing)).toEqual([])
  })

  it('every note in the catalog exists in both languages', () => {
    const keys = LANGUAGES.flatMap(language => language.managers.flatMap(manager =>
      (manager.notes ?? []).map(note => note.text)))
    expect(keys.length).toBeGreaterThan(0)
    expect([...new Set(keys)].flatMap(missing)).toEqual([])
  })

  it('every key a client connection carries exists in both languages', () => {
    const keys = new Set<string>()
    for (const language of CLIENT_CONNECTION_LANGUAGES) {
      for (const manager of language.managers) {
        for (const pipScope of ['temporary', 'user', 'venv'] as const) {
          const connection = getClientConnection(language.id, manager.id, 'http://10.0.0.2:23333', { pipScope })
          if (!connection) throw new Error(`No connection for ${language.id}/${manager.id}`)
          keys.add(connection.scope.labelKey).add(connection.scope.hintKey).add(connection.test.hintKey)
        }
      }
    }
    expect([...keys].flatMap(missing)).toEqual([])
  })

  it('every password rule the setup page can report exists in both languages', () => {
    // SetupWizard builds `setup.password_<issue>` from the policy's answer;
    // the shared vectors name every issue the policy can give.
    const { vectors } = JSON.parse(readFileSync(fileURLToPath(
      new URL('../../internal/credential/testdata/password_vectors.json', import.meta.url)), 'utf8')) as {
      vectors: { username: string; password: string; reason: string }[]
    }
    const issues = new Set(['required', ...vectors.map(vector => vector.reason).filter(Boolean)])
    expect(issues.size).toBe(7)
    expect([...issues].map(issue => `setup.password_${issue}`).flatMap(missing)).toEqual([])
  })

  it('reports a key that no language defines', () => {
    expect(missing('quickstart.managerHints.noSuchManager')).toEqual([
      'en: quickstart.managerHints.noSuchManager', 'zh: quickstart.managerHints.noSuchManager',
    ])
  })
})
