import { Routes, Route, Link, NavLink } from 'react-router'
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

interface PortalStats {
  service: { status: string; version: string }
  extra_indexes?: Array<{ kind?: string; path: string }>
}

// The endpoint is an action inside the service-information group. The full
// URL remains available to assistive technology and is always copied even
// when its visible label collapses at narrower widths.
function EndpointPill() {
  const { t } = useTranslation()
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const url = window.location.origin
  // Drop the protocol for visual density — the click-to-copy still
  // copies the full URL with scheme.
  const compact = url.replace(/^https?:\/\//, '')

  async function handleCopy() {
    if (await copyText(url)) {
      setCopyState('copied')
      setTimeout(() => setCopyState('idle'), 2000)
    } else {
      setCopyState('failed')
      setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  const copied = copyState === 'copied'

  return (
    <>
      <button
        type="button"
        onClick={handleCopy}
        className="portal-header-control portal-endpoint-pill hit-extend stripe-focus-ring"
        aria-label={t('portal.copyEndpointNamed', { endpoint: url })}
        title={url}
        data-copy-state={copyState}
      >
        <span className="portal-endpoint-label">{compact}</span>
        {/* Both icons stay in the DOM and cross-fade so copy success reads as
            a state change instead of a hard snap. */}
        <span className="portal-endpoint-icon" aria-hidden="true">
          <span
            style={{
              position: 'absolute',
              inset: 0,
              color: copyState === 'failed' ? 'var(--danger-text)' : 'var(--text-subtle)',
              opacity: copied ? 0 : 1,
              transform: copied ? 'scale(0.25)' : 'scale(1)',
              filter: copied ? 'blur(4px)' : 'blur(0)',
              transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
            }}
          >
            <Icon name={copyState === 'failed' ? 'warning' : 'content_copy'} size="sm" />
          </span>
          <span
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
            <Icon name="check" size="sm" />
          </span>
        </span>
      </button>
      <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {copyState === 'copied'
          ? t('portal.endpointCopied')
          : copyState === 'failed'
            ? t('portal.endpointCopyFailed')
            : ''}
      </span>
    </>
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

  const { data } = useQuery<PortalStats>({
    queryKey: ['stats-status'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const serviceStatus = data?.service?.status ?? 'unknown'
  const dotStatus = serviceStatus === 'healthy'
    ? 'healthy'
    : serviceStatus === 'degraded'
      ? 'degraded'
      : serviceStatus === 'failed'
        ? 'failed'
        : 'unknown'
  const statusLabel = dotStatus === 'healthy'
    ? t('portal.online')
    : dotStatus === 'degraded'
      ? t('degraded')
      : dotStatus === 'failed'
        ? t('portal.offline')
        : t('portal.statusUnknown')
  const pytorchIndexPath = data?.extra_indexes?.find(index => index.kind === 'pytorch')?.path

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
          <Link
            to="/"
            aria-label="Depsilo"
            className="portal-header-brand"
            style={{ display: 'flex', alignItems: 'center', gap: 8, textDecoration: 'none', flexShrink: 0 }}
          >
            <Logo size={28} />
            <span
              className="portal-brand-name"
              style={{
                fontFamily: 'var(--font-display)',
                fontSize: 15,
                fontWeight: 700,
                letterSpacing: '-0.025em',
                color: 'var(--text)',
              }}
            >
              Depsilo
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
          <nav
            aria-label={t('portal.navigation')}
            className="portal-header-nav"
            style={{ display: 'flex', alignItems: 'center', gap: 2 }}
          >
            <NavTab to="/" label={t('portal.quickStart')} />
            <NavTab to="/monitor" label={t('portal.monitor')} />
          </nav>

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* Information, preferences, and navigation intentionally use
              separate groups so equal geometry does not flatten semantics. */}
          <div className="portal-header-actions">
            <div className="portal-header-group portal-service-group" data-portal-control-group="service">
              <EndpointPill />
              <span
                className="portal-header-control portal-status-pill"
                role="status"
                data-status={dotStatus}
                aria-label={t('portal.serviceStatusNamed', { status: statusLabel })}
              >
                <span className="portal-status-dot" aria-hidden="true">
                  <StatusDot status={dotStatus} size={6} live={dotStatus === 'healthy'} />
                </span>
                <span className="portal-status-compact-icon" aria-hidden="true">
                  <Icon
                    name={
                      dotStatus === 'healthy'
                        ? 'check'
                        : dotStatus === 'degraded'
                          ? 'warning'
                          : dotStatus === 'failed'
                            ? 'close'
                            : 'help'
                    }
                    size="sm"
                  />
                </span>
                <span className="portal-status-label" aria-hidden="true">{statusLabel}</span>
              </span>
            </div>
            <div
              className="portal-header-group portal-tools-group"
              data-portal-control-group="preferences"
              role="group"
              aria-label={t('portal.displayPreferences')}
            >
              <LangToggle variant="portal" />
              <ThemeToggle variant="portal" />
            </div>
            <a
              href="/admin"
              className="portal-header-control portal-admin-link hit-extend stripe-focus-ring"
              aria-label={t('portal.adminPanel')}
              title={t('portal.adminPanel')}
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
          <Route index element={<QuickStart pytorchIndexPath={pytorchIndexPath} />} />
          <Route path="monitor" element={<MonitorV2 />} />
          <Route path="*" element={<RouteNotFound area="portal" />} />
        </Routes>
      </main>
    </div>
  )
}
