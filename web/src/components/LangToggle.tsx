import { useTranslation } from 'react-i18next'

export default function LangToggle() {
  const { i18n, t } = useTranslation()
  const isZh = i18n.language === 'zh'

  function toggle() {
    const next = isZh ? 'en' : 'zh'
    i18n.changeLanguage(next)
    localStorage.setItem('lang', next)
  }

  return (
    <button
      type="button"
      onClick={toggle}
      className="inline-flex items-center justify-center stripe-focus-ring"
      aria-label={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      title={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      style={{
        fontSize: 11,
        fontWeight: 500,
        minWidth: 40,
        minHeight: 40,
        padding: '8px',
        color: 'var(--text-muted)',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        fontFamily: 'var(--font-mono)',
        background: 'none',
        cursor: 'pointer',
      }}
    >
      {isZh ? 'EN' : '中'}
    </button>
  )
}
