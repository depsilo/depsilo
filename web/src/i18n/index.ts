import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import zh from './zh'
import en from './en'

const savedLang = localStorage.getItem('lang') === 'en' ? 'en' : 'zh'
const htmlLang = (language: string) => language.startsWith('zh') ? 'zh-CN' : 'en'

i18n.use(initReactI18next).init({
  resources: {
    zh,
    en,
  },
  lng: savedLang,
  fallbackLng: 'zh',
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
