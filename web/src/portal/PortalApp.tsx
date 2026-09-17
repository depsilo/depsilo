import { Routes, Route, Link, NavLink } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import { copyText } from '@/lib/clipboard'
import { cn, formatVersion } from '@/lib/utils'
import Logo from '@/components/app/logo'
import LangToggle from '@/components/app/language-toggle'
import ThemeToggle from '@/components/app/theme-toggle'
import StatusDot from '@/components/app/status-dot'
import Icon from '@/components/app/icon'
import { lazyRoute } from '@/routing/lazyRoute'
import RouteNotFound from '@/routing/RouteNotFound'
import { useTransientState } from '@/hooks/useTransientFlag'
import { resolveServiceOrigin } from '@/lib/packageManagerConfig'

const QuickStart = lazyRoute(() => import('@/portal/pages/QuickStart'))
const MonitorV2 = lazyRoute(() => import('@/portal/pages/Monitor'))

interface PortalStats {
  service: { status: string; version: string }
  extra_indexes?: Array<{ kind?: string; path: string }>
}

/**
 * One control geometry for the whole header: every segment is 40px tall so the
 * touch target is real rather than implied by a decorative hit-area shim.
 */
const HEADER_CONTROL =
  'inline-flex h-10 min-h-10 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap transition-colors'

const HEADER_GROUP =
  'inline-flex h-10 shrink-0 items-center rounded-md border border-border bg-card ' +
  '[&>*+*]:border-l [&>*+*]:border-border [&>*:first-child]:rounded-l-md [&>*:last-child]:rounded-r-md ' +
  'max-[560px]:border-0'

/**
 * Status colour is a semantic role, not a decoration: it must stay readable
 * and it must never imply that an unknown result is a failure.
 */
const STATUS_TONE_CLASS =
  'data-[status=healthy]:text-success data-[status=degraded]:text-warning ' +
  'data-[status=failed]:text-destructive data-[status=unavailable]:text-destructive ' +
  'data-[status=unknown]:text-muted-foreground data-[status=loading]:text-muted-foreground'

const CONTENT_MAX_WIDTH = 'max-w-[clamp(1280px,92vw,1840px)]'

// The endpoint is an action inside the service-information group. The full
// URL remains available to assistive technology and is always copied even
// when its visible label collapses at narrower widths.
function EndpointPill() {
  const { t } = useTranslation()
  const [copyState, showCopyState] = useTransientState<'idle' | 'copied' | 'failed'>('idle')
  const url = resolveServiceOrigin()
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
        className={cn(
          HEADER_CONTROL,
          'cursor-pointer px-2.5 font-mono text-[11.5px] text-muted-foreground hover:bg-accent hover:text-foreground',
          'max-[960px]:w-10 max-[960px]:px-0 max-[560px]:hidden',
          copyState === 'failed' && 'text-destructive',
        )}
        aria-label={t('portal.copyEndpointNamed', { endpoint: url })}
        title={url}
        data-copy-state={copyState}
      >
        <span className="tabular-nums max-[960px]:hidden">{compact}</span>
        {/* Both icons stay in the DOM and cross-fade so copy success reads as
            a state change instead of a hard snap. */}
        <span className="relative inline-flex size-4" aria-hidden="true">
          <span
            className="absolute inset-0 inline-flex items-center justify-center"
            style={{
              color: copyState === 'failed' ? 'var(--destructive)' : 'var(--muted-foreground)',
              opacity: copied ? 0 : 1,
              transform: copied ? 'scale(0.25)' : 'scale(1)',
              filter: copied ? 'blur(4px)' : 'blur(0)',
              transition: 'opacity 200ms cubic-bezier(0.2, 0, 0, 1), transform 200ms cubic-bezier(0.2, 0, 0, 1), filter 200ms cubic-bezier(0.2, 0, 0, 1)',
            }}
          >
            <Icon name={copyState === 'failed' ? 'warning' : 'content_copy'} size="sm" />
          </span>
          <span
            className="absolute inset-0 inline-flex items-center justify-center"
            style={{
              color: 'var(--success)',
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
      className={({ isActive }) => cn(
        'relative inline-flex min-h-10 items-center rounded-md px-2.5 py-1.5 text-[13px] no-underline transition-colors',
        'max-[560px]:min-w-10 max-[560px]:justify-center max-[560px]:px-1',
        isActive
          ? 'font-semibold text-foreground'
          : 'font-medium text-muted-foreground hover:text-foreground',
      )}
    >
      {({ isActive }) => (
        <>
          <span className="max-[560px]:hidden">{label}</span>
          <span data-portal-nav-compact-label className="hidden max-[560px]:inline" aria-hidden="true">
            {compactLabel}
          </span>
          {isActive && (
            <span
              className="absolute -bottom-1.5 right-2.5 left-2.5 h-[1.5px] rounded-xs bg-primary max-[560px]:right-2 max-[560px]:left-2"
              aria-hidden="true"
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
    staleTime: 30_000,
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
      <span data-portal-status-dot className="inline-flex max-[560px]:hidden" aria-hidden="true">
        <StatusDot
          status={statusTone === 'loading' || statusTone === 'unavailable' ? 'unknown' : resolvedStatus}
          size={6}
          live={resolvedStatus === 'healthy' && !isError}
        />
      </span>
      <span data-portal-status-compact-icon className="hidden max-[560px]:inline-flex" aria-hidden="true">
        <Icon
          name={statusIcon}
          size="sm"
          className={statusTone === 'loading' ? 'animate-spin motion-reduce:animate-none' : ''}
        />
      </span>
      <span data-portal-status-label className="max-[560px]:hidden" aria-hidden="true">{statusLabel}</span>
    </>
  )

  return (
    <div className="min-h-screen bg-background">
      <header className="sticky top-0 z-30 border-b border-border bg-background/90 backdrop-blur-sm backdrop-saturate-150">
        {/* The inner track matches the main content width, so header controls
            and page content share edges on very wide displays. */}
        <div
          data-portal-header-inner
          className={cn(
            'mx-auto flex h-[52px] w-full items-center gap-4 px-[clamp(12px,2vw,28px)]',
            CONTENT_MAX_WIDTH,
            'max-[560px]:gap-1 max-[560px]:pl-[max(10px,env(safe-area-inset-left))] max-[560px]:pr-[max(10px,env(safe-area-inset-right))]',
            'max-[340px]:gap-0.5 max-[340px]:pl-[max(4px,env(safe-area-inset-left))] max-[340px]:pr-[max(4px,env(safe-area-inset-right))]',
          )}
        >
          <Link
            to="/"
            aria-label="Depsilo"
            className="flex min-h-10 shrink-0 items-center gap-2 no-underline"
          >
            <Logo size={28} />
            <span
              data-portal-brand-name
              className="text-[15px] font-bold text-foreground max-[380px]:hidden"
            >
              Depsilo
            </span>
            <span
              className="ml-0.5 rounded-sm border border-border px-1.5 py-px font-mono text-[10px] text-muted-foreground max-[760px]:hidden"
              title={data?.service?.version}
            >
              {formatVersion(data?.service?.version)}
            </span>
          </Link>

          <nav aria-label={t('portal.navigation')} className="flex min-w-0 items-center gap-0.5">
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

          <div className="flex-1" />

          {/* Information, preferences, and navigation intentionally use
              separate groups so equal geometry does not flatten semantics. */}
          <div className="flex min-w-0 shrink-0 items-center gap-2 max-[560px]:gap-1">
            <div
              className={cn(HEADER_GROUP, 'max-[560px]:w-10')}
              data-portal-control-group="service"
            >
              <EndpointPill />
              {isError ? (
                <button
                  type="button"
                  className={cn(
                    HEADER_CONTROL,
                    'cursor-pointer px-2.5 text-[12px] font-medium max-[560px]:w-10 max-[560px]:min-w-10 max-[560px]:px-0',
                    STATUS_TONE_CLASS,
                  )}
                  data-portal-status-pill
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
                  className={cn(
                    HEADER_CONTROL,
                    'px-2.5 text-[12px] font-medium max-[560px]:w-10 max-[560px]:min-w-10 max-[560px]:px-0',
                    STATUS_TONE_CLASS,
                  )}
                  role="status"
                  data-portal-status-pill
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
              className={cn(HEADER_GROUP, 'max-[560px]:w-20')}
              data-portal-control-group="preferences"
              role="group"
              aria-label={t('portal.displayPreferences')}
            >
              <LangToggle variant="portal" />
              <ThemeToggle variant="portal" />
            </div>
            <a
              href="/admin"
              className={cn(
                HEADER_CONTROL,
                'rounded-md border border-border bg-accent px-2.5 text-[12px] font-semibold text-primary no-underline hover:bg-muted',
                'max-[560px]:w-10 max-[560px]:px-0',
              )}
              data-portal-admin-link
              aria-label={t('portal.adminPanel')}
              title={t('portal.adminPanel')}
            >
              <Icon name="admin_panel_settings" size="sm" />
              <span data-portal-admin-label className="max-[560px]:hidden">{t('portal.adminPanel')}</span>
            </a>
          </div>
        </div>
      </header>

      <main
        className={cn(
          'mx-auto px-[clamp(16px,2.1vw,32px)] pt-[clamp(22px,2.4vw,40px)] pb-12',
          CONTENT_MAX_WIDTH,
        )}
      >
        <Routes>
          <Route index element={<QuickStart pytorchIndexPath={pytorchIndexPath} />} />
          <Route path="monitor" element={<MonitorV2 />} />
          <Route path="*" element={<RouteNotFound area="portal" />} />
        </Routes>
      </main>
    </div>
  )
}
