import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import MainLayout from './components/MainLayout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import CacheManage from './pages/CacheManage'
import Upstreams from './pages/Upstreams'
import AccessLogs from './pages/AccessLogs'
import Users from './pages/Users'
import Settings from './pages/Settings'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const token = localStorage.getItem('token')

  if (!token) {
    return <Navigate to="/admin/login" state={{ from: location }} replace />
  }

  return <>{children}</>
}

export default function AdminApp() {
  return (
    <Routes>
      <Route path="login" element={<Login />} />
      <Route
        element={
          <RequireAuth>
            <MainLayout />
          </RequireAuth>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="cache" element={<CacheManage />} />
        <Route path="upstreams" element={<Upstreams />} />
        <Route path="logs" element={<AccessLogs />} />
        <Route path="users" element={<Users />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}
