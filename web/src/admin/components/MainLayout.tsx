import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import Icon from '@/components/Icon'
import Badge from '@/components/Badge'
import ThemeToggle from '@/components/ThemeToggle'
import { authApi } from '@/lib/api'

interface NavItem {
  label: string
  to: string
  icon: string
  end?: boolean
}

const monitorItems: NavItem[] = [
  { label: 'Dashboard', to: '/admin', icon: 'dashboard', end: true },
  { label: '访问日志', to: '/admin/logs', icon: 'receipt_long' },
]

const manageItems: NavItem[] = [
  { label: '缓存管理', to: '/admin/cache', icon: 'storage' },
  { label: '上游源', to: '/admin/upstreams', icon: 'cloud_sync' },
  { label: '用户管理', to: '/admin/users', icon: 'group' },
  { label: '系统设置', to: '/admin/settings', icon: 'settings' },
]

const pageTitles: Record<string, string> = {
  '/admin': 'Dashboard',
  '/admin/logs': '访问日志',
  '/admin/cache': '缓存管理',
  '/admin/upstreams': '上游源',
  '/admin/users': '用户管理',
  '/admin/settings': '系统设置',
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
  const navigate = useNavigate()
  const location = useLocation()
  const user = JSON.parse(localStorage.getItem('user') || '{"username":"admin","role":"admin"}')

  const pageTitle = pageTitles[location.pathname] || 'Dashboard'

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
        <div className="px-6 py-5">
          <span className="text-base font-bold text-on-surface">RepoCache</span>
          <span className="ml-2 text-[10px] text-on-surface-variant font-mono">v0.1.0</span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p className="px-6 mb-2 text-xs tracking-widest text-on-surface-variant font-medium uppercase">
            监控
          </p>
          {monitorItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}

          <p className="px-6 mt-6 mb-2 text-xs tracking-widest text-on-surface-variant font-medium uppercase">
            管理
          </p>
          {manageItems.map((item) => (
            <SidebarNavItem key={item.to} item={item} />
          ))}
        </nav>

        {/* User info */}
        <div className="border-t border-outline-variant/10 px-4 py-3">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary-container text-primary text-sm font-medium shrink-0">
              {user.username?.[0]?.toUpperCase() || 'A'}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-on-surface truncate">{user.username}</p>
              <Badge variant={user.role === 'admin' ? 'success' : 'default'}>{user.role}</Badge>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent text-on-surface-variant hover:text-on-surface cursor-pointer transition-colors p-1"
              title="退出登录"
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
          <ThemeToggle />
          <span className="flex items-center gap-1.5 text-[10px] text-on-surface-variant font-mono">
            <span className="w-1.5 h-1.5 rounded-full bg-success inline-block" />
            Healthy
          </span>
          <div className="flex h-7 w-7 items-center justify-center rounded-full bg-primary-container text-primary text-xs font-medium">
            {user.username?.[0]?.toUpperCase() || 'A'}
          </div>
        </div>
      </header>

      {/* Main content */}
      <main className="ml-[200px] mt-14 p-8 bg-bg min-h-[calc(100vh-3.5rem)]">
        <Outlet />
      </main>
    </div>
  )
}
