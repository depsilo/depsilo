import { Button } from '@/components/ui/button'
import { useTranslation } from 'react-i18next'
import { useThemePreference, type ThemePreference } from '@/lib/theme'
import Icon, { type IconName } from './Icon'

const CYCLE: ThemePreference[] = ['system', 'light', 'dark']

const ICONS: Record<ThemePreference, IconName> = {
  light: 'light_mode',
  dark: 'dark_mode',
  system: 'computer',
}

interface ThemeToggleProps {
  labeled?: boolean
  variant?: 'default' | 'portal' | 'admin'
}

export default function ThemeToggle({ labeled = false, variant = 'default' }: ThemeToggleProps) {
  const { t } = useTranslation()
  const [theme, setTheme] = useThemePreference()

  const LABELS: Record<ThemePreference, string> = {
    light: t('theme.light'),
    dark: t('theme.dark'),
    system: t('theme.auto'),
  }

  function cycle() {
    const idx = CYCLE.indexOf(theme)
    setTheme(CYCLE[(idx + 1) % CYCLE.length])
  }

  const label = t('theme.changeNamed', { theme: LABELS[theme] })
  const visibleLabel = t('theme.controlNamed', { theme: LABELS[theme] })

  if (variant === 'portal') {
    return (
      <Button variant="ghost"
        type="button"
        data-theme-toggle="portal"
        aria-label={label}
        title={label}
        onClick={cycle}
        className="portal-header-control portal-theme-button"
      >
        <Icon name={ICONS[theme]} size="sm" />
        <span className="portal-theme-label">{LABELS[theme]}</span>
      </Button>
    )
  }

  if (labeled) {
    return (
      <Button variant={variant === 'admin' ? 'ghost' : 'outline'}
        type="button"
        data-theme-toggle="labeled"
        aria-label={visibleLabel}
        title={label}
        onClick={cycle}
        className="h-[40px] min-w-[40px] gap-1.5 px-2.5 text-muted-foreground"
      >
        <Icon name={ICONS[theme]} size="sm" />
        <span className="text-[11px] font-[600] sm:hidden">{LABELS[theme]}</span>
        <span className="hidden text-[11px] font-[600] sm:inline">
          {visibleLabel}
        </span>
      </Button>
    )
  }

  return (
    <Button type="button" data-theme-toggle="icon" title={label} onClick={cycle} variant="outline" size="icon" data-icon-button aria-label={label} className="text-muted-foreground">
      <Icon name={ICONS[theme]} size="sm" />
    </Button>
  )
}
