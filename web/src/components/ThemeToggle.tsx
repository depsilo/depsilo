import * as React from 'react'
import { useTranslation } from 'react-i18next'

type Theme = 'light' | 'dark' | 'system'
const STORAGE_KEY = 'theme'
const CYCLE: Theme[] = ['system', 'light', 'dark']

function readTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {}
  return 'system'
}

function writeTheme(t: Theme) {
  try { localStorage.setItem(STORAGE_KEY, t) } catch {}
}

function applyTheme(t: Theme) {
  const root = document.documentElement
  root.classList.remove('light', 'dark')
  if (t === 'light') root.classList.add('light')
  else if (t === 'dark') root.classList.add('dark')
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

const ICONS: Record<Theme, React.ReactNode> = {
  light: (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.2" />
      <g stroke="currentColor" strokeWidth="1.2" strokeLinecap="round">
        <path d="M8 1.5v1.5M8 13v1.5M14.5 8H13M3 8H1.5M12.6 3.4l-1 1M4.4 11.6l-1 1M12.6 12.6l-1-1M4.4 4.4l-1-1" />
      </g>
    </svg>
  ),
  dark: (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M13 9.5A5.5 5.5 0 016.5 3a5.5 5.5 0 106.5 6.5z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" />
    </svg>
  ),
  system: (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="2" y="3" width="12" height="8.5" rx="1.5" stroke="currentColor" strokeWidth="1.2" />
      <path d="M5.5 14h5M8 11.5V14" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  ),
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
    <button
      type="button"
      onClick={cycle}
      title={LABELS[theme]}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        height: 26,
        padding: '0 8px',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        background: 'var(--bg-soft)',
        color: 'var(--text-muted)',
        fontSize: 11,
        fontWeight: 500,
        cursor: 'pointer',
        transition: 'background 120ms ease, color 120ms ease',
      }}
    >
      {ICONS[theme]}
      <span>{LABELS[theme]}</span>
    </button>
  )
}
