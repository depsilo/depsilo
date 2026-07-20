import { Routes, Route, Link, NavLink } from 'react-router-dom'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import { formatVersion } from '@/lib/utils'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import StatusDot from '@/components/StatusDot'
import Icon from '@/components/Icon'
import { lazyRoute } from '@/routing/lazyRoute'
import RouteNotFound from '@/routing/RouteNotFound'

const QuickStart = lazyRoute(() => import('@/portal/pages/QuickStart'))
const MonitorV2 = lazyRoute(() => import('@/portal/pages/Monitor'))

// EndpointPill — small monospace pill in the header so operators can
// copy the URL for sharing without exposing it as a giant hero element.
// Hidden on viewports under 720px to keep the topbar from wrapping.
function EndpointPill() {
  const [copied, setCopied] = useState(false)
  const url = window.location.origin
  // Drop the protocol for visual density — the click-to-copy still
  // copies the full URL with scheme.
  const compact = url.replace(/^https?:\/\//, '')

  async function handleCopy() {
    if (await copyText(url)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="active:scale-[0.96] portal-endpoint-pill hit-extend"
      title={url}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '3px 8px 3px 10px',
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        fontSize: 11.5,
        fontFamily: 'var(--font-mono)',
        color: 'var(--text-muted)',
        cursor: 'pointer',
        transition: 'background 120ms ease, color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
      }}
      onMouseEnter={e => {
        e.currentTarget.style.background = 'var(--bg-hover)'
        e.currentTarget.style.color = 'var(--text)'
      }}
      onMouseLeave={e => {
        e.currentTarget.style.background = 'var(--bg-soft)'
        e.currentTarget.style.color = 'var(--text-muted)'
      }}
    >
      <span style={{ letterSpacing: '-0.01em' }}>{compact}</span>
      {/* Both glyphs stay in the DOM and cross-fade (opacity + scale +
          blur) so the ⧉ → ✓ swap reads as a state change instead of a
          hard snap. */}
      <span style={{ position: 'relative', display: 'inline-flex', width: 11, height: 13, fontSize: 10 }}>
        <span
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            color: 'var(--text-subtle)',
            opacity: copied ? 0 : 1,
            transform: copied ? 'scale(0.25)' : 'scale(1)',
            filter: copied ? 'blur(4px)' : 'blur(0)',
            transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
          }}
        >
          ⧉
        </span>
        <span
          aria-hidden
          style={{
            position: 'absolute',
            inset: 0,
            color: 'var(--ok-text)',
            opacity: copied ? 1 : 0,
            transform: copied ? 'scale(1)' : 'scale(0.25)',
            filter: copied ? 'blur(0)' : 'blur(4px)',
            transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
          }}
        >
          ✓
        </span>
      </span>
    </button>
  )
}

interface NavTabProps {
  to: string
  label: string
}

function NavTab({ to, label }: NavTabProps) {
  return (
    <NavLink
      to={to}
      end
      className="hit-extend stripe-focus-ring"
      style={({ isActive }) => ({
        display: 'inline-flex',
        alignItems: 'center',
        textDecoration: 'none',
        position: 'relative',
        padding: '6px 10px',
        fontSize: 13,
        fontWeight: isActive ? 600 : 500,
        letterSpacing: isActive ? '-0.005em' : '0',
        color: isActive ? 'var(--text)' : 'var(--text-soft)',
        borderRadius: 6,
        whiteSpace: 'nowrap',
        transition: 'color 120ms ease',
      })}
      onMouseEnter={event => { event.currentTarget.style.color = 'var(--text)' }}
      onMouseLeave={event => {
        event.currentTarget.style.color = event.currentTarget.getAttribute('aria-current') === 'page'
          ? 'var(--text)'
          : 'var(--text-soft)'
      }}
    >
      {({ isActive }) => (
        <>
          {label}
          {isActive && (
            <span
              aria-hidden="true"
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
        </>
      )}
    </NavLink>
  )
}

export default function PortalAppV2() {
  const { t } = useTranslation()

  const { data } = useQuery<{ service: { status: string; version: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const serviceStatus = data?.service?.status ?? 'unknown'
  const dotStatus = serviceStatus === 'healthy' ? 'healthy' : serviceStatus === 'degraded' ? 'degraded' : 'failed'

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-page)' }}>
      <div className="page-wash" />
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
          className="portal-header-inner"
          style={{
            height: 52,
            // Tracks the main content width below so header content and
            // page content share edges on wide (2560px+) displays.
            maxWidth: 'clamp(1280px, 92vw, 1840px)',
            margin: '0 auto',
            padding: '0 clamp(12px, 2vw, 28px)',
            display: 'flex',
            alignItems: 'center',
            gap: 16,
          }}
        >
          <Link to="/" style={{ display: 'flex', alignItems: 'center', gap: 8, textDecoration: 'none', flexShrink: 0 }}>
            <Logo size={28} />
            <span
              className="portal-brand-name"
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
              className="portal-version"
              title={data?.service?.version}
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
              {formatVersion(data?.service?.version)}
            </span>
          </Link>

          {/* Nav tabs */}
          <nav aria-label={t('portal.navigation')} style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <NavTab to="/" label={t('portal.quickStart')} />
            <NavTab to="/monitor" label={t('portal.monitor')} />
          </nav>

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* Right side controls */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {/* Endpoint pill — copy-able URL, hidden on narrow viewports. */}
            <EndpointPill />
            {/* Status pill — tinted chip matching service health */}
            {data && (
              <span
                className="portal-status-pill"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '3px 8px',
                  borderRadius: 6,
                  fontSize: 11,
                  fontWeight: 500,
                  fontFamily: 'var(--font-mono)',
                  background:
                    dotStatus === 'healthy'
                      ? 'var(--ok-fill)'
                      : dotStatus === 'degraded'
                        ? 'var(--warn-fill)'
                        : 'var(--danger-fill)',
                  color:
                    dotStatus === 'healthy'
                      ? 'var(--ok-text)'
                      : dotStatus === 'degraded'
                        ? 'var(--warn-text)'
                        : 'var(--danger-text)',
                }}
              >
                <StatusDot status={dotStatus} size={6} live={dotStatus === 'healthy'} />
                {dotStatus === 'healthy' ? t('portal.online') : t('portal.offline')}
              </span>
            )}
            <LangToggle />
            <span className="portal-theme-control" style={{ display: 'inline-flex' }}>
              <ThemeToggle />
            </span>
            <a
              href="/admin"
              className="portal-admin-link hit-extend"
              aria-label={t('portal.adminPanel')}
              title={t('portal.adminPanel')}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '4px 10px',
                fontSize: 12,
                fontWeight: 500,
                color: 'var(--brand-text)',
                background: 'var(--brand-soft)',
                border: '0.5px solid var(--brand-border)',
                borderRadius: 6,
                textDecoration: 'none',
                whiteSpace: 'nowrap',
              }}
            >
              <Icon name="admin_panel_settings" size="sm" />
              <span className="portal-admin-label">{t('portal.adminPanel')}</span>
              <span className="portal-admin-mobile-label">{t('portal.adminShort')}</span>
            </a>
          </div>
        </div>
      </header>

      <main style={{ maxWidth: 'clamp(1280px, 92vw, 1840px)', margin: '0 auto', padding: 'clamp(22px, 2.4vw, 40px) clamp(16px, 2.1vw, 32px) 48px' }}>
        <Routes>
          <Route index element={<QuickStart />} />
          <Route path="monitor" element={<MonitorV2 />} />
          <Route path="*" element={<RouteNotFound area="portal" />} />
        </Routes>
      </main>
    </div>
  )
}
