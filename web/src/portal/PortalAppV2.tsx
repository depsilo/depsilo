import { Routes, Route, NavLink, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import BadgeV2 from '@/components/BadgeV2'
import QuickStartV2 from '@/portal/pages/QuickStartV2'
import PackagesV2 from '@/portal/pages/PackagesV2'
import PackageDetailV2 from '@/portal/pages/PackageDetailV2'
import LiveStreamV2 from '@/portal/pages/LiveStreamV2'
import ServiceStatusV2 from '@/portal/pages/ServiceStatusV2'

export default function PortalAppV2() {
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

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    `px-3 py-1.5 text-[14px] font-[400] transition-colors duration-150 ${
      isActive
        ? 'border-b-2'
        : 'border-b-2 border-transparent'
    }`

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg)' }}>
      {/* Frosted glass header */}
      <header
        className="fixed top-0 inset-x-0 z-50 h-14"
        style={{
          background: 'color-mix(in srgb, var(--surface) 85%, transparent)',
          backdropFilter: 'blur(12px)',
          WebkitBackdropFilter: 'blur(12px)',
          borderBottom: '1px solid var(--border)',
        }}
      >
        <div className="mx-auto relative h-full max-w-[1080px] px-6">
          {/* Left: Logo — absolutely positioned left */}
          <Link to="/" className="absolute left-6 top-1/2 -translate-y-1/2 flex items-center gap-2.5 no-underline">
            <Logo height={28} />
            <span className="text-[18px] font-[300] tracking-tight" style={{ color: 'var(--heading)' }}>Depsilo</span>
          </Link>

          {/* Center: Nav — absolutely centered on page, immune to left/right width changes */}
          <nav className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 flex items-center gap-1">
            <NavLink to="/" end className={navLinkClass} style={({ isActive }) => ({ color: isActive ? 'var(--heading)' : 'var(--body)', borderBottomColor: isActive ? 'var(--stripe-purple)' : 'transparent' })}>
              {t('portal.quickStart')}
            </NavLink>
            <NavLink to="/packages" className={navLinkClass} style={({ isActive }) => ({ color: isActive ? 'var(--heading)' : 'var(--body)', borderBottomColor: isActive ? 'var(--stripe-purple)' : 'transparent' })}>
              {t('portal.packages')}
            </NavLink>
            <NavLink to="/live" className={navLinkClass} style={({ isActive }) => ({ color: isActive ? 'var(--heading)' : 'var(--body)', borderBottomColor: isActive ? 'var(--stripe-purple)' : 'transparent' })}>
              {t('portal.live')}
            </NavLink>
            <NavLink to="/status" className={navLinkClass} style={({ isActive }) => ({ color: isActive ? 'var(--heading)' : 'var(--body)', borderBottomColor: isActive ? 'var(--stripe-purple)' : 'transparent' })}>
              {t('portal.serviceStatus')}
            </NavLink>
          </nav>

          {/* Right — absolutely positioned right, each item has fixed width to prevent mutual shifting */}
          <div className="absolute right-6 top-1/2 -translate-y-1/2 flex items-center gap-3">
            <LangToggle />
            <ThemeToggle />
            {data && (
              <span className="inline-flex items-center justify-center min-w-[72px]">
                <BadgeV2 variant={isHealthy ? 'success' : 'error'} className="whitespace-nowrap">
                  <span className={`inline-block w-1.5 h-1.5 rounded-full mr-1.5 ${isHealthy ? 'bg-success' : 'bg-error'}`} />
                  {isHealthy ? t('portal.online') : t('portal.offline')}
                </BadgeV2>
              </span>
            )}
            <Link
              to="/admin"
              className="inline-flex items-center justify-center min-w-[88px] text-[14px] font-[400] no-underline transition-colors duration-150 whitespace-nowrap"
              style={{ color: 'var(--stripe-purple)' }}
            >
              {t('portal.adminPanel')}
            </Link>
          </div>
        </div>
      </header>

      {/* Content */}
      <main className="pt-14 max-w-[1080px] mx-auto px-6 py-8">
        <Routes>
          <Route index element={<QuickStartV2 />} />
          <Route path="packages" element={<PackagesV2 />} />
          <Route path="packages/:type/:name" element={<PackageDetailV2 />} />
          <Route path="live" element={<LiveStreamV2 />} />
          <Route path="status" element={<ServiceStatusV2 />} />
        </Routes>
      </main>
    </div>
  )
}
