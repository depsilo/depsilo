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
          ? 'stripe-focus-ring inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-sm border-0 bg-transparent px-2 font-mono text-meta font-medium text-muted-foreground transition-[background,color,transform] duration-150 hover:bg-accent hover:text-foreground active:scale-[0.98]'
          : 'inline-flex size-10 cursor-pointer items-center justify-center rounded-sm border border-border bg-transparent font-mono text-meta font-medium text-muted-foreground'}
      aria-label={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      title={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
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
