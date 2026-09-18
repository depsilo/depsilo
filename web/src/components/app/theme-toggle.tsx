import { Monitor, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useThemePreference, type ThemePreference } from '@/lib/theme'
import Icon from '@/components/app/icon'
import type { LucideIcon } from 'lucide-react'
// The Portal header owns one segment geometry; the language control declares it.
import { PORTAL_SEGMENT_CLASS } from '@/components/app/language-toggle'
import IconButtonControl from '@/components/app/icon-button-control'

const CYCLE: ThemePreference[] = ['system', 'light', 'dark']

const ICONS: Record<ThemePreference, LucideIcon> = {
  light: Sun,
  dark: Moon,
  system: Monitor,
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
        className={PORTAL_SEGMENT_CLASS}
      >
        <Icon icon={ICONS[theme]} size="sm" />
        <span className="max-[760px]:hidden">{LABELS[theme]}</span>
      </button>
    )
  }

  if (labeled) {
    const labeledClassName = variant === 'admin'
      ? 'stripe-focus-ring inline-flex h-[40px] min-w-[40px] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] border-0 bg-transparent px-2.5 text-muted-foreground transition-[background,color,transform] duration-150 hover:bg-accent hover:text-foreground active:scale-[0.98]'
      : 'stripe-focus-ring inline-flex h-[41px] min-w-[41px] shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-[6px] border border-input bg-card px-2.5 text-muted-foreground transition-[background,color,border-color,transform] duration-150 hover:bg-accent hover:text-foreground active:scale-[0.98]'
    return (
      <button
        type="button"
        data-theme-toggle="labeled"
        aria-label={visibleLabel}
        title={label}
        onClick={cycle}
        className={labeledClassName}
      >
        <Icon icon={ICONS[theme]} size="sm" />
        <span className="text-meta font-semibold sm:hidden">{LABELS[theme]}</span>
        <span className="hidden text-meta font-semibold sm:inline">
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
      className="border border-border bg-muted"
    />
  )
}
