import { Link, NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import BadgeV2 from '@/components/Badge'
import StatusDot from '@/components/StatusDot'
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
      className="flex items-center gap-3 px-5 py-2 text-[14px] transition-colors duration-150 no-underline relative"
      style={({ isActive }) => ({
        color: isActive ? 'var(--brand-text)' : 'var(--text-soft)',
        background: isActive ? 'var(--brand-soft)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--brand)' : '2px solid transparent',
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
  const user = JSON.parse(localStorage.getItem('user') || '{"username":"admin","role":"admin"}')

  const { data: stats } = useQuery<{ service: { version: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => (await statsApi.getStats()).data,
    refetchInterval: 30000,
    staleTime: 30000,
  })

  const monitorItems: NavItem[] = [
    { label: t('nav.dashboard'), to: '/admin', icon: 'dashboard', end: true },
    { label: t('bandwidth.title'), to: '/admin/bandwidth', icon: 'bar_chart' },
    { label: t('nav.accessLogs'), to: '/admin/logs', icon: 'receipt_long' },
    { label: t('nav.auditLogs'), to: '/admin/audit', icon: 'policy', pro: true },
  ]

  const manageItems: NavItem[] = [
    { label: t('nav.cacheManage'), to: '/admin/cache', icon: 'storage' },
    { label: t('nav.upstreams'), to: '/admin/upstreams', icon: 'cloud_sync' },
    { label: t('nav.userManage'), to: '/admin/users', icon: 'group' },
    { label: t('nav.rules'), to: '/admin/rules', icon: 'shield', pro: true },
    { label: t('nav.security'), to: '/admin/security', icon: 'security', pro: true },
    { label: t('nav.projects'), to: '/admin/projects', icon: 'folder_managed', pro: true },
    { label: t('nav.settings'), to: '/admin/settings', icon: 'settings' },
  ]

  const pageTitles: Record<string, string> = {
    '/admin': t('nav.dashboard'),
    '/admin/bandwidth': t('bandwidth.title'),
    '/admin/logs': t('nav.accessLogs'),
    '/admin/audit': t('nav.auditLogs'),
    '/admin/cache': t('nav.cacheManage'),
    '/admin/upstreams': t('nav.upstreams'),
    '/admin/users': t('nav.userManage'),
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
      {/* Sidebar — 220px */}
      <aside
        className="fixed left-0 top-0 z-30 h-screen w-[220px] flex flex-col"
        style={{ background: 'var(--bg-card)', borderRight: '0.5px solid var(--border)' }}
      >
        {/* Logo */}
        <div className="px-5 py-5 flex items-center gap-2.5">
          <Logo size={26} />
          <span className="text-[18px] font-[300] tracking-tight" style={{ color: 'var(--text-base)' }}>Depsilo</span>
          <span className="text-[10px] font-mono rounded-[4px] px-1.5 py-0.5 ml-auto" style={{ background: 'var(--bg-hover)', color: 'var(--text-soft)', border: '1px solid var(--border)' }}>{formatVersion(stats?.service?.version)}</span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p style={{ fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600, letterSpacing: '0.12em', textTransform: 'uppercase', color: 'var(--text-subtle)', padding: '8px 16px 4px' }}>
            {t('nav.monitor')}
          </p>
          {monitorItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}

          <p style={{ fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600, letterSpacing: '0.12em', textTransform: 'uppercase', color: 'var(--text-subtle)', padding: '24px 16px 4px' }}>
            {t('nav.manage')}
          </p>
          {manageItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}
        </nav>

        {/* User info */}
        <div style={{ borderTop: '0.5px solid var(--border)' }} className="px-3 py-3">
          <div className="flex items-center gap-2.5 rounded-[6px] px-2 py-2 group transition-colors duration-150 cursor-default"
            onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--bg-hover)' }}
            onMouseLeave={(e) => { e.currentTarget.style.background = '' }}
          >
            <div
              className="flex h-8 w-8 items-center justify-center rounded-[6px] text-[13px] font-[400] shrink-0"
              style={{ background: 'var(--brand)', color: 'white' }}
            >
              {user.username?.[0]?.toUpperCase() || 'A'}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-[400] truncate leading-tight" style={{ color: 'var(--text-base)' }}>{user.username}</p>
              <p className="text-[10px] leading-tight mt-0.5" style={{ color: 'var(--text-soft)' }}>
                {user.role === 'admin' ? t('nav.admin') : t('nav.readonly')}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent opacity-0 group-hover:opacity-100 cursor-pointer transition-all duration-150 p-1 rounded-[4px]"
              style={{ color: 'var(--text-soft)' }}
              title={t('nav.logout')}
              onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--text-base)' }}
              onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-soft)' }}
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
        <h1 className="text-[14px] font-[400]" style={{ color: 'var(--text-base)' }}>{pageTitle}</h1>
        <div className="flex items-center gap-3">
          <Link
            to="/"
            className="text-[12px] font-[500] no-underline transition-colors duration-150 inline-flex items-center gap-1"
            style={{ color: 'var(--brand-text)' }}
            onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--brand-strong)' }}
            onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--brand-text)' }}
            title={t('portal.backLink')}
          >
            {t('portal.backLink')}
          </Link>
          <LangToggle />
          <ThemeToggle />
          <div className="flex items-center gap-1.5 text-[11px] font-mono" style={{ color: 'var(--text-soft)' }}>
            <StatusDot status="healthy" size={6} />
            Healthy
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="ml-[220px] p-8 min-h-screen" style={{ paddingTop: 48, background: 'var(--bg-page)' }}>
        <Outlet />
      </main>
    </div>
  )
}
