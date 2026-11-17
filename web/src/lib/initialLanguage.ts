export type UiLanguage = 'zh' | 'en'

/** The interface language a language tag selects: Chinese for any `zh` tag, English otherwise. */
export const uiLanguage = (tag?: string | null): UiLanguage => /^zh/i.test(tag ?? '') ? 'zh' : 'en'

/**
 * A stored choice always wins. Without one the interface follows the browser's
 * preferred language; a browser that reports none gets English.
 */
export const initialLanguage = (stored: string | null, preferred?: string | null): UiLanguage =>
  stored === 'zh' || stored === 'en' ? stored : uiLanguage(preferred)
