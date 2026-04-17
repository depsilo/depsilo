import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import BadgeV2 from '@/components/Badge'
import { authApi } from '@/lib/api'

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
      className="flex items-center gap-3 px-5 py-2 text-[14px] font-[400] transition-colors duration-150 no-underline relative"
      style={({ isActive }) => ({
        color: isActive ? 'var(--heading)' : 'var(--body)',
        background: isActive ? 'rgba(83,58,253,0.06)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--stripe-purple)' : '2px solid transparent',
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
    <div className="min-h-screen" style={{ background: 'var(--bg)' }}>
      {/* Sidebar — 240px */}
      <aside
        className="fixed left-0 top-0 z-30 h-screen w-[240px] flex flex-col"
        style={{ background: 'var(--surface)', borderRight: '1px solid var(--border)' }}
      >
        {/* Logo */}
        <div className="px-5 py-5 flex items-center gap-2.5">
          <Logo height={26} />
          <span className="text-[18px] font-[300] tracking-tight" style={{ color: 'var(--heading)' }}>Depsilo</span>
          <span className="text-[10px] font-mono rounded-[4px] px-1.5 py-0.5 ml-auto" style={{ background: 'var(--surface-low)', color: 'var(--body)', border: '1px solid var(--border)' }}>v0.1</span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p className="px-5 mb-1 text-[11px] tracking-widest font-[400] uppercase" style={{ color: 'var(--body)' }}>
            {t('nav.monitor')}
          </p>
          {monitorItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}

          <p className="px-5 mt-6 mb-1 text-[11px] tracking-widest font-[400] uppercase" style={{ color: 'var(--body)' }}>
            {t('nav.manage')}
          </p>
          {manageItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}
        </nav>

        {/* User info */}
        <div style={{ borderTop: '1px solid var(--border)' }} className="px-3 py-3">
          <div className="flex items-center gap-2.5 rounded-[6px] px-2 py-2 group transition-colors duration-150 cursor-default"
            onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--surface-low)' }}
            onMouseLeave={(e) => { e.currentTarget.style.background = '' }}
          >
            <div
              className="flex h-8 w-8 items-center justify-center rounded-[6px] text-[13px] font-[400] shrink-0"
              style={{ background: 'var(--stripe-purple)', color: 'var(--on-primary)' }}
            >
              {user.username?.[0]?.toUpperCase() || 'A'}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-[400] truncate leading-tight" style={{ color: 'var(--heading)' }}>{user.username}</p>
              <p className="text-[10px] leading-tight mt-0.5" style={{ color: 'var(--body)' }}>
                {user.role === 'admin' ? t('nav.admin') : t('nav.readonly')}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent opacity-0 group-hover:opacity-100 cursor-pointer transition-all duration-150 p-1 rounded-[4px]"
              style={{ color: 'var(--body)' }}
              title={t('nav.logout')}
              onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--heading)' }}
              onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--body)' }}
            >
              <Icon name="logout" size="sm" />
            </button>
          </div>
        </div>
      </aside>

      {/* Top bar — 56px */}
      <header
        className="fixed top-0 left-[240px] right-0 h-14 z-40 flex items-center justify-between px-8"
        style={{
          background: 'color-mix(in srgb, var(--surface) 85%, transparent)',
          backdropFilter: 'blur(12px)',
          WebkitBackdropFilter: 'blur(12px)',
          borderBottom: '1px solid var(--border)',
        }}
      >
        <h1 className="text-[14px] font-[400]" style={{ color: 'var(--heading)' }}>{pageTitle}</h1>
        <div className="flex items-center gap-3">
          <LangToggle />
          <ThemeToggle />
          <div className="flex items-center gap-1.5 text-[11px] font-mono" style={{ color: 'var(--body)' }}>
            <span className="w-1.5 h-1.5 rounded-full inline-block" style={{ background: 'var(--success)' }} />
            Healthy
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="ml-[240px] mt-14 p-8 min-h-[calc(100vh-3.5rem)]" style={{ background: 'var(--bg)' }}>
        <Outlet />
      </main>
    </div>
  )
}
