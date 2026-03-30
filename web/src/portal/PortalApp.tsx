import { Routes, Route, NavLink, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import Button from '@/components/Button'
import QuickStart from '@/portal/pages/QuickStart'
import Packages from '@/portal/pages/Packages'
import PackageDetail from '@/portal/pages/PackageDetail'
import LiveStream from '@/portal/pages/LiveStream'
import ServiceStatus from '@/portal/pages/ServiceStatus'

export default function PortalApp() {
  const { t } = useTranslation()
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
          <Link to="/" className="flex items-center gap-2.5">
            <Logo height={30} />
            <span className="text-xl font-bold text-on-surface tracking-tight">Depsilo</span>
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
              {t('portal.quickStart')}
            </NavLink>
            <NavLink
              to="/packages"
              className={({ isActive }) =>
                `px-3 py-1.5 text-sm font-medium transition-colors ${
                  isActive
                    ? 'text-primary'
                    : 'text-on-surface-variant hover:text-on-surface'
                }`
              }
            >
              {t('portal.packages')}
            </NavLink>
            <NavLink
              to="/live"
              className={({ isActive }) =>
                `px-3 py-1.5 text-sm font-medium transition-colors ${
                  isActive
                    ? 'text-primary'
                    : 'text-on-surface-variant hover:text-on-surface'
                }`
              }
            >
              {t('portal.live')}
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
              {t('portal.serviceStatus')}
            </NavLink>
          </nav>

          {/* Right: ThemeToggle + status pill + admin link */}
          <div className="flex items-center gap-3">
            <LangToggle />
            <ThemeToggle />

            {data && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-surface-container px-2.5 py-1 text-xs font-medium">
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    isHealthy ? 'bg-success' : 'bg-error'
                  }`}
                />
                <span className={isHealthy ? 'text-success' : 'text-error'}>
                  {isHealthy ? t('portal.online') : t('portal.offline')}
                </span>
              </span>
            )}

            <Link to="/admin">
              <Button variant="ghost" className="text-xs">
                {t('portal.adminPanel')}
              </Button>
            </Link>
          </div>
        </div>
      </header>

      {/* Content */}
      <main className="pt-14 max-w-4xl mx-auto px-6 py-8">
        <Routes>
          <Route index element={<QuickStart />} />
          <Route path="packages" element={<Packages />} />
          <Route path="packages/:type/:name" element={<PackageDetail />} />
          <Route path="live" element={<LiveStream />} />
          <Route path="status" element={<ServiceStatus />} />
        </Routes>
      </main>
    </div>
  )
}
