import { Routes, Route, NavLink, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { statsApi } from '@/lib/api'
import ThemeToggle from '@/components/ThemeToggle'
import Button from '@/components/Button'
import QuickStart from '@/portal/pages/QuickStart'
import ServiceStatus from '@/portal/pages/ServiceStatus'

export default function PortalApp() {
  const { data } = useQuery<{ service: { status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => {
      const res = await statsApi.getStats()
      return res.data
    },
    refetchInterval: 30000,
  })

  const isHealthy = data?.service?.status === 'healthy'

  return (
    <div className="min-h-screen bg-bg">
      {/* Fixed header */}
      <header className="fixed top-0 inset-x-0 z-50 h-14 bg-surface-low">
        <div className="mx-auto flex h-full max-w-4xl items-center justify-between px-6">
          {/* Left: Logo */}
          <Link to="/" className="text-lg font-bold text-on-surface">
            RepoCache
          </Link>

          {/* Center: Nav */}
          <nav className="flex items-center gap-1">
            <NavLink
              to="/"
              end
              className={({ isActive }) =>
                `px-3 py-1.5 text-sm font-medium transition-colors ${
                  isActive
                    ? 'text-primary'
                    : 'text-on-surface-variant hover:text-on-surface'
                }`
              }
            >
              快速开始
            </NavLink>
            <NavLink
              to="/status"
              className={({ isActive }) =>
                `px-3 py-1.5 text-sm font-medium transition-colors ${
                  isActive
                    ? 'text-primary'
                    : 'text-on-surface-variant hover:text-on-surface'
                }`
              }
            >
              服务状态
            </NavLink>
          </nav>

          {/* Right: ThemeToggle + status pill + admin link */}
          <div className="flex items-center gap-3">
            <ThemeToggle />

            {data && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-surface-container px-2.5 py-1 text-xs font-medium">
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    isHealthy ? 'bg-success' : 'bg-error'
                  }`}
                />
                <span className={isHealthy ? 'text-success' : 'text-error'}>
                  {isHealthy ? '服务在线' : '离线'}
                </span>
              </span>
            )}

            <Link to="/admin">
              <Button variant="ghost" className="text-xs">
                管理后台 →
              </Button>
            </Link>
          </div>
        </div>
      </header>

      {/* Content */}
      <main className="pt-14 max-w-4xl mx-auto px-6 py-8">
        <Routes>
          <Route index element={<QuickStart />} />
          <Route path="status" element={<ServiceStatus />} />
        </Routes>
      </main>
    </div>
  )
}
