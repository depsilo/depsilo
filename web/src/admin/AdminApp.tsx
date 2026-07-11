import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import MainLayoutV2 from './components/MainLayout'
import LoginV2 from './pages/Login'
import DashboardV2 from './pages/Dashboard'
import BandwidthReportV2 from './pages/BandwidthReport'
import CacheManageV2 from './pages/CacheManage'
import UpstreamsV2 from './pages/Upstreams'
import AccessLogsV2 from './pages/AccessLogs'
import UsersV2 from './pages/Users'
import SettingsV2 from './pages/Settings'
import AuditLogsV2 from './pages/AuditLogs'
import RulesV2 from './pages/Rules'
import Security from './pages/Security'
import Projects from './pages/Projects'
import Quarantine from './pages/Quarantine'
import License from './pages/License'
import { usePrincipal } from '@/hooks/usePrincipal'
import QueryErrorState from '@/components/QueryErrorState'
import { useTranslation } from 'react-i18next'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const token = localStorage.getItem('token')
  const { principal, isPending, isError, refetch } = usePrincipal(Boolean(token))

  if (!token) return <Navigate to="/admin/login" state={{ from: location }} replace />
  if (isPending) return <div aria-busy="true" className="min-h-screen" />
  if (isError || !principal) {
    return (
      <main className="grid min-h-screen place-items-center p-4">
        <QueryErrorState message={t('auth.principalLoadError')} onRetry={() => { void refetch() }} />
      </main>
    )
  }
  return <>{children}</>
}

export default function AdminAppV2() {
  return (
    <>
      <Routes>
      <Route path="login" element={<LoginV2 />} />
      <Route
        element={
          <RequireAuth>
            <MainLayoutV2 />
          </RequireAuth>
        }
      >
        <Route index element={<DashboardV2 />} />
        <Route path="bandwidth" element={<BandwidthReportV2 />} />
        <Route path="cache" element={<CacheManageV2 />} />
        <Route path="upstreams" element={<UpstreamsV2 />} />
        <Route path="logs" element={<AccessLogsV2 />} />
        <Route path="audit" element={<AuditLogsV2 />} />
        <Route path="quarantine" element={<Quarantine />} />
        <Route path="rules" element={<RulesV2 />} />
        <Route path="security" element={<Security />} />
        <Route path="projects" element={<Projects />} />
        <Route path="users" element={<UsersV2 />} />
        <Route path="license" element={<License />} />
        <Route path="settings" element={<SettingsV2 />} />
      </Route>
    </Routes>
    </>
  )
}
