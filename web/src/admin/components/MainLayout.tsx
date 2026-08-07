/**
 * THESIS: Dependency Flowline organizes Admin around operational workspaces, not a flat inventory of pages.
 * OWN-WORLD: Instrument neutrals, precise keylines, signal green, compact task links, and one calm white or matte-dark canvas.
 * STORY: Operators confirm service health, investigate history, configure sources, govern risk, and maintain the system.
 * FIRST VIEWPORT: A 232px workspace rail frames a quiet utility bar and focused content; desktop destinations default open with independent disclosure controls.
 * FORM: Structure candidate 4, flowline plus attention staging, seed 543e896c.
 * FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, and DESIGN.md
 */
import { type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'

import BadgeV2 from '@/components/Badge'
import DrawerV2 from '@/components/Drawer'
import Icon from '@/components/Icon'
import IconButton from '@/components/IconButton'
import LangToggle from '@/components/LangToggle'
import Logo from '@/components/Logo'
import ThemeToggle from '@/components/ThemeToggle'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authApi, statsApi } from '@/lib/api'
import { formatVersion } from '@/lib/utils'
import { adminNavigationGroups, resolveAdminRoute } from '../routes'

interface NavItem {
  label: string
  to: string
  pro?: boolean
}

interface NavSection {
  id: string
  label: string
  icon: string
  href: string
  active: boolean
  current: boolean
  items: NavItem[]
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

function LocalNavItem({
  item,
  onNavigate,
}: {
  item: NavItem
  onNavigate?: () => void
}) {
  return (
    <NavLink
      to={item.to}
      end
      onClick={onNavigate}
      className="flex min-h-9 items-center rounded-[6px] px-3 py-1.5 text-[13px] no-underline transition-colors duration-150 hover:bg-[var(--admin-rail-hover)]"
      style={({ isActive }) => ({
        color: isActive ? 'var(--brand-text)' : 'var(--text-soft)',
        background: isActive ? 'var(--brand-soft)' : undefined,
        fontWeight: isActive ? 600 : 500,
      })}
    >
      <span className="min-w-0 flex-1 truncate">{item.label}</span>
      {item.pro && <BadgeV2 variant="pro">Pro</BadgeV2>}
    </NavLink>
  )
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
  const [expansionOverrides, setExpansionOverrides] = useState<Record<string, boolean>>({})
  const preferredFocusSectionId = sections.find(section => section.active)?.id ?? sections[0]?.id

  const isSectionExpanded = (section: NavSection) => (
    section.items.length > 1
    && (expansionOverrides[section.id] ?? (surface === 'sidebar' || section.active))
  )

  const toggleSection = (section: NavSection) => {
    const nextExpanded = !isSectionExpanded(section)
    setExpansionOverrides(current => ({ ...current, [section.id]: nextExpanded }))
  }

  return (
    <>
      <div data-admin-sidebar-header className={`flex shrink-0 items-center gap-2.5 py-5 pl-5 ${reserveCloseSpace ? 'pr-16' : 'pr-5'}`}>
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t('portal.backLink')}
          title={t('portal.backLink')}
          className="stripe-focus-ring flex min-w-0 items-center gap-2.5 rounded-[6px] no-underline transition-opacity duration-150 hover:opacity-75"
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
          className="ml-auto inline-flex min-w-16 max-w-[76px] items-center justify-center truncate whitespace-nowrap rounded-[4px] border px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
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
          {sections.map((section) => {
            const expanded = isSectionExpanded(section)
            const workspaceCurrent = section.current && section.items.length === 1
            const navigationId = `${surface}-${section.id}-navigation`
            return (
              <div
                key={section.id}
                data-admin-nav-group={section.id}
                data-admin-nav-active={section.active ? 'true' : 'false'}
                data-admin-nav-expanded={expanded ? 'true' : 'false'}
              >
                <div
                  data-admin-workspace-row
                  className="flex min-w-0 items-center rounded-[7px] transition-colors duration-150 hover:bg-[var(--admin-rail-hover)]"
                  style={{
                    background: workspaceCurrent ? 'var(--brand-soft)' : undefined,
                  }}
                >
                  <Link
                    ref={section.id === preferredFocusSectionId ? firstNavigationRef : undefined}
                    to={section.href}
                    onClick={onNavigate}
                    aria-current={section.current && section.items.length === 1 ? 'page' : undefined}
                    className="stripe-focus-ring flex min-h-[40px] min-w-0 flex-1 items-center gap-2.5 rounded-[7px] px-2.5 py-2 text-[13px] no-underline"
                    style={{
                      color: section.active ? 'var(--brand-text)' : 'var(--text-soft)',
                      fontWeight: section.active ? 650 : 550,
                    }}
                  >
                    <span
                      className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[6px]"
                      style={{
                        background: section.active
                          ? (workspaceCurrent ? 'var(--bg-card)' : 'var(--brand-soft)')
                          : 'transparent',
                        color: section.active ? 'var(--brand-text)' : 'var(--text-subtle)',
                      }}
                    >
                      <Icon name={section.icon} size="sm" />
                    </span>
                    <span className="min-w-0 flex-1 truncate">{section.label}</span>
                  </Link>

                  {section.items.length > 1 && (
                    <IconButton
                      data-admin-workspace-toggle={section.id}
                      icon={expanded ? 'expand_less' : 'chevron_right'}
                      label={t(expanded ? 'nav.collapseWorkspace' : 'nav.expandWorkspace', { workspace: section.label })}
                      aria-expanded={expanded}
                      aria-controls={navigationId}
                      onClick={() => toggleSection(section)}
                      className="hover:bg-[var(--admin-rail-hover)]"
                      style={{ color: section.active ? 'var(--brand-text)' : 'var(--text-subtle)' }}
                    />
                  )}
                </div>

                {expanded && (
                  <div
                    id={navigationId}
                    data-admin-local-navigation={section.id}
                    className="mt-1 ml-9 space-y-0.5"
                  >
                    {section.items.map(item => (
                      <LocalNavItem key={item.to} item={item} onNavigate={onNavigate} />
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </nav>

      <div data-admin-sidebar-footer className="shrink-0 px-3 py-3" style={{ borderTop: '0.5px solid var(--border)' }}>
        <div className="group flex cursor-default items-center gap-2.5 rounded-[6px] px-2 py-2 transition-colors duration-150 hover:bg-[var(--admin-rail-hover)]">
          <div
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[6px] text-[13px] font-[600]"
            style={{ background: 'var(--hit)', color: 'var(--on-hit)' }}
          >
            {username?.[0]?.toUpperCase() || 'A'}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] font-[500] leading-tight" style={{ color: 'var(--text)' }}>{username}</p>
            <p className="mt-0.5 text-[11px] leading-tight" style={{ color: 'var(--text-subtle)' }}>
              {canWrite ? t('nav.admin') : t('nav.readonly')}
            </p>
          </div>
          <button
            type="button"
            onClick={onLogout}
            className="stripe-focus-ring inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-[4px] bg-transparent p-1.5 text-[var(--text-soft)] opacity-100 transition-[opacity,color,transform] duration-150 hover:text-[var(--text)] focus-visible:opacity-100 active:scale-[0.96] lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100"
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

  const { data: stats } = useQuery<{ service: { version: string; status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => (await statsApi.getStats()).data,
    refetchInterval: 30000,
    staleTime: 30000,
  })

  const activeRoute = resolveAdminRoute(location.pathname)
  const sections: NavSection[] = adminNavigationGroups.map(group => ({
    id: group.id,
    label: t(group.titleKey),
    icon: group.icon,
    href: group.href,
    active: activeRoute?.navGroup === group.id,
    current: activeRoute?.href === group.href,
    items: group.routes.map(route => ({
      label: t(route.titleKey),
      to: route.href,
      pro: route.pro,
    })),
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
    try { await authApi.logout() } catch { /* logout remains local when the server is unavailable */ }
    localStorage.removeItem('token')
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
        <header
          data-admin-topbar
          className="fixed top-0 right-0 left-0 z-20 flex h-12 items-center gap-x-2.5 border-b border-[var(--border)] px-4 sm:px-6 lg:left-[232px] lg:px-8"
          style={{ background: 'var(--admin-canvas)' }}
        >
          <button
            type="button"
            className="inline-flex h-[40px] w-[40px] shrink-0 items-center justify-center rounded-[6px] bg-transparent text-[var(--text-soft)] transition-[background,color,transform] duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.96] lg:hidden"
            onClick={() => setMobileNavOpen(true)}
            aria-label={t('nav.openNavigation')}
            aria-expanded={mobileNavOpen}
            aria-controls="admin-mobile-navigation"
          >
            <Icon name="menu" size="sm" />
          </button>
          <div
            data-admin-breadcrumb
            aria-label={t('nav.adminNavigation')}
            className="min-w-0 flex-1"
          >
            <div
              className={`min-w-0 items-center gap-1.5 text-[12px] font-[550] ${showPageBreadcrumb ? 'flex' : 'flex lg:hidden'}`}
            >
              {activeSection && (
                <span className="truncate" style={{ color: showPageBreadcrumb ? 'var(--text-subtle)' : 'var(--text)' }}>
                  {activeSection.label}
                </span>
              )}
              {showPageBreadcrumb && (
                <>
                  {activeSection && (
                    <span aria-hidden="true" className="text-[var(--text-subtle)]">
                      <Icon name="chevron_right" size="sm" />
                    </span>
                  )}
                  <span className="truncate" style={{ color: 'var(--text)' }}>{pageTitle}</span>
                </>
              )}
            </div>
          </div>
          <div
            data-admin-preferences
            className="flex shrink-0 items-center gap-1"
          >
            <LangToggle variant="admin" />
            <ThemeToggle labeled variant="admin" />
          </div>
        </header>

        <main className="min-h-screen pt-16 pb-6" style={{ background: 'var(--admin-canvas)' }}>
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
