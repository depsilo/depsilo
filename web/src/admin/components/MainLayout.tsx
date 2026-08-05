import { type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'

import BadgeV2 from '@/components/Badge'
import DrawerV2 from '@/components/Drawer'
import Icon from '@/components/Icon'
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
  icon: string
  pro?: boolean
}

interface NavSection {
  id: string
  label: string
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

function SidebarNavItem({
  item,
  linkRef,
  onNavigate,
}: {
  item: NavItem
  linkRef?: RefObject<HTMLAnchorElement | null>
  onNavigate?: () => void
}) {
  return (
    <NavLink
      ref={linkRef}
      to={item.to}
      end
      onClick={onNavigate}
      className="mx-2 flex items-center gap-2.5 rounded-[6px] px-3 py-2 text-[13px] no-underline transition-colors duration-150"
      style={({ isActive }) => ({
        color: isActive ? 'var(--brand-text)' : 'var(--text-soft)',
        background: isActive ? 'var(--brand-soft)' : 'transparent',
        fontWeight: isActive ? 600 : 500,
      })}
    >
      <Icon name={item.icon} size="sm" />
      <span className="flex-1">{item.label}</span>
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
  return (
    <>
      <div data-admin-sidebar-header className={`flex items-center gap-2.5 py-5 pl-5 ${reserveCloseSpace ? 'pr-16' : 'pr-5'}`}>
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t('portal.backLink')}
          title={t('portal.backLink')}
          className="stripe-focus-ring flex min-w-0 items-center gap-2.5 rounded-[6px] no-underline transition-opacity duration-150 hover:opacity-75"
        >
          <Logo size={26} />
          <span className="text-[15px] font-[700]" style={{ color: 'var(--text)' }}>depsilo</span>
        </Link>
        <span
          className="ml-auto inline-flex min-w-16 items-center justify-center rounded-[4px] border px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
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
        className="flex-1 overflow-y-auto py-2"
      >
        {sections.map((section, sectionIndex) => (
          <div key={section.id} data-admin-nav-group={section.id}>
            <p
              className={`${sectionIndex === 0 ? 'mt-2' : 'mt-4'} mb-1.5 px-5 font-mono text-[11px] font-[600] uppercase`}
              style={{ color: 'var(--text-subtle)' }}
            >
              {section.label}
            </p>
            {section.items.map((item, itemIndex) => (
              <SidebarNavItem
                key={item.to}
                item={item}
                linkRef={sectionIndex === 0 && itemIndex === 0 ? firstNavigationRef : undefined}
                onNavigate={onNavigate}
              />
            ))}
          </div>
        ))}
      </nav>

      <div data-admin-sidebar-footer className="px-3 py-3" style={{ borderTop: '0.5px solid var(--border)' }}>
        <div className="group flex cursor-default items-center gap-2.5 rounded-[6px] px-2 py-2 transition-colors duration-150 hover:bg-[var(--bg-hover)]">
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

  const sections: NavSection[] = adminNavigationGroups.map(group => ({
    id: group.id,
    label: t(group.titleKey),
    items: group.routes.map(route => ({
      label: t(route.titleKey),
      to: route.href,
      icon: route.icon,
      pro: route.pro,
    })),
  }))
  const activeRoute = resolveAdminRoute(location.pathname)
  const pageTitle = activeRoute ? t(activeRoute.titleKey) : t('notFound.title')
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
    <div data-admin-shell className="relative z-[1] flex min-h-screen" style={{ background: 'var(--admin-canvas)' }}>
      <aside
        className="fixed inset-y-0 left-0 z-30 hidden w-[220px] flex-col lg:flex"
        style={{ background: 'var(--bg-card)', borderRight: '0.5px solid var(--border)' }}
      >
        <SidebarContent {...sidebarProps} surface="sidebar" onLogout={() => { void handleLogout() }} />
      </aside>

      <DrawerV2
        open={mobileNavOpen}
        onOpenChange={setMobileNavOpen}
        title={t('nav.adminNavigation')}
        initialFocus={firstMobileNavigationRef}
      >
        <div id="admin-mobile-navigation" className="flex h-full flex-col">
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

      <div data-admin-main className="min-w-0 flex-1 lg:ml-[220px]" style={{ background: 'var(--admin-canvas)' }}>
        <header
          data-admin-topbar
          className="aurora-rim-bottom fixed top-0 right-0 left-0 z-20 flex h-12 items-center gap-x-2.5 px-4 sm:px-6 lg:left-[220px] lg:px-8"
          style={{ background: 'var(--admin-canvas)' }}
        >
          <button
            type="button"
            className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-[6px] bg-transparent text-[var(--text-soft)] transition-[background,color,transform] duration-150 hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:scale-[0.96] lg:hidden"
            onClick={() => setMobileNavOpen(true)}
            aria-label={t('nav.openNavigation')}
            aria-expanded={mobileNavOpen}
            aria-controls="admin-mobile-navigation"
          >
            <Icon name="menu" size="sm" />
          </button>
          <h1 className="min-w-0 flex-1 truncate text-[17px] font-[600]" style={{ color: 'var(--text)' }}>
            {pageTitle}
          </h1>
          <div className="flex shrink-0 items-center gap-1.5 sm:gap-2.5">
            <LangToggle />
            <ThemeToggle labeled />
          </div>
        </header>

        <main className="min-h-screen pt-16 pb-6 md:pt-20" style={{ background: 'var(--admin-canvas)' }}>
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
