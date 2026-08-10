import { Routes, Route, Link, NavLink } from 'react-router'
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
import { useTransientState } from '@/hooks/useTransientFlag'

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
  const [copyState, showCopyState] = useTransientState<'idle' | 'copied' | 'failed'>('idle')
  const url = window.location.origin
  // Drop the protocol for visual density — the click-to-copy still
  // copies the full URL with scheme.
  const compact = url.replace(/^https?:\/\//, '')

  async function handleCopy() {
    if (await copyText(url)) {
      showCopyState('copied', 2_000)
    } else {
      showCopyState('failed', 3_000)
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
  compactLabel: string
}

function NavTab({ to, label, compactLabel }: NavTabProps) {
  return (
    <NavLink
      to={to}
      end
      aria-label={label}
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
          <span className="portal-nav-label">{label}</span>
          <span className="portal-nav-compact-label" aria-hidden="true">{compactLabel}</span>
          {isActive && (
            <span
              className="portal-nav-active-indicator"
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

  const { data, isPending, isError, refetch } = useQuery<PortalStats>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => {
      const res = await statsApi.getStats({ signal })
      return res.data
    },
    refetchInterval: 30000,
    retry: false,
  })

  const serviceStatus = data?.service?.status
  const resolvedStatus = serviceStatus === 'healthy'
    ? 'healthy'
    : serviceStatus === 'degraded'
      ? 'degraded'
      : serviceStatus === 'failed'
        ? 'failed'
        : 'unknown'
  const statusTone = isPending
    ? 'loading'
    : isError && !data
      ? 'unavailable'
      : resolvedStatus
  const resolvedStatusLabel = resolvedStatus === 'healthy'
    ? t('portal.online')
    : resolvedStatus === 'degraded'
      ? t('degraded')
      : resolvedStatus === 'failed'
        ? t('portal.offline')
        : t('portal.statusUnknown')
  const statusLabel = statusTone === 'loading'
    ? t('portal.statusLoading')
    : statusTone === 'unavailable'
      ? t('portal.statusUnavailable')
      : resolvedStatusLabel
  const statusQueryState = isPending ? 'loading' : isError ? (data ? 'stale' : 'error') : 'success'
  const statusRetryLabel = data
    ? t('portal.retryServiceStatusWithFallback', { status: statusLabel })
    : t('portal.retryServiceStatus')
  const statusIcon = statusTone === 'loading'
    ? 'progress_activity'
    : statusTone === 'unavailable'
      ? 'refresh'
      : resolvedStatus === 'healthy'
        ? 'check'
        : resolvedStatus === 'degraded'
          ? 'warning'
          : resolvedStatus === 'failed'
            ? 'close'
            : 'help'
  const pytorchIndexPath = data?.extra_indexes?.find(index => index.kind === 'pytorch')?.path

  const statusContent = (
    <>
      <span className="portal-status-dot" aria-hidden="true">
        <StatusDot
          status={statusTone === 'loading' || statusTone === 'unavailable' ? 'unknown' : resolvedStatus}
          size={6}
          live={resolvedStatus === 'healthy' && !isError}
        />
      </span>
      <span className="portal-status-compact-icon" aria-hidden="true">
        <Icon
          name={statusIcon}
          size="sm"
          className={statusTone === 'loading' ? 'animate-spin motion-reduce:animate-none' : ''}
        />
      </span>
      <span className="portal-status-label" aria-hidden="true">{statusLabel}</span>
    </>
  )

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
            <NavTab
              to="/"
              label={t('portal.quickStart')}
              compactLabel={t('portal.quickStartShort')}
            />
            <NavTab
              to="/monitor"
              label={t('portal.monitor')}
              compactLabel={t('portal.monitorShort')}
            />
          </nav>

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* Information, preferences, and navigation intentionally use
              separate groups so equal geometry does not flatten semantics. */}
          <div className="portal-header-actions">
            <div className="portal-header-group portal-service-group" data-portal-control-group="service">
              <EndpointPill />
              {isError ? (
                <button
                  type="button"
                  className="portal-header-control portal-status-pill stripe-focus-ring"
                  data-status={statusTone}
                  data-query-state={statusQueryState}
                  aria-label={statusRetryLabel}
                  title={statusRetryLabel}
                  onClick={() => { void refetch() }}
                >
                  {statusContent}
                </button>
              ) : (
                <span
                  className="portal-header-control portal-status-pill"
                  role="status"
                  data-status={statusTone}
                  data-query-state={statusQueryState}
                  aria-label={t('portal.serviceStatusNamed', { status: statusLabel })}
                >
                  {statusContent}
                </span>
              )}
              {isError && (
                <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
                  {statusRetryLabel}
                </span>
              )}
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
