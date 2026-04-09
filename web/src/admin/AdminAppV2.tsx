import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import MainLayoutV2 from './components/MainLayoutV2'
import LoginV2 from './pages/LoginV2'
import DashboardV2 from './pages/DashboardV2'
import CacheManageV2 from './pages/CacheManageV2'
import UpstreamsV2 from './pages/UpstreamsV2'
import AccessLogsV2 from './pages/AccessLogsV2'
import UsersV2 from './pages/UsersV2'
import SettingsV2 from './pages/SettingsV2'
import AuditLogsV2 from './pages/AuditLogsV2'
import RulesV2 from './pages/RulesV2'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const token = localStorage.getItem('token')

  if (!token) {
    return <Navigate to="/admin/login" state={{ from: location }} replace />
  }

  return <>{children}</>
}

export default function AdminAppV2() {
  return (
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
        <Route path="cache" element={<CacheManageV2 />} />
        <Route path="upstreams" element={<UpstreamsV2 />} />
        <Route path="logs" element={<AccessLogsV2 />} />
        <Route path="audit" element={<AuditLogsV2 />} />
        <Route path="rules" element={<RulesV2 />} />
        <Route path="users" element={<UsersV2 />} />
        <Route path="settings" element={<SettingsV2 />} />
      </Route>
    </Routes>
  )
}
