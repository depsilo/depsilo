import { describe, expect, it } from 'vitest'
import { initialLanguage, uiLanguage } from '../src/lib/initialLanguage'

describe('initial interface language', () => {
  it('keeps a stored choice whatever the browser prefers', () => {
    expect(initialLanguage('en', 'zh-CN')).toBe('en')
    expect(initialLanguage('zh', 'en-US')).toBe('zh')
  })

  it('follows the browser language when nothing is stored', () => {
    expect(initialLanguage(null, 'zh-CN')).toBe('zh')
    expect(initialLanguage(null, 'zh-TW')).toBe('zh')
    expect(initialLanguage(null, 'zh')).toBe('zh')
    expect(initialLanguage(null, 'ZH-Hans-CN')).toBe('zh')
    expect(initialLanguage(null, 'en-US')).toBe('en')
    expect(initialLanguage(null, 'de-DE')).toBe('en')
  })

  it('ignores a stored value that is not a supported language', () => {
    expect(initialLanguage('fr', 'zh-CN')).toBe('zh')
    expect(initialLanguage('', 'en-GB')).toBe('en')
  })

  it('falls back to English when the environment reports no language', () => {
    // i18n/index.ts passes `globalThis.navigator?.language`, undefined without a navigator.
    expect(initialLanguage(null, undefined)).toBe('en')
    expect(initialLanguage(null, '')).toBe('en')
    expect(initialLanguage(null)).toBe('en')
    expect(initialLanguage('zh')).toBe('zh')
  })

  it('maps any language tag to one of the two interface languages', () => {
    expect(uiLanguage('zh')).toBe('zh')
    expect(uiLanguage('zh-CN')).toBe('zh')
    expect(uiLanguage('en')).toBe('en')
    expect(uiLanguage('en-US')).toBe('en')
    expect(uiLanguage(null)).toBe('en')
  })
})
