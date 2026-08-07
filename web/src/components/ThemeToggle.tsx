import { useTranslation } from 'react-i18next'
import { useThemePreference, type ThemePreference } from '@/lib/theme'
import Icon from './Icon'
import IconButtonControl from './IconButtonControl'

const CYCLE: ThemePreference[] = ['system', 'light', 'dark']

const ICONS: Record<ThemePreference, string> = {
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
      <button
        type="button"
        data-theme-toggle="portal"
        aria-label={label}
        title={label}
        onClick={cycle}
        className="portal-header-control portal-theme-button stripe-focus-ring"
      >
        <Icon name={ICONS[theme]} size="sm" />
        <span className="portal-theme-label">{LABELS[theme]}</span>
      </button>
    )
  }

  if (labeled) {
    const labeledClassName = variant === 'admin'
      ? 'stripe-focus-ring inline-flex h-[40px] min-w-[40px] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] border-0 bg-transparent px-2.5 text-[var(--text-soft)] transition-[background,color,transform] duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.98]'
      : 'stripe-focus-ring inline-flex h-[41px] min-w-[41px] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] border border-[var(--border-strong)] bg-[var(--bg-card)] px-2.5 text-[var(--text-soft)] transition-[background,color,border-color,transform] duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.98]'
    return (
      <button
        type="button"
        data-theme-toggle="labeled"
        aria-label={visibleLabel}
        title={label}
        onClick={cycle}
        className={labeledClassName}
      >
        <Icon name={ICONS[theme]} size="sm" />
        <span className="text-[11px] font-[600] sm:hidden">{LABELS[theme]}</span>
        <span className="hidden text-[11px] font-[600] sm:inline">
          {visibleLabel}
        </span>
      </button>
    )
  }

  return (
    <IconButtonControl
      data-theme-toggle="icon"
      icon={ICONS[theme]}
      label={label}
      title={label}
      onClick={cycle}
      style={{ border: '0.5px solid var(--border)', background: 'var(--bg-soft)' }}
    />
  )
}
