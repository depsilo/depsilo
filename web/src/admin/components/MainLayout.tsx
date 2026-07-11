import { type RefObject, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

import BadgeV2 from '@/components/Badge'
import DrawerV2 from '@/components/Drawer'
import Icon from '@/components/Icon'
import LangToggle from '@/components/LangToggle'
import Logo from '@/components/Logo'
import ThemeToggle from '@/components/ThemeToggle'
import NowStrip from '@/admin/components/NowStrip'
import { usePrincipal } from '@/hooks/usePrincipal'
import { authApi, statsApi } from '@/lib/api'
import { formatVersion } from '@/lib/utils'

interface NavItem {
  label: string
  to: string
  icon: string
  end?: boolean
  pro?: boolean
}

interface SidebarContentProps {
  monitorItems: NavItem[]
  manageItems: NavItem[]
  username?: string
  canWrite: boolean
  version?: string
  firstNavigationRef?: RefObject<HTMLAnchorElement | null>
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
      end={item.end}
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
  monitorItems,
  manageItems,
  username,
  canWrite,
  version,
  firstNavigationRef,
  onNavigate,
  onLogout,
}: SidebarContentProps) {
  const { t } = useTranslation()
  return (
    <>
      <div className="flex items-center gap-2.5 px-5 py-5">
        <Logo size={26} />
        <span className="text-[15px] font-[700]" style={{ color: 'var(--text)' }}>depsilo</span>
        <span
          className="ml-auto inline-flex min-w-16 items-center justify-center rounded-[4px] border px-1.5 py-0.5 font-mono text-[10px] tabular-nums"
          title={version}
          style={{ background: 'var(--bg-hover)', color: 'var(--text-soft)', borderColor: 'var(--border)' }}
        >
          {formatVersion(version)}
        </span>
      </div>

      <nav className="flex-1 overflow-y-auto py-2">
        <p
          className="mt-2 mb-1.5 px-5 font-mono text-[10px] font-[600] uppercase"
          style={{ color: 'var(--text-subtle)' }}
        >
          {t('nav.monitor')}
        </p>
        {monitorItems.map((item, index) => (
          <SidebarNavItem
            key={item.to}
            item={item}
            linkRef={index === 0 ? firstNavigationRef : undefined}
            onNavigate={onNavigate}
          />
        ))}

        <p
          className="mt-6 mb-1.5 px-5 font-mono text-[10px] font-[600] uppercase"
          style={{ color: 'var(--text-subtle)' }}
        >
          {t('nav.manage')}
        </p>
        {manageItems.map(item => (
          <SidebarNavItem key={item.to} item={item} onNavigate={onNavigate} />
        ))}
      </nav>

      <div className="px-3 py-3" style={{ borderTop: '0.5px solid var(--border)' }}>
        <div className="group flex cursor-default items-center gap-2.5 rounded-[6px] px-2 py-2 transition-colors duration-150 hover:bg-[var(--bg-hover)]">
          <div
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[6px] text-[13px] font-[600]"
            style={{ background: 'var(--hit)', color: 'var(--on-hit)' }}
          >
            {username?.[0]?.toUpperCase() || 'A'}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13px] font-[500] leading-tight" style={{ color: 'var(--text)' }}>{username}</p>
            <p className="mt-0.5 text-[10px] leading-tight" style={{ color: 'var(--text-subtle)' }}>
              {canWrite ? t('nav.admin') : t('nav.readonly')}
            </p>
          </div>
          <button
            type="button"
            onClick={onLogout}
            className="inline-flex min-h-10 min-w-10 cursor-pointer items-center justify-center rounded-[4px] bg-transparent p-1.5 text-[var(--text-soft)] opacity-100 transition-[opacity,color,transform] duration-150 hover:text-[var(--text)] active:scale-[0.96] lg:opacity-0 lg:group-hover:opacity-100"
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

  const monitorItems: NavItem[] = [
    { label: t('nav.dashboard'), to: '/admin', icon: 'dashboard', end: true },
    { label: t('bandwidth.title'), to: '/admin/bandwidth', icon: 'bar_chart' },
    { label: t('nav.accessLogs'), to: '/admin/logs', icon: 'receipt_long' },
    { label: t('nav.auditLogs'), to: '/admin/audit', icon: 'policy' },
    { label: t('nav.quarantine'), to: '/admin/quarantine', icon: 'shield_lock' },
  ]
  const manageItems: NavItem[] = [
    { label: t('nav.cacheManage'), to: '/admin/cache', icon: 'storage' },
    { label: t('nav.upstreams'), to: '/admin/upstreams', icon: 'cloud_sync' },
    { label: t('nav.userManage'), to: '/admin/users', icon: 'group' },
    { label: t('license.title'), to: '/admin/license', icon: 'key' },
    { label: t('nav.rules'), to: '/admin/rules', icon: 'shield' },
    { label: t('nav.security'), to: '/admin/security', icon: 'security' },
    { label: t('nav.projects'), to: '/admin/projects', icon: 'folder_managed', pro: true },
    { label: t('nav.settings'), to: '/admin/settings', icon: 'settings' },
  ]
  const pageTitles: Record<string, string> = {
    '/admin': t('nav.dashboard'),
    '/admin/bandwidth': t('bandwidth.title'),
    '/admin/logs': t('nav.accessLogs'),
    '/admin/audit': t('nav.auditLogs'),
    '/admin/quarantine': t('nav.quarantine'),
    '/admin/cache': t('nav.cacheManage'),
    '/admin/upstreams': t('nav.upstreams'),
    '/admin/users': t('nav.userManage'),
    '/admin/license': t('license.title'),
    '/admin/rules': t('nav.rules'),
    '/admin/security': t('nav.security'),
    '/admin/projects': t('nav.projects'),
    '/admin/settings': t('nav.settings'),
  }
  const sidebarProps = {
    monitorItems,
    manageItems,
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
    <div className="flex min-h-screen" style={{ background: 'var(--bg-page)' }}>
      <div className="page-wash" />
      <aside
        className="fixed inset-y-0 left-0 z-30 hidden w-[220px] flex-col lg:flex"
        style={{ background: 'var(--bg-card)', borderRight: '0.5px solid var(--border)' }}
      >
        <SidebarContent {...sidebarProps} onLogout={() => { void handleLogout() }} />
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
            firstNavigationRef={firstMobileNavigationRef}
            onNavigate={() => setMobileNavOpen(false)}
            onLogout={() => { void handleLogout() }}
          />
        </div>
      </DrawerV2>

      <div data-admin-main className="min-w-0 flex-1 lg:ml-[220px]">
        <header
          className="aurora-rim-bottom fixed top-0 right-0 left-0 z-20 flex h-24 flex-wrap items-center gap-x-2.5 px-4 sm:px-6 md:h-12 md:flex-nowrap lg:left-[220px] lg:px-8"
          style={{
            background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
            backdropFilter: 'saturate(180%) blur(8px)',
            WebkitBackdropFilter: 'saturate(180%) blur(8px)',
          }}
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
          <h1 className="min-w-0 flex-1 truncate text-[17px] font-[600] md:flex-none md:shrink" style={{ color: 'var(--text)' }}>
            {pageTitles[location.pathname] || t('nav.dashboard')}
          </h1>
          <div className="order-last min-w-0 basis-full overflow-hidden md:order-none md:flex-1 md:basis-auto md:px-3 lg:px-5">
            <NowStrip variant="topbar" />
          </div>
          <div className="flex shrink-0 items-center gap-1.5 sm:gap-2.5">
            <Link
              to="/"
              className="hidden items-center gap-1 text-[12px] font-[500] text-[var(--text-soft)] no-underline transition-colors duration-150 hover:text-[var(--text)] sm:inline-flex"
              title={t('portal.backLink')}
            >
              {t('portal.backLink')}
            </Link>
            <LangToggle />
            <ThemeToggle />
          </div>
        </header>

        <main className="min-h-screen bg-[var(--bg-page)] pt-28 pb-6 md:pt-20">
          <div data-admin-outlet className="mx-auto w-full max-w-[1840px] px-4 sm:px-6 lg:px-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
