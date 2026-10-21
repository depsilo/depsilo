import { Button } from '@/components/ui/button'
import { useTranslation } from 'react-i18next'
import Icon from './Icon'

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
    <Button variant={portal || admin ? 'ghost' : 'outline'}
      type="button"
      onClick={toggle}
      data-language-toggle={portal ? 'portal' : admin ? 'admin' : 'default'}
      className={portal
        ? 'portal-header-control portal-language-control'
        : 'min-h-[40px] min-w-[40px] px-2 font-mono text-[11px] font-medium text-muted-foreground'}
      aria-label={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}
      title={t(isZh ? 'language.switchToEnglish' : 'language.switchToChinese')}

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
    </Button>
  )
}
