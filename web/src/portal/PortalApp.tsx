import { Routes, Route, useLocation, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import StatusDot from '@/components/StatusDot'
import QuickStart from '@/portal/pages/QuickStart'
import MonitorV2 from '@/portal/pages/Monitor'

const VERSION = 'v2.4.1'

interface NavTabProps {
  to: string
  label: string
  isActive: boolean
}

function NavTab({ to, label, isActive }: NavTabProps) {
  return (
    <Link
      to={to}
      style={{ textDecoration: 'none' }}
    >
      <button
        style={{
          position: 'relative',
          padding: '6px 10px',
          fontSize: 13,
          fontWeight: isActive ? 600 : 500,
          letterSpacing: isActive ? '-0.005em' : '0',
          color: isActive ? 'var(--text)' : 'var(--text-soft)',
          borderRadius: 6,
          background: 'none',
          border: 'none',
          cursor: 'pointer',
        }}
      >
        {label}
        {isActive && (
          <span
            style={{
              position: 'absolute',
              left: 10,
              right: 10,
              bottom: '-15px',
              height: '1.5px',
              background: 'var(--grad-brand)',
              borderRadius: 1,
            }}
          />
        )}
      </button>
    </Link>
  )
}

export default function PortalAppV2() {
  const { t } = useTranslation()
  const location = useLocation()

  const { data } = useQuery<{ service: { status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const serviceStatus = data?.service?.status ?? 'unknown'
  const dotStatus = serviceStatus === 'healthy' ? 'healthy' : serviceStatus === 'degraded' ? 'degraded' : 'failed'

  const isQuickStart = location.pathname === '/' || location.pathname === ''
  const isMonitor = location.pathname.startsWith('/monitor')

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-page)' }}>
      <header
        style={{
          position: 'sticky',
          top: 0,
          zIndex: 30,
          background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
          backdropFilter: 'saturate(180%) blur(8px)',
          WebkitBackdropFilter: 'saturate(180%) blur(8px)',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <div
          style={{
            height: 52,
            maxWidth: 1240,
            margin: '0 auto',
            padding: '0 28px',
            display: 'flex',
            alignItems: 'center',
            gap: 24,
          }}
        >
          {/* Logo area */}
          <Link to="/" style={{ display: 'flex', alignItems: 'center', gap: 8, textDecoration: 'none', flexShrink: 0 }}>
            <Logo size={28} />
            <span
              style={{
                fontSize: 15,
                fontWeight: 700,
                letterSpacing: '-0.025em',
                color: 'var(--text)',
              }}
            >
              depsilo
            </span>
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 10,
                color: 'var(--text-subtle)',
                padding: '1px 5px',
                border: '0.5px solid var(--border)',
                borderRadius: 4,
                marginLeft: 2,
              }}
            >
              {VERSION}
            </span>
          </Link>

          {/* Nav tabs */}
          <nav style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <NavTab to="/" label={t('portal.quickStart')} isActive={isQuickStart} />
            <NavTab to="/monitor" label={t('portal.monitor')} isActive={isMonitor} />
          </nav>

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* Right side controls */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
            {/* Status pill */}
            {data && (
              <span
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '3px 8px',
                  border: '0.5px solid var(--border)',
                  borderRadius: 20,
                  fontSize: 12,
                  color: 'var(--text-soft)',
                  fontWeight: 500,
                }}
              >
                <StatusDot status={dotStatus} size={6} live={dotStatus === 'healthy'} />
                {dotStatus === 'healthy' ? t('portal.online') : t('portal.offline')}
              </span>
            )}
            <LangToggle />
            <ThemeToggle />
          </div>
        </div>
      </header>

      <main style={{ maxWidth: 1240, margin: '0 auto', padding: '32px 28px' }}>
        <Routes>
          <Route index element={<QuickStart />} />
          <Route path="monitor" element={<MonitorV2 />} />
        </Routes>
      </main>
    </div>
  )
}
