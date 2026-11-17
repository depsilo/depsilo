import i18n, { type BackendModule } from 'i18next'
import { initReactI18next } from 'react-i18next'
import { initialLanguage, uiLanguage } from '@/lib/initialLanguage'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'

// `navigator` is absent outside a browser.
const savedLang = initialLanguage(readLocalStorage('lang'), globalThis.navigator?.language)
const htmlLang = (language: string) => language.startsWith('zh') ? 'zh-CN' : 'en'

const languageResources = {
  zh: () => import('./zh'),
  en: () => import('./en'),
} as const

const lazyLanguageBackend: BackendModule = {
  type: 'backend',
  init() {},
  read(language, _namespace, callback) {
    languageResources[uiLanguage(language)]()
      .then(({ default: resource }) => callback(null, resource.translation))
      .catch((error: unknown) => {
        callback(error instanceof Error ? error : new Error('failed to load translations'), null)
      })
  },
}

await i18n.use(lazyLanguageBackend).use(initReactI18next).init({
  lng: savedLang,
  fallbackLng: false,
  supportedLngs: ['zh', 'en'],
  load: 'languageOnly',
  interpolation: {
    escapeValue: false,
  },
})

document.documentElement.lang = htmlLang(i18n.resolvedLanguage || savedLang)
i18n.on('languageChanged', language => {
  writeLocalStorage('lang', uiLanguage(language))
  document.documentElement.lang = htmlLang(language)
})

export default i18n
