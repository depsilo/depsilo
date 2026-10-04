/**
 * THESIS: Dependency Flowline organizes Admin around operational workspaces, not a flat inventory of pages.
 * OWN-WORLD: Instrument neutrals, precise keylines, signal green, compact task links, and one calm white or matte-dark canvas.
 * STORY: Operators confirm service health, investigate history, configure sources, govern risk, and maintain the administration.
 * FIRST VIEWPORT: A 232px workspace rail frames focused content; six workspace links lead to page-local tabs.
 * FORM: Structure candidate 4, flowline plus attention staging, seed 543e896c.
 * FINISH: unreviewed is unfinished; this build ends with the finish review and the verdict.
 */
import { type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useLocation, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'

import DrawerV2 from '@/components/Drawer'
import Icon, { type IconName } from '@/components/Icon'
import LangToggle from '@/components/LangToggle'
import Logo from '@/components/Logo'
import ThemeToggle from '@/components/ThemeToggle'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authApi, statsApi } from '@/lib/api'
import { removeLocalStorage } from '@/lib/storage'
import { formatVersion } from '@/lib/utils'
import { adminNavigationGroups, resolveAdminRoute } from '../routes'
import '../admin-shell.css'

interface NavSection {
  id: string
  label: string
  icon: IconName
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
      <div data-admin-sidebar-header className={`flex shrink-0 items-center gap-2.5 py-5 pl-5 ${reserveCloseSpace ? 'pr-[72px]' : 'pr-5'}`}>
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t('portal.backLink')}
          title={t('portal.backLink')}
          className="stripe-focus-ring flex min-w-0 items-center gap-2.5 rounded-md no-underline transition-opacity duration-150 hover:opacity-75"
        >
          <Logo size={26} />
          <span
            className="text-[15px] font-[700]"
            style={{ color: 'var(--text)', fontFamily: 'var(--font-display)' }}
          >
            Depsilo
          </span>
        </Link>
        <span
          className="ml-auto inline-flex min-w-16 max-w-[76px] items-center justify-center truncate whitespace-nowrap rounded-sm border px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
          title={version}
          style={{ background: 'var(--bg-hover)', color: 'var(--text-soft)', borderColor: 'var(--border)' }}
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
                className="flex min-w-0 items-center rounded-md transition-colors duration-150 hover:bg-[var(--admin-rail-hover)]"
                style={{
                  background: section.active ? 'var(--brand-soft)' : undefined,
                }}
              >
                <Link
                  ref={section.id === preferredFocusSectionId ? firstNavigationRef : undefined}
                  to={section.href}
                  onClick={onNavigate}
                  aria-current={section.active ? (section.current ? 'page' : 'location') : undefined}
                  className="stripe-focus-ring flex min-h-[40px] min-w-0 flex-1 items-center gap-2.5 rounded-md px-2.5 py-2 text-[13px] no-underline"
                  style={{
                    color: section.active ? 'var(--brand-text)' : 'var(--text-soft)',
                    fontWeight: section.active ? 650 : 550,
                  }}
                >
                  <span
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md"
                    style={{
                      background: section.active ? 'var(--bg-card)' : 'transparent',
                      color: section.active ? 'var(--brand-text)' : 'var(--text-subtle)',
                    }}
                  >
                    <Icon name={section.icon} size="sm" />
                  </span>
                  <span className="min-w-0 flex-1 truncate">{section.label}</span>
                </Link>
              </div>
            </div>
          ))}
        </div>
      </nav>

      <div data-admin-sidebar-footer className="shrink-0 px-3 py-3" style={{ borderTop: '1px solid var(--border)' }}>
        <div data-admin-preferences className="mb-1 flex items-center gap-1 px-1">
          <LangToggle variant="admin" />
          <ThemeToggle labeled variant="admin" />
        </div>
        <Link
          to="/admin/users"
          onClick={onNavigate}
          aria-label={t('nav.instanceManagement')}
          className="stripe-focus-ring mb-2 flex min-h-10 items-center gap-2.5 rounded-md px-2 py-2 text-[13px] font-[550] no-underline transition-colors hover:bg-[var(--admin-rail-hover)]"
          style={{ color: 'var(--text-soft)' }}
        >
          <Icon name="settings" size="sm" />
          <span>{t('nav.instanceManagement')}</span>
        </Link>
        <div className="group flex cursor-default items-center gap-2.5 rounded-md px-2 py-2 transition-colors duration-150 hover:bg-[var(--admin-rail-hover)]">
          <div
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[13px] font-[600]"
            style={{ background: 'var(--hit)', color: 'var(--on-hit)' }}
          >
            {username?.[0]?.toUpperCase() || 'A'}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] font-[500] leading-tight" style={{ color: 'var(--text)' }}>{username}</p>
            <p className="mt-0.5 text-[11px] leading-tight" style={{ color: 'var(--text-muted)' }}>
              {canWrite ? t('nav.admin') : t('nav.readonly')}
            </p>
          </div>
          <button
            type="button"
            onClick={onLogout}
            className="stripe-focus-ring inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-sm bg-transparent p-1.5 text-[var(--text-soft)] opacity-100 transition-[opacity,color,transform] duration-150 hover:text-[var(--text)] focus-visible:opacity-100 active:scale-[0.96] lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100"
            aria-label={t('nav.logout')}
          >
            <Icon name="logout" size="sm" />
          </button>
        </div>
      </div>
    </>
  )
}

export default function MainLayoutV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const firstMobileNavigationRef = useRef<HTMLAnchorElement>(null)
  const { principal, canWrite } = usePrincipal()
  const activeRoute = resolveAdminRoute(location.pathname)

  const { data: stats } = useQuery<{ service: { version: string; status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async ({ signal }) => (await statsApi.getStats({ signal })).data,
    refetchInterval: 30000,
    staleTime: 30000,
  })

  const sections: NavSection[] = adminNavigationGroups.filter(group => !group.hiddenFromSidebar).map(group => ({
    id: group.id,
    label: t(group.titleKey),
    icon: group.icon,
    href: group.href,
    active: activeRoute?.navGroup === group.id,
    current: activeRoute?.href === group.href,
  }))
  const sidebarProps = {
    sections,
    username: principal?.username,
    canWrite,
    version: stats?.service?.version,
  }

  const handleLogout = async () => {
    try { await authApi.logout() } catch { /* logout remains local when the server is unavailable */ }
    removeLocalStorage('token')
    queryClient.clear()
    navigate('/admin/login', { replace: true })
  }

  return (
    <div
      data-admin-shell
      data-admin-concept="dependency-flowline"
      className="relative z-[1] flex min-h-screen"
      style={{ background: 'var(--admin-canvas)' }}
    >
      <aside
        className="fixed inset-y-0 left-0 z-30 hidden w-[232px] flex-col lg:flex"
        style={{ background: 'var(--admin-rail)', borderRight: '1px solid var(--border-soft)' }}
      >
        <SidebarContent {...sidebarProps} surface="sidebar" onLogout={() => { void handleLogout() }} />
      </aside>

      <DrawerV2
        open={mobileNavOpen}
        onOpenChange={setMobileNavOpen}
        title={t('nav.adminNavigation')}
        initialFocus={firstMobileNavigationRef}
      >
        <div id="admin-mobile-navigation" className="flex h-full min-h-0 flex-col overflow-hidden">
          <SidebarContent
            {...sidebarProps}
            surface="drawer"
            firstNavigationRef={firstMobileNavigationRef}
            reserveCloseSpace
            onNavigate={() => setMobileNavOpen(false)}
            onLogout={() => { void handleLogout() }}
          />
        </div>
      </DrawerV2>

      <div data-admin-main className="min-w-0 flex-1 lg:ml-[232px]" style={{ background: 'var(--admin-canvas)' }}>
        <main className="min-h-screen pb-8 pt-4 lg:pt-6" style={{ background: 'var(--admin-canvas)' }}>
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            {/* The Admin shell has no utility bar. On narrow screens the
                workspace rail is off-canvas, so the drawer trigger sits at the
                top of the content instead of occupying a header row. */}
            <button
              type="button"
              data-admin-nav-trigger
              className="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-md bg-transparent text-[var(--text-soft)] transition-[background,color,transform] duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.96] lg:hidden"
              onClick={() => setMobileNavOpen(true)}
              aria-label={t('nav.openNavigation')}
              aria-expanded={mobileNavOpen}
              aria-controls="admin-mobile-navigation"
            >
              <Icon name="menu" size="sm" />
            </button>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
