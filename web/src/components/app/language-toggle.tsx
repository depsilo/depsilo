import { Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/**
 * Portal header segments share one geometry: a 40px target that collapses to
 * its icon before it collapses out of the viewport.
 */
export const PORTAL_SEGMENT_CLASS =
  'inline-flex h-10 min-h-10 shrink-0 cursor-pointer items-center justify-center gap-1.5 bg-transparent px-2.5 text-label font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground max-[760px]:w-10 max-[760px]:px-0'

interface LangToggleProps {
  variant?: 'default' | 'portal' | 'admin'
}

export default function LangToggle({ variant = 'default' }: LangToggleProps) {
  const { i18n, t } = useTranslation()
  const isZh = i18n.language === 'zh'
  const portal = variant === 'portal'
  const admin = variant === 'admin'

  function toggle() {
    const next = isZh ? 'en' : 'zh'
    void i18n.changeLanguage(next)
  }

  return (
    <button
      type="button"
      onClick={toggle}
      data-language-toggle={portal ? 'portal' : admin ? 'admin' : 'default'}
      className={portal
        ? PORTAL_SEGMENT_CLASS
        : admin
          ? 'stripe-focus-ring inline-flex min-h-[40px] min-w-[40px] cursor-pointer items-center justify-center rounded-[6px] border-0 bg-transparent px-2 font-mono text-meta font-medium text-muted-foreground transition-[background,color,transform] duration-150 hover:bg-accent hover:text-foreground active:scale-[0.98]'
          : 'inline-flex items-center justify-center stripe-focus-ring'}
      aria-label={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      title={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      style={portal || admin ? undefined : {
        fontSize: 11,
        fontWeight: 500,
        minWidth: 40,
        minHeight: 40,
        padding: '8px',
        color: 'var(--muted-foreground)',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        fontFamily: 'var(--font-mono)',
        background: 'none',
        cursor: 'pointer',
      }}
    >
      {portal ? (
        <>
          <Languages className="max-[760px]:hidden icon icon-sm" aria-hidden="true" />
          <span className="max-[760px]:hidden">{isZh ? '中文' : 'EN'}</span>
          <span className="hidden max-[760px]:inline" aria-hidden="true">
            {isZh ? '中' : 'EN'}
          </span>
        </>
      ) : (isZh ? 'EN' : '中')}
    </button>
  )
}
