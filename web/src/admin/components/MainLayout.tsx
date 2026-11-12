import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
/**
 * THESIS: Dependency Flowline organizes Admin around operational workspaces, not a flat inventory of pages.
 * OWN-WORLD: Instrument neutrals, precise keylines, signal green, compact task links, and one calm white or matte-dark canvas.
 * STORY: Operators confirm service health, investigate history, configure sources, govern risk, and maintain the administration.
 * FIRST VIEWPORT: A 232px workspace rail frames focused content; six workspace links lead to page-local tabs.
 *   The rail collapses to a 60px icon strip on request, and the choice persists for this browser.
 * FORM: Structure candidate 4, flowline plus attention staging, seed 543e896c.
 * FINISH: unreviewed is unfinished; this build ends with the finish review and the verdict.
 */
import { type ReactElement, type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useLocation, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'


import Icon, { type IconName } from '@/components/Icon'
import LangToggle from '@/components/LangToggle'
import Logo from '@/components/Logo'
import ThemeToggle from '@/components/ThemeToggle'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authApi, statsApi } from '@/lib/api'
import { readLocalStorage, removeLocalStorage, writeLocalStorage } from '@/lib/storage'
import { adminNavigationGroups, getAdminRouteHref, resolveAdminRoute } from '../routes'
import SidebarVersion from './SidebarVersion'
import '../admin-shell.css'

/** Whether the desktop rail is collapsed to icons. A per-browser preference. */
const RAIL_STORAGE_KEY = 'depsilo-admin-rail'

/**
 * In the collapsed rail a control shows only its icon, so its name moves to a
 * tooltip. The tree is the same in both states (the tooltip is only disabled),
 * so toggling the rail never remounts the control that has keyboard focus.
 */
function RailTooltip({ label, enabled, children }: { label: string; enabled: boolean; children: ReactElement }) {
  return (
    <Tooltip disabled={!enabled}>
      <TooltipTrigger render={children} />
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  )
}

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
  isAdmin: boolean
  version?: string
  firstNavigationRef?: RefObject<HTMLAnchorElement | null>
  reserveCloseSpace?: boolean
  /** Desktop rail only: show icons without labels. */
  collapsed?: boolean
  onToggleCollapsed?: () => void
  onNavigate?: () => void
  onLogout: () => void
}

function SidebarContent({
  sections,
  surface,
  username,
  isAdmin,
  version,
  firstNavigationRef,
  reserveCloseSpace = false,
  collapsed = false,
  onToggleCollapsed,
  onNavigate,
  onLogout,
}: SidebarContentProps) {
  const { t } = useTranslation()
  const preferredFocusSectionId = sections.find(section => section.active)?.id ?? sections[0]?.id

  return (
    <>
      <div data-admin-sidebar-header className={`flex shrink-0 items-center gap-2.5 py-5 ${collapsed ? 'justify-center px-0' : `pl-5 ${reserveCloseSpace ? 'pr-[72px]' : 'pr-5'}`}`}>
        <Link
          data-admin-brand-link
          to="/"
          onClick={onNavigate}
          aria-label={t('portal.backLink')}
          title={t('portal.backLink')}
          className="stripe-focus-ring flex min-w-0 items-center gap-2.5 rounded-md no-underline transition-opacity duration-150 hover:opacity-75"
        >
          <Logo size={26} />
          {!collapsed && (
            <span
              className="text-[15px] font-[700]"
              style={{ color: 'var(--text)', fontFamily: 'var(--font-display)' }}
            >
              Depsilo
            </span>
          )}
        </Link>
        {!collapsed && <SidebarVersion version={version} />}
      </div>

      <nav
        data-admin-nav-scroll
        data-admin-nav-surface={surface}
        aria-label={t('nav.adminNavigation')}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain py-2"
      >
        <div className={`space-y-2 ${collapsed ? 'px-2' : 'px-2.5'}`}>
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
                <RailTooltip label={section.label} enabled={collapsed}>
                  <Link
                    ref={section.id === preferredFocusSectionId ? firstNavigationRef : undefined}
                    to={section.href}
                    onClick={onNavigate}
                    aria-current={section.active ? (section.current ? 'page' : 'location') : undefined}
                    className={`stripe-focus-ring flex min-h-[40px] min-w-0 flex-1 items-center gap-2.5 rounded-md py-2 text-[13px] no-underline ${collapsed ? 'justify-center px-0' : 'px-2.5'}`}
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
                    <span className={collapsed ? 'sr-only' : 'min-w-0 flex-1 truncate'}>{section.label}</span>
                  </Link>
                </RailTooltip>
              </div>
            </div>
          ))}
        </div>
      </nav>

      <div data-admin-sidebar-footer className={`shrink-0 py-3 ${collapsed ? 'px-2' : 'px-3'}`} style={{ borderTop: '1px solid var(--border)' }}>
        {onToggleCollapsed && (
          <RailTooltip label={t('nav.expandRail')} enabled={collapsed}>
            <Button
              variant="ghost"
              type="button"
              data-admin-rail-toggle
              aria-expanded={!collapsed}
              aria-controls="admin-rail-navigation"
              aria-label={collapsed ? t('nav.expandRail') : undefined}
              onClick={onToggleCollapsed}
              className={`mb-1 w-full gap-2.5 text-[13px] font-[550] text-muted-foreground ${collapsed ? 'justify-center px-0' : 'justify-start px-2'}`}
            >
              <Icon name={collapsed ? 'left_panel_open' : 'left_panel_close'} size="sm" />
              {!collapsed && <span>{t('nav.collapseRail')}</span>}
            </Button>
          </RailTooltip>
        )}
        <div data-admin-preferences className={`mb-1 flex items-center gap-1 ${collapsed ? 'flex-col' : 'px-1'}`}>
          <LangToggle variant="admin" />
          <ThemeToggle labeled={!collapsed} variant="admin" />
        </div>
        {isAdmin && (
          <RailTooltip label={t('nav.instanceManagement')} enabled={collapsed}>
            <Link
              to={getAdminRouteHref('users')}
              onClick={onNavigate}
              aria-label={t('nav.instanceManagement')}
              className={`stripe-focus-ring mb-2 flex min-h-10 items-center gap-2.5 rounded-md py-2 text-[13px] font-[550] no-underline transition-colors hover:bg-[var(--admin-rail-hover)] ${collapsed ? 'justify-center px-0' : 'px-2'}`}
              style={{ color: 'var(--text-soft)' }}
            >
              <Icon name="settings" size="sm" />
              {!collapsed && <span>{t('nav.instanceManagement')}</span>}
            </Link>
          </RailTooltip>
        )}
        <div className={`group flex cursor-default items-center gap-2.5 rounded-md transition-colors duration-150 ${collapsed ? 'flex-col gap-1 px-0 py-1' : 'px-2 py-2 hover:bg-[var(--admin-rail-hover)]'}`}>
          {collapsed ? (
            <RailTooltip label={`${username ?? ''} · ${t('account.title')}`} enabled>
              <Link
                to={getAdminRouteHref('account')}
                aria-label={t('account.title')}
                className="stripe-focus-ring flex h-10 w-10 items-center justify-center rounded-md no-underline"
              >
                <span
                  className="flex h-8 w-8 items-center justify-center rounded-md text-[13px] font-[600]"
                  style={{ background: 'var(--hit)', color: 'var(--on-hit)' }}
                >
                  {username?.[0]?.toUpperCase() || 'A'}
                </span>
              </Link>
            </RailTooltip>
          ) : (
            <>
              <div
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[13px] font-[600]"
                style={{ background: 'var(--hit)', color: 'var(--on-hit)' }}
              >
                {username?.[0]?.toUpperCase() || 'A'}
              </div>
              <Link to={getAdminRouteHref('account')} onClick={onNavigate} aria-label={t('account.title')} className="stripe-focus-ring min-w-0 flex-1 rounded-md py-1 no-underline">
                <p className="truncate text-[13px] font-[500] leading-tight" style={{ color: 'var(--text)' }}>{username}</p>
                <p className="mt-0.5 text-[11px] leading-tight" style={{ color: 'var(--text-muted)' }}>
                  {isAdmin ? t('nav.admin') : t('nav.readonly')} · {t('account.title')}
                </p>
              </Link>
            </>
          )}
          <RailTooltip label={t('nav.logout')} enabled={collapsed}>
            <Button variant="ghost" size="icon"
              type="button"
              onClick={onLogout}
              className={collapsed
                ? 'text-muted-foreground'
                : 'text-muted-foreground opacity-100 focus-visible:opacity-100 lg:opacity-0 lg:group-hover:opacity-100 lg:group-focus-within:opacity-100'}
              aria-label={t('nav.logout')}
            >
              <Icon name="logout" size="sm" />
            </Button>
          </RailTooltip>
        </div>
      </div>
    </>
  )
}

export default function MainLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [railCollapsed, setRailCollapsed] = useState(() => readLocalStorage(RAIL_STORAGE_KEY) === 'collapsed')
  const firstMobileNavigationRef = useRef<HTMLAnchorElement>(null)
  const { principal } = usePrincipal()
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
    isAdmin: principal?.role === 'admin',
    version: stats?.service?.version,
  }

  const toggleRail = () => {
    const next = !railCollapsed
    writeLocalStorage(RAIL_STORAGE_KEY, next ? 'collapsed' : 'expanded')
    setRailCollapsed(next)
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
      data-admin-rail={railCollapsed ? 'collapsed' : 'expanded'}
      className="relative z-[1] flex min-h-screen"
      style={{ background: 'var(--admin-canvas)' }}
    >
      <aside
        id="admin-rail-navigation"
        className="admin-rail fixed inset-y-0 left-0 z-30 hidden flex-col lg:flex"
        style={{ background: 'var(--admin-rail)', borderRight: '1px solid var(--border-soft)' }}
      >
        <SidebarContent
          {...sidebarProps}
          surface="sidebar"
          collapsed={railCollapsed}
          onToggleCollapsed={toggleRail}
          onLogout={() => { void handleLogout() }}
        />
      </aside>

      <Sheet open={mobileNavOpen} onOpenChange={setMobileNavOpen} modal>
        <SheetContent side="left" initialFocus={firstMobileNavigationRef} finalFocus>
          <SheetTitle className="sr-only">{t('nav.adminNavigation')}</SheetTitle>
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
        </SheetContent>
      </Sheet>

      <div data-admin-main className="admin-main min-w-0 flex-1" style={{ background: 'var(--admin-canvas)' }}>
        <main className="min-h-screen pb-8 pt-4 lg:pt-6" style={{ background: 'var(--admin-canvas)' }}>
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            {/* The Admin shell has no utility bar. On narrow screens the
                workspace rail is off-canvas, so the drawer trigger sits at the
                top of the content instead of occupying a header row. */}
            <Button variant="ghost" size="icon"
              type="button"
              data-admin-nav-trigger
              className="mb-3 text-muted-foreground lg:hidden"
              onClick={() => setMobileNavOpen(true)}
              aria-label={t('nav.openNavigation')}
              aria-expanded={mobileNavOpen}
              aria-controls="admin-mobile-navigation"
            >
              <Icon name="menu" size="sm" />
            </Button>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
