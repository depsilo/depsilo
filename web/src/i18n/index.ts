import i18n, { type BackendModule } from 'i18next'
import { initReactI18next } from 'react-i18next'

const savedLang = localStorage.getItem('lang') === 'en' ? 'en' : 'zh'
const htmlLang = (language: string) => language.startsWith('zh') ? 'zh-CN' : 'en'

const languageResources = {
  zh: () => import('./zh'),
  en: () => import('./en'),
} as const

const lazyLanguageBackend: BackendModule = {
  type: 'backend',
  init() {},
  read(language, _namespace, callback) {
    const supportedLanguage = language.startsWith('en') ? 'en' : 'zh'
    languageResources[supportedLanguage]()
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
  localStorage.setItem('lang', language.startsWith('zh') ? 'zh' : 'en')
  document.documentElement.lang = htmlLang(language)
})

export default i18n
