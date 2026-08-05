import { useTranslation } from 'react-i18next'
import Icon from './Icon'

interface LangToggleProps {
  variant?: 'default' | 'portal'
}

export default function LangToggle({ variant = 'default' }: LangToggleProps) {
  const { i18n, t } = useTranslation()
  const isZh = i18n.language === 'zh'
  const portal = variant === 'portal'

  function toggle() {
    const next = isZh ? 'en' : 'zh'
    i18n.changeLanguage(next)
    localStorage.setItem('lang', next)
  }

  return (
    <button
      type="button"
      onClick={toggle}
      data-language-toggle={portal ? 'portal' : 'default'}
      className={portal
        ? 'portal-header-control portal-language-control stripe-focus-ring'
        : 'inline-flex items-center justify-center stripe-focus-ring'}
      aria-label={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      title={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      style={portal ? undefined : {
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
      {portal ? (
        <>
          <Icon name="language" size="sm" />
          <span className="portal-language-label">{isZh ? '中文' : 'EN'}</span>
          <span className="portal-language-compact-label" aria-hidden="true">
            {isZh ? '中' : 'EN'}
          </span>
        </>
      ) : (isZh ? 'EN' : '中')}
    </button>
  )
}
