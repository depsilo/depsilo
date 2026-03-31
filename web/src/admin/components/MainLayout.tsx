import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import { authApi } from '@/lib/api'

interface NavItem {
  label: string
  to: string
  icon: string
  end?: boolean
}

function SidebarNavItem({ item }: { item: NavItem }) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      className={({ isActive }) =>
        `flex items-center gap-3 px-6 py-2.5 text-sm transition-colors ${
          isActive
            ? 'bg-primary/10 border-r-2 border-primary text-on-surface font-medium'
            : 'text-on-surface-variant hover:text-on-surface hover:bg-surface-container border-r-2 border-transparent'
        }`
      }
    >
      <Icon name={item.icon} size="sm" />
      {item.label}
    </NavLink>
  )
}

export default function MainLayout() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const user = JSON.parse(localStorage.getItem('user') || '{"username":"admin","role":"admin"}')

  const monitorItems: NavItem[] = [
    { label: t('nav.dashboard'), to: '/admin', icon: 'dashboard', end: true },
    { label: t('nav.accessLogs'), to: '/admin/logs', icon: 'receipt_long' },
    { label: t('nav.auditLogs'), to: '/admin/audit', icon: 'policy' },
  ]

  const manageItems: NavItem[] = [
    { label: t('nav.cacheManage'), to: '/admin/cache', icon: 'storage' },
    { label: t('nav.upstreams'), to: '/admin/upstreams', icon: 'cloud_sync' },
    { label: t('nav.userManage'), to: '/admin/users', icon: 'group' },
    { label: t('nav.rules'), to: '/admin/rules', icon: 'shield' },
    { label: t('nav.settings'), to: '/admin/settings', icon: 'settings' },
  ]

  const pageTitles: Record<string, string> = {
    '/admin': t('nav.dashboard'),
    '/admin/logs': t('nav.accessLogs'),
    '/admin/audit': t('nav.auditLogs'),
    '/admin/cache': t('nav.cacheManage'),
    '/admin/upstreams': t('nav.upstreams'),
    '/admin/users': t('nav.userManage'),
    '/admin/rules': t('nav.rules'),
    '/admin/settings': t('nav.settings'),
  }

  const pageTitle = pageTitles[location.pathname] || t('nav.dashboard')

  const handleLogout = async () => {
    try {
      await authApi.logout()
    } catch {
      // ignore
    }
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/admin/login', { replace: true })
  }

  return (
    <div className="min-h-screen bg-bg">
      {/* Sidebar */}
      <aside className="fixed left-0 top-0 z-30 h-screen w-[200px] bg-surface-low border-r border-outline-variant/10 flex flex-col">
        {/* Logo */}
        <div className="px-5 py-5 flex items-center gap-2.5">
          <Logo height={28} />
          <span className="text-lg font-bold text-on-surface tracking-tight">Depsilo</span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p className="px-6 mb-2 text-xs tracking-widest text-on-surface-variant font-medium uppercase">
            {t('nav.monitor')}
          </p>
          {monitorItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}

          <p className="px-6 mt-6 mb-2 text-xs tracking-widest text-on-surface-variant font-medium uppercase">
            {t('nav.manage')}
          </p>
          {manageItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}
        </nav>

        {/* User info */}
        <div className="border-t border-outline-variant/10 px-3 py-3">
          <div className="flex items-center gap-2.5 rounded-lg px-2 py-2 hover:bg-surface-container transition-colors group">
            <div className="relative shrink-0">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-primary/80 to-primary text-on-primary text-xs font-semibold shadow-sm">
                {user.username?.[0]?.toUpperCase() || 'A'}
              </div>
              {user.role === 'admin' && (
                <span className="absolute -bottom-0.5 -right-0.5 flex h-3 w-3 items-center justify-center rounded-full bg-success ring-2 ring-surface-low">
                  <Icon name="check" className="text-[8px] text-white" />
                </span>
              )}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-medium text-on-surface truncate leading-tight">{user.username}</p>
              <p className="text-[10px] text-on-surface-variant leading-tight mt-0.5">
                {user.role === 'admin' ? t('nav.admin') : t('nav.readonly')}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent text-on-surface-variant/0 group-hover:text-on-surface-variant hover:!text-on-surface cursor-pointer transition-all p-1 rounded-md hover:bg-surface-low"
              title={t('nav.logout')}
            >
              <Icon name="logout" size="sm" />
            </button>
          </div>
        </div>
      </aside>

      {/* Top bar */}
      <header className="fixed top-0 left-[200px] right-0 h-14 bg-surface-low/80 backdrop-blur-md z-40 border-b border-outline-variant/10 flex items-center justify-between px-8">
        <h1 className="text-sm font-medium text-on-surface">{pageTitle}</h1>
        <div className="flex items-center gap-3">
          <LangToggle />
          <ThemeToggle />
          <span className="flex items-center gap-1.5 text-[10px] text-on-surface-variant font-mono">
            <span className="w-1.5 h-1.5 rounded-full bg-success inline-block" />
            Healthy
          </span>
        </div>
      </header>

      {/* Main content */}
      <main className="ml-[200px] mt-14 p-8 bg-bg min-h-[calc(100vh-3.5rem)]">
        <Outlet />
      </main>
    </div>
  )
}
