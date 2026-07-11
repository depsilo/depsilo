import * as React from 'react'
import { useTranslation } from 'react-i18next'
import IconButton from './IconButton'

type Theme = 'light' | 'dark' | 'system'
// Storage key matches the Instrument brief. The legacy "theme" key
// is migrated on first read so users with the previous build keep
// their choice without a re-pick.
const STORAGE_KEY = 'depsilo-theme'
const LEGACY_STORAGE_KEY = 'theme'
const CYCLE: Theme[] = ['system', 'light', 'dark']

function readTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
    const legacy = localStorage.getItem(LEGACY_STORAGE_KEY)
    if (legacy === 'light' || legacy === 'dark' || legacy === 'system') {
      localStorage.setItem(STORAGE_KEY, legacy)
      return legacy
    }
  } catch {}
  // Instrument default = dark.
  return 'dark'
}

function writeTheme(t: Theme) {
  try { localStorage.setItem(STORAGE_KEY, t) } catch {}
}

function applyTheme(t: Theme) {
  const root = document.documentElement
  root.classList.remove('light', 'dark')
  // Resolve "system" to the OS preference up front; we set both the
  // class (for Tailwind's dark: variant) and the data-theme attribute
  // (for Instrument's tokens.css selectors) so CSS that targets either
  // form works without a media query subscription.
  let resolved: 'light' | 'dark'
  if (t === 'system') {
    resolved = window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  } else {
    resolved = t
  }
  root.classList.add(resolved)
  root.setAttribute('data-theme', resolved)
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setTheme] = React.useState<Theme>(readTheme)

  React.useEffect(() => {
    applyTheme(theme)
    writeTheme(theme)

    if (theme === 'system') {
      const mq = window.matchMedia('(prefers-color-scheme: dark)')
      const onChange = () => applyTheme('system')
      mq.addEventListener('change', onChange)
      return () => mq.removeEventListener('change', onChange)
    }
  }, [theme])

  return [theme, setTheme]
}

const ICONS: Record<Theme, string> = {
  light: 'light_mode',
  dark: 'dark_mode',
  system: 'computer',
}

export default function ThemeToggle() {
  const { t } = useTranslation()
  const [theme, setTheme] = useTheme()

  const LABELS: Record<Theme, string> = {
    light: t('theme.light'),
    dark: t('theme.dark'),
    system: t('theme.auto'),
  }

  function cycle() {
    const idx = CYCLE.indexOf(theme)
    setTheme(CYCLE[(idx + 1) % CYCLE.length])
  }

  return (
    <IconButton
      icon={ICONS[theme]}
      label={t('theme.changeNamed', { theme: LABELS[theme] })}
      onClick={cycle}
      style={{ border: '0.5px solid var(--border)', background: 'var(--bg-soft)' }}
    />
  )
}
