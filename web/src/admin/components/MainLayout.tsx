import { Link, NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import BadgeV2 from '@/components/Badge'
import NowStrip from '@/admin/components/NowStrip'
import { authApi, statsApi } from '@/lib/api'
import { formatVersion } from '@/lib/utils'

interface NavItem {
  label: string
  to: string
  icon: string
  end?: boolean
  pro?: boolean
}

function SidebarNavItem({ item }: { item: NavItem }) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      className="flex items-center gap-2.5 mx-2 px-3 py-2 text-[13px] rounded-[6px] transition-colors duration-150 no-underline"
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

export default function MainLayoutV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()

  // Resilient localStorage parse: a corrupted "user" entry should not white-screen admin.
  const user: { username: string; role: string } = (() => {
    try {
      const raw = localStorage.getItem('user')
      if (raw) return JSON.parse(raw)
    } catch {
      // fall through to default
    }
    return { username: 'admin', role: 'admin' }
  })()

  const { data: stats } = useQuery<{ service: { version: string; status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => (await statsApi.getStats()).data,
    refetchInterval: 30000,
    staleTime: 30000,
  })

  // Pro badges removed from audit / rules / security on 2026-06-29 —
  // those features moved to open-source as part of the pricing reset
  // (DIRECTION.md "Pro narrowed to multi-project" decision). The Pro
  // badge now stays on Projects only, which is the single remaining
  // Pro-gated UI surface.
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

  const pageTitle = pageTitles[location.pathname] || t('nav.dashboard')

  const handleLogout = async () => {
    try { await authApi.logout() } catch { /* ignore */ }
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/admin/login', { replace: true })
  }

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-page)' }}>
      {/* Page-wide ambient gradient — same mesh used in Portal so the
          two halves of the product feel cohesive. */}
      <div className="page-wash" />
      {/* Sidebar — 220px */}
      <aside
        className="fixed left-0 top-0 z-30 h-screen w-[220px] flex flex-col"
        style={{ background: 'var(--bg-card)', borderRight: '0.5px solid var(--border)' }}
      >
        {/* Logo */}
        <div className="px-5 py-5 flex items-center gap-2.5">
          <Logo size={26} />
          <span className="text-[16px] font-[600] tracking-[-0.025em]" style={{ color: 'var(--text)' }}>Depsilo</span>
          {/* Min-width pre-reserves the chip's eventual filled size so the
              first paint ("—" while stats are loading) doesn't shrink the
              chip and jolt the logo row when the real version arrives. */}
          <span
            className="text-[10px] font-mono rounded-[4px] px-1.5 py-0.5 ml-auto inline-flex items-center justify-center tabular-nums"
            title={stats?.service?.version}
            style={{ background: 'var(--bg-hover)', color: 'var(--text-soft)', border: '1px solid var(--border)', minWidth: 64 }}
          >
            {formatVersion(stats?.service?.version)}
          </span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p
            className="font-mono uppercase mt-2 mb-1.5 px-5"
            style={{ fontSize: 10, fontWeight: 600, letterSpacing: '0.12em', color: 'var(--text-subtle)' }}
          >
            {t('nav.monitor')}
          </p>
          {monitorItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}

          <p
            className="font-mono uppercase mt-6 mb-1.5 px-5"
            style={{ fontSize: 10, fontWeight: 600, letterSpacing: '0.12em', color: 'var(--text-subtle)' }}
          >
            {t('nav.manage')}
          </p>
          {manageItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}
        </nav>

        {/* User info */}
        <div style={{ borderTop: '0.5px solid var(--border)' }} className="px-3 py-3">
          <div className="flex items-center gap-2.5 rounded-[6px] px-2 py-2 group transition-colors duration-150 cursor-default hover:bg-[var(--bg-hover)]">
            <div
              className="flex h-8 w-8 items-center justify-center rounded-[6px] text-[13px] font-[600] shrink-0"
              style={{ background: 'var(--brand)', color: 'white' }}
            >
              {user.username?.[0]?.toUpperCase() || 'A'}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-[500] truncate leading-tight" style={{ color: 'var(--text)' }}>{user.username}</p>
              <p className="text-[10px] leading-tight mt-0.5" style={{ color: 'var(--text-subtle)' }}>
                {user.role === 'admin' ? t('nav.admin') : t('nav.readonly')}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent opacity-0 group-hover:opacity-100 cursor-pointer transition-[opacity,color,transform] duration-150 active:scale-[0.96] p-1.5 rounded-[4px] text-[var(--text-soft)] hover:text-[var(--text)]"
              title={t('nav.logout')}
            >
              <Icon name="logout" size="sm" />
            </button>
          </div>
        </div>
      </aside>

      {/* Top bar — 48px */}
      <header
        className="aurora-rim-bottom fixed top-0 left-[220px] right-0 z-40 flex items-center justify-between px-8"
        style={{
          height: 48,
          background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
          backdropFilter: 'saturate(180%) blur(8px)',
          WebkitBackdropFilter: 'saturate(180%) blur(8px)',
        }}
      >
        <h1 className="text-[17px] font-[600] tracking-[-0.015em] flex-shrink-0" style={{ color: 'var(--text)' }}>{pageTitle}</h1>

        {/* Now strip rides between the page title and the right-side controls.
            Single-row layout sized to fit the 48px topbar; carries the live
            status + bandwidth signal on every admin page so the operator
            never has to navigate just to check liveness. */}
        <div className="flex-1 min-w-0 mx-6">
          <NowStrip variant="topbar" />
        </div>

        <div className="flex items-center gap-2.5 flex-shrink-0">
          <Link
            to="/"
            className="text-[12px] font-[500] no-underline transition-colors duration-150 inline-flex items-center gap-1 text-[var(--text-soft)] hover:text-[var(--text)]"
            title={t('portal.backLink')}
          >
            {t('portal.backLink')}
          </Link>
          <LangToggle />
          <ThemeToggle />
        </div>
      </header>

      {/* Main content */}
      <main className="ml-[220px] p-8 min-h-screen" style={{ paddingTop: 80, background: 'var(--bg-page)' }}>
        <Outlet />
      </main>
    </div>
  )
}
