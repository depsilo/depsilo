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
      title={isZh ? 'Switch to English' : '切换到中文'}
      style={{
        fontSize: 11,
        fontWeight: 500,
        padding: '4px 8px',
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
