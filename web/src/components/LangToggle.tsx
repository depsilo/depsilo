import { useTranslation } from 'react-i18next'

export default function LangToggle() {
  const { i18n } = useTranslation()
  const isZh = i18n.language === 'zh'

  function toggle() {
    const next = isZh ? 'en' : 'zh'
    i18n.changeLanguage(next)
    localStorage.setItem('lang', next)
  }

  return (
    <button
      onClick={toggle}
      className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors text-xs font-medium px-1.5 py-1 rounded-md hover:bg-surface-container"
      title={isZh ? 'Switch to English' : '切换到中文'}
    >
      {isZh ? 'EN' : '中'}
    </button>
  )
}
