/**
 * The authenticated Admin shell: a persistent workspace rail, a quiet utility
 * bar, and one content outlet. Stage A keeps the shell's geometry (a 232px
 * rail, a 48px topbar) and rebuilds it on semantic tokens, so Admin and Portal
 * share one palette and differ only by layout.
 */
import { ChevronRight, LogOut, Menu, Settings } from 'lucide-react'
import { type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useLocation, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'

import Button from '@/components/app/button'
import Drawer from '@/components/app/drawer'
import Icon from '@/components/app/icon'
import type { LucideIcon } from 'lucide-react'
import LanguageToggle from '@/components/app/language-toggle'
import Logo from '@/components/app/logo'
import Notice from '@/components/app/notice'
import ThemeToggle from '@/components/app/theme-toggle'
import { usePrincipal } from '@/hooks/usePrincipal'
import { adminApi, authApi, statsApi } from '@/lib/api'
import type { PolicyStatus } from '@/lib/adminApi.types'
import { removeLocalStorage } from '@/lib/storage'
import { cn, formatTime, formatVersion } from '@/lib/utils'
import { adminNavigationGroups, resolveAdminRoute } from '../routes'

const RAIL_WIDTH = '232px'

interface NavSection {
  id: string
  label: string
  icon: LucideIcon
  href: string
  active: boolean
  current: boolean
}

interface SidebarContentProps {
  sections: NavSection[]
  surface: 'sidebar' | 'drawer'
  username?: string
  canWrite: boolean
  version?: string
  firstNavigationRef?: RefObject<HTMLAnchorElement | null>
  reserveCloseSpace?: boolean
  onNavigate?: () => void
  onLogout: () => void
}

function SidebarContent({
  sections,
  surface,
  username,
  canWrite,
  version,
  firstNavigationRef,
  reserveCloseSpace = false,
  onNavigate,
  onLogout,
}: SidebarContentProps) {
  const { t } = useTranslation()
  const preferredFocusSectionId = sections.find(section => section.active)?.id ?? sections[0]?.id

  return (
    <>
      <div
        data-admin-sidebar-header
        className={cn('flex shrink-0 items-center gap-2.5 py-5 pl-5', reserveCloseSpace ? 'pr-16' : 'pr-5')}
      >
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t('portal.backLink')}
          title={t('portal.backLink')}
          className="flex min-w-0 items-center gap-2.5 rounded-md text-foreground no-underline transition-opacity hover:opacity-75"
        >
          <Logo size={26} />
          <span className="text-title font-bold">Depsilo</span>
        </Link>
        <span
          className="ml-auto inline-flex min-w-16 max-w-[76px] items-center justify-center truncate rounded-sm border border-sidebar-border bg-sidebar-accent px-1.5 py-0.5 font-mono text-meta tabular-nums text-muted-foreground"
          title={version}
        >
          {formatVersion(version)}
        </span>
      </div>

      <nav
        data-admin-nav-scroll
        data-admin-nav-surface={surface}
        aria-label={t('nav.adminNavigation')}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain py-2"
      >
        <div className="space-y-2 px-2.5">
          {sections.map(section => (
            <div
              key={section.id}
              data-admin-nav-group={section.id}
              data-admin-nav-active={section.active ? 'true' : 'false'}
            >
              <div
                data-admin-workspace-row
                data-admin-workspace-current={section.active ? 'true' : undefined}
                className={cn(
                  'flex min-w-0 items-center rounded-md border border-transparent transition-colors hover:bg-sidebar-accent',
                  section.active && 'border-sidebar-border bg-sidebar-accent',
                )}
              >
                <Link
                  ref={section.id === preferredFocusSectionId ? firstNavigationRef : undefined}
                  to={section.href}
                  onClick={onNavigate}
                  aria-current={section.active ? (section.current ? 'page' : 'location') : undefined}
                  className={cn(
                    'flex min-h-10 min-w-0 flex-1 items-center gap-2.5 rounded-md px-2.5 py-2 text-body no-underline',
                    section.active
                      ? 'font-semibold text-sidebar-accent-foreground'
                      : 'font-medium text-muted-foreground hover:text-sidebar-accent-foreground',
                  )}
                >
                  <span
                    className={cn(
                      'flex size-7 shrink-0 items-center justify-center rounded-sm',
                      section.active ? 'bg-background text-foreground' : 'text-muted-foreground',
                    )}
                  >
                    <Icon icon={section.icon} size="sm" />
                  </span>
                  <span className="min-w-0 flex-1 truncate">{section.label}</span>
                </Link>
              </div>
            </div>
          ))}
        </div>
      </nav>

      <div
        data-admin-sidebar-footer
        className="shrink-0 border-t border-sidebar-border px-3 py-3"
      >
        <Link
          to="/admin/users"
          onClick={onNavigate}
          aria-label={t('nav.instanceManagement')}
          className="mb-2 flex min-h-10 items-center gap-2.5 rounded-md px-2 py-2 text-body font-medium text-muted-foreground no-underline transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        >
          <Settings className="icon icon-sm" aria-hidden="true" />
          <span>{t('nav.instanceManagement')}</span>
        </Link>
        <div className="group flex cursor-default items-center gap-2.5 rounded-md px-2 py-2 transition-colors hover:bg-sidebar-accent">
          <div className="flex size-8 shrink-0 items-center justify-center rounded-sm bg-primary text-body font-semibold text-primary-foreground">
            {username?.[0]?.toUpperCase() || 'A'}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-body leading-tight font-medium text-foreground">{username}</p>
            <p className="mt-0.5 text-meta leading-tight text-muted-foreground">
              {canWrite ? t('nav.admin') : t('nav.readonly')}
            </p>
          </div>
          {/* Keyboard users must see this control even though pointing devices
              only reveal it on row hover. */}
          <button
            type="button"
            onClick={onLogout}
            className="inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-sm bg-transparent p-1.5 text-muted-foreground opacity-100 transition-[opacity,color] hover:text-foreground focus-visible:opacity-100 lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100"
            aria-label={t('nav.logout')}
          >
            <LogOut className="icon icon-sm" aria-hidden="true" />
          </button>
        </div>
      </div>
    </>
  )
}

function formatPolicySnapshotAge(seconds: number, language: string): string {
  if (!Number.isFinite(seconds) || seconds < 0) return ''

  const age = Math.round(seconds)
  const locale = language.startsWith('zh') ? 'zh-CN' : 'en-US'
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'always' })
  if (age < 60) return relative.format(-age, 'second')
  const minutes = Math.round(age / 60)
  if (minutes < 60) return relative.format(-minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (hours < 24) return relative.format(-hours, 'hour')
  return relative.format(-Math.round(hours / 24), 'day')
}

interface PolicyStatusBannerProps {
  status?: PolicyStatus
  unavailable: boolean
  refreshing: boolean
  onRefresh: () => unknown
}

function PolicyStatusBanner({ status, unavailable, refreshing, onRefresh }: PolicyStatusBannerProps) {
  const { t, i18n } = useTranslation()
  // `degraded` also covers the no-last-known-good case. Only name the snapshot
  // message when the engine explicitly says an old snapshot is in use, so an
  // Operator is never told a snapshot exists when the first load never landed.
  const statusDegraded = status?.using_stale_snapshot === true
  const statusUnavailable = unavailable || (
    status !== undefined
    && status.status !== 'healthy'
    && status.status !== 'ready'
    && !statusDegraded
  )
  if (!statusUnavailable && !statusDegraded) return null

  const snapshotLoadedAt = status?.snapshot_loaded_at ?? status?.last_successful_refresh
  const refreshTime = snapshotLoadedAt
    ? (formatPolicySnapshotAge(status?.snapshot_age_seconds ?? Number.NaN, i18n.language)
      || formatTime(snapshotLoadedAt, 'relative', i18n.language))
    : null

  return (
    <div
      data-admin-policy-status-banner
      role="status"
      aria-live="polite"
      aria-atomic="true"
      className="mb-4"
    >
      <Notice tone="warning">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="font-semibold">
              {statusUnavailable ? t('policy.statusUnavailable') : t('policy.staleSnapshot')}
            </p>
            {!statusUnavailable && (
              <p className="mt-0.5 text-label">
                {refreshTime
                  ? t('policy.lastSuccessfulRefresh', { time: refreshTime })
                  : t('policy.neverRefreshed')}
              </p>
            )}
          </div>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            aria-busy={refreshing || undefined}
            disabled={refreshing}
            onClick={() => { void onRefresh() }}
          >
            {refreshing ? t('policy.refreshing') : t('policy.refresh')}
          </Button>
        </div>
      </Notice>
    </div>
  )
}

export default function AdminShellLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const firstMobileNavigationRef = useRef<HTMLAnchorElement>(null)
  const { principal, canWrite } = usePrincipal()
  const activeRoute = resolveAdminRoute(location.pathname)
  // Policy runtime state belongs to Overview and Security. Other workspaces must
  // not spend a request or reserve banner space for it.
  const policySurface = activeRoute?.navGroup === 'overview' || activeRoute?.navGroup === 'security'

  const { data: stats } = useQuery<{ service: { version: string; status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => (await statsApi.getStats({ signal })).data,
    refetchInterval: 30000,
    staleTime: 30000,
  })

  const policyStatusQuery = useQuery<PolicyStatus>({
    queryKey: ['admin', 'policy', 'status'],
    queryFn: async ({ signal }) => (await adminApi.getPolicyStatus({ signal })).data,
    enabled: policySurface,
    refetchInterval: 30000,
    staleTime: 30000,
    refetchOnWindowFocus: true,
    retry: false,
  })

  const sections: NavSection[] = adminNavigationGroups
    .filter(group => !group.hiddenFromSidebar)
    .map(group => ({
      id: group.id,
      label: t(group.titleKey),
      icon: group.icon,
      href: group.href,
      active: activeRoute?.navGroup === group.id,
      current: activeRoute?.href === group.href,
    }))
  const pageTitle = activeRoute ? t(activeRoute.titleKey) : t('notFound.title')
  const activeSection = sections.find(section => section.active)
  const showPageBreadcrumb = !activeSection || activeSection.label !== pageTitle
  const sidebarProps = {
    sections,
    username: principal?.username,
    canWrite,
    version: stats?.service?.version,
  }

  const handleLogout = async () => {
    try { await authApi.logout() } catch { /* logout stays local when the server is unreachable */ }
    removeLocalStorage('token')
    queryClient.clear()
    navigate('/admin/login', { replace: true })
  }

  return (
    <div data-admin-shell className="relative flex min-h-screen bg-background">
      <aside
        className="fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-sidebar-border bg-sidebar lg:flex"
        style={{ width: RAIL_WIDTH }}
      >
        <SidebarContent {...sidebarProps} surface="sidebar" onLogout={() => { void handleLogout() }} />
      </aside>

      <Drawer
        open={mobileNavOpen}
        onOpenChange={setMobileNavOpen}
        title={t('nav.adminNavigation')}
        initialFocus={firstMobileNavigationRef}
      >
        <div id="admin-mobile-navigation" className="flex h-full min-h-0 flex-col overflow-hidden bg-sidebar">
          <SidebarContent
            {...sidebarProps}
            surface="drawer"
            firstNavigationRef={firstMobileNavigationRef}
            reserveCloseSpace
            onNavigate={() => setMobileNavOpen(false)}
            onLogout={() => { void handleLogout() }}
          />
        </div>
      </Drawer>

      <div data-admin-main className="min-w-0 flex-1 bg-background lg:ml-[232px]">
        <header
          data-admin-topbar
          className="fixed inset-x-0 top-0 z-20 flex h-12 items-center gap-x-2.5 border-b border-border bg-background px-4 sm:px-6 lg:left-[232px] lg:px-8"
        >
          <button
            type="button"
            className="inline-flex size-10 shrink-0 items-center justify-center rounded-md bg-transparent text-muted-foreground transition-colors hover:bg-accent hover:text-foreground lg:hidden"
            onClick={() => setMobileNavOpen(true)}
            aria-label={t('nav.openNavigation')}
            aria-expanded={mobileNavOpen}
            aria-controls="admin-mobile-navigation"
          >
            <Menu className="icon icon-sm" aria-hidden="true" />
          </button>
          <div data-admin-breadcrumb className="min-w-0 flex-1">
            <div
              className={cn(
                'min-w-0 items-center gap-1.5 text-label font-medium',
                showPageBreadcrumb ? 'flex' : 'flex lg:hidden',
              )}
            >
              {activeSection && (
                <span className={cn('truncate', showPageBreadcrumb ? 'text-muted-foreground' : 'text-foreground')}>
                  {activeSection.label}
                </span>
              )}
              {showPageBreadcrumb && (
                <>
                  {activeSection && (
                    <span aria-hidden="true" className="text-muted-foreground">
                      <ChevronRight className="icon icon-sm" aria-hidden="true" />
                    </span>
                  )}
                  <span className="truncate text-foreground">{pageTitle}</span>
                </>
              )}
            </div>
          </div>
          <div data-admin-preferences className="flex shrink-0 items-center gap-1">
            <LanguageToggle variant="admin" />
            <ThemeToggle labeled variant="admin" />
          </div>
        </header>

        <main className="min-h-screen bg-background pt-16 pb-6">
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            {policySurface && (
              <PolicyStatusBanner
                status={policyStatusQuery.data}
                unavailable={policyStatusQuery.isError}
                refreshing={policyStatusQuery.isFetching}
                onRefresh={() => policyStatusQuery.refetch()}
              />
            )}
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
